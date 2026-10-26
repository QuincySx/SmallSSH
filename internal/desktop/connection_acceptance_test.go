package desktop

import (
	"bytes"
	"context"
	"encoding/pem"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"golang.org/x/crypto/ssh"
	"sshclient/internal/broker"
	"sshclient/internal/client"
	"sshclient/internal/localipc"
	"sshclient/internal/testserver"
	"sshclient/internal/vault"
)

// This harness uses the production form, encryption, broker and SSH protocol.
// Only the OS file-selection result is supplied instead of opening any window.
type connectionFlowHarness struct {
	t       *testing.T
	a       *app
	ui      *ui.Tester
	fixture *testserver.Server
	server  *broker.Server
	dir     string
	pass    []byte
	ctx     context.Context
	updates chan func()
}

func newConnectionFlowHarness(t *testing.T) *connectionFlowHarness {
	t.Helper()
	dir, err := os.MkdirTemp("", "flow-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	if err = localipc.Prepare(dir); err != nil {
		t.Fatal(err)
	}
	h := &connectionFlowHarness{t: t, dir: dir, pass: []byte("isolated acceptance vault password"), fixture: testserver.New(t), updates: make(chan func(), 64)}
	v, err := vault.New(h.pass)
	if err != nil {
		t.Fatal(err)
	}
	if err = vault.Save(broker.VaultPath(dir), v, true); err != nil {
		t.Fatal(err)
	}
	v.Close()
	h.server, err = broker.Open(dir, h.pass)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	h.ctx = ctx
	t.Cleanup(func() {
		cancel()
		for _, s := range h.a.sessions {
			if s.term != nil {
				s.term.Close()
			}
		}
		h.server.Close()
	})
	h.resetApp()
	h.a.newConnection()
	h.ui.Frame()
	return h
}

func (h *connectionFlowHarness) resetApp() {
	h.a = &app{ctx: h.ctx, dir: h.dir, client: client.Client{Dir: h.dir}, online: true, server: h.server, dispatch: func(fn func()) { h.updates <- fn }, groups: map[string]*bool{}}
	h.ui = ui.NewTester(h.a.connectionView, 820, 1600)
}

func (h *connectionFlowHarness) click(label string) {
	h.t.Helper()
	if err := h.ui.Click(label); err != nil {
		h.t.Fatal(err)
	}
}

func (h *connectionFlowHarness) input(label, value string) {
	h.t.Helper()
	h.click(label + "输入")
	mod := ui.Ctrl
	if runtime.GOOS == "darwin" {
		mod = ui.Super
	}
	h.ui.Key(mod, ui.KeyA)
	h.ui.Type(value)
}

func (h *connectionFlowHarness) wait(done func() bool) {
	h.t.Helper()
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	for !done() {
		select {
		case update := <-h.updates:
			update()
			h.ui.Frame()
		case <-timer.C:
			h.t.Fatal("connection flow did not finish", h.a.connection.phase, h.a.connection.errors)
		}
	}
}

func (h *connectionFlowHarness) idle() { h.wait(func() bool { return h.a.connection.phase == "" }) }

func (h *connectionFlowHarness) fillTarget() {
	h.input("名称", "测试服务器 · 中文 ' $ ` \\")
	h.input("服务器地址", h.fixture.Profile().Host)
	h.input("用户名", h.fixture.Profile().User)
	h.input("端口", strconv.Itoa(h.fixture.Profile().Port))
	h.input("分组", "本机测试")
	h.ui.Key(0, ui.KeyTab)
}

func (h *connectionFlowHarness) selectAuth(label string) {
	h.click("登录方式选择")
	h.click(label)
}

