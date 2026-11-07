package localipc

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const (
	b03aSubprocessHelperEnv  = "SMALLSSH_B03A_EXECUTABLE_SERVER_HELPER"
	b03aSubprocessDirEnv     = "SMALLSSH_B03A_EXECUTABLE_SERVER_DIR"
	b03aSubprocessHelperName = "TestB03aExecutableServerSubprocessHelper"
)

func TestB03aExecutableCodeIDMatchesServerProcess(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	code, err := ExecutableCodeID(executable)
	if err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp("", "ipc-codeid-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	listener, err := Listen(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	type result struct {
		identity *ProcessIdentity
		err      error
	}
	results := make(chan result, 1)
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			results <- result{err: err}
			return
		}
		identity, err := IdentifyServer(connection)
		connection.Close()
		results <- result{identity: identity, err: err}
	}()
	connection, err := Dial(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	got := <-results
	if got.err != nil {
		t.Fatal(got.err)
	}
	defer got.identity.Close()
	if got.identity.PID != os.Getpid() || got.identity.CodeID != code || got.identity.Verify() != nil {
		t.Fatalf("server process identity does not match executable identity: %#v", got.identity)
	}
}

func TestB03aExecutableCodeIDFileContract(t *testing.T) {
	if _, err := ExecutableCodeID(""); err == nil {
		t.Fatal("accepted an empty executable path")
	}
	if _, err := ExecutableCodeID(t.TempDir()); err == nil {
		t.Fatal("accepted a directory as an executable")
	}
	if _, err := ExecutableCodeID(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("accepted a missing executable")
	}
	content := []byte("not a signed program\n")
	path := filepath.Join(t.TempDir(), "plain-file")
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	code, err := ExecutableCodeID(path)
	if runtime.GOOS == "darwin" {
		if err == nil {
			t.Fatal("accepted a plain file as signed executable code")
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(content)
	if want := "sha256:" + hex.EncodeToString(digest[:]); code != want {
		t.Fatalf("plain-file identity = %q, want %q", code, want)
	}
}

func TestB03aSyntheticExecutableTransport(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp("", "ipc-executable-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	listener, err := Listen(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	configuration, _, err := ServerTLS()
	if err != nil {
		t.Fatal(err)
	}
	defer DestroyServerTLS(configuration)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	type serverResult struct {
		pid      int
		code     string
		verified bool
		binding  string
		action   string
		err      error
	}
	results := make(chan serverResult, 1)
	go func() {
		raw, err := listener.Accept()
		if err != nil {
			results <- serverResult{err: err}
			return
		}
		defer raw.Close()
		identity, err := IdentifyServer(raw)
		if err != nil {
			results <- serverResult{err: err}
			return
		}
		defer identity.Close()
		pid, code, verified := identity.PID, identity.CodeID, identity.Verify() == nil
		connection := tls.Server(raw, configuration)
		connection.SetDeadline(time.Now().Add(10 * time.Second))
		if err := connection.HandshakeContext(ctx); err != nil {
			results <- serverResult{err: err}
			return
		}
		binding, err := ChannelBinding(connection)
		if err != nil {
			results <- serverResult{err: err}
			return
		}
		wire := &Wire{Conn: connection, WriteTimeout: 5 * time.Second}
		var request map[string]string
		if err := wire.Receive(&request); err != nil {
			results <- serverResult{err: err}
			return
		}
		if err := wire.Send(map[string]string{"reply": "synthetic response"}); err != nil {
			results <- serverResult{err: err}
			return
		}
		results <- serverResult{pid: pid, code: code, verified: verified, binding: binding, action: request["action"]}
	}()
	wire, err := ConnectExecutable(ctx, dir, executable)
	if err != nil {
		t.Fatal(err)
	}
	defer wire.Close()
	binding, err := ChannelBinding(wire.Conn)
	if err != nil {
		t.Fatal(err)
	}
	if err := wire.Send(map[string]string{"action": "synthetic request"}); err != nil {
		t.Fatal(err)
	}
	var response map[string]string
	if err := wire.Receive(&response); err != nil {
		t.Fatal(err)
	}
	if response["reply"] != "synthetic response" {
		t.Fatalf("unexpected response: %#v", response)
	}
	got := <-results
	if got.err != nil {
		t.Fatal(got.err)
	}
	expectedCode, err := ExecutableCodeID(executable)
	if err != nil {
		t.Fatal(err)
	}
	if got.action != "synthetic request" || got.binding != binding || got.pid != os.Getpid() || got.code != expectedCode || !got.verified {
		t.Fatalf("transport or process identity changed: %#v", got)
	}
}

func TestB03aExecutableServerSubprocessHelper(t *testing.T) {
	if os.Getenv(b03aSubprocessHelperEnv) != "1" {
		return
	}
	os.Exit(b03aRunExecutableServerSubprocess())
}

func b03aRunExecutableServerSubprocess() int {
	dir := os.Getenv(b03aSubprocessDirEnv)
	if dir == "" {
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	listener, err := Listen(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer listener.Close()
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()
	configuration, _, err := ServerTLS()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer DestroyServerTLS(configuration)
	if _, err := fmt.Fprintln(os.Stdout, "listening"); err != nil {
		return 1
	}
	raw, err := listener.Accept()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer raw.Close()
	if err := raw.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	connection := tls.Server(raw, configuration)
	if err := connection.HandshakeContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	binding, err := ChannelBinding(connection)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	wire := &Wire{Conn: connection, WriteTimeout: 5 * time.Second}
	var request map[string]string
	if err := wire.Receive(&request); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if request["action"] != "synthetic subprocess request" {
		return 1
	}
	if err := wire.Send(map[string]string{"reply": "synthetic subprocess response", "binding": binding}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if _, err := fmt.Fprintln(os.Stdout, "ready"); err != nil {
		return 1
	}
	type controlResult struct {
		line string
		err  error
	}
	control := make(chan controlResult, 1)
	go func() {
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		control <- controlResult{line: strings.TrimSpace(line), err: err}
	}()
	select {
	case <-ctx.Done():
		return 3
	case got := <-control:
		if got.err != nil || got.line != "finish" {
			return 1
		}
		return 0
	}
}

func TestB03aExecutableServerIdentityAcrossProcess(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" && runtime.GOOS != "windows" {
		t.Skip("kernel process identity is unavailable on this platform")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := b03aShortTempDir(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "-test.run=^"+b03aSubprocessHelperName+"$")
	command.Env = []string{b03aSubprocessHelperEnv + "=1", b03aSubprocessDirEnv + "=" + dir}
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
	reader := bufio.NewReader(output)
	line, err := b03aReadLine(ctx, reader)
	if err != nil || line != "listening" {
		t.Fatalf("helper did not listen: %q: %v", line, err)
	}
	wire, err := ConnectExecutable(ctx, dir, executable)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = wire.Close() })
	connection, ok := wire.Conn.(*executableServerConn)
	if !ok || connection.identity == nil {
		t.Fatal("executable transport did not retain the server identity")
	}
	identity := connection.identity
	expectedCode, err := ExecutableCodeID(executable)
	if err != nil {
		t.Fatal(err)
	}
	if identity.PID != command.Process.Pid || identity.PID == os.Getpid() || identity.CodeID != expectedCode || identity.Verify() != nil {
		t.Fatalf("server identity = %#v, child pid = %d, parent pid = %d", identity, command.Process.Pid, os.Getpid())
	}
	binding, err := ChannelBinding(wire.Conn)
	if err != nil {
		t.Fatal(err)
	}
	if err := wire.Send(map[string]string{"action": "synthetic subprocess request"}); err != nil {
		t.Fatal(err)
	}
	if err := wire.Conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var response map[string]string
	if err := wire.Receive(&response); err != nil {
		t.Fatal(err)
	}
	if err := wire.Conn.SetReadDeadline(time.Time{}); err != nil {
		t.Fatal(err)
	}
	if response["reply"] != "synthetic subprocess response" || response["binding"] != binding {
		t.Fatalf("unexpected subprocess response: %#v", response)
	}
	line, err = b03aReadLine(ctx, reader)
	if err != nil || line != "ready" {
		t.Fatalf("helper did not remain ready after traffic: %q: %v", line, err)
	}
	control := b03aParentServerIdentity(t, ctx)
	defer control.Close()
	if control.PID != os.Getpid() || control.CodeID != expectedCode || control.Verify() != nil || identity.Same(control) {
		t.Fatalf("parent control identity = %#v, child identity = %#v", control, identity)
	}
	if _, err := fmt.Fprintln(input, "finish"); err != nil {
		t.Fatal(err)
	}
	err = command.Wait()
	waited = true
	if err != nil {
		t.Fatalf("helper failed: %v: %s", err, stderr.String())
	}
	if err := identity.Verify(); err == nil {
		t.Fatal("exited server process identity still verified")
	}
}

func b03aReadLine(ctx context.Context, reader *bufio.Reader) (string, error) {
	type result struct {
		line string
		err  error
	}
	lines := make(chan result, 1)
	go func() {
		line, err := reader.ReadString('\n')
		lines <- result{line: strings.TrimSpace(line), err: err}
	}()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case got := <-lines:
		return got.line, got.err
	}
}

func b03aShortTempDir(t *testing.T) string {
	t.Helper()
	base := ""
	if runtime.GOOS != "windows" {
		base = "/tmp"
	}
	dir, err := os.MkdirTemp(base, "b03a-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func b03aParentServerIdentity(t *testing.T, ctx context.Context) *ProcessIdentity {
	t.Helper()
	dir := b03aShortTempDir(t)
	listener, err := Listen(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	type result struct {
		identity *ProcessIdentity
		err      error
	}
	results := make(chan result, 1)
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			results <- result{err: err}
			return
		}
		identity, err := IdentifyServer(connection)
		_ = connection.Close()
		results <- result{identity: identity, err: err}
	}()
	connection, err := Dial(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	select {
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	case got := <-results:
		if got.err != nil {
			t.Fatal(got.err)
		}
		return got.identity
	}
	return nil
}

func TestB03aRejectsUnexpectedProgramWithoutSending(t *testing.T) {
	other := b03aDifferentExecutable(t)
	dir, err := os.MkdirTemp("", "ipc-unexpected-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	listener, err := Listen(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	type observation struct {
		data []byte
		err  error
	}
	observed := make(chan observation, 1)
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			observed <- observation{err: err}
			return
		}
		defer connection.Close()
		connection.SetReadDeadline(time.Now().Add(5 * time.Second))
		data, err := io.ReadAll(connection)
		observed <- observation{data: data, err: err}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	wire, err := ConnectExecutable(ctx, dir, other)
	if err == nil {
		wire.Close()
		t.Fatal("accepted a server running a different executable")
	}
	if !errors.Is(err, ErrBrokerIdentity) {
		t.Fatalf("unexpected rejection: %v", err)
	}
	got := <-observed
	if got.err != nil {
		t.Fatal(got.err)
	}
	if len(got.data) != 0 {
		t.Fatal("sent application data to a server with the wrong code identity")
	}
}

func TestB03aRejectsInvalidExecutable(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing-service")
	if wire, err := ConnectExecutable(context.Background(), dir, missing); err == nil {
		wire.Close()
		t.Fatal("accepted an invalid executable path")
	}
}

func TestB03aIdentifyServerRejectsPlainPipe(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	if identity, err := IdentifyServer(a); err == nil || identity != nil {
		t.Fatal("accepted a connection without a kernel server identity")
	}
}

func TestB03aSendVerifiesBeforeWriting(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	denied := errors.New("synthetic server identity change")
	wire := &Wire{Conn: a, Verify: func() error { return denied }}
	if err := wire.Send(map[string]string{"action": "synthetic unlock material"}); !errors.Is(err, denied) {
		t.Fatal("send identity failure was lost", err)
	}
	assertPeerClosed(t, b)
}

func b03aDifferentExecutable(t *testing.T) string {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	self, err := ExecutableCodeID(executable)
	if err != nil {
		t.Fatal(err)
	}
	var candidates []string
	switch runtime.GOOS {
	case "darwin":
		candidates = []string{"/usr/bin/true", "/bin/true", "/usr/bin/false", filepath.Join(runtime.GOROOT(), "bin", "go")}
	case "linux":
		candidates = []string{"/bin/true", "/usr/bin/true"}
	case "windows":
		candidates = []string{filepath.Join(runtime.GOROOT(), "bin", "go.exe")}
	default:
		t.Skip("no second signed executable available on this platform")
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		code, err := ExecutableCodeID(candidate)
		if err == nil && code != self {
			return candidate
		}
	}
	t.Skip("no different trusted executable available")
	return ""
}
