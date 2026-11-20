package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"sshclient/internal/accesspolicy"
	"sshclient/internal/approval"
	"sshclient/internal/auditlog"
	"sshclient/internal/broker"
	"sshclient/internal/client"
	"sshclient/internal/localipc"
	"sshclient/internal/testserver"
	"sshclient/internal/transfer"
	"sshclient/internal/vault"
)

func sessionFixture(t *testing.T) (client.Client, context.Context) {
	return sessionFixtureWithPolicy(t, accesspolicy.Policy{Unrestricted: true})
}

func sessionFixtureWithPolicy(t *testing.T, policy accesspolicy.Policy) (client.Client, context.Context) {
	t.Helper()
	dir, err := os.MkdirTemp("", "cli-session-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	if err := localipc.Prepare(dir); err != nil {
		t.Fatal(err)
	}
	fixture := testserver.New(t)
	owner, err := vault.New([]byte("synthetic CLI zero trust test password"))
	if err != nil {
		t.Fatal(err)
	}
	profile := fixture.Profile()
	profile.Policy = policy
	if err = owner.Put(profile); err != nil {
		t.Fatal(err)
	}
	server, err := broker.OpenSession(dir, owner)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	return client.Client{Dir: dir, Owner: server.Vault}, ctx
}

func approveCLISession(t *testing.T, ctx context.Context, c client.Client, done <-chan error) approval.Request {
	t.Helper()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case err := <-done:
			t.Fatal("session ended before consent", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-tick.C:
			result, err := c.Call(ctx, broker.Message{Action: "pending"})
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range result.Requests {
				if r.Operation.Kind != "grant" || r.Status != "pending" {
					continue
				}
				if r.Peer.PID != os.Getpid() || r.Peer.Instance == "" || r.Peer.CodeID == "" {
					t.Fatal("missing kernel process instance", r.Peer)
				}
				if runtime.GOOS == "darwin" && r.Peer.Bundle == "" {
					t.Fatal("missing signing identifier", r.Peer)
				}
				if err := c.DecideReviewedWithOwner(ctx, r, true, false, c.Owner); err != nil {
					t.Fatal(err)
				}
				return r
			}
		}
	}
}

func TestCLIStdioSessionUsesOneLiveGrantAndRevokesOnEOF(t *testing.T) {
	c, ctx := sessionFixture(t)
	input := strings.NewReader(`{"id":"one","operation":{"kind":"exec","command":"first"}}
{"id":"two","operation":{"kind":"exec","command":"second"}}
{"id":"cross-host","operation":{"kind":"exec","host":"another-host","command":"must-not-run"}}
{"id":"status","action":"status"}
`)
	var out, notices bytes.Buffer
	done := make(chan error, 1)
	go func() {
		_, err := serveSession(ctx, c, "CLI fixture", []string{"fixture", "full", "2m", "1m"}, input, &out, &notices)
		done <- err
	}()
	parent := approveCLISession(t, ctx, c, done)
	if parent.Operation.Grant.LifetimeSeconds != 120 || parent.Operation.Grant.IdleSeconds != 60 {
		t.Fatal("CLI changed grant limits")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	decoder := json.NewDecoder(&out)
	for i := range 4 {
		var result sessionOutput
		if err := decoder.Decode(&result); err != nil {
			t.Fatal(err)
		}
		if i < 2 && (result.Error != "" || !bytes.Contains(result.Stdout, []byte([]string{"first", "second"}[i]))) {
			t.Fatal("delegated command failed", result)
		}
		if i == 2 && result.Error == "" {
			t.Fatal("stdio session crossed host scope")
		}
		if i == 3 && (result.Request == nil || result.Request.ID != parent.ID || result.Request.Status != "approved") {
			t.Fatal("stdio session lost original control channel")
		}
	}
	if _, err := os.Stat(filepath.Join(c.Dir, "client-sessions")); !os.IsNotExist(err) {
		t.Fatal("persisted session credentials", err)
	}
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		status, err := c.Call(ctx, broker.Message{Action: "status", ID: parent.ID})
		if err == nil && status.Request != nil && status.Request.Status == "revoked" {
			break
		}
		select {
		case <-tick.C:
		case <-ctx.Done():
			t.Fatal("EOF left session reusable", ctx.Err())
		}
	}
}

func TestCLIStdioScopedSessionKeepsApprovedRootsInMemory(t *testing.T) {
	c, ctx := sessionFixture(t)
	var out, notices bytes.Buffer
	done := make(chan error, 1)
	go func() {
		_, err := serveSession(ctx, c, "scoped fixture", []string{"--read-root", "/srv/read", "--write-root", "/srv/write", "fixture", "full", "2m", "1m"}, strings.NewReader(`{"id":"status","action":"status"}`), &out, &notices)
		done <- err
	}()
	parent := approveCLISession(t, ctx, c, done)
	if parent.Operation.Grant.ReadRoot != "/srv/read" || parent.Operation.Grant.WriteRoot != "/srv/write" {
		t.Fatal("CLI changed roots before consent")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	var result sessionOutput
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &result); err != nil {
		t.Fatal(err)
	}
	if result.Request == nil || result.Request.Operation.Grant != parent.Operation.Grant {
		t.Fatal("approved roots changed in live session")
	}
}

