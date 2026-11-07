//go:build !windows

package localipc

import (
	"context"
	"errors"
	"net"
	"os"
	"syscall"
	"testing"
)

func TestIsExecutableEndpointUnavailableUnix(t *testing.T) {
	missing := &net.OpError{
		Op:  "dial",
		Net: "unix",
		Err: &os.SyscallError{Syscall: "connect", Err: syscall.ENOENT},
	}
	refused := &net.OpError{
		Op:  "dial",
		Net: "unix",
		Err: &os.SyscallError{Syscall: "connect", Err: syscall.ECONNREFUSED},
	}
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "missing endpoint", err: missing, want: true},
		{name: "refused endpoint", err: refused, want: true},
		{name: "prepare permission", err: &os.PathError{Op: "open", Path: "/private/service", Err: os.ErrPermission}},
		{name: "prepare missing", err: &os.PathError{Op: "open", Path: "/private/service", Err: os.ErrNotExist}},
		{name: "wrong operation", err: &net.OpError{Op: "listen", Net: "unix", Err: syscall.ENOENT}},
		{name: "wrong network", err: &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}},
		{name: "context deadline", err: &net.OpError{Op: "dial", Net: "unix", Err: context.DeadlineExceeded}},
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

	wrapped := &executableDialError{err: missing}
	var operation *net.OpError
	if !errors.As(wrapped, &operation) {
		t.Fatal("wrapped error no longer exposes *net.OpError")
	}
	if !errors.Is(wrapped, os.ErrNotExist) {
		t.Fatal("wrapped error no longer matches os.ErrNotExist")
	}
}