func (h *connectionFlowHarness) confirmIdentity() {
	h.t.Helper()
	auths := h.fixture.Auths.Load()
	h.click(h.a.connectionActionLabel(false))
	h.idle()
	if h.a.connection.identity == nil || h.a.connection.identity.Fingerprint != h.fixture.Profile().Fingerprint || h.fixture.Auths.Load() != auths {
		h.t.Fatal("identity probe sent credentials or returned wrong identity", h.a.connection.errors)
	}
	if !h.a.connection.review || !h.ui.HasText("确认服务器身份") || h.ui.HasText("服务器密码输入") || h.ui.HasText("保险库主密码输入") {
		h.t.Fatal("identity confirmation is not a separate page", h.ui.Texts())
	}
}

func (h *connectionFlowHarness) stored(id string) vault.Profile {
	h.t.Helper()
	v, err := vault.Read(broker.VaultPath(h.dir), h.pass)
	if err != nil {
		h.t.Fatal(err)
	}
	defer v.Close()
	p, err := v.Profile(id)
	if err != nil {
		h.t.Fatal(err)
	}
	return p
}

func (h *connectionFlowHarness) assertStoredLogin(id string) {
	h.t.Helper()
	owner, err := vault.Read(broker.VaultPath(h.dir), h.pass)
	if err != nil {
		h.t.Fatal(err)
	}
	s, err := h.a.client.ConnectWithOwner(h.ctx, id, owner)
	owner.Close()
	if err != nil {
		h.t.Fatal("stored credentials could not log in", err)
	}
	defer s.Close()
	c := &client.TerminalConn{Stream: s}
	marker := "saved credentials · 中文 ' $ ` \\"
	if _, err = c.Write([]byte(marker)); err != nil {
		h.t.Fatal(err)
	}
	got := make([]byte, len(marker))
	if _, err = io.ReadFull(c, got); err != nil || string(got) != marker {
		h.t.Fatal("stored login did not echo", err)
	}
}

