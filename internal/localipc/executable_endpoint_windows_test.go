//go:build windows

package localipc

import (
	"context"
	"errors"
	"os"
	"testing"

	"golang.org/x/sys/windows"
)

func TestIsExecutableEndpointUnavailableWindows(t *testing.T) {
	missingFile := &os.PathError{Op: "open", Path: `\\.\pipe\ssh-client-test`, Err: windows.ERROR_FILE_NOT_FOUND}
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "file not found", err: missingFile, want: true},
		{name: "path not found", err: &os.PathError{Op: "open", Path: `\\.\pipe\ssh-client-test`, Err: windows.ERROR_PATH_NOT_FOUND}, want: true},
		{name: "pipe busy", err: &os.PathError{Op: "open", Path: `\\.\pipe\ssh-client-test`, Err: windows.ERROR_PIPE_BUSY}},
		{name: "context deadline", err: &os.PathError{Op: "open", Path: `\\.\pipe\ssh-client-test`, Err: context.DeadlineExceeded}},
		{name: "permission", err: &os.PathError{Op: "open", Path: `\\.\pipe\ssh-client-test`, Err: windows.ERROR_ACCESS_DENIED}},
		{name: "wrong operation", err: &os.PathError{Op: "read", Path: `\\.\pipe\ssh-client-test`, Err: windows.ERROR_FILE_NOT_FOUND}},
		{name: "identity error", err: errors.New("broker identity mismatch")},
		{name: "ordinary error", err: errors.New("dial failed")},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			wrapped := &executableDialError{err: test.err}
			if got := IsExecutableEndpointUnavailable(wrapped); got != test.want {
				t.Fatalf("IsExecutableEndpointUnavailable() = %v, want %v", got, test.want)
			}
			if test.want && IsExecutableEndpointUnavailable(test.err) {
				t.Fatal("unwrapped dial error classified as unavailable")
			}
		})
	}

	wrapped := &executableDialError{err: missingFile}
	var pathErr *os.PathError
	if !errors.As(wrapped, &pathErr) {
		t.Fatal("wrapped error no longer exposes *os.PathError")
	}
	if !errors.Is(wrapped, windows.ERROR_FILE_NOT_FOUND) {
		t.Fatal("wrapped error no longer matches ERROR_FILE_NOT_FOUND")
	}
}
