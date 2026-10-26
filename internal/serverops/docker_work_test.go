package serverops_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"sshclient/internal/serverops"
	"sshclient/internal/testdocker"
)

func TestComposePlanUsesPrivateModelAndPreservesQuotedSource(t *testing.T) {
	f := testdocker.New(t)
	command, err := f.Target.Command("plan")
	if err != nil {
		t.Fatal(err)
	}
	out, stderr, code := f.Execute(command)
	if code != 0 {
		t.Fatal(code, stderr, out)
	}
	plan, err := serverops.ParseComposePlan(f.Target, out)
	if err != nil || len(plan.Images) != 1 || plan.Images[0] != "example/web:1" {
		t.Fatal(plan, err, out)
	}
	data, _ := json.Marshal(plan)
	if strings.Contains(string(data), "fixture-private-value") || strings.Contains(string(data), "database") {
		t.Fatal("private model or unselected service exposed", string(data))
	}
	changed := strings.Replace(out, "fixture-private-value", "changed-private-value", 1)
	next, err := serverops.ParseComposePlan(f.Target, changed)
	if err != nil || next.Hash == plan.Hash {
		t.Fatal("resolved environment change not bound", err)
	}
	large1 := strings.Replace(out, `"name":"sample"`, `"name":"sample","x-number":9007199254740992`, 1)
	large2 := strings.Replace(large1, "9007199254740992", "9007199254740993", 1)
	plan1, e1 := serverops.ParseComposePlan(f.Target, large1)
	plan2, e2 := serverops.ParseComposePlan(f.Target, large2)
	if e1 != nil || e2 != nil || plan1.Hash == plan2.Hash {
		t.Fatal("configuration digest rounded large numbers", e1, e2)
	}
	buildOnly := strings.Replace(out, `"image":"example/web:1"`, `"build":"."`, 1)
	if _, err = serverops.ParseComposePlan(f.Target, buildOnly); err == nil {
		t.Fatal("build-only update accepted")
	}
	for _, stage := range []string{"pull", "up", "status"} {
		command, err = f.Target.Command(stage)
		if err != nil {
			t.Fatal(err)
		}
		out, stderr, code = f.Execute(command)
		if code != 0 {
			t.Fatal(stage, code, stderr)
		}
		if stage == "status" {
			if _, err = serverops.ParseComposeStatus(out, []string{"web"}); err != nil {
				t.Fatal(err)
			}
		}
	}
	calls, _ := os.ReadFile(f.Calls)
	if strings.Contains(string(calls), "down") || strings.Contains(string(calls), "--remove-orphans=true") || !strings.Contains(string(calls), "--remove-orphans=false") || !strings.Contains(string(calls), "--no-deps --no-build --pull never --wait") {
		t.Fatal(string(calls))
	}
}

func TestDockerWorkRejectsAmbiguousTargetsAndUnsafeOptions(t *testing.T) {
	f := testdocker.New(t)
	for _, mutate := range []func(*serverops.ComposeRequest){func(r *serverops.ComposeRequest) { r.Project = "--help" }, func(r *serverops.ComposeRequest) { r.Directory = "../project" }, func(r *serverops.ComposeRequest) { r.Files = "/a.yml,relative.yml" }, func(r *serverops.ComposeRequest) { r.Services = "web;id" }, func(r *serverops.ComposeRequest) { r.Services = "web,web" }, func(r *serverops.ComposeRequest) { r.Target.ID = "short" }, func(r *serverops.ComposeRequest) { r.Target.Socket = "tcp://wrong" }} {
		r := f.Target
		mutate(&r)
		if err := r.Validate(false); err == nil {
			t.Fatal("unsafe Compose request", r)
		}
	}
	r := serverops.DockerRequest{Target: f.Target.Target, Tail: -1, Follow: true}
	if _, err := r.Command("docker-logs"); err == nil {
		t.Fatal("unbounded log tail accepted")
	}
	r = serverops.DockerRequest{Target: f.Target.Target, Shell: "sh; id"}
	if _, err := r.Command("docker-terminal"); err == nil {
		t.Fatal("arbitrary shell accepted")
	}
	for _, text := range []string{`[]`, `[{"Service":"web","State":"running","Health":"unhealthy"}]`, `[{"Service":"another","State":"running"}]`, `[{"Service":"web","State":"exited","ExitCode":1}]`} {
		if _, err := serverops.ParseComposeStatus(text, []string{"web"}); err == nil {
			t.Fatal("bad final state accepted", text)
		}
	}
}
