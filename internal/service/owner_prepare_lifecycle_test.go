package service_test

import (
	"context"
	"testing"
	"time"

	"sshclient/internal/protocol"
	"sshclient/internal/service"
	"sshclient/internal/vault"
)

type b04c2b2Prepared struct {
	hello    b04c2b1Hello
	message  protocol.ServiceMessage
	response protocol.ServiceMessage
}

func prepareB04c2b2(t *testing.T, dir string, source *vault.Session, label string) b04c2b2Prepared {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	hello := helloB04c2b1(t, ctx, dir)
	message := newB04c2b1PrepareMessage(t, source, hello)
	t.Cleanup(func() { clearB04c2b1Message(&message) })
	response := exchangeB04c2b1(t, label, hello.wire, message)
	if response.Action != "ownerPrepared" || response.ErrorCode != "" || response.ID == "" {
		t.Fatalf("%s: action=%q errorCode=%q idPresent=%t", label, response.Action, response.ErrorCode, response.ID != "")
	}
	return b04c2b2Prepared{hello: hello, message: message, response: response}
}

func waitB04c2b2Pending(t *testing.T, host *service.Host, want int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if got := service.PendingOwnersForTesting(host); got == want {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("pending owners=%d, want %d", service.PendingOwnersForTesting(host), want)
		case <-ticker.C:
		}
	}
}

func expectB04c2b2Closed(t *testing.T, label string, hello b04c2b1Hello) {
	t.Helper()
	if err := hello.wire.Conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		if b04c2b1ConnectionEnded(err) {
			return
		}
		t.Fatalf("%s deadline: %v", label, err)
	}
	if err := hello.wire.Send(protocol.ServiceMessage{
		Action: "status", Major: protocol.ServiceProtocolMajor, Minor: hello.minor,
		Epoch: hello.epoch, Generation: hello.generation,
	}); err != nil {
		if b04c2b1ConnectionEnded(err) {
			return
		}
		t.Fatalf("%s send: %v", label, err)
	}
	var response protocol.ServiceMessage
	if err := hello.wire.Receive(&response); err != nil {
		if b04c2b1ConnectionEnded(err) {
			return
		}
		t.Fatalf("%s receive: %v", label, err)
	}
	t.Fatalf("%s status succeeded after close: action=%q errorCode=%q", label, response.Action, response.ErrorCode)
}

func TestOwnerPrepareCapacityReleasedOnDisconnect(t *testing.T) {
	fixture := newB04c2b1Fixture(t)
	first := prepareB04c2b2(t, fixture.dir, fixture.source, "prepare-a")
	second := prepareB04c2b2(t, fixture.dir, fixture.source, "prepare-b")
	waitB04c2b2Pending(t, fixture.host, 2)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	thirdHello := helloB04c2b1(t, ctx, fixture.dir)
	thirdMessage := newB04c2b1PrepareMessage(t, fixture.source, thirdHello)
	defer clearB04c2b1Message(&thirdMessage)
	thirdResponse := exchangeB04c2b1(t, "capacity", thirdHello.wire, thirdMessage)
	if thirdResponse.Action != "error" || thirdResponse.ErrorCode != "invalid_owner" {
		t.Fatalf("capacity: action=%q errorCode=%q", thirdResponse.Action, thirdResponse.ErrorCode)
	}
	waitB04c2b2Pending(t, fixture.host, 2)
	if status := fixture.host.Status(); status.State != protocol.ServiceStateLocked {
		t.Fatalf("capacity changed host state: %q", status.State)
	}

	_ = first.hello.wire.Close()
	waitB04c2b2Pending(t, fixture.host, 1)
	fourth := prepareB04c2b2(t, fixture.dir, fixture.source, "prepare-d")
	waitB04c2b2Pending(t, fixture.host, 2)

	_ = second.hello.wire.Close()
	_ = fourth.hello.wire.Close()
	waitB04c2b2Pending(t, fixture.host, 0)
	if status := fixture.host.Status(); status.State != protocol.ServiceStateLocked {
		t.Fatalf("disconnect changed host state: %q", status.State)
	}
}

