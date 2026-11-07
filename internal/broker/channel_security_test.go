package broker_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sshclient/internal/approval"
	"sshclient/internal/broker"
	"sshclient/internal/client"
	"sshclient/internal/localipc"
)

func wireCall(t *testing.T, w *localipc.Wire, q broker.Message) broker.Message {
	t.Helper()
	w.Conn.SetDeadline(time.Now().Add(5 * time.Second))
	if err := w.Send(q); err != nil {
		t.Fatal(err)
	}
	var r broker.Message
	if err := w.Receive(&r); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestCopiedKeysTicketsAndSignaturesCannotMoveToANewConnection(t *testing.T) {
	h := newRenewalHarness(t)
	connect := func() *localipc.Wire {
		w, err := localipc.ConnectVerified(h.ctx, h.dir, h.s.PublicKeyPin())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { w.Close() })
		return w
	}
	control := connect()
	public, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	r := wireCall(t, control, broker.Message{Action: "request", TTL: 300, Operation: approval.Operation{Host: "fixture", Client: "same program and label", Kind: "grant", Grant: approval.GrantScope{PublicKey: base64.RawURLEncoding.EncodeToString(public), Permission: "full", LifetimeSeconds: 3600, IdleSeconds: 60}}})
	if r.Error != "" || r.Request == nil {
		t.Fatal("session request failed", r.Error)
	}
	parent, ticket := *r.Request, r.Ticket
	if err := h.c.DecideReviewedWithOwner(h.ctx, parent, true, false, h.s.Vault); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"sessionStatus", "renewalChallenge", "withdraw"} {
		if result := wireCall(t, connect(), broker.Message{Action: action, ID: parent.ID, Ticket: ticket}); result.Error == "" {
			t.Fatal("copied ticket acquired authority on a new connection", action)
		}
	}
	operation := connect()
	opened := wireCall(t, operation, broker.Message{Action: "openOperation"})
	if opened.Error != "" || opened.Channel == "" {
		t.Fatal("operation channel unavailable", opened.Error)
	}
	op := approval.Operation{Host: "fixture", Client: "same program and label", Kind: "exec", Command: "bound-channel-command"}
	challenge := wireCall(t, control, broker.Message{Action: "delegationChallenge", ID: parent.ID, Ticket: ticket, Channel: opened.Channel, Operation: op, TTL: 60})
	if challenge.Delegation == nil {
		t.Fatal("challenge missing", challenge.Error)
	}
	// A valid signature alone is insufficient on any fresh control connection.
	proof := ed25519.Sign(key, approval.DelegationProofMessage(*challenge.Delegation))
	if result := wireCall(t, connect(), broker.Message{Action: "delegate", ID: challenge.Delegation.ID, Proof: proof}); result.Error == "" {
		t.Fatal("copied private key/signature bypassed connection ownership")
	}
	child := wireCall(t, control, broker.Message{Action: "delegate", ID: challenge.Delegation.ID, Proof: proof})
	if child.Request == nil || child.Error != "" {
		t.Fatal("original connection lost authority", child.Error)
	}
	if result := wireCall(t, connect(), broker.Message{Action: "run", ID: child.Request.ID, Ticket: child.Ticket}); result.Error == "" || h.commands.Load() != 0 {
		t.Fatal("stolen child ticket executed")
	}
	ready := wireCall(t, operation, broker.Message{Action: "run", ID: child.Request.ID, Ticket: child.Ticket})
	if ready.Action != "ready" {
		t.Fatal("bound operation did not start", ready.Error)
	}
	for {
		var result broker.Message
		if err := operation.Receive(&result); err != nil {
			t.Fatal(err)
		}
		if result.Error != "" {
			t.Fatal(result.Error)
		}
		if result.Action == "done" {
			break
		}
	}
	operation.Close()
	if h.commands.Load() != 1 {
		t.Fatal("legitimate command did not run exactly once")
	}
	// Capture a valid renewal signature, then disconnect its original channel.
	h.offset.Store(int64(61 * time.Second))
	h.s.Approvals.Sweep()
	renewal := wireCall(t, control, broker.Message{Action: "renewalChallenge", ID: parent.ID, Ticket: ticket})
	if renewal.Renewal == nil {
		t.Fatal("renewal challenge missing", renewal.Error)
	}
	renewalProof := ed25519.Sign(key, approval.RenewalProofMessage(*renewal.Renewal))
	control.Close()
	waitRevoked(t, h.c, h.ctx, parent.ID)
	if result := wireCall(t, connect(), broker.Message{Action: "renewSession", ID: renewal.Renewal.ID, Proof: renewalProof}); result.Error == "" {
		t.Fatal("disconnected session was resurrected with a saved key")
	}
	page, err := h.c.Call(h.ctx, broker.Message{Action: "pending"})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range page.Requests {
		if r.Renewal && r.Status == "pending" {
			t.Fatal("disconnected session queued renewal")
		}
	}
}

