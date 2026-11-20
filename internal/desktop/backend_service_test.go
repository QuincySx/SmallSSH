package desktop

import (
	"context"
	"github.com/egoist/mygo/ui"
	"os"
	"runtime"
	"testing"
	"time"

	"sshclient/internal/client"
	"sshclient/internal/protocol"
	"sshclient/internal/service"
	"sshclient/internal/vault"
)

func TestDesktopManagedSessionDropsRootAndLocksThroughIPC(t *testing.T) {
	base := ""
	if runtime.GOOS != "windows" {
		base = "/tmp"
	}
	dir, err := os.MkdirTemp(base, "sshd-ui-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	source, err := vault.New([]byte("synthetic desktop managed password"))
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	if err := vault.Save(vault.Path(dir), source, true); err != nil {
		t.Fatal(err)
	}
	host, err := service.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := attachDesktopSession(ctx, client.ServiceClient{Dir: dir, Executable: executable}, source, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if _, err := source.Proof([]byte("root must be gone")); err == nil {
		t.Fatal("desktop retained a root signer")
	}
	data := client.Client{Dir: dir, BrokerKey: session.PublicKeyPin()}
	if _, err := data.ListSnippets(ctx, session); err != nil {
		t.Fatal("managed owner could not query", err)
	}

	updates := make(chan func(), 8)
	a := &app{ctx: ctx, dir: dir, online: true, server: session, dispatch: func(fn func()) { updates <- fn }}
	a.lock()
	if _, err := session.Proof([]byte("locked UI")); err == nil {
		t.Fatal("lock did not revoke frontend authority synchronously")
	}
	select {
	case <-a.unlockCleanup:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case fn := <-updates:
		fn()
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if a.status != tr(nil, "已锁定，连接与授权已撤销") {
		t.Fatal("lock was not confirmed", a.status)
	}

	if host.Status().State != protocol.ServiceStateLocked {
		t.Fatal("desktop lock left core unlocked")
	}
	reopened, err := vault.Read(vault.Path(dir), []byte("synthetic desktop managed password"))
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if reopened.AuditHead() == "" {
		t.Fatal("lock lost the audit anchor")
	}
}

func TestDesktopLockDoesNotClaimRemoteRevocationWithoutConfirmation(t *testing.T) {
	source := generatedApprovalOwner(t)
	defer source.Close()
	updates := make(chan func(), 8)
	session := &desktopSession{owner: source, pin: "synthetic-pin", seal: func() error { return context.DeadlineExceeded }, close: source.Close}
	a := &app{ctx: context.Background(), dir: t.TempDir(), online: true, server: session, dispatch: func(fn func()) { updates <- fn }}
	a.lock()
	if _, err := session.Proof([]byte("locked UI")); err == nil {
		t.Fatal("local capability was not revoked")
	}
	select {
	case <-a.unlockCleanup:
	case <-time.After(5 * time.Second):
		t.Fatal("lock cleanup stalled")
	}
	select {
	case update := <-updates:
		update()
	case <-time.After(5 * time.Second):
		t.Fatal("lock result was not reported")
	}
	if a.online || a.status != tr(nil, "界面已锁定，连接服务未确认锁定") {
		t.Fatal("failed RPC was presented as confirmed revocation", a.status)
	}
	view := ui.NewTester(a.lockedView, 1000, 760)
	feedback, ok := view.Find(tr(nil, "解锁错误"))
	card, cardOK := view.Find(tr(nil, "保险库卡片"))
	if a.unlockError != a.status || !ok || !cardOK || feedback.H <= 0 || feedback.Y < card.Y || feedback.Y+feedback.H > card.Y+card.H {
		t.Fatal("lock failure is not visible within the locked card", feedback, card)
	}
}
