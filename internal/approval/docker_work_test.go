package approval

import (
	"context"
	"strings"
	"testing"
	"time"

	"sshclient/internal/serverops"
)

func TestDockerRequestsBindIdentityScopeAndOwnerLifetime(t *testing.T) {
	m := New(prove)
	target := serverops.DockerTarget{Socket: "/var/run/docker.sock", Engine: "engine", ID: strings.Repeat("a", 64)}
	op := Operation{Host: "fixture", Client: "desktop spoof", Kind: "docker-terminal", Docker: serverops.DockerRequest{Target: target, Shell: "sh"}}
	r, _, err := m.Create(op, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*Operation){func(o *Operation) { o.Docker.Target.ID = strings.Repeat("b", 64) }, func(o *Operation) { o.Docker.Shell = "bash" }, func(o *Operation) { o.Docker.Target.Engine = "other" }, func(o *Operation) { o.Kind = "shell" }} {
		altered := r
		change(&altered.Operation)
		proof, _ := DecisionProof(altered, true, prove)
		if m.Decide(r.ID, true, proof) == nil {
			t.Fatal("altered Docker target was approved")
		}
	}
	owner, ticket, err := m.CreateOwner(op)
	if err != nil {
		t.Fatal(err)
	}
	approve(t, m, owner)
	ctx, _, stop, err := m.Start(context.Background(), owner.ID, ticket)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	if _, deadline := ctx.Deadline(); deadline {
		t.Fatal("owner container terminal expires while in use")
	}
	parent := Request{Operation: Operation{Host: "fixture", Kind: "grant", Grant: GrantScope{Permission: "read"}}, Status: "approved"}
	if grantAllows(parent, op) {
		t.Fatal("read grant allowed interactive container shell")
	}
	logs := op
	logs.Kind = "docker-logs"
	logs.Docker = serverops.DockerRequest{Target: target, Tail: 200, Follow: true}
	if !grantAllows(parent, logs) {
		t.Fatal("read grant denied fixed log reader")
	}
	logs.Docker.Shell = "sh"
	if _, _, err = m.Create(logs, time.Minute); err == nil {
		t.Fatal("log request smuggled terminal options")
	}
	ordinary := Operation{Host: "fixture", Client: "test", Kind: "exec", Command: "true", Docker: op.Docker}
	if _, _, err = m.Create(ordinary, time.Minute); err == nil {
		t.Fatal("ignored Docker payload accepted")
	}
	m.Lock()
	if ctx.Err() == nil {
		t.Fatal("lock retained container authority")
	}
}

func TestComposeProofBindsReviewedModelFilesAndServiceSelection(t *testing.T) {
	m := New(prove)
	op := Operation{Host: "fixture", Client: "AI", Kind: "compose-update", Compose: serverops.ComposeRequest{Target: serverops.DockerTarget{Socket: "/var/run/docker.sock", Engine: "engine", ID: strings.Repeat("a", 64)}, Project: "sample", Directory: "/srv/sample", Files: "/srv/sample/compose.yaml", Services: "web", ConfigHash: strings.Repeat("a", 64)}}
	r, _, err := m.Create(op, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	for _, alter := range []func(*serverops.ComposeRequest){func(c *serverops.ComposeRequest) { c.ConfigHash = strings.Repeat("b", 64) }, func(c *serverops.ComposeRequest) { c.Services = "api,web" }, func(c *serverops.ComposeRequest) { c.Files = "/srv/sample/other.yaml" }, func(c *serverops.ComposeRequest) { c.Project = "other" }} {
		changed := r
		alter(&changed.Operation.Compose)
		proof, _ := DecisionProof(changed, true, prove)
		if m.Decide(r.ID, true, proof) == nil {
			t.Fatal("reviewed Compose scope was substituted")
		}
	}
	parent := Request{Operation: Operation{Host: "fixture", Kind: "grant", Grant: GrantScope{Permission: "read"}}, Status: "approved"}
	if grantAllows(parent, op) {
		t.Fatal("read grant allowed Compose update")
	}
	approve(t, m, r)
}
