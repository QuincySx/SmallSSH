//go:build linux

package systemauth

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/godbus/dbus/v5"
	"os"
	"os/exec"
	"path/filepath"
	"sshclient/internal/vault"
	"strings"
	"sync"
)

type linuxBackend struct {
	mu          sync.Mutex
	directories map[string]string
}

func newBackend(window uintptr) Backend { return &linuxBackend{} }
func (*linuxBackend) Name() string      { return LinuxFingerprintName }
func linuxDevice(ctx context.Context) (*dbus.Conn, dbus.BusObject, error) {
	c, e := dbus.ConnectSystemBus()
	if e != nil {
		return nil, nil, e
	}
	var p dbus.ObjectPath
	e = c.Object("net.reactivated.Fprint", "/net/reactivated/Fprint/Manager").CallWithContext(ctx, "net.reactivated.Fprint.Manager.GetDefaultDevice", 0).Store(&p)
	if e != nil {
		c.Close()
		return nil, nil, errors.New("未找到 fprintd 指纹设备")
	}
	return c, c.Object("net.reactivated.Fprint", p), nil
}
func (*linuxBackend) Probe(ctx context.Context) error {
	st, e := os.Stat("/usr/bin/secret-tool")
	if e != nil || !st.Mode().IsRegular() || st.Mode().Perm()&0022 != 0 {
		return errors.New("需要系统 Secret Service 和 secret-tool")
	}
	c, d, e := linuxDevice(ctx)
	if e != nil {
		return e
	}
	defer c.Close()
	var fingers []string
	if e = d.CallWithContext(ctx, "net.reactivated.Fprint.Device.ListEnrolledFingers", 0, "").Store(&fingers); e != nil {
		return e
	}
	if len(fingers) == 0 {
		return errors.New("请先在系统设置录入指纹")
	}
	return nil
}
func verifyLinux(ctx context.Context) error {
	c, d, e := linuxDevice(ctx)
	if e != nil {
		return e
	}
	defer c.Close()
	iface := "net.reactivated.Fprint.Device"
	var owner string
	if e = c.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.GetNameOwner", 0, "net.reactivated.Fprint").Store(&owner); e != nil {
		return e
	}
	if e = d.CallWithContext(ctx, iface+".Claim", 0, "").Err; e != nil {
		return e
	}
	defer d.Call(iface+".Release", 0)
	options := []dbus.MatchOption{dbus.WithMatchSender("net.reactivated.Fprint"), dbus.WithMatchObjectPath(d.Path()), dbus.WithMatchInterface(iface), dbus.WithMatchMember("VerifyStatus")}
	if e = c.AddMatchSignal(options...); e != nil {
		return e
	}
	ch := make(chan *dbus.Signal, 16)
	c.Signal(ch)
	defer c.RemoveSignal(ch)
	if e = d.CallWithContext(ctx, iface+".VerifyStart", 0, "any").Err; e != nil {
		return e
	}
	defer d.Call(iface+".VerifyStop", 0)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case s, ok := <-ch:
			if !ok {
				return errors.New("指纹服务连接已关闭")
			}
			// Match rules filter broadcasts; directed messages can still reach
			// this connection. Verify the daemon's unique sender explicitly.
			if s.Sender != owner || s.Path != d.Path() || s.Name != iface+".VerifyStatus" || len(s.Body) != 2 {
				continue
			}
			status, _ := s.Body[0].(string)
			done, _ := s.Body[1].(bool)
			if status == "verify-match" {
				return nil
			}
			if done {
				if status == "verify-no-match" {
					return ErrFingerprintNotRecognized
				}
				return fmt.Errorf("指纹验证未通过: %s", status)
			}
		}
	}
}

type linuxEnvelope struct {
	ID     string `json:"id"`
	Nonce  []byte `json:"nonce"`
	Cipher []byte `json:"cipher"`
}

func (b *linuxBackend) Wrap(ctx context.Context, key []byte) (json.RawMessage, error) {
	return b.wrap(ctx, "", key)
}

func (b *linuxBackend) WrapForDirectory(ctx context.Context, dir string, key []byte) (json.RawMessage, error) {
	directory, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	var sealed json.RawMessage
	err = withKeyringLock(ctx, directory, func() error {
		var wrapErr error
		sealed, wrapErr = b.wrap(ctx, directory, key)
		return wrapErr
	})
	return sealed, err
}