func TestCLIMaintenanceUsesOneApprovalForPreviewAndBackupResume(t *testing.T) {
	c, ctx := sessionFixture(t)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(dir, "archive.enc")
	data := []byte("synthetic backup data")
	if err := os.WriteFile(source, data, 0600); err != nil {
		t.Fatal(err)
	}
	remote := testserver.RemotePath(source)
	local := filepath.Join(dir, "backup.enc")
	var stdout, notices bytes.Buffer
	in, writer := io.Pipe()
	defer writer.Close()
	done := make(chan error, 1)
	go func() {
		_, err := serveWithPermission(ctx, c, "Maintenance fixture", []string{"--read-root", remote, "fixture"}, in, &stdout, &notices, approval.MaintenancePermission)
		done <- err
	}()
	parent := approveCLISession(t, ctx, c, done)
	if scope := parent.Operation.Grant; scope.Permission != approval.MaintenancePermission || scope.ReadRoot != remote || scope.LifetimeSeconds != 3600 {
		t.Fatal("wrong maintenance scope", scope)
	}
	job, err := transfer.NewWithClient(c).AddDownload("fixture", local, remote, parent.Operation.Endpoint, parent.Operation.HostFingerprint, false)
	if err != nil {
		t.Fatal(err)
	}
	part := filepath.Join(dir, ".backup.enc.ssh-part-"+job.ID)
	if err := os.WriteFile(part, data[:9], 0600); err != nil {
		t.Fatal(err)
	}
	requests := []sessionInput{
		{ID: "inspect", Action: "run", Operation: approval.Operation{Kind: "exec", Command: "uname -a"}},
		{ID: "preview", Action: "preview-upgrade"},
		{ID: "resume", Action: "resume-backup", TransferID: job.ID},
		{ID: "copy", Action: "backup", Remote: remote, Local: filepath.Join(dir, "second.enc")},
		{ID: "install", Action: "run", Operation: approval.Operation{Kind: "exec", Command: "apt-get upgrade -y"}},
		{ID: "reboot", Action: "run", Operation: approval.Operation{Kind: "exec", Command: "reboot"}},
		{ID: "other-path", Action: "run", Operation: approval.Operation{Kind: "get", Path: testserver.RemotePath(filepath.Join(dir, "outside.enc"))}},
		{ID: "override", Action: "run", Operation: approval.Operation{Kind: "upgrade-preview", Command: "apt-get upgrade -y"}},
	}
	encoder := json.NewEncoder(writer)
	for _, request := range requests {
		if err := encoder.Encode(request); err != nil {
			t.Fatal(err)
		}
	}
	writer.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	decoder := json.NewDecoder(&stdout)
	for i, request := range requests {
		var result sessionOutput
		if err := decoder.Decode(&result); err != nil {
			t.Fatal(err)
		}
		if result.ID != request.ID || (result.Error != "") != (i >= 4) {
			t.Fatal(request.ID, result.Error)
		}
		if i == 1 && !bytes.Contains(result.Stdout, []byte("/usr/bin/apt-get --simulate --no-download upgrade")) {
			t.Fatal("fixture did not receive the fixed APT command", string(result.Stdout))
		}
	}
	for _, name := range []string{local, filepath.Join(dir, "second.enc")} {
		if got, err := os.ReadFile(name); err != nil || !bytes.Equal(got, data) {
			t.Fatal("backup contents", name, got, err)
		}
	}
	page, err := c.History(ctx, auditlog.Query{Limit: 100}, c.Owner)
	if err != nil {
		t.Fatal(err)
	}
	requested, delegated, rejected := 0, 0, 0
	for _, event := range page.Events {
		if event.Event == "requested" {
			requested++
		}
		if event.ParentID == parent.ID {
			switch event.Event {
			case "delegated":
				delegated++
			case "denied":
				rejected++
			}
		}
	}
	if requested != 1 || delegated != 4 || rejected < 3 {
		t.Fatal("requests, delegated operations, and refusals", requested, delegated, rejected)
	}
}

func TestCLILegacySessionFilesFailClosedBeforeConnecting(t *testing.T) {
	for _, args := range [][]string{{"request", "fixture", "old.json"}, {"status", "old.json"}, {"revoke", "old.json"}} {
		if _, err := sessionCommand(context.Background(), client.Client{}, "fixture", args); !errors.Is(err, client.ErrSessionNotExportable) {
			t.Fatal("accepted cross-process session files", args, err)
		}
	}
	original := os.Args
	defer func() { os.Args = original }()
	t.Setenv("SSH_CLIENT_SESSION", "old.json")
	for _, args := range [][]string{{"smallssh", "exec", "fixture", "must-not-run"}, {"smallssh", "--session", "old.json", "exec", "fixture", "must-not-run"}} {
		os.Args = args
		if _, err := run(); !errors.Is(err, client.ErrSessionNotExportable) {
			t.Fatal("saved session silently became a new request", err)
		}
	}
}
