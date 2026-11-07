package localipc

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestExecutableDialErrorPreservesCause(t *testing.T) {
	cause := errors.New("dial failed")
	wrapped := &executableDialError{err: cause}
	if wrapped.Error() != cause.Error() {
		t.Fatalf("Error() = %q, want %q", wrapped.Error(), cause.Error())
	}
	if !errors.Is(wrapped, cause) {
		t.Fatal("wrapped error does not preserve its cause")
	}
}

func TestIsExecutableEndpointUnavailableRequiresWrapper(t *testing.T) {
	if IsExecutableEndpointUnavailable(nil) {
		t.Fatal("nil classified as an unavailable executable endpoint")
	}
	if IsExecutableEndpointUnavailable(errors.New("dial failed")) {
		t.Fatal("ordinary error classified as an unavailable executable endpoint")
	}
}

func TestConnectExecutableReportsUnavailableEndpoint(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable() error = %v", err)
	}

	tempRoot := ""
	if runtime.GOOS != "windows" {
		tempRoot = "/tmp"
	}
	dir, err := os.MkdirTemp(tempRoot, "b05e2a-")
	if err != nil {
		t.Fatalf("os.MkdirTemp() error = %v", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Errorf("os.RemoveAll(%q) error = %v", dir, err)
		}
	})
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatalf("os.Chmod(%q) error = %v", dir, err)
	}
	serviceDir := filepath.Join(dir, "service")
	if err := os.Mkdir(serviceDir, 0o700); err != nil {
		t.Fatalf("os.Mkdir(%q) error = %v", serviceDir, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	wire, err := ConnectExecutable(ctx, serviceDir, executable)
	if wire != nil {
		wire.Close()
		t.Fatalf("ConnectExecutable() = %#v, want nil wire", wire)
	}
	if err == nil {
		t.Fatal("ConnectExecutable() error = nil, want unavailable endpoint error")
	}
	if !IsExecutableEndpointUnavailable(err) {
		t.Fatalf("ConnectExecutable() error = %v, want unavailable executable endpoint", err)
	}
}