func TestOwnerPrepareDeadlineDoesNotSlideWithStatus(t *testing.T) {
	fixture := newB04c2b1Fixture(t)
	prepared := prepareB04c2b2(t, fixture.dir, fixture.source, "deadline-prepare")
	waitB04c2b2Pending(t, fixture.host, 1)
	start := time.Now()

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var closedAt time.Time

waitForClose:
	for {
		select {
		case <-ctx.Done():
			t.Fatal("pending connection did not close within 40 seconds")
		case <-ticker.C:
			if err := prepared.hello.wire.Conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
				if !b04c2b1ConnectionEnded(err) {
					t.Fatalf("status deadline: %v", err)
				}
				closedAt = time.Now()
				break waitForClose
			}
			message := protocol.ServiceMessage{
				Action: "status", Major: protocol.ServiceProtocolMajor, Minor: prepared.hello.minor,
				Epoch: prepared.hello.epoch, Generation: prepared.hello.generation,
			}
			if err := prepared.hello.wire.Send(message); err != nil {
				if !b04c2b1ConnectionEnded(err) {
					t.Fatalf("status send: %v", err)
				}
				closedAt = time.Now()
				break waitForClose
			}
			var response protocol.ServiceMessage
			if err := prepared.hello.wire.Receive(&response); err != nil {
				if !b04c2b1ConnectionEnded(err) {
					t.Fatalf("status receive: %v", err)
				}
				closedAt = time.Now()
				break waitForClose
			}
			if response.Action != "status" || response.ErrorCode != "" || response.Status == nil ||
				response.Status.State != protocol.ServiceStateLocked {
				t.Fatalf("status: action=%q errorCode=%q", response.Action, response.ErrorCode)
			}
		}
	}

	elapsed := closedAt.Sub(start)
	if elapsed < 25*time.Second {
		t.Fatalf("pending closed too early: %s", elapsed)
	}
	if elapsed >= 40*time.Second {
		t.Fatalf("pending close used the full context: %s", elapsed)
	}
	waitB04c2b2Pending(t, fixture.host, 0)
	if status := fixture.host.Status(); status.State != protocol.ServiceStateLocked {
		t.Fatalf("pending expiry changed host state: %q", status.State)
	}

	fresh := prepareB04c2b2(t, fixture.dir, fixture.source, "prepare-after-expiry")
	waitB04c2b2Pending(t, fixture.host, 1)
	_ = fresh.hello.wire.Close()
	waitB04c2b2Pending(t, fixture.host, 0)
}

func TestOwnerPrepareHostCloseReleasesPendingAndRestartIsLocked(t *testing.T) {
	fixture := newB04c2b1Fixture(t)
	first := prepareB04c2b2(t, fixture.dir, fixture.source, "close-prepare-a")
	second := prepareB04c2b2(t, fixture.dir, fixture.source, "close-prepare-b")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	idle := helloB04c2b1(t, ctx, fixture.dir)
	waitB04c2b2Pending(t, fixture.host, 2)

	closed := make(chan error, 1)
	go func() { closed <- fixture.host.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Host.Close did not return within 5 seconds")
	}
	select {
	case <-fixture.host.Done():
	default:
		t.Fatal("Host.Close returned before Done closed")
	}
	waitB04c2b2Pending(t, fixture.host, 0)
	expectB04c2b2Closed(t, "closed-a-status", first.hello)
	expectB04c2b2Closed(t, "closed-b-status", second.hello)
	expectB04c2b2Closed(t, "closed-idle-status", idle)

	reopened, err := service.Open(fixture.dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if status := reopened.Status(); status.State != protocol.ServiceStateLocked {
		t.Fatalf("reopened host state: %q", status.State)
	}
	restartCtx, restartCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer restartCancel()
	restartedHello := helloB04c2b1(t, restartCtx, fixture.dir)
	if restartedHello.epoch == first.hello.epoch {
		t.Fatal("restarted host reused its old epoch")
	}
	stale := exchangeB04c2b1(t, "stale-message", restartedHello.wire, first.message)
	if stale.Action != "error" || stale.ErrorCode != "invalid_owner" {
		t.Fatalf("stale-message: action=%q errorCode=%q", stale.Action, stale.ErrorCode)
	}

	fresh := prepareB04c2b2(t, fixture.dir, fixture.source, "restart-prepare")
	if status := reopened.Status(); status.State != protocol.ServiceStateLocked {
		t.Fatalf("restart prepare changed host state: %q", status.State)
	}
	waitB04c2b2Pending(t, reopened, 1)
	_ = fresh.hello.wire.Close()
	waitB04c2b2Pending(t, reopened, 0)
}
