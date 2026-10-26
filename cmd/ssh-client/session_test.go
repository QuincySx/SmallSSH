package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sshclient/internal/approval"
	"sshclient/internal/broker"
	"sshclient/internal/client"
	"sshclient/internal/localipc"
	"sshclient/internal/testserver"
	"sshclient/internal/vault"
)

func TestCLISessionRequestApproveUseAndRevoke(t *testing.T) {
	dir, err := os.MkdirTemp("", "cli-session-fixture-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	if err = localipc.Prepare(dir); err != nil {
		t.Fatal(err)
	}
	f := testserver.New(t)
	pass := []byte("generated CLI fixture vault password")
	v, err := vault.New(pass)
	if err != nil {
		t.Fatal(err)
	}
	if err = v.Put(f.Profile()); err != nil {
		t.Fatal(err)
	}
	if err = vault.Save(broker.VaultPath(dir), v, true); err != nil {
		t.Fatal(err)
	}
	v.Close()
	s, err := broker.Open(dir, pass)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c := client.Client{Dir: dir}
	done := make(chan error, 1)
	go func() {
		_, err := sessionCommand(ctx, c, "CLI fixture", []string{"request", "fixture", "fixture.json", "full", "2m", "1m"})
		done <- err
	}()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	var parent approval.Request
wait:
	for {
		select {
		case <-deadline.C:
			t.Fatal("CLI did not request approval")
		case <-tick.C:
			pending, err := c.Call(ctx, broker.Message{Action: "pending"})
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range pending.Requests {
				if r.Operation.Kind == "grant" {
					parent = r
					break wait
				}
			}
		}
	}
	if parent.Operation.Grant.Permission != "full" || parent.Operation.Grant.LifetimeSeconds != 120 || parent.Operation.Grant.IdleSeconds != 60 {
		t.Fatal("CLI changed requested limits")
	}
	if err = c.Decide(ctx, parent.ID, true, false, pass); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("CLI did not save the approved session")
	}
	if _, err = os.Stat(filepath.Join(dir, "client-sessions", "fixture.json")); err != nil {
		t.Fatal(err)
	}
	savedArgs := os.Args
	defer func() { os.Args = savedArgs }()
	os.Args = []string{"ssh-client", "--data-dir", dir, "--session", "fixture.json", "exec", "fixture", "CLI delegated execution"}
	if code, err := run(); err != nil || code != 0 {
		t.Fatal("CLI did not execute through its delegated session", code, err)
	}
	// Subsequent commands use the same explicitly selected capability. There
	// is still exactly one owner-approved parent, and no per-command prompts.
	t.Setenv("SSH_CLIENT_SESSION", "fixture.json")
	os.Args = []string{"ssh-client", "--data-dir", dir, "exec", "fixture", "CLI environment execution"}
	for range 3 {
		if code, err := run(); err != nil || code != 0 {
			t.Fatal("CLI environment did not reuse the approved parent", code, err)
		}
	}
	os.Args = []string{"ssh-client", "--data-dir", dir, "exec", "different-server", "must not run"}
	if code, err := run(); err == nil || code == 0 {
		t.Fatal("session environment crossed server scope")
	}
	before, err := c.Call(ctx, broker.Message{Action: "pending"})
	if err != nil {
		t.Fatal(err)
	}
	parents, children := 0, 0
	for _, r := range before.Requests {
		if r.Operation.Kind == "grant" {
			parents++
			continue
		}
		if r.Status == "pending" || r.ParentID != parent.ID {
			t.Fatal("repeat command queued another approval or changed parent", r.Status, r.ParentID)
		}
		children++
	}
	if parents != 1 || children != 4 {
		t.Fatal("CLI did not reuse one parent for every command", parents, children)
	}
	if _, err = sessionCommand(ctx, c, "fixture", []string{"status", "fixture.json"}); err != nil {
		t.Fatal(err)
	}
	if _, err = sessionCommand(ctx, c, "fixture", []string{"revoke", "fixture.json"}); err != nil {
		t.Fatal(err)
	}
	os.Args = []string{"ssh-client", "--data-dir", dir, "exec", "fixture", "revoked environment execution"}
	if code, err := run(); err == nil || code == 0 {
		t.Fatal("CLI reused a revoked session")
	}
	after, err := c.Call(ctx, broker.Message{Action: "pending"})
	if err != nil || len(after.Requests) != len(before.Requests) {
		t.Fatal("revoked session silently requested another approval", err)
	}
	// A bad environment fails closed, but session management remains usable.
	t.Setenv("SSH_CLIENT_SESSION", "missing-session.json")
	if code, err := run(); err == nil || code == 0 || !strings.Contains(err.Error(), "missing-session.json") {
		t.Fatal("missing environment session fell back to an unscoped request", code, err)
	}
	os.Args = []string{"ssh-client", "--data-dir", dir, "session", "status", "fixture.json"}
	if code, err := run(); err != nil || code != 0 {
		t.Fatal("stale environment blocked session status", code, err)
	}
}
