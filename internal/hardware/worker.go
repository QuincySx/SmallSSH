// Package hardware runs PKCS#11 providers in a disposable native worker. PINs
// travel over inherited pipes, never command arguments, files or public RPCs.
package hardware

import (
	"bufio"
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"sshclient/internal/connectcfg"
)

const workerFlag = "--smallssh-pkcs11-worker"

type Key struct {
	Token, Serial, Model, ID, Label, Fingerprint, Algorithm string
	Public                                                  []byte `json:"public,omitempty"`
}
type request struct {
	Action string
	Config connectcfg.Hardware
	Digest []byte
	Hash   crypto.Hash
}
type response struct {
	Keys      []Key
	Signature []byte
	Error     string
}
type worker struct {
	mu     sync.Mutex
	once   sync.Once
	cmd    *exec.Cmd
	input  io.WriteCloser
	output *bufio.Scanner
	public crypto.PublicKey
}

func ModuleHash(path string) (string, error) {
	if !filepath.IsAbs(path) || strings.ContainsAny(path, "\x00\r\n") || runtime.GOOS == "windows" && strings.HasPrefix(path, `\\`) {
		return "", errors.New("PKCS#11 模块需为本机完整路径")
	}
	f, e := os.Open(path)
	if e != nil {
		return "", errors.New("无法读取 PKCS#11 模块")
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() || st.Size() > 256<<20 {
		return "", errors.New("PKCS#11 模块需为不超过 256 MiB 的普通文件")
	}
	h := sha256.New()
	if _, e = io.Copy(h, io.LimitReader(f, (256<<20)+1)); e != nil {
		return "", e
	}
	after, e := f.Stat()
	if e != nil || after.Size() != st.Size() || after.ModTime() != st.ModTime() {
		return "", errors.New("PKCS#11 模块在读取期间变化")
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func start() (*worker, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	command := exec.Command(executable, workerFlag)
	configureWorker(command)
	input, e := command.StdinPipe()
	if e != nil {
		return nil, e
	}
	output, e := command.StdoutPipe()
	if e != nil {
		input.Close()
		return nil, e
	}
	command.Stderr = io.Discard
	if e = command.Start(); e != nil {
		input.Close()
		return nil, e
	}
	scanner := bufio.NewScanner(output)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	return &worker{cmd: command, input: input, output: scanner}, nil
}
func (w *worker) call(q request) (response, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if e := json.NewEncoder(w.input).Encode(q); e != nil {
		return response{}, errors.New("硬件密钥工作进程已关闭")
	}
	if !w.output.Scan() {
		return response{}, errors.New("硬件密钥工作进程中断或超时")
	}
	var r response
	if json.Unmarshal(w.output.Bytes(), &r) != nil {
		return r, errors.New("硬件密钥响应无效")
	}
	if r.Error != "" {
		return r, errors.New(r.Error)
	}
	return r, nil
}
func (w *worker) Close() error {
	w.once.Do(func() {
		w.input.Close()
		if w.mu.TryLock() {
			w.mu.Unlock()
			done := make(chan struct{})
			go func() { _ = w.cmd.Wait(); close(done) }()
			timer := time.NewTimer(100 * time.Millisecond)
			defer timer.Stop()
			select {
			case <-done:
				return
			case <-timer.C:
				_ = w.cmd.Process.Kill()
				<-done
			}
		} else {
			_ = w.cmd.Process.Kill()
			_ = w.cmd.Wait()
		}
	})
	return nil
}
func (w *worker) Public() crypto.PublicKey { return w.public }
func (w *worker) Sign(_ io.Reader, digest []byte, opts crypto.SignerOpts) ([]byte, error) {
	if _, ok := opts.(*rsa.PSSOptions); ok {
		return nil, errors.New("硬件密钥不提供 RSA-PSS SSH 签名")
	}
	r, e := w.call(request{Action: "sign", Digest: digest, Hash: opts.HashFunc()})
	return r.Signature, e
}
func Inspect(ctx context.Context, config connectcfg.Hardware) (string, []Key, error) {
	if e := ctx.Err(); e != nil {
		return "", nil, e
	}
	if len(config.PIN) > 1024 || len(config.TokenSerial) > 32 || config.PIN != "" && config.TokenSerial == "" {
		return "", nil, errors.New("请先选择令牌序列号再输入 PIN；PIN 最多 1024 字节")
	}
	var hash string
	if config.Module == connectcfg.MacSmartCard {
		if config.PIN != "" {
			return "", nil, errors.New("系统智能卡的 PIN 由 macOS 验证")
		}
	} else {
		var err error
		hash, err = ModuleHash(config.Module)
		if err != nil {
			return "", nil, err
		}
	}
	w, e := start()
	if e != nil {
		return "", nil, e
	}
	defer w.Close()
	stop := context.AfterFunc(ctx, func() { w.Close() })
	defer stop()
	config.ModuleSHA256 = hash
	r, e := w.call(request{Action: "inspect", Config: config})
	return hash, r.Keys, e
}
func Open(ctx context.Context, config connectcfg.Hardware) (ssh.Signer, io.Closer, error) {
	if e := ctx.Err(); e != nil {
		return nil, nil, e
	}
	if e := config.Validate(); e != nil {
		return nil, nil, e
	}
	w, e := start()
	if e != nil {
		return nil, nil, e
	}
	stop := context.AfterFunc(ctx, func() { w.Close() })
	defer stop()
	r, e := w.call(request{Action: "open", Config: config})
	if e != nil || len(r.Keys) != 1 {
		w.Close()
		if e == nil {
			e = errors.New("硬件公钥选择不唯一")
		}
		return nil, nil, e
	}
	if config.Module == connectcfg.MacSmartCard && (r.Keys[0].Serial != config.TokenSerial || r.Keys[0].ID != config.KeyID) {
		w.Close()
		return nil, nil, errors.New("系统智能卡身份已变化")
	}
	w.public, e = x509.ParsePKIXPublicKey(r.Keys[0].Public)
	if e != nil {
		w.Close()
		return nil, nil, e
	}
	signer, e := ssh.NewSignerFromSigner(w)
	if e != nil {
		w.Close()
		return nil, nil, e
	}
	if ssh.FingerprintSHA256(signer.PublicKey()) != config.Fingerprint {
		w.Close()
		return nil, nil, errors.New("硬件公钥指纹已变化")
	}
	return signer, w, nil
}

type hardwareSession interface {
	keys(config connectcfg.Hardware, selectKey bool) ([]Key, error)
	sign(digest []byte, hash crypto.Hash) ([]byte, error)
	Close()
}

func openHardwareSession(config connectcfg.Hardware) (hardwareSession, error) {
	if config.Module == connectcfg.MacSmartCard {
		return openMacSmartCard()
	}
	module, err := openNative(config)
	if err != nil {
		return nil, err
	}
	return module, nil
}

// MaybeWorker is checked before either application's flag parser or UI starts.
func MaybeWorker() bool {
	if len(os.Args) != 2 || os.Args[1] != workerFlag {
		return false
	}
	// C_Initialize(NULL) declares serial use. Keep all provider calls and
	// teardown on one native thread in this isolated process.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), 64<<10)
	encoder := json.NewEncoder(os.Stdout)
	var module hardwareSession
	defer func() {
		if module != nil {
			module.Close()
		}
	}()
	for scanner.Scan() {
		var q request
		r := response{}
		e := json.Unmarshal(scanner.Bytes(), &q)
		if e == nil {
			switch q.Action {
			case "inspect", "open":
				if module != nil {
					e = errors.New("硬件工作进程不能重新选取密钥")
					break
				}
				module, e = openHardwareSession(q.Config)
				if e == nil {
					r.Keys, e = module.keys(q.Config, q.Action == "open")
				}
			case "sign":
				if module == nil {
					e = errors.New("硬件会话尚未建立")
				} else {
					r.Signature, e = module.sign(q.Digest, q.Hash)
				}
			default:
				e = errors.New("未知硬件操作")
			}
		}
		q.Config.PIN = ""
		if e != nil {
			r.Error = fmt.Sprint(e)
		}
		if encoder.Encode(r) != nil || e != nil || q.Action == "inspect" {
			return true
		}
	}
	return true
}
