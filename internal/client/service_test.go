package client

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"sshclient/internal/localipc"
	"sshclient/internal/protocol"
	"sshclient/internal/service"
)

func TestServiceClientStatusUsesVerifiedExecutable(t *testing.T) {
	base := b03bClientTestDir(t)
	host, err := service.Open(base)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = host.Close() })
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	sdk := ServiceClient{Dir: base, Executable: executable}
	status, err := sdk.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if status.State != protocol.ServiceStateLocked || status.StartedAt.IsZero() || status.ProtocolMajor != protocol.ServiceProtocolMajor || status.ProtocolMinor != protocol.ServiceProtocolMinor {
		t.Fatalf("unexpected service status: %#v", status)
	}
	second, err := sdk.Status(ctx)
	if err != nil || !second.StartedAt.Equal(status.StartedAt) || second.State != status.State {
		t.Fatalf("each status call did not use an independent consistent connection: %#v %v", second, err)
	}
	if _, err := (ServiceClient{Dir: base, Executable: filepath.Join(base, "missing-server")}).Status(ctx); err == nil {
		t.Fatal("accepted a missing trusted executable")
	}
	cancelled, stop := context.WithCancel(context.Background())
	stop()
	if _, err := sdk.Status(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled status did not preserve its error: %v", err)
	}
}

func TestServiceClientRejectsDifferentExecutable(t *testing.T) {
	base := b03bClientTestDir(t)
	host, err := service.Open(base)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = host.Close() })
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	self, err := localipc.ExecutableCodeID(executable)
	if err != nil {
		t.Fatal(err)
	}
	var candidates []string
	switch runtime.GOOS {
	case "darwin":
		candidates = []string{"/usr/bin/true", "/bin/true", filepath.Join(runtime.GOROOT(), "bin", "go")}
	case "linux":
		candidates = []string{"/bin/true", "/usr/bin/true"}
	case "windows":
		candidates = []string{filepath.Join(runtime.GOROOT(), "bin", "go.exe")}
	default:
		t.Skip("no second signed executable available")
	}
	for _, candidate := range candidates {
		code, err := localipc.ExecutableCodeID(candidate)
		if err != nil || code == self {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := (ServiceClient{Dir: base, Executable: candidate}).Status(ctx); err == nil {
			t.Fatal("accepted a different executable as the service host")
		}
		return
	}
	t.Skip("no different trusted executable available")
}

func b03bClientTestDir(t *testing.T) string {
	t.Helper()
	base := ""
	if runtime.GOOS != "windows" {
		base = "/tmp"
	}
	dir, err := os.MkdirTemp(base, "b03b-client-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	return dir
}
