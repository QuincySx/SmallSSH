package service_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"sshclient/internal/approval"
	"sshclient/internal/broker"
	"sshclient/internal/client"
	"sshclient/internal/localipc"
	"sshclient/internal/protocol"
	"sshclient/internal/service"
	"sshclient/internal/testserver"
)

type b04c3cCopyResult struct {
	code int
	err  error
}

func waitStartedB04c3c(t *testing.T, ctx context.Context, server *testserver.Server, label string) {
	t.Helper()
	select {
	case <-server.Started:
	case <-ctx.Done():
		t.Fatalf("%s did not start: %v", label, ctx.Err())
	case <-time.After(5 * time.Second):
		t.Fatalf("%s did not start", label)
	}
}

func copyB04c3c(stream *client.Stream) <-chan b04c3cCopyResult {
	result := make(chan b04c3cCopyResult, 1)
	go func() {
		code, _, err := stream.Copy(io.Discard, io.Discard)
		result <- b04c3cCopyResult{code: code, err: err}
	}()
	return result
}

func waitCopyB04c3c(t *testing.T, result <-chan b04c3cCopyResult, label string) b04c3cCopyResult {
	t.Helper()
	select {
	case value := <-result:
		return value
	case <-time.After(5 * time.Second):
		t.Fatalf("%s did not finish", label)
		return b04c3cCopyResult{}
	}
}

func expectCopyRunningB04c3c(t *testing.T, result <-chan b04c3cCopyResult, label string) {
	t.Helper()
	select {
	case value := <-result:
		t.Fatalf("%s ended before revocation: code=%d err=%v", label, value.code, value.err)
	default:
	}
}

func expectQueryFailsB04c3c(t *testing.T, base context.Context, data client.Client, signer client.OwnerProof, label string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(base, 2*time.Second)
	defer cancel()
	if _, err := data.ListSnippets(ctx, signer); err == nil {
		t.Fatalf("%s query unexpectedly succeeded", label)
	}
}

func expectControlEndedB04c3c(t *testing.T, wire *localipc.Wire, label string) {
	t.Helper()
	if err := wire.Conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var message protocol.ServiceMessage
	if err := wire.Receive(&message); err == nil {
		t.Fatalf("%s received a response instead of a close: action=%q errorCode=%q", label, message.Action, message.ErrorCode)
	} else if !b04c2b1ConnectionEnded(err) {
		t.Fatalf("%s did not receive a close error: %v", label, err)
	}
}

func expectStatusClosedB04c3c(t *testing.T, hello b04c2b1Hello, label string) {
	t.Helper()
	if err := hello.wire.Conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	err := hello.wire.Send(protocol.ServiceMessage{
		Action: "status", Major: protocol.ServiceProtocolMajor, Minor: hello.minor,
		Epoch: hello.epoch, Generation: hello.generation,
	})
	if err != nil {
		if b04c2b1ConnectionEnded(err) {
			return
		}
		t.Fatalf("%s status send failed without a close: %v", label, err)
	}
	var response protocol.ServiceMessage
	if err := hello.wire.Receive(&response); err != nil {
		if b04c2b1ConnectionEnded(err) {
			return
		}
		t.Fatalf("%s status receive failed without a close: %v", label, err)
	}
	t.Fatalf("%s status succeeded after close: action=%q errorCode=%q", label, response.Action, response.ErrorCode)
}