func waitRevoked(t *testing.T, c client.Client, ctx context.Context, id string) {
	t.Helper()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		r, err := c.Call(ctx, broker.Message{Action: "sessionStatus", ID: id})
		if err == nil && r.Request != nil && r.Request.Status == "revoked" {
			return
		}
		select {
		case <-tick.C:
		case <-ctx.Done():
			t.Fatal("disconnected session kept authority", ctx.Err())
		}
	}
}

// The helper runs this exact test executable in a distinct OS process. It gets
// only synthetic fixture metadata and a public broker pin through its environment.
func TestIPCIdentitySubprocess(t *testing.T) {
	mode := os.Getenv("SMALLSSH_IPC_TEST_MODE")
	if mode == "" {
		return
	}
	os.Exit(runIPCIdentityChild(mode))
}

func runIPCIdentityChild(mode string) int {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	dir, pin := os.Getenv("SMALLSSH_IPC_TEST_DIR"), os.Getenv("SMALLSSH_IPC_TEST_PIN")
	encoder := json.NewEncoder(os.Stdout)
	if mode == "no-pin" {
		w, err := localipc.ConnectVerified(ctx, dir, "")
		if err == nil {
			w.Close()
			_ = encoder.Encode(broker.Message{Action: "paired"})
			return 0
		}
		_ = encoder.Encode(broker.Message{Error: err.Error()})
		return 0
	}
	if mode == "grant" {
		c := client.Client{Dir: dir, BrokerKey: pin}
		r, d, err := c.RequestSession(ctx, "fixture", "same executable", "full", time.Hour, time.Minute)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		defer d.Close()
		if encoder.Encode(broker.Message{Request: &r, Ticket: d.Ticket}) != nil {
			return 1
		}
		_, _ = io.Copy(io.Discard, os.Stdin)
		return 0
	}
	w, err := localipc.ConnectVerified(ctx, dir, pin)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer w.Close()
	if w.Send(broker.Message{Action: "openOperation"}) != nil {
		return 1
	}
	var result broker.Message
	if w.Receive(&result) != nil || encoder.Encode(result) != nil {
		return 1
	}
	decoder := json.NewDecoder(os.Stdin)
	for {
		var q broker.Message
		if decoder.Decode(&q) != nil {
			return 0
		}
		if w.Send(q) != nil || w.Receive(&result) != nil || encoder.Encode(result) != nil {
			return 1
		}
	}
}

func startIPCChild(t *testing.T, h *renewalHarness, mode string) (*exec.Cmd, io.WriteCloser, *json.Decoder) {
	t.Helper()
	command := exec.CommandContext(h.ctx, os.Args[0], "-test.run=^TestIPCIdentitySubprocess$")
	command.Env = []string{"SMALLSSH_IPC_TEST_MODE=" + mode, "SMALLSSH_IPC_TEST_DIR=" + h.dir, "SMALLSSH_IPC_TEST_PIN=" + h.s.PublicKeyPin()}
	input, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err = command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { input.Close(); _ = command.Process.Kill(); _ = command.Wait() })
	return command, input, json.NewDecoder(output)
}

