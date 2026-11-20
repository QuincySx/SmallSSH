//go:build service_integration

package client

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"sshclient/internal/protocol"
	"sshclient/internal/vault"
)

// script supplies a private controlling terminal; it never opens a desktop
// window or types into the user's terminal. The password is synthetic.
func TestPackagedCLIAgentUsesDaemonAndLocksOnInterrupt(t *testing.T) {
	cli := os.Getenv("SSH_CLIENT_TEST_CLI")
	daemon := os.Getenv("SSH_CLIENT_TEST_DAEMON")
	if cli == "" || daemon == "" {
		t.Skip("set the packaged CLI and daemon paths")
	}
	cli, err := filepath.Abs(cli)
	if err != nil {
		t.Fatal(err)
	}
	daemon, err = filepath.Abs(daemon)
	if err != nil {
		t.Fatal(err)
	}
	dir := b03bClientTestDir(t)
	pass := []byte("synthetic private terminal password")
	source, err := vault.New(pass)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	if err := vault.Save(vault.Path(dir), source, true); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	local := &LocalService{client: ServiceClient{Dir: dir, Executable: daemon}}
	if err := local.Ensure(ctx); err != nil {
		t.Fatal(err)
	}
	before, err := local.client.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		pending, err := local.client.PrepareOwner(cleanup, source, 0)
		if err != nil {
			t.Error("cleanup prepare", err)
			return
		}
		defer pending.Close()
		owner, err := pending.Commit(cleanup)
		if err != nil {
			t.Error("cleanup commit", err)
			return
		}
		defer owner.Close()
		if _, err := owner.Stop(cleanup); err != nil {
			t.Error("cleanup stop", err)
		}
	}()
	cmd := exec.Command("/usr/bin/script", "-q", "/dev/null", cli, "--data-dir", dir, "agent")
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	finished := false
	defer func() {
		if !finished {
			_, _ = input.Write([]byte{3})
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	chunks := make(chan []byte, 32)
	go func() {
		defer close(chunks)
		buf := make([]byte, 4096)
		for {
			n, err := output.Read(buf)
			if n > 0 {
				chunks <- append([]byte(nil), buf[:n]...)
			}
			if err != nil {
				return
			}
		}
	}()
	var seen bytes.Buffer
	await := func(marker string) {
		t.Helper()
		for !bytes.Contains(seen.Bytes(), []byte(marker)) {
			select {
			case part, ok := <-chunks:
				if !ok {
					t.Fatalf("CLI exited before %s: %s", marker, seen.String())
				}
				seen.Write(part)
			case <-ctx.Done():
				t.Fatalf("CLI did not reach %s: %s", marker, seen.String())
			}
		}
	}
	await("主密码:")
	if _, err := input.Write(append(append([]byte(nil), pass...), '\n')); err != nil {
		t.Fatal(err)
	}
	await("保持此窗口打开")
	active, err := local.client.Status(ctx)
	if err != nil || active.State != protocol.ServiceStateUnlocked || !active.StartedAt.Equal(before.StartedAt) {
		t.Fatal("agent did not reuse the daemon", err)
	}
	if _, err := input.Write([]byte{3}); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		finished = true
		if err != nil {
			t.Fatal("CLI failed to exit cleanly", err)
		}
	case <-ctx.Done():
		t.Fatal("CLI did not exit after interrupt")
	}
	_, _ = io.Copy(io.Discard, output)
	status, err := local.client.Status(ctx)
	if err != nil || status.State != protocol.ServiceStateLocked {
		t.Fatal("CLI exit did not lock daemon", err)
	}
	request := exec.CommandContext(ctx, cli, "--data-dir", dir, "ls", "fixture", "/")
	if output, err := request.CombinedOutput(); err == nil || !bytes.Contains(output, []byte(ErrVaultLocked.Error())) {
		t.Fatalf("packaged request did not report the lock: %s (%v)", output, err)
	}
	status, err = local.client.Status(ctx)
	if err != nil || status.State != protocol.ServiceStateLocked || status.UnlockNoticeUntil == nil {
		t.Fatalf("packaged request did not leave a notice for the desktop: %#v %v", status, err)
	}
}
