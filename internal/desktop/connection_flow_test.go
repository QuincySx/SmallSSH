package desktop

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/broker"
	"sshclient/internal/client"
	"sshclient/internal/localipc"
	"sshclient/internal/testserver"
	"sshclient/internal/vault"
)

func TestConnectionUIValidatesTrustSavesEncryptedAndEditsWithoutSecretReadback(t *testing.T) {
	fixture := testserver.New(t)
	dir, err := os.MkdirTemp("", "conn-ui-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := localipc.Prepare(dir); err != nil {
		t.Fatal(err)
	}
	pass := []byte("isolated full connection flow password")
	v, err := vault.New(pass)
	if err != nil {
		t.Fatal(err)
	}
	if err = vault.Save(broker.VaultPath(dir), v, true); err != nil {
		t.Fatal(err)
	}
	v.Close()
	server, err := broker.Open(dir, pass)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	updates := make(chan func(), 64)
	a := &app{ctx: ctx, dir: dir, client: client.Client{Dir: dir, Owner: server.Vault}, online: true, server: server, dispatch: func(fn func()) { updates <- fn }}
	a.newConnection()
	a.connection.connect = false
	tt := ui.NewTester(a.connectionView, 820, 1400)
	if tt.HasText("连接 ID") || tt.HasText("已核对的主机指纹") {
		t.Fatal("internal setup fields still exposed", tt.Texts())
	}
	if err = tt.Click("保存连接"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("请填写连接名称") || a.connection.phase != "" {
		t.Fatal("invalid form proceeded", tt.Texts())
	}
	input := func(label, value string) {
		t.Helper()
		if err := tt.Click(label + "输入"); err != nil {
			t.Fatal(err)
		}
		tt.Type(value)
	}
	input("名称", "UI server")
	input("服务器地址", fixture.Profile().Host)
	input("用户名", fixture.Profile().User)
	input("服务器密码", fixture.Profile().Password)
	a.port = strconv.Itoa(fixture.Profile().Port)
	tt.Frame()
	if err = tt.Click("保存连接"); err != nil {
		t.Fatal(err)
	}
	wait := func(done func() bool) {
		t.Helper()
		deadline := time.NewTimer(15 * time.Second)
		defer deadline.Stop()
		for !done() {
			select {
			case update := <-updates:
				update()
				tt.Frame()
			case <-deadline.C:
				t.Fatal("UI operation did not complete", a.connection.phase, a.connection.errors)
			}
		}
	}
	wait(func() bool { return a.connection.identity != nil || a.connection.phase == "" })
	if a.connection.identity == nil || fixture.Auths.Load() != 0 {
		t.Fatal("identity step missing or already authenticated", a.connection.errors)
	}
	if !a.connection.review || !tt.HasText("确认服务器身份") || tt.HasText("我已通过可信渠道核对服务器指纹") || tt.HasText("保险库主密码输入") {
		t.Fatal("identity review is hidden in the form or requires another unlock", tt.Texts())
	}
	if err = tt.Click("信任并保存"); err != nil {
		t.Fatal(err)
	}

	wait(func() bool { return a.page == 1 || a.connection.phase == "" })
	if a.page != 1 || len(a.connections) != 1 {
		t.Fatal("save did not return to workspace", a.connection.errors)
	}
	if a.form.Password != "" || a.ownerPass != "" {
		t.Fatal("successful save retained input secrets")
	}
	data, err := os.ReadFile(broker.VaultPath(dir))
	if err != nil || bytes.Contains(data, []byte(fixture.Profile().Password)) {
		t.Fatal("password not encrypted", err)
	}
	a.selected = a.connections[0].ID
	a.editConnection()
	tt.Frame()
	if a.form.Password != "" || !tt.HasText("更换登录凭据") {
		t.Fatal("editing read back secrets or lacks replacement control")
	}
	if err = tt.Click("名称输入"); err != nil {
		t.Fatal(err)
	}
	primary := ui.Ctrl
	if runtime.GOOS == "darwin" {
		primary = ui.Super
	}
	tt.Key(primary, ui.KeyA)
	tt.Type("Renamed by UI")
	if err = tt.Click("保存连接"); err != nil {
		t.Fatal(err)
	}
	wait(func() bool { return a.page == 1 || a.connection.phase == "" })
	stored, err := vault.Read(broker.VaultPath(dir), pass)
	if err != nil {
		t.Fatal(err)
	}
	defer stored.Close()
	p, err := stored.Profile(a.connections[0].ID)
	if err != nil || p.Name != "Renamed by UI" || p.Password != fixture.Profile().Password {
		t.Fatal("metadata edit changed credential or failed", err)
	}
}

func TestConnectionCancellationAndTestVaultGuard(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := &app{ctx: ctx, online: true, dir: filepath.Join(t.TempDir(), "ssh-client-workbench-preview-qa")}
	a.newConnection()
	a.form.Name = "test"
	a.form.Host = "8.130.176.127"
	a.form.User = "root"
	a.form.Password = "synthetic test only"
	if _, ok := a.connectionDraft(); ok || a.connection.errors["host"] == "" {
		t.Fatal("real host accepted into weak QA vault")
	}
	a.form.Host = "127.0.0.1"
	probe, seq, gen := a.editorOperation("probe")
	a.ownerPass = "temporary owner input"
	a.cancelConnectionEditor()
	if probe.Err() == nil || a.editorCurrent(seq, gen) || a.page != 1 || a.form.Password != "" || a.ownerPass != "" {
		t.Fatal("cancel left operations or secrets behind")
	}
	a.newConnection()
	a.form.Host = "127.0.0.1"
	a.form.Fingerprint = "previous identity"
	a.connection.trustedTarget = targetID("127.0.0.1", 22)
	a.port = "2222"
	a.form.Name = "test"
	a.form.User = "test"
	a.form.Password = "synthetic test only"
	change, _ := a.connectionDraft()
	if change.Profile.Fingerprint != "" {
		t.Fatal("identity reused for another endpoint")
	}
}

func TestPrivateKeyPickerReturnsToUIAndIgnoresCancelledSelection(t *testing.T) {
	f := testserver.New(t)
	path := filepath.Join(t.TempDir(), "generated-test-key.pem")
	if err := os.WriteFile(path, []byte(f.PrivateKey), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	updates := make(chan func(), 8)
	a := &app{ctx: ctx, online: true, dispatch: func(fn func()) { updates <- fn }}
	a.newConnection()
	a.form.Name = "unfinished connection"
	release := make(chan struct{})
	a.selectPrivateKey(func() ([]string, error) {
		<-release
		return []string{path}, nil
	})
	if a.connection.phase != "picker" || a.form.Name != "unfinished connection" {
		t.Fatal("picker blocked UI or lost draft")
	}
	a.cancelConnectionEditor()
	close(release)
	drain := func() {
		t.Helper()
		select {
		case update := <-updates:
			update()
		case <-time.After(5 * time.Second):
			t.Fatal("picker did not return to UI")
		}
	}
	drain()
	if a.keyFile != "" || a.page != 1 {
		t.Fatal("late picker result restored cancelled data")
	}
	a.newConnection()
	a.selectPrivateKey(func() ([]string, error) { return nil, nil })
	drain()
	if a.connection.phase != "" || a.keyFile != "" {
		t.Fatal("native cancellation left form busy")
	}
	a.selectPrivateKey(func() ([]string, error) { return []string{path}, nil })
	drain()
	drain()
	if a.connection.phase != "" || a.connection.keyInfo == nil || a.connection.errors["key"] != "" {
		t.Fatal("selected key was not checked", a.connection.errors)
	}
}