func (b *linuxBackend) wrap(ctx context.Context, dir string, key []byte) (json.RawMessage, error) {
	// The OS keyring protects at rest; fprintd enforces this application's
	// fresh user-presence gate. Same-user keyring extraction is not prevented.
	k := make([]byte, 32)
	rand.Read(k)
	defer wipe(k)
	block, _ := aes.NewCipher(k)
	gcm, _ := cipher.NewGCM(block)
	nonce := make([]byte, gcm.NonceSize())
	rand.Read(nonce)
	r := linuxEnvelope{ID: vault.ShortID(), Nonce: nonce}
	r.Cipher = gcm.Seal(nil, nonce, key, []byte(r.ID))
	// Register before storing the device key: a crash at any later point leaves
	// enough non-secret information for the next daemon to remove this entry.
	if dir != "" {
		if err := registerKeyringID(dir, r.ID); err != nil {
			return nil, err
		}
		b.mu.Lock()
		if b.directories == nil {
			b.directories = make(map[string]string)
		}
		b.directories[r.ID] = dir
		b.mu.Unlock()
	}
	cmd := exec.CommandContext(ctx, "/usr/bin/secret-tool", "store", "--label=SmallSSH 设备密钥", "service", "ssh-workbench", "id", r.ID)
	cmd.Stdin = strings.NewReader(base64.StdEncoding.EncodeToString(k))
	if e := cmd.Run(); e != nil {
		return nil, errors.New("系统钥匙串保存失败")
	}
	return json.Marshal(r)
}
func (*linuxBackend) Unwrap(ctx context.Context, data json.RawMessage, reason string) ([]byte, error) {
	var r linuxEnvelope
	if e := json.Unmarshal(data, &r); e != nil {
		return nil, e
	}
	if len(r.Nonce) != 12 || len(r.ID) != 24 {
		return nil, errors.New("系统解锁记录无效")
	}
	if e := verifyLinux(ctx); e != nil {
		return nil, e
	}
	cmd := exec.CommandContext(ctx, "/usr/bin/secret-tool", "lookup", "service", "ssh-workbench", "id", r.ID)
	var out bytes.Buffer
	cmd.Stdout = &out
	if e := cmd.Run(); e != nil || out.Len() > 1024 {
		return nil, errors.New("无法读取系统保护的设备密钥")
	}
	k, e := base64.StdEncoding.DecodeString(strings.TrimSpace(out.String()))
	if e != nil || len(k) != 32 {
		return nil, errors.New("系统设备密钥无效")
	}
	defer wipe(k)
	block, _ := aes.NewCipher(k)
	gcm, _ := cipher.NewGCM(block)
	return gcm.Open(nil, r.Nonce, r.Cipher, []byte(r.ID))
}

func (b *linuxBackend) Remove(ctx context.Context, data json.RawMessage) error {
	var r linuxEnvelope
	if e := json.Unmarshal(data, &r); e != nil {
		return e
	}
	if !validKeyringID(r.ID) {
		return errors.New("系统钥匙串条目标识无效")
	}
	b.mu.Lock()
	directory := b.directories[r.ID]
	b.mu.Unlock()
	remove := func() error {
		if err := removeLinuxKeyringRecord(ctx, r.ID); err != nil {
			return err
		}
		if directory != "" {
			if err := unregisterKeyringID(directory, r.ID); err != nil {
				return err
			}
			b.mu.Lock()
			delete(b.directories, r.ID)
			b.mu.Unlock()
		}
		return nil
	}
	if directory != "" {
		return withKeyringLock(ctx, directory, remove)
	}
	// Legacy or another frontend's record supplies no filesystem target. Its
	// key removal is idempotent; the registered owner handles marker cleanup.
	return remove()
}

// CleanupOrphans runs only while the daemon exclusively owns its service lock,
// before it accepts any frontend that could create new sealed records.
func CleanupOrphans(ctx context.Context, dir string) error {
	directory, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	return cleanupKeyringIDs(ctx, directory, removeLinuxKeyringRecord)
}

func removeLinuxKeyringRecord(ctx context.Context, id string) error {
	if !validKeyringID(id) {
		return errors.New("系统钥匙串条目标识无效")
	}
	bus, err := dbus.ConnectSessionBus()
	if err != nil {
		return err
	}
	defer bus.Close()
	var unlocked, locked []dbus.ObjectPath
	attrs := map[string]string{"service": "ssh-workbench", "id": id}
	if err := bus.Object("org.freedesktop.secrets", "/org/freedesktop/secrets").CallWithContext(ctx, "org.freedesktop.Secret.Service.SearchItems", 0, attrs).Store(&unlocked, &locked); err != nil {
		return err
	}
	// An entry already removed by normal cleanup is a successful no-op. Locked
	// collections are left for a later retry instead of prompting on startup.
	if len(locked) > 0 {
		return errors.New("系统钥匙串已锁定，暂缓清理设备密钥")
	}
	if len(unlocked) == 0 {
		return nil
	}
	for _, item := range unlocked {
		var prompt dbus.ObjectPath
		if err := bus.Object("org.freedesktop.secrets", item).CallWithContext(ctx, "org.freedesktop.Secret.Item.Delete", 0).Store(&prompt); err != nil {
			return err
		}
		if prompt != "/" {
			return errors.New("系统钥匙串需要验证，暂缓清理设备密钥")
		}
	}
	return nil
}
