package broker_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"sshclient/internal/approval"
	"sshclient/internal/broker"
	"sshclient/internal/vault"
)

func TestOwnerManagementCannotFollowChangedConnectionTarget(t *testing.T) {
	c, fixture := setup(t)
	owner, err := vault.Read(broker.VaultPath(c.Dir), pass)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	p := fixture.Profile()
	endpoint := fmt.Sprintf("%s@%s:%d", p.User, p.Host, p.Port)
	for _, op := range []approval.Operation{
		{Host: p.ID, Kind: "exec", Command: "true", Endpoint: "other@elsewhere:22", HostFingerprint: p.Fingerprint},
		{Host: p.ID, Kind: "exec", Command: "true", Endpoint: endpoint, HostFingerprint: "SHA256:changed"},
	} {
		op.Client = "management test"
		if _, _, err := c.RequestWithOwner(context.Background(), op, owner); err == nil || !strings.Contains(err.Error(), "连接目标已变化") {
			t.Fatal("changed target accepted")
		}
	}
	request, _, err := c.RequestWithOwner(context.Background(), approval.Operation{Host: p.ID, Kind: "exec", Client: "management test", Command: "true", Endpoint: endpoint, HostFingerprint: p.Fingerprint}, owner)
	if err != nil || request.Operation.Endpoint != endpoint || !request.OwnerIssued {
		t.Fatal(request, err)
	}
	if fixture.Auths.Load() != 0 {
		t.Fatal("target validation dialled SSH")
	}
}
