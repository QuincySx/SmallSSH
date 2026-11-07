package broker_test

import (
	"bytes"
	"context"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"sshclient/internal/approval"
	"sshclient/internal/broker"
	"sshclient/internal/client"
	"sshclient/internal/localipc"
	"sshclient/internal/ownercap"
	"sshclient/internal/testserver"
)

func newAdditionalManagedOwner(t *testing.T, server *broker.Server, dir string) (client.Client, *ownercap.Signer, *broker.OwnerLease, context.CancelFunc) {
	t.Helper()
	controlDir := managedSessionsTempDir(t, "b04b2-control-member-")
	accepted := make(chan struct {
		conn net.Conn
		err  error
	}, 1)
	var release func()
	var listener net.Listener
	var caller, serverConn net.Conn
	var identity *localipc.ProcessIdentity
	var cancel context.CancelFunc
	var signer *ownercap.Signer
	t.Cleanup(func() {
		if cancel != nil {
			cancel()
		}
		if serverConn != nil {
			_ = serverConn.Close()
		}
		if caller != nil {
			_ = caller.Close()
		}
		if identity != nil {
			identity.Close()
		}
		if listener != nil {
			_ = listener.Close()
		}
		if release != nil {
			release()
		}
		if signer != nil {
			signer.Close()
		}
	})

	var err error
	release, err = localipc.Acquire(controlDir)
	if err != nil {
		t.Fatal(err)
	}
	listener, err = localipc.Listen(controlDir)
	if err != nil {
		t.Fatal(err)
	}
	dialCtx, dialCancel := context.WithTimeout(context.Background(), 2*time.Second)
	caller, err = localipc.Dial(dialCtx, controlDir)
	dialCancel()
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		conn, err := listener.Accept()
		accepted <- struct {
			conn net.Conn
			err  error
		}{conn: conn, err: err}
	}()
	select {
	case result := <-accepted:
		if result.err != nil {
			t.Fatal(result.err)
		}
		serverConn = result.conn
	case <-time.After(2 * time.Second):
		t.Fatal("timed out accepting the additional local control connection")
	}
	identity, err = localipc.Identify(serverConn)
	if err != nil {
		t.Fatal(err)
	}
	if identity == nil || identity.Verify() != nil {
		t.Fatal("additional local control identity is unavailable")
	}

	ctrlCtx, ctrlCancel := context.WithCancel(context.Background())
	cancel = ctrlCancel
	signer, err = ownercap.NewSigner()
	if err != nil {
		t.Fatal(err)
	}
	lease, err := server.RegisterOwner(ctrlCtx, identity, signer.PublicKey(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if err = signer.Bind(lease.Info()); err != nil {
		t.Fatal(err)
	}
	return client.Client{
		Dir:       dir,
		BrokerKey: server.PublicKeyPin(),
		Owner:     signer,
	}, signer, lease, ctrlCancel
}

func TestManagedOwnerRevocationIsIsolated(t *testing.T) {
	fixture := newManagedSessionsFixtureWithServer(t, testserver.NewWithStreamExec(t, func(sessionCtx context.Context, command string, _ io.Reader, stdout, _ io.Writer) int {
		if command == "block" {
			<-sessionCtx.Done()
			return 0
		}
		_, _ = io.WriteString(stdout, "fixture stdout: "+command)
		return 0
	}))
	additional, additionalSigner, additionalLease, cancelAdditional := newAdditionalManagedOwner(t, fixture.server, fixture.dir)
	defer cancelAdditional()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	waitStarted := func(label string) {
		t.Helper()
		select {
		case <-fixture.ssh.Started:
		case <-ctx.Done():
			t.Fatalf("%s exec did not start: %v", label, ctx.Err())
		case <-time.After(2 * time.Second):
			t.Fatalf("%s exec did not start", label)
		}
	}

	streamA, err := fixture.cli.StartWithOwner(ctx, approval.Operation{
		Host:    fixture.profile.ID,
		Kind:    "exec",
		Command: "block",
		Client:  "synthetic managed owner A",
	}, fixture.signer)
	if err != nil {
		t.Fatal(err)
	}
	defer streamA.Close()
	waitStarted("managed owner A")

	streamB, err := additional.StartWithOwner(ctx, approval.Operation{
		Host:    fixture.profile.ID,
		Kind:    "exec",
		Command: "block",
		Client:  "synthetic managed owner B",
	}, additionalSigner)
	if err != nil {
		t.Fatal(err)
	}
	defer streamB.Close()
	waitStarted("managed owner B")

	type copyResult struct {
		code int
		err  error
	}
	finishedA := make(chan copyResult, 1)
	finishedB := make(chan copyResult, 1)
	go func() {
		code, _, err := streamA.Copy(io.Discard, io.Discard)
		finishedA <- copyResult{code: code, err: err}
	}()
	go func() {
		code, _, err := streamB.Copy(io.Discard, io.Discard)
		finishedB <- copyResult{code: code, err: err}
	}()
	select {
	case result := <-finishedA:
		t.Fatalf("managed owner A ended before revocation: %#v", result)
	default:
	}
	select {
	case result := <-finishedB:
		t.Fatalf("managed owner B ended before revocation: %#v", result)
	default:
	}

	foreign := client.Client{Dir: fixture.dir, BrokerKey: fixture.server.PublicKeyPin()}
	foreignRequest, foreignTicket, err := foreign.Request(ctx, approval.Operation{
		Host:    fixture.profile.ID,
		Kind:    "exec",
		Command: "printf independent-after-owner-revoke",
		Client:  "synthetic independent AI",
	}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	root := client.Client{Dir: fixture.dir, BrokerKey: fixture.server.PublicKeyPin(), Owner: fixture.server.Vault}
	if err = root.DecideReviewedWithOwner(ctx, foreignRequest, true, false, fixture.server.Vault); err != nil {
		t.Fatalf("root proof could not approve the independent request: %v", err)
	}
	approved, err := fixture.server.Approvals.Get(foreignRequest.ID)
	if err != nil || approved.Status != "approved" {
		t.Fatalf("independent request = %#v, %v", approved, err)
	}

	fixture.cancelOwner()
	select {
	case <-fixture.lease.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("managed owner A lease did not finish after control cancellation")
	}
	select {
	case <-finishedA:
	case <-time.After(2 * time.Second):
		t.Fatal("managed owner A stream did not finish after control cancellation")
	}

	queryCtx, queryCancel := context.WithTimeout(ctx, 2*time.Second)
	_, err = fixture.cli.ListSnippets(queryCtx, fixture.signer)
	queryCancel()
	if err == nil {
		t.Fatal("managed owner A signer still read the workspace after revocation")
	}

	select {
	case <-additionalLease.Done():
		t.Fatal("managed owner B lease ended with owner A")
	default:
	}
	select {
	case result := <-finishedB:
		t.Fatalf("managed owner B stream ended with owner A: %#v", result)
	default:
	}
	storedB, err := fixture.server.Approvals.Get(streamB.RequestID)
	if err != nil || storedB.Status != "running" {
		t.Fatalf("managed owner B request = %#v, %v", storedB, err)
	}
	queryCtx, queryCancel = context.WithTimeout(ctx, 2*time.Second)
	_, err = additional.ListSnippets(queryCtx, additionalSigner)
	queryCancel()
	if err != nil {
		t.Fatalf("managed owner B could not read after owner A revocation: %v", err)
	}

	foreignStream, err := foreign.Start(ctx, foreignRequest.ID, foreignTicket, 80, 24)
	if err != nil {
		t.Fatalf("independent request could not start after owner A revocation: %v", err)
	}
	defer foreignStream.Close()
	var stdout, stderr bytes.Buffer
	code, files, err := foreignStream.Copy(&stdout, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 || len(files) != 0 || !strings.Contains(stdout.String(), "independent-after-owner-revoke") {
		t.Fatalf("independent exec result: code=%d files=%d stdout=%q stderr=%q", code, len(files), stdout.String(), stderr.String())
	}

	fixture.server.RevokeOwnerSession(additionalLease.Info().ID)
	select {
	case <-additionalLease.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("managed owner B lease did not finish after revocation")
	}
	select {
	case <-finishedB:
	case <-time.After(2 * time.Second):
		t.Fatal("managed owner B stream did not finish after revocation")
	}
}

func TestManagedOwnerCoreCloseEndsLease(t *testing.T) {
	fixture := newManagedSessionsFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := fixture.cli.ListSnippets(ctx, fixture.signer); err != nil {
		t.Fatalf("managed query failed before core close: %v", err)
	}
	fixture.server.Close()
	select {
	case <-fixture.lease.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("managed lease did not finish after core close")
	}
	queryCtx, queryCancel := context.WithTimeout(ctx, 2*time.Second)
	_, err := fixture.cli.ListSnippets(queryCtx, fixture.signer)
	queryCancel()
	if err == nil {
		t.Fatal("managed signer still read the workspace after core close")
	}
}
