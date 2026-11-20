package approval

import (
	"sshclient/internal/connectcfg"
	"testing"
	"time"
)

func TestTemporaryRouteRequiresOwnerAndCannotBeDelegated(t *testing.T) {
	m := New(prove)
	op := Operation{Host: "server", Kind: "shell", Client: "test", Route: connectcfg.RouteSelection{Mode: "saved", ID: "once"}}
	if _, _, err := m.Create(op, time.Minute); err == nil {
		t.Fatal("ordinary client selected owner route")
	}
	r, _, err := m.CreateOwner(op)
	if err != nil || r.Operation.Route != op.Route {
		t.Fatal(r, err)
	}
	changed := r
	changed.Operation.Route = connectcfg.RouteSelection{Mode: "direct"}
	proof, _ := DecisionProof(changed, true, prove)
	if err := m.Decide(r.ID, true, proof); err == nil {
		t.Fatal("route substitution was not signed")
	}
	parent := Request{Status: "approved", Operation: Operation{Host: op.Host, Kind: "grant", Grant: GrantScope{Permission: "full"}}}
	if grantAllows(parent, op) {
		t.Fatal("full delegation changed route")
	}
	op.Kind, op.Grant = "grant", GrantScope{Permission: "full"}
	if _, _, err := m.CreateOwner(op); err == nil {
		t.Fatal("grant carried temporary route")
	}
}