func generatedKeyFile(t *testing.T, f *testserver.Server, encrypted bool) (string, string, string) {
	t.Helper()
	raw, phrase := f.PrivateKey, ""
	if encrypted {
		key, err := ssh.ParseRawPrivateKey([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		phrase = "isolated key passphrase · 中文"
		block, err := ssh.MarshalPrivateKeyWithPassphrase(key, "acceptance-only", []byte(phrase))
		if err != nil {
			t.Fatal(err)
		}
		raw = string(pem.EncodeToMemory(block))
	}
	path := filepath.Join(t.TempDir(), "generated key with spaces.pem")
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	return path, raw, phrase
}

func TestConnectionCredentialsSurviveBrokerRestartAndSourceKeyRemoval(t *testing.T) {
	for _, kind := range []string{"password", "private-key", "encrypted-private-key"} {
		t.Run(kind, func(t *testing.T) {
			h := newConnectionFlowHarness(t)
			h.fillTarget()
			var keyPath, keyRaw, phrase string
			if kind == "password" {
				h.input("服务器密码", h.fixture.Profile().Password)
			} else {
				h.selectAuth("已有私钥")
				keyPath, keyRaw, phrase = generatedKeyFile(t, h.fixture, kind == "encrypted-private-key")
				if phrase == "" {
					h.input("私钥文件", keyPath)
					h.click("检查私钥")
				} else {
					h.a.keyPicker = func() ([]string, error) { return []string{keyPath}, nil }
					h.click("选择私钥文件…")
					h.idle()
					if !h.ui.HasText("已加密，请填写私钥口令") {
						t.Fatal("encrypted import did not request passphrase")
					}
					h.input("私钥口令", "wrong test phrase")
					h.click("检查私钥")
					h.idle()
					if h.a.connection.keyInfo != nil || !h.ui.HasText("口令不正确") {
						t.Fatal("incorrect key passphrase was accepted")
					}
					h.input("私钥口令", phrase)
					h.click("检查私钥")
				}
				h.idle()
				if h.a.connection.keyInfo == nil || h.a.connection.keyInfo.Fingerprint != ssh.FingerprintSHA256(h.fixture.Key.PublicKey()) {
					t.Fatal("import did not validate generated private key")
				}
			}
			h.click("保存后连接")
			h.confirmIdentity()
			id := h.a.form.ID
			h.click("信任并保存")
			h.wait(func() bool { return h.a.page == 1 || h.a.connection.phase == "" })
			if h.a.page != 1 || h.a.form.Password != "" || h.a.keyFile != "" || h.a.form.KeyPassphrase != "" || h.a.form.PrivateKey != "" {
				t.Fatal("save failed or retained input secrets", h.a.connection.errors)
			}
			sealed, err := os.ReadFile(broker.VaultPath(h.dir))
			if err != nil {
				t.Fatal(err)
			}
			for _, secret := range []string{h.fixture.Profile().Password, keyRaw, phrase} {
				if secret != "" && bytes.Contains(sealed, []byte(secret)) {
					t.Fatal("vault contains plaintext credential")
				}
			}
			if keyPath != "" {
				if err = os.Remove(keyPath); err != nil {
					t.Fatal(err)
				}
			}
			h.server.Close()
			h.server, err = broker.Open(h.dir, h.pass)
			if err != nil {
				t.Fatal(err)
			}
			h.resetApp()
			h.a.connections = h.server.Vault.Summaries()
			h.a.selected = id
			h.a.editConnection()
			h.ui.Frame()
			if h.a.form.Password != "" || h.a.form.PrivateKey != "" || h.a.keyFile != "" || !h.ui.HasText("更换登录凭据") {
				t.Fatal("edit read back secrets or required source key again")
			}
			h.input("名称", "重启后修改名称")
			h.click("保存连接")
			h.wait(func() bool { return h.a.page == 1 || h.a.connection.phase == "" })
			p := h.stored(id)
			if p.Name != "重启后修改名称" || (kind == "password" && p.Password != h.fixture.Profile().Password) || (kind != "password" && (p.PrivateKey != keyRaw || p.KeyPassphrase != phrase)) {
				t.Fatal("editing after reopen changed stored credentials")
			}
			h.assertStoredLogin(id)
			log, err := os.ReadFile(filepath.Join(h.dir, "audit.jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			for _, secret := range []string{h.fixture.Profile().Password, keyRaw, phrase} {
				if secret != "" && bytes.Contains(log, []byte(secret)) {
					t.Fatal("audit contains credential material")
				}
			}
		})
	}
}

func TestPrivateKeyPickerButtonCanCancelAndRejectPublicKey(t *testing.T) {
	h := newConnectionFlowHarness(t)
	h.selectAuth("已有私钥")
	h.input("名称", "unsaved draft")
	h.a.keyPicker = func() ([]string, error) { return nil, nil }
	h.click("选择私钥文件…")
	h.idle()
	if h.a.form.Name != "unsaved draft" || h.a.keyFile != "" {
		t.Fatal("picker cancellation discarded draft")
	}
	path := filepath.Join(t.TempDir(), "only-public-key.pub")
	if err := os.WriteFile(path, ssh.MarshalAuthorizedKey(h.fixture.Key.PublicKey()), 0600); err != nil {
		t.Fatal(err)
	}
	h.a.keyPicker = func() ([]string, error) { return []string{path}, nil }
	h.click("选择私钥文件…")
	h.idle()
	if !h.ui.HasText("选择的是公钥") || h.a.connection.keyInfo != nil {
		t.Fatal("public key file was accepted as private key")
	}
	h.input("私钥口令", "temporary draft secret")
	h.click("取消")
	if h.a.page != 1 || h.a.form.KeyPassphrase != "" || h.a.ownerPass != "" || h.a.keyFile != "" {
		t.Fatal("cancel retained secret input")
	}
}

func TestConnectionFormFooterFitsSmallWindowAndHostCanBeRechecked(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := &app{ctx: ctx, online: true, groups: map[string]*bool{}, sidebarWidth: 190, toolsHidden: true}
	a.newConnection()
	tt := ui.NewTester(a.view, 1000, 760)
	for _, label := range []string{"取消", "保存并连接"} {
		r, ok := tt.Find(label)
		if !ok || r.X < 0 || r.Y < 0 || r.X+r.W > 1000 || r.Y+r.H > 760 {
			t.Fatal("form action is outside minimum window", label, r)
		}
	}
	a.form.Fingerprint = "previous confirmed identity"
	a.connection.trustedTarget = targetID("127.0.0.1", 22)
	tt.Frame()
	if err := tt.Click("重新核对服务器身份"); err != nil {
		t.Fatal(err)
	}
	if a.form.Fingerprint != "" || a.connection.trustedTarget != "" || !tt.HasText("保存并连接") {
		t.Fatal("recheck silently retained old trust")
	}
	if !strings.Contains(connectionError(context.DeadlineExceeded), "超时") {
		t.Fatal("timeout did not explain retry")
	}
}

func TestConnectionCancellationClosesStalledSSHAndDoesNotRestoreDraft(t *testing.T) {
	for _, stage := range []string{"probe", "connect"} {
		t.Run(stage, func(t *testing.T) {
			h := newConnectionFlowHarness(t)
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			accepted := make(chan net.Conn, 1)
			go func() {
				if c, err := listener.Accept(); err == nil {
					accepted <- c
				}
			}()
			_, port, _ := net.SplitHostPort(listener.Addr().String())
			h.fillTarget()
			h.input("端口", port)
			h.input("服务器密码", "synthetic cancelled input")
			id := h.a.form.ID
			if stage == "connect" {
				// A previously confirmed identity whose SSH server has stopped
				// responding: save completes, login waits for a banner.
				h.a.form.Fingerprint = h.fixture.Profile().Fingerprint
				n, _ := strconv.Atoi(port)
				h.a.connection.trustedTarget = targetID("127.0.0.1", n)
				h.ui.Frame()
				h.click("保存并连接")
				h.wait(func() bool { return h.a.connection.phase == "connect" })
			} else {
				h.click(h.a.connectionActionLabel(false))
			}
			var c net.Conn
			select {
			case c = <-accepted:
				defer c.Close()
			case <-time.After(5 * time.Second):
				t.Fatal("SSH did not reach stalled endpoint")
			}
			button := "取消"
			if stage == "connect" {
				button = "取消连接（配置已保存）"
			}
			h.click(button)
			if h.a.page != 1 || h.a.form.Password != "" || h.a.ownerPass != "" {
				t.Fatal("cancel did not return or clear input")
			}
			// Read the harmless client identification banner, then require EOF.
			c.SetReadDeadline(time.Now().Add(3 * time.Second))
			if _, err = io.ReadAll(c); err != nil {
				t.Fatal("cancel left SSH network connection open", err)
			}
			h.a.newConnection()
			h.ui.Frame()
			select {
			case update := <-h.updates:
				update()
			case <-time.After(5 * time.Second):
				t.Fatal("cancelled operation did not finish")
			}
			if h.a.page != 0 || h.a.form.ID == id || h.a.form.Password != "" || len(h.a.connection.errors) != 0 {
				t.Fatal("late operation restored cancelled form")
			}
			if stage == "probe" && len(h.server.Vault.Summaries()) != 0 {
				t.Fatal("cancelled probe saved a connection")
			}
			if stage == "connect" && h.stored(id).ID != id {
				t.Fatal("cancelling login removed already saved configuration")
			}
		})
	}
}

func TestConnectionSidebarCannotNavigateDuringAtomicSave(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := &app{ctx: ctx, online: true, groups: map[string]*bool{}, sidebarWidth: 190, toolsHidden: true,
		connections: []vault.Summary{{ID: "other", Name: "另一台服务器", Host: "127.0.0.1", User: "fixture"}}}
	a.newConnection()
	a.form.Name = "正在保存的连接"
	a.form.Password = "temporary secret"
	a.connection.phase = "save"
	tt := ui.NewTester(a.view, 1000, 760)
	for _, label := range []string{"另一台服务器", "新建连接", "取消"} {
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
		if a.page != 0 || a.selected != "" || a.form.Name != "正在保存的连接" || a.form.Password != "temporary secret" {
			t.Fatal("save was interrupted by navigation", label)
		}
	}
}