func TestOwnerControlDisconnectOnlyRevokesItsStreams(t *testing.T) {
	fixture := newB04c2b1Fixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	server := testserver.NewWithStreamExec(t, func(ctx context.Context, command string, _ io.Reader, stdout, _ io.Writer) int {
		if command == "block" {
			<-ctx.Done()
			return 0
		}
		_, _ = io.WriteString(stdout, "fixture stdout: "+command)
		return 0
	})
	profile := server.Profile()
	if err := fixture.source.SaveProfile(broker.VaultPath(fixture.dir), profile); err != nil {
		t.Fatal(err)
	}

	preparedA := prepareB04c3c(t, ctx, fixture, "prepare-a")
	committedA := commitB04c3c(t, fixture, preparedA)
	preparedB := prepareB04c3c(t, ctx, fixture, "prepare-b")
	committedB := commitB04c3c(t, fixture, preparedB)
	core := service.CoreForTesting(fixture.host)
	if core == nil {
		t.Fatal("committed host has no core")
	}

	streamA, err := committedA.data.StartWithOwner(ctx, approval.Operation{
		Host: profile.ID, Kind: "exec", Command: "block",
	}, preparedA.signer)
	if err != nil {
		t.Fatal(err)
	}
	defer streamA.Close()
	waitStartedB04c3c(t, ctx, server, "owner A exec")
	streamB, err := committedB.data.StartWithOwner(ctx, approval.Operation{
		Host: profile.ID, Kind: "exec", Command: "block",
	}, preparedB.signer)
	if err != nil {
		t.Fatal(err)
	}
	defer streamB.Close()
	waitStartedB04c3c(t, ctx, server, "owner B exec")

	finishedA := copyB04c3c(streamA)
	finishedB := copyB04c3c(streamB)
	expectCopyRunningB04c3c(t, finishedA, "owner A stream")
	expectCopyRunningB04c3c(t, finishedB, "owner B stream")

	foreign := client.Client{Dir: fixture.dir, BrokerKey: committedA.data.BrokerKey}
	independentCommand := "printf independent-after-control-close"
	independent, ticket, err := foreign.Request(ctx, approval.Operation{
		Host: profile.ID, Kind: "exec", Command: independentCommand, Client: "synthetic independent",
	}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	root := client.Client{Dir: fixture.dir, BrokerKey: committedA.data.BrokerKey, Owner: fixture.source}
	if err = root.DecideReviewedWithOwner(ctx, independent, true, false, fixture.source); err != nil {
		t.Fatalf("root approval failed: %v", err)
	}
	approved, err := core.Approvals.Get(independent.ID)
	if err != nil || approved.Status != "approved" {
		t.Fatalf("independent approval status=%q err=%v", approved.Status, err)
	}

	_ = preparedA.hello.wire.Close()
	waitCopyB04c3c(t, finishedA, "owner A stream")
	expectQueryFailsB04c3c(t, ctx, committedA.data, preparedA.signer, "owner A")
	expectCopyRunningB04c3c(t, finishedB, "owner B stream")
	if _, err := committedB.data.ListSnippets(ctx, preparedB.signer); err != nil {
		t.Fatalf("owner B query failed after owner A disconnected: %v", err)
	}
	if status := fixture.host.Status(); status.State != protocol.ServiceStateUnlocked {
		t.Fatalf("host state after owner A disconnected=%q", status.State)
	}
	sdkStatus, err := (client.ServiceClient{Dir: fixture.dir, Executable: b03bExecutable(t)}).Status(ctx)
	if err != nil || sdkStatus.State != protocol.ServiceStateUnlocked {
		t.Fatalf("SDK state after owner A disconnected=%q err=%v", sdkStatus.State, err)
	}

	foreignStream, err := foreign.Start(ctx, independent.ID, ticket, 80, 24)
	if err != nil {
		t.Fatalf("independent request could not start: %v", err)
	}
	defer foreignStream.Close()
	var output bytes.Buffer
	code, _, err := foreignStream.Copy(&output, io.Discard)
	if err != nil || code != 0 || !strings.Contains(output.String(), independentCommand) {
		t.Fatalf("independent request code=%d output=%q err=%v", code, output.String(), err)
	}

	core.RevokeOwnerSession(committedB.info.ID)
	expectControlEndedB04c3c(t, preparedB.hello.wire, "owner B control")
	waitCopyB04c3c(t, finishedB, "owner B stream")
	expectQueryFailsB04c3c(t, ctx, committedB.data, preparedB.signer, "owner B")
	rootCtx, rootCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer rootCancel()
	if _, err := root.ListSnippets(rootCtx, fixture.source); err != nil {
		t.Fatalf("root query failed after owner B revocation: %v", err)
	}
}

func TestHostCloseEndsCommittedCoreAndRestartRejectsOldSigner(t *testing.T) {
	fixture := newB04c2b1Fixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	prepared := prepareB04c3c(t, ctx, fixture, "prepare-close")
	committed := commitB04c3c(t, fixture, prepared)
	if _, err := committed.data.ListSnippets(ctx, prepared.signer); err != nil {
		t.Fatalf("initial owner query failed: %v", err)
	}

	closeResult := make(chan error, 1)
	go func() { closeResult <- fixture.host.Close() }()
	select {
	case err := <-closeResult:
		if err != nil {
			t.Fatalf("host close failed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("host close did not return")
	}
	select {
	case <-fixture.host.Done():
	default:
		t.Fatal("host close returned before Done was closed")
	}
	expectStatusClosedB04c3c(t, prepared.hello, "closed host control")
	expectQueryFailsB04c3c(t, ctx, committed.data, prepared.signer, "old host")

	release, err := localipc.Acquire(fixture.dir)
	if err != nil {
		t.Fatalf("broker directory lock remained held: %v", err)
	}
	release()

	newHost, err := service.Open(fixture.dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = newHost.Close() })
	if status := newHost.Status(); status.State != protocol.ServiceStateLocked {
		t.Fatalf("restarted host state=%q, want locked", status.State)
	}
	restartedFixture := b04c2b1Fixture{dir: fixture.dir, host: newHost, source: fixture.source}
	restartedPrepared := prepareB04c3c(t, ctx, restartedFixture, "prepare-restart")
	if restartedPrepared.hello.epoch == prepared.hello.epoch {
		t.Fatal("restarted host reused the old service epoch")
	}
	restarted := commitB04c3c(t, restartedFixture, restartedPrepared)
	if restarted.data.BrokerKey != committed.data.BrokerKey {
		t.Fatal("restart did not use the same broker public key pin")
	}
	expectQueryFailsB04c3c(t, ctx, committed.data, prepared.signer, "old signer after restart")
	if _, err := restarted.data.ListSnippets(ctx, restartedPrepared.signer); err != nil {
		t.Fatalf("new owner query failed after rejecting the old signer: %v", err)
	}
}

func TestOwnerCommitRaceWithHostCloseLeavesNoCore(t *testing.T) {
	for index := range 3 {
		t.Run(fmt.Sprintf("race-%d", index), func(t *testing.T) {
			fixture := newB04c2b1Fixture(t)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			prepared := prepareB04c3c(t, ctx, fixture, "prepare-race")

			type commitResult struct {
				response protocol.ServiceMessage
				err      error
			}
			start := make(chan struct{})
			commitDone := make(chan commitResult, 1)
			closeDone := make(chan error, 1)
			go func() {
				<-start
				if err := prepared.hello.wire.Conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
					commitDone <- commitResult{err: err}
					return
				}
				message := b04c3cCommitMessage(prepared.hello, prepared.pendingID)
				if err := prepared.hello.wire.Send(message); err != nil {
					commitDone <- commitResult{err: err}
					return
				}
				var response protocol.ServiceMessage
				err := prepared.hello.wire.Receive(&response)
				commitDone <- commitResult{response: response, err: err}
			}()
			go func() {
				<-start
				closeDone <- fixture.host.Close()
			}()
			close(start)

			select {
			case err := <-closeDone:
				if err != nil {
					t.Fatalf("host close failed: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("host close did not return")
			}
			select {
			case <-commitDone:
			case <-time.After(5 * time.Second):
				t.Fatal("commit attempt did not converge")
			}
			select {
			case <-fixture.host.Done():
			default:
				t.Fatal("host close returned before Done was closed")
			}
			if got := service.PendingOwnersForTesting(fixture.host); got != 0 {
				t.Fatalf("pending owners after host close=%d, want 0", got)
			}
			dialCtx, dialCancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer dialCancel()
			connection, err := localipc.Dial(dialCtx, fixture.dir)
			if err == nil {
				_ = connection.Close()
				t.Fatal("closed host left a broker listener")
			}
			release, err := localipc.Acquire(fixture.dir)
			if err != nil {
				t.Fatalf("closed host left the broker directory locked: %v", err)
			}
			release()
		})
	}
}
