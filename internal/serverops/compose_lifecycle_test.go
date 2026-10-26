package serverops_test

import (
	"os"
	"strings"
	"testing"

	"sshclient/internal/serverops"
	"sshclient/internal/testdocker"
)

func TestComposeLifecycleGuardsSnapshotAndOnlyInvokesRequestedAction(t *testing.T) {
	for _, action := range []string{"start", "stop", "restart"} {
		for _, mode := range []string{"", "changed-members", "changed-source", "lifecycle-fail"} {
			t.Run(action+"/"+mode, func(t *testing.T) {
				f := testdocker.New(t)
				os.WriteFile(f.State, []byte(mode), 0600)
				command, err := serverops.ComposeLifecycle(f.Target, action, []serverops.ComposeMember{{ID: f.Target.Target.ID, Service: "web"}})
				if err != nil {
					t.Fatal(err)
				}
				out, stderr, code := f.Execute(command)
				if (code == 0) != (mode == "") {
					t.Fatal(action, mode, code, out, stderr)
				}
				calls, _ := os.ReadFile(f.Calls)
				if strings.HasPrefix(mode, "changed-") && len(calls) != 0 {
					t.Fatal("identity change reached the mutation", string(calls))
				}
				if mode == "" && !strings.Contains(string(calls), action+" "+action) {
					t.Fatal(string(calls))
				}
				for _, forbidden := range []string{"compose_local up", "compose_local pull", "compose_local down", "container rm", "--volumes", ".Config.Env"} {
					if strings.Contains(command, forbidden) {
						t.Fatal("lifecycle unexpectedly rebuilds, removes or reads secrets", forbidden)
					}
				}
			})
		}
	}
}

func TestComposeLifecycleRejectsScopeSubstitution(t *testing.T) {
	f := testdocker.New(t)
	members := []serverops.ComposeMember{{ID: f.Target.Target.ID, Service: "web"}}
	for _, action := range []string{"down", "rm", "stop;id", "up"} {
		if _, err := serverops.ComposeLifecycle(f.Target, action, members); err == nil {
			t.Fatal("unsafe action accepted", action)
		}
	}
	for _, rows := range [][]serverops.ComposeMember{nil, {{ID: "short", Service: "web"}}, {{ID: f.Target.Target.ID, Service: "--help"}}, {members[0], members[0]}, {{ID: strings.Repeat("b", 64), Service: "web"}}, {{ID: f.Target.Target.ID, Service: "another"}}} {
		if _, err := serverops.ComposeLifecycle(f.Target, "stop", rows); err == nil {
			t.Fatal("unreviewed members accepted", rows)
		}
	}
	r := f.Target
	r.Services = "database,web"
	if _, err := serverops.ComposeLifecycle(r, "stop", members); err == nil {
		t.Fatal("additional service reached the command")
	}
}
