package desktop

import (
	"context"
	"errors"
	"os"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/approval"
	"sshclient/internal/client"
	"sshclient/internal/service"
)

func TestLockedRequestRestoresExistingUnlockViewOnce(t *testing.T) {
	base := ""
	if runtime.GOOS != "windows" {
		base = "/tmp"
	}
	dir, err := os.MkdirTemp(base, "sshu-notice-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	host, err := service.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = host.Close() })
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := &app{dir: dir, ctx: ctx, dispatch: func(fn func()) { fn() }, startup: newStartup(false), unlockService: client.ServiceClient{Dir: dir, Executable: executable}}
	var events []string
	fakePresence(a, &events)
	a.presence.backgrounded = true
	reader := a.pollUnlockNotice(a.epoch.Load(), nil)
	defer func() { reader.Close() }()
	if len(events) != 0 {
		t.Fatal("idle locked app requested focus", events)
	}
	c := client.Client{Dir: dir, ServiceExecutable: executable}
	if _, _, err := c.Request(ctx, approval.Operation{Host: "fixture", Kind: "list", Path: "/"}, time.Minute); !errors.Is(err, client.ErrVaultLocked) {
		t.Fatal(err)
	}
	reader = a.pollUnlockNotice(a.epoch.Load(), reader)
	reader = a.pollUnlockNotice(a.epoch.Load(), reader)
	if !reflect.DeepEqual(events, []string{"dock-on", "show", "focus"}) || a.online {
		t.Fatalf("request did not reveal the still-locked window exactly once: %v", events)
	}
	tt := ui.NewTester(a.view, 900, 640)
	tt.Frame()
	if _, ok := tt.Find("主密码"); !ok {
		t.Fatal("restored locked window did not offer the existing password entry")
	}
}

func TestUnlockNoticeRespectsWindowAndVaultState(t *testing.T) {
	now := time.Now()
	for _, state := range []string{"expired", "old-epoch", "unlocked", "create-vault", "dev-hidden", "quitting", "already-visible"} {
		t.Run(state, func(t *testing.T) {
			a := &app{}
			var events []string
			fakePresence(a, &events)
			until := now.Add(time.Second)
			switch state {
			case "expired":
				until = now
			case "old-epoch":
				a.epoch.Add(1)
			case "unlocked":
				a.online = true
			case "create-vault":
				a.create = true
			case "dev-hidden":
				a.startup = newStartup(true)
			case "quitting":
				a.presence.quitting = true
			case "already-visible":
				a.applicationActive = true
				a.presence.effects.mainNeedsRestore = func() bool { return false }
			}
			a.showUnlockNotice(0, until, now)
			if len(events) != 0 {
				t.Fatal("unexpected window actions", events)
			}
		})
	}
}
