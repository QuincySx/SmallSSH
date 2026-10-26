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
	"sshclient/internal/vault"
	"strings"
)

type linuxBackend struct{}

func newBackend(window uintptr) Backend { return linuxBackend{} }
func (linuxBackend) Name() string       { return "Linux 指纹认证" }
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
func (linuxBackend) Probe(ctx context.Context) error {
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

func (linuxBackend) Wrap(ctx context.Context, key []byte) (json.RawMessage, error) {
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
	cmd := exec.CommandContext(ctx, "/usr/bin/secret-tool", "store", "--label=SmallSSH 设备密钥", "service", "ssh-workbench", "id", r.ID)
	cmd.Stdin = strings.NewReader(base64.StdEncoding.EncodeToString(k))
	if e := cmd.Run(); e != nil {
		return nil, errors.New("系统钥匙串保存失败")
	}
	return json.Marshal(r)
}
func (linuxBackend) Unwrap(ctx context.Context, data json.RawMessage, reason string) ([]byte, error) {
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

func (linuxBackend) Remove(ctx context.Context, data json.RawMessage) error {
	var r linuxEnvelope
	if e := json.Unmarshal(data, &r); e != nil {
		return e
	}
	id, e := base64.RawURLEncoding.DecodeString(r.ID)
	if e != nil || len(id) != 18 || len(r.ID) != 24 {
		return errors.New("系统钥匙串条目标识无效")
	}
	if e = exec.CommandContext(ctx, "/usr/bin/secret-tool", "clear", "service", "ssh-workbench", "id", r.ID).Run(); e != nil {
		return errors.New("系统钥匙串条目清理失败")
	}
	return nil
}
