package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"sshclient/internal/client"
	"sshclient/internal/localipc"
	"sshclient/internal/protocol"
	"sshclient/internal/service"
)

func TestHostStartsLockedWithoutVaultAndCloses(t *testing.T) {
	base := b03bTestDir(t)
	host, err := service.Open(base)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = host.Close() })
	status := host.Status()
	if status.State != protocol.ServiceStateLocked || status.StartedAt.IsZero() || status.ProtocolMajor != protocol.ServiceProtocolMajor || status.ProtocolMinor != protocol.ServiceProtocolMinor {
		t.Fatalf("unexpected locked status: %#v", status)
	}
	if _, err := os.Stat(filepath.Join(base, "vault.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("opening the service touched the vault: %v", err)
	}
	encoded, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"connections", "hostAddress", "notes", "command", "credential"} {
		if strings.Contains(string(encoded), private) {
			t.Fatalf("locked status exposed private data %q: %s", private, encoded)
		}
	}
	select {
	case <-host.Done():
		t.Fatal("host stopped before Close")
	default:
	}
	if err := host.Close(); err != nil {
		t.Fatal(err)
	}
	if err := host.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-host.Done():
	case <-time.After(time.Second):
		t.Fatal("Close did not converge")
	}
}

func TestHostSingleInstanceSDKAndEpochRotation(t *testing.T) {
	base := b03bTestDir(t)
	executable := b03bExecutable(t)
	first, err := service.Open(base)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Close() })
	sdk := client.ServiceClient{Dir: base, Executable: executable}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if status, err := sdk.Status(ctx); err != nil || status.State != protocol.ServiceStateLocked {
		t.Fatalf("SDK could not query first host: %#v %v", status, err)
	}
	epochBefore := b03bHelloEpoch(t, base)
	second, err := service.Open(base)
	if err == nil {
		_ = second.Close()
		t.Fatal("second host acquired the same service lock")
	}
	if status, err := sdk.Status(ctx); err != nil || status.State != protocol.ServiceStateLocked {
		t.Fatalf("second host failure disturbed the first host: %#v %v", status, err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	third, err := service.Open(base)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = third.Close() })
	epochAfter := b03bHelloEpoch(t, base)
	if epochBefore == "" || epochBefore == epochAfter {
		t.Fatalf("service epoch did not rotate: %q then %q", epochBefore, epochAfter)
	}
	if status, err := sdk.Status(ctx); err != nil || status.State != protocol.ServiceStateLocked {
		t.Fatalf("reopened host is not locked: %#v %v", status, err)
	}
}

func TestServiceProtocolBoundaries(t *testing.T) {
	base := b03bTestDir(t)
	host, err := service.Open(base)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = host.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	majorWire := b03bConnect(t, ctx, base)
	if err := majorWire.Send(protocol.ServiceMessage{Action: "hello", Major: protocol.ServiceProtocolMajor + 1}); err != nil {
		t.Fatal(err)
	}
	var majorResponse protocol.ServiceMessage
	if err := majorWire.Receive(&majorResponse); err != nil {
		t.Fatal(err)
	}
	if majorResponse.Action != "hello" || majorResponse.ErrorCode != "unsupported_version" {
		t.Fatalf("unexpected major mismatch response: %#v", majorResponse)
	}
	if err := majorWire.Conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	var extra protocol.ServiceMessage
	if err := majorWire.Receive(&extra); err == nil {
		t.Fatal("host read an operation after rejecting the major version")
	}
	_ = majorWire.Close()

	noHello := b03bConnect(t, ctx, base)
	if err := noHello.Send(protocol.ServiceMessage{Action: "status", Major: protocol.ServiceProtocolMajor}); err != nil {
		t.Fatal(err)
	}
	var noHelloResponse protocol.ServiceMessage
	if err := noHello.Receive(&noHelloResponse); err != nil {
		t.Fatal(err)
	}
	if noHelloResponse.ErrorCode != "hello_required" {
		t.Fatalf("status without hello was not rejected: %#v", noHelloResponse)
	}
	_ = noHello.Close()

	actionWire := b03bConnect(t, ctx, base)
	if err := actionWire.Send(map[string]any{"action": "hello", "major": protocol.ServiceProtocolMajor, "minor": protocol.ServiceProtocolMinor, "role": "owner"}); err != nil {
		t.Fatal(err)
	}
	var hello protocol.ServiceMessage
	if err := actionWire.Receive(&hello); err != nil {
		t.Fatal(err)
	}
	if err := actionWire.Send(map[string]any{"action": "unlock", "major": protocol.ServiceProtocolMajor, "minor": protocol.ServiceProtocolMinor, "epoch": hello.Epoch, "role": "owner", "password": "synthetic-not-used"}); err != nil {
		t.Fatal(err)
	}
	var actionResponse protocol.ServiceMessage
	if err := actionWire.Receive(&actionResponse); err != nil {
		t.Fatal(err)
	}
	if actionResponse.ErrorCode != "unsupported_action" {
		t.Fatalf("unlock was not rejected while locked: %#v", actionResponse)
	}
	_ = actionWire.Close()

	sdk := client.ServiceClient{Dir: base, Executable: b03bExecutable(t)}
	if status, err := sdk.Status(ctx); err != nil || status.State != protocol.ServiceStateLocked {
		t.Fatalf("self-reported role changed host state: %#v %v", status, err)
	}

	minorWire := b03bConnect(t, ctx, base)
	if err := minorWire.Send(protocol.ServiceMessage{Action: "hello", Major: protocol.ServiceProtocolMajor, Minor: protocol.ServiceProtocolMinor + 5}); err != nil {
		t.Fatal(err)
	}
	var minorHello protocol.ServiceMessage
	if err := minorWire.Receive(&minorHello); err != nil {
		t.Fatal(err)
	}
	if minorHello.Minor != protocol.ServiceProtocolMinor {
		t.Fatalf("hello did not report the host minor version: %#v", minorHello)
	}
	if err := minorWire.Send(protocol.ServiceMessage{Action: "status", Major: protocol.ServiceProtocolMajor, Minor: minorHello.Minor, Epoch: minorHello.Epoch}); err != nil {
		t.Fatal(err)
	}
	var statusResponse protocol.ServiceMessage
	if err := minorWire.Receive(&statusResponse); err != nil {
		t.Fatal(err)
	}
	if statusResponse.Status == nil || statusResponse.Status.ProtocolMinor != protocol.ServiceProtocolMinor || statusResponse.Status.State != protocol.ServiceStateLocked {
		t.Fatalf("minor negotiation changed the locked status: %#v", statusResponse)
	}
	_ = minorWire.Close()
}

