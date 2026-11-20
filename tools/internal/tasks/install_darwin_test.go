package tasks

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"sshclient/internal/localipc"
)

func TestInstallIdentityChild(t *testing.T) {
	dir := os.Getenv("SMALLSSH_INSTALL_IDENTITY_CHILD")
	if dir == "" {
		return
	}
	listener, err := localipc.Listen(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	fmt.Println("ready")
	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		go func() { defer conn.Close(); _, _ = io.Copy(io.Discard, conn) }()
	}
}

func TestInstallIdenticalDaemonKeepsRunningProcessIdentity(t *testing.T) {
	root := t.TempDir()
	destination, stage := filepath.Join(root, "SmallSSH.app"), filepath.Join(root, "candidate.app")
	rel := filepath.Join("Contents", "Resources", "smallsshd")
	current, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	copyExecutable := func(bundle string) {
		t.Helper()
		path := filepath.Join(bundle, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		in, err := os.Open(current)
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0700)
		if err != nil {
			t.Fatal(err)
		}
		_, err = io.Copy(out, in)
		closeErr := out.Close()
		if err != nil || closeErr != nil {
			t.Fatal(err, closeErr)
		}
	}
	copyExecutable(destination)
	copyExecutable(stage)
	// Keep Darwin's Unix socket path below its short sockaddr_un limit.
	dir, err := os.MkdirTemp("", "install-ipc-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, filepath.Join(destination, rel), "-test.run=^TestInstallIdentityChild$")
	command.Env = append(os.Environ(), "SMALLSSH_INSTALL_IDENTITY_CHILD="+dir)
	command.Stderr = os.Stderr
	output, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = command.Process.Kill(); _ = command.Wait() })
	if line, err := bufio.NewReader(output).ReadString('\n'); err != nil || line != "ready\n" {
		t.Fatal("identity fixture failed", line, err)
	}
	raw, err := localipc.Dial(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	identity, err := localipc.IdentifyServer(raw)
	if err != nil {
		t.Fatal(err)
	}
	defer identity.Close()
	if err := identity.Verify(); err != nil {
		t.Fatal("initial process identity", err)
	}
	if err := reuseInstalledExecutables(stage, destination); err != nil {
		t.Fatal(err)
	}
	if err := replaceInstalledBundle(stage, destination); err != nil {
		t.Fatal(err)
	}
	if err := identity.Verify(); err != nil {
		t.Fatal("replacement invalidated a running unchanged daemon", err)
	}
	fresh, err := localipc.Dial(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	currentIdentity, err := localipc.IdentifyServer(fresh)
	if err != nil {
		t.Fatal("new connection cannot verify the unchanged daemon", err)
	}
	defer currentIdentity.Close()
	if !identity.Same(currentIdentity) {
		t.Fatal("replacement changed the process identity")
	}
}
