package broker_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"sshclient/internal/approval"
	"sshclient/internal/broker"
	"sshclient/internal/client"
	"sshclient/internal/localipc"
	"sshclient/internal/protocol"
)

const (
	managedProofChildEnv    = "SMALLSSH_MANAGED_PROOF_CHILD"
	managedProofChildMarker = "managed-child-denied"
)

type managedProofChildInput struct {
	Dir          string         `json:"dir"`
	PublicKeyPin string         `json:"publicPin"`
	Message      broker.Message `json:"message"`
}

func TestManagedOwnerCannotDecideAnotherManagedOwner(t *testing.T) {
	fixture := newManagedSessionsFixture(t)
	additional, additionalSigner, _, cancelAdditional := newAdditionalManagedOwner(t, fixture.server, fixture.dir)
	defer cancelAdditional()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if _, err := additional.ListSnippets(ctx, additionalSigner); err != nil {
		t.Fatalf("managed owner B could not read before cross-session review: %v", err)
	}

	const command = "printf managed-owner-cross-session"
	request, ticket, err := fixture.cli.RequestWithOwner(ctx, approval.Operation{
		Host:    fixture.profile.ID,
		Kind:    "exec",
		Command: command,
		Client:  "synthetic managed owner A",
	}, fixture.signer)
	if err != nil {
		t.Fatal(err)
	}
	if !request.OwnerIssued {
		t.Fatal("managed owner A request was not marked OwnerIssued")
	}
	assertPending := func(stage string) {
		t.Helper()
		stored, getErr := fixture.server.Approvals.Get(request.ID)
		if getErr != nil || stored.Status != "pending" {
			t.Fatalf("%s: stored managed owner A request = %#v, %v", stage, stored, getErr)
		}
	}
	assertPending("before cross-session decisions")

	for _, decision := range []struct {
		name   string
		allow  bool
		revoke bool
	}{
		{name: "allow", allow: true},
		{name: "deny", allow: false},
		{name: "revoke", revoke: true},
	} {
		err = additional.DecideReviewedWithOwner(ctx, request, decision.allow, decision.revoke, additionalSigner)
		if err == nil {
			t.Fatalf("managed owner B %s decision accepted for owner A's request", decision.name)
		}
		assertPending("after owner B " + decision.name)
	}

	if err = fixture.cli.DecideReviewedWithOwner(ctx, request, true, false, fixture.signer); err != nil {
		t.Fatalf("managed owner A could not approve its own request: %v", err)
	}
	stream, err := fixture.cli.Start(ctx, request.ID, ticket, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()

	var stdout, stderr bytes.Buffer
	code, files, err := stream.Copy(&stdout, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 || len(files) != 0 || !strings.Contains(stdout.String(), command) {
		t.Fatalf("managed owner A exec result: code=%d files=%d stdout=%q stderr=%q", code, len(files), stdout.String(), stderr.String())
	}
}

func TestManagedOwnerCopiedProofRejectedAcrossProcess(t *testing.T) {
	fixture := newManagedSessionsFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if _, err := fixture.cli.ListSnippets(ctx, fixture.signer); err != nil {
		t.Fatalf("parent managed query failed before child proof: %v", err)
	}
	raw := client.Client{Dir: fixture.dir, BrokerKey: fixture.server.PublicKeyPin()}
	signed := signedManagedWorkspaceRead(t, ctx, raw, fixture.signer, broker.Message{
		Action:    "workspaceRead",
		Workspace: &protocol.WorkspaceQuery{Kind: "snippet-list", Limit: 1},
	})

	var stdin bytes.Buffer
	if err := json.NewEncoder(&stdin).Encode(managedProofChildInput{
		Dir:          fixture.dir,
		PublicKeyPin: fixture.server.PublicKeyPin(),
		Message:      signed,
	}); err != nil {
		t.Fatal("could not encode managed proof child input")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("could not locate test executable: %v", err)
	}
	childCtx, childCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer childCancel()
	command := exec.CommandContext(childCtx, executable, "-test.run=^TestManagedOwnerCopiedProofChild$")
	command.Env = []string{managedProofChildEnv + "=1"}
	command.Stdin = bytes.NewReader(stdin.Bytes())
	output, err := command.Output()
	if err != nil {
		t.Fatalf("managed proof child process failed: %v", err)
	}
	if !bytes.Contains(output, []byte(managedProofChildMarker)) {
		t.Fatal("managed proof child did not report the denial marker")
	}

	if _, err = fixture.cli.ListSnippets(ctx, fixture.signer); err != nil {
		t.Fatalf("parent managed query failed after child proof was rejected: %v", err)
	}
}

func TestManagedOwnerCopiedProofChild(t *testing.T) {
	if os.Getenv(managedProofChildEnv) != "1" {
		return
	}
	var input managedProofChildInput
	if err := json.NewDecoder(io.LimitReader(os.Stdin, 2<<20)).Decode(&input); err != nil {
		t.Fatal("managed proof child could not decode input")
	}
	if input.Dir == "" || input.PublicKeyPin == "" || input.Message.Action == "" || len(input.Message.Proof) == 0 {
		t.Fatal("managed proof child input is incomplete")
	}

	connect := func() *localipc.Wire {
		dialCtx, dialCancel := context.WithTimeout(context.Background(), 5*time.Second)
		wire, err := localipc.ConnectVerified(dialCtx, input.Dir, input.PublicKeyPin)
		dialCancel()
		if err != nil {
			t.Fatal("managed proof child could not connect to broker")
		}
		if err = wire.Conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
			_ = wire.Close()
			t.Fatal("managed proof child could not set connection deadline")
		}
		return wire
	}

	reachable := connect()
	if err := reachable.Send(broker.Message{Action: "hosts"}); err != nil {
		_ = reachable.Close()
		t.Fatal("managed proof child could not send reachability request")
	}
	var reachabilityResponse broker.Message
	if err := reachable.Receive(&reachabilityResponse); err != nil {
		_ = reachable.Close()
		t.Fatal("managed proof child could not receive reachability response")
	}
	_ = reachable.Close()
	if reachabilityResponse.Error != "" {
		t.Fatal("managed proof child broker endpoint returned an error")
	}

	denied := connect()
	defer denied.Close()
	if err := denied.Send(input.Message); err != nil {
		t.Fatal("managed proof child could not send copied proof")
	}
	var response broker.Message
	if err := denied.Receive(&response); err != nil {
		t.Fatal("managed proof child could not receive copied proof response")
	}
	if response.Action != "error" || response.Error != approval.ErrDenied.Error() {
		t.Fatal("managed proof child copied proof was not rejected as denied")
	}
	fmt.Fprint(os.Stdout, managedProofChildMarker)
}