func TestCloseEndsIdleConnectionsPromptly(t *testing.T) {
	base := b03bTestDir(t)
	host, err := service.Open(base)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = host.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	incomplete, err := localipc.Dial(ctx, filepath.Join(base, "service"))
	if err != nil {
		t.Fatal(err)
	}
	defer incomplete.Close()
	idle, err := localipc.ConnectExecutable(ctx, filepath.Join(base, "service"), b03bExecutable(t))
	if err != nil {
		t.Fatal(err)
	}
	defer idle.Close()
	closed := make(chan error, 1)
	go func() { closed <- host.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close waited for an idle or incomplete connection")
	}
	select {
	case <-host.Done():
	case <-time.After(time.Second):
		t.Fatal("host did not report completion")
	}
}

func TestServiceConnectionLimit(t *testing.T) {
	base := b03bTestDir(t)
	host, err := service.Open(base)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = host.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	executable := b03bExecutable(t)
	wires := make([]*localipc.Wire, 0, 16)
	defer func() {
		for _, wire := range wires {
			_ = wire.Close()
		}
	}()
	for range 16 {
		wire, err := localipc.ConnectExecutable(ctx, filepath.Join(base, "service"), executable)
		if err != nil {
			t.Fatal(err)
		}
		wires = append(wires, wire)
	}
	short, stop := context.WithTimeout(context.Background(), 3*time.Second)
	defer stop()
	extra, err := localipc.ConnectExecutable(short, filepath.Join(base, "service"), executable)
	if err == nil {
		_ = extra.Close()
		t.Fatal("host accepted more than sixteen active connections")
	}
}

func b03bTestDir(t *testing.T) string {
	t.Helper()
	base := ""
	if runtime.GOOS != "windows" {
		base = "/tmp"
	}
	dir, err := os.MkdirTemp(base, "b03b-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func b03bExecutable(t *testing.T) string {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return executable
}

func b03bConnect(t *testing.T, ctx context.Context, base string) *localipc.Wire {
	t.Helper()
	wire, err := localipc.ConnectExecutable(ctx, filepath.Join(base, "service"), b03bExecutable(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = wire.Close() })
	if err := wire.Conn.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	return wire
}

func b03bHelloEpoch(t *testing.T, base string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	wire := b03bConnect(t, ctx, base)
	if err := wire.Send(protocol.ServiceMessage{Action: "hello", Major: protocol.ServiceProtocolMajor, Minor: protocol.ServiceProtocolMinor}); err != nil {
		t.Fatal(err)
	}
	var hello protocol.ServiceMessage
	if err := wire.Receive(&hello); err != nil {
		t.Fatal(err)
	}
	if hello.Action != "hello" || hello.ErrorCode != "" || len(hello.Epoch) != 32 {
		t.Fatalf("unexpected hello response: %#v", hello)
	}
	return hello.Epoch
}
