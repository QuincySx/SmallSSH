package ownercap

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"testing"
	"time"

	"sshclient/internal/localipc"
)

const (
	subprocessHelperEnv  = "SMALLSSH_B04A_OWNERCAP_HELPER"
	subprocessHelperDir  = "SMALLSSH_B04A_OWNERCAP_DIR"
	subprocessHelperName = "TestOwnercapSubprocessHelper"
)

func TestOwnercapSubprocessHelper(t *testing.T) {
	if os.Getenv(subprocessHelperEnv) != "1" {
		return
	}
	os.Exit(ownercapSubprocessHelper())
}

func ownercapSubprocessHelper() int {
	dir := os.Getenv(subprocessHelperDir)
	if dir == "" {
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	connection, err := localipc.Dial(ctx, dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer connection.Close()
	if _, err := localipc.IdentifyServer(connection); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if _, err := fmt.Fprintln(os.Stdout, "ready"); err != nil {
		return 1
	}
	line, err := readLineWithin(ctx, bufio.NewReader(os.Stdin))
	if err != nil || line != "finish" {
		return 1
	}
	return 0
}

func TestChildProcessIdentityCannotReuseParentProof(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" && runtime.GOOS != "windows" {
		t.Skip("kernel process identity is unavailable on this platform")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	expectedCode, err := localipc.ExecutableCodeID(executable)
	if err != nil {
		t.Fatal(err)
	}
	dir := shortTestDir(t)
	listener, err := localipc.Listen(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "-test.run=^"+subprocessHelperName+"$")
	command.Env = []string{subprocessHelperEnv + "=1", subprocessHelperDir + "=" + dir}
	input, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := command.StdoutPipe()
	if err != nil {
		_ = input.Close()
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		_ = input.Close()
		_ = output.Close()
		t.Fatal(err)
	}
	waited := false
	t.Cleanup(func() {
		_ = input.Close()
		if !waited {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	})
	type accepted struct {
		conn     net.Conn
		identity *localipc.ProcessIdentity
		err      error
	}
	acceptedCh := make(chan accepted)
	acceptDone := make(chan struct{})
	defer close(acceptDone)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case acceptedCh <- accepted{err: err}:
			case <-acceptDone:
			}
			return
		}
		identity, err := localipc.Identify(conn)
		result := accepted{conn: conn, identity: identity, err: err}
		select {
		case acceptedCh <- result:
		case <-acceptDone:
			if identity != nil {
				identity.Close()
			}
			_ = conn.Close()
		}
	}()
	line, err := readLineWithin(ctx, bufio.NewReader(output))
	if err != nil || line != "ready" {
		_ = command.Process.Kill()
		_ = command.Wait()
		waited = true
		t.Fatalf("helper did not ready: %q: %v: %s", line, err, stderr.String())
	}
	got := receiveWithin(t, acceptedCh, "helper connection identity")
	if got.err != nil {
		if got.conn != nil {
			_ = got.conn.Close()
		}
		if got.identity != nil {
			got.identity.Close()
		}
		t.Fatal(got.err)
	}
	childConnection := got.conn
	defer childConnection.Close()
	childIdentity := got.identity
	defer childIdentity.Close()
	if childIdentity == nil || childIdentity.PID != command.Process.Pid || childIdentity.PID == os.Getpid() || childIdentity.CodeID != expectedCode || childIdentity.Verify() != nil {
		t.Fatalf("child identity = %#v, child pid = %d, parent pid = %d", childIdentity, command.Process.Pid, os.Getpid())
	}

	control := newIdentityPair(t)
	parentIdentity := control.caller
	if parentIdentity.CodeID != childIdentity.CodeID || parentIdentity.PID != os.Getpid() || parentIdentity.Verify() != nil || childIdentity.Same(parentIdentity) {
		t.Fatalf("parent identity = %#v, child identity = %#v", parentIdentity, childIdentity)
	}
	registry, err := NewRegistry(testEpoch, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(registry.Close)
	signer, err := NewSigner()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(signer.Close)
	controlCtx, controlCancel := context.WithCancel(ctx)
	defer controlCancel()
	info, err := registry.Register(controlCtx, childIdentity, signer.PublicKey(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := signer.Bind(info); err != nil {
		t.Fatal(err)
	}
	payload := []byte("synthetic management request")
	proof, err := signer.Proof(payload)
	if err != nil {
		t.Fatal(err)
	}
	if sessionID, err := registry.Verify(childIdentity, payload, proof); err != nil || sessionID != info.ID {
		t.Fatalf("child identity proof = %q, %v", sessionID, err)
	}
	if _, err := registry.Verify(parentIdentity, payload, proof); err == nil {
		t.Fatal("same executable in another process reused the child proof")
	}

	if _, err := fmt.Fprintln(input, "finish"); err != nil {
		t.Fatal(err)
	}
	waitErr := command.Wait()
	waited = true
	if waitErr != nil {
		t.Fatalf("helper failed: %v: %s", waitErr, stderr.String())
	}
	if err := childIdentity.Verify(); err == nil {
		t.Fatal("child process identity still verified after Wait")
	}
	if _, err := registry.Verify(childIdentity, payload, proof); err == nil {
		t.Fatal("exited child identity remained usable")
	}
}