func TestSameExecutableInAnotherProcessCannotUseOrReceiveTheSession(t *testing.T) {
	h := newRenewalHarness(t)
	parent, d, err := h.c.RequestSession(h.ctx, "fixture", "same executable", "full", time.Hour, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err = h.c.DecideReviewedWithOwner(h.ctx, parent, true, false, h.s.Vault); err != nil {
		t.Fatal(err)
	}
	_, _, paired := startIPCChild(t, h, "no-pin")
	var identity broker.Message
	if err = paired.Decode(&identity); err != nil || identity.Action != "paired" || identity.Error != "" {
		t.Fatal("external client could not validate the owner-installed trust anchor", err, identity.Error)
	}
	process, input, output := startIPCChild(t, h, "operation")
	var opened broker.Message
	if err = output.Decode(&opened); err != nil || opened.Channel == "" {
		t.Fatal("helper did not open channel", err, opened.Error)
	}
	if process.Process.Pid == parent.Peer.PID {
		t.Fatal("fixture did not cross a process boundary")
	}
	op := approval.Operation{Host: "fixture", Client: "same executable", Kind: "exec", Command: "must-not-transfer"}
	if _, err = h.c.CallWithSession(h.ctx, broker.Message{Action: "delegationChallenge", ID: parent.ID, Ticket: d.Ticket, Channel: opened.Channel, Operation: op, TTL: 60}, d); err == nil {
		t.Fatal("authorized a different instance of the same program")
	}
	child, ticket, err := h.c.RequestDelegated(h.ctx, op, time.Minute, d)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []broker.Message{{Action: "sessionStatus", ID: parent.ID, Ticket: d.Ticket}, {Action: "renewalChallenge", ID: parent.ID, Ticket: d.Ticket}, {Action: "run", ID: child.ID, Ticket: ticket}} {
		if err = json.NewEncoder(input).Encode(q); err != nil {
			t.Fatal(err)
		}
		var result broker.Message
		if err = output.Decode(&result); err != nil || result.Error == "" {
			t.Fatal("copied metadata inherited process authority", q.Action, err, result.Error)
		}
	}
	if h.commands.Load() != 0 {
		t.Fatal("foreign process executed SSH")
	}
	stream, err := h.c.Start(h.ctx, child.ID, ticket, 0, 0)
	if err != nil {
		t.Fatal("foreign attempt consumed legitimate operation", err)
	}
	defer stream.Close()
	if _, _, err = stream.Copy(io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
}

func TestExternalClientUsesOnlyOwnerPairedBrokerIdentity(t *testing.T) {
	h := newRenewalHarness(t)
	check := func(want string) {
		t.Helper()
		_, _, output := startIPCChild(t, h, "no-pin")
		var result broker.Message
		if err := output.Decode(&result); err != nil {
			t.Fatal(err)
		}
		if want == "" {
			if result.Action != "paired" || result.Error != "" {
				t.Fatal("owner-installed trust was not used", result.Error)
			}
		} else if result.Error != want {
			t.Fatal("unpaired or changed broker did not fail closed", result.Error)
		}
	}
	check("")
	if err := localipc.PairBroker(h.dir, "sha256:"+strings.Repeat("0", 64), true); err != nil {
		t.Fatal(err)
	}
	check(localipc.ErrBrokerIdentity.Error())
	if err := os.Remove(filepath.Join(h.dir, "broker-trust.json")); err != nil {
		t.Fatal(err)
	}
	check(localipc.ErrBrokerPairing.Error())
	if err := localipc.PairBroker(h.dir, h.s.PublicKeyPin(), false); err != nil {
		t.Fatal(err)
	}
	check("")
}

func TestProcessRestartCreatesANewKeyAndRequiresFreshConsent(t *testing.T) {
	h := newRenewalHarness(t)
	first, _, firstOutput := startIPCChild(t, h, "grant")
	var original broker.Message
	if err := firstOutput.Decode(&original); err != nil || original.Request == nil {
		t.Fatal("first process did not request grant", err)
	}
	if err := h.c.DecideReviewedWithOwner(h.ctx, *original.Request, true, false, h.s.Vault); err != nil {
		t.Fatal(err)
	}
	if err := first.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	waitRevoked(t, h.c, h.ctx, original.Request.ID)
	_, _, secondOutput := startIPCChild(t, h, "grant")
	var restarted broker.Message
	if err := secondOutput.Decode(&restarted); err != nil || restarted.Request == nil {
		t.Fatal("restarted process did not request grant", err)
	}
	a, b := original.Request, restarted.Request
	if b.Status != "pending" || a.ID == b.ID || a.Operation.Grant.PublicKey == b.Operation.Grant.PublicKey || a.Peer.Instance == b.Peer.Instance || a.Peer.CodeID != b.Peer.CodeID {
		t.Fatal("restart reused authority or failed to bind the same program to a new process instance")
	}
	if h.commands.Load() != 0 {
		t.Fatal("restart executed before owner consent")
	}
}

func TestControlDisconnectStopsAnActiveSSHChild(t *testing.T) {
	c, fixture := setup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	parent, d, err := c.RequestSession(ctx, "fixture", "disconnect fixture", "full", time.Hour, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	approve(t, c, parent)
	child, ticket, err := c.RequestDelegated(ctx, approval.Operation{Host: "fixture", Client: "disconnect fixture", Kind: "exec", Command: "block"}, time.Minute, d)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := c.Start(ctx, child.ID, ticket, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	select {
	case <-fixture.Started:
	case <-ctx.Done():
		t.Fatal("SSH child never started")
	}
	d.Close()
	done := make(chan error, 1)
	go func() { _, _, err := stream.Copy(io.Discard, io.Discard); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("disconnected child reported success")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("control disconnect left SSH child alive")
	}
	waitRevoked(t, c, ctx, parent.ID)
}

func TestConcurrentClientStartConsumesOneChannelWithoutInterruptingTheWinner(t *testing.T) {
	h := newRenewalHarness(t)
	parent, d, err := h.c.RequestSession(h.ctx, "fixture", "concurrent start fixture", "full", time.Hour, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := h.c.DecideReviewedWithOwner(h.ctx, parent, true, false, h.s.Vault); err != nil {
		t.Fatal(err)
	}
	child, ticket, err := h.c.RequestDelegated(h.ctx, approval.Operation{Host: "fixture", Client: "concurrent start fixture", Kind: "exec", Command: "exactly-once"}, time.Minute, d)
	if err != nil {
		t.Fatal(err)
	}
	type result struct {
		stream *client.Stream
		err    error
	}
	results := make(chan result, 8)
	for range 8 {
		go func() { stream, err := h.c.Start(h.ctx, child.ID, ticket, 0, 0); results <- result{stream, err} }()
	}
	var winner *client.Stream
	for range 8 {
		got := <-results
		if got.err == nil {
			if winner != nil {
				got.stream.Close()
				t.Fatal("consumed one operation channel twice")
			}
			winner = got.stream
		}
	}
	if winner == nil {
		t.Fatal("no client start succeeded")
	}
	defer winner.Close()
	var out bytes.Buffer
	code, _, err := winner.Copy(&out, io.Discard)
	if err != nil || code != 0 || out.String() != "exactly-once" || h.commands.Load() != 1 {
		t.Fatal("a losing start interrupted or replayed the winner", err, code, out.String(), h.commands.Load())
	}
}
