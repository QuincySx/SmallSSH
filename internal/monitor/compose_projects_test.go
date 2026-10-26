package monitor

import (
	"math"
	"strings"
	"testing"
)

func TestComposeProjectsGroupObservedContainersAndSumOnlyCurrentMembers(t *testing.T) {
	rows := []Container{
		{ID: "web-1", Names: "site-web-1", Project: "site", Service: "web", State: "running", Status: "Up 1 hour (healthy)"},
		{ID: "web-2", Names: "site-web-2", Project: "site", Service: "web", State: "running", Status: "Up 1 hour"},
		{ID: "db", Project: "site", Service: "db", State: "running", Status: "Up 1 hour (unhealthy)"},
		{ID: "stopped", Project: "site", Service: "worker", State: "exited", Status: "Exited (0) 1 hour ago"},
		{ID: "other", Labels: "com.docker.compose.project=other", State: "running"},
		{ID: "plain", Names: "site-cache", State: "running"},
	}
	metrics := map[string]ContainerStats{
		"web-1":          {CPU: 120, Memory: 64 << 20, CPUValid: true, MemoryValid: true},
		"web-2":          {CPU: 5, Memory: 32 << 20, CPUValid: true, MemoryValid: true},
		"db":             {CPU: 10, Memory: 128 << 20, CPUValid: true, MemoryValid: true},
		"stopped":        {CPU: 99, Memory: 999, CPUValid: true, MemoryValid: true},
		"old-deleted-id": {CPU: 99, Memory: 999, CPUValid: true, MemoryValid: true},
	}
	projects := ComposeProjects(append(rows, rows[0]), metrics)
	if len(projects) != 2 || projects[0].Name != "other" {
		t.Fatal("project labels were not used, or a name was guessed", projects)
	}
	p := projects[1]
	if len(p.Containers) != 4 || p.Services != 3 || p.Running != 3 || p.Faults != 1 || p.State() != "attention" || p.Containers[0].ID != "db" {
		t.Fatal("project membership, replicas or health status lost", p)
	}
	if p.CPU != 135 || p.Memory != 224<<20 || p.CPUSamples != 3 || p.MemorySamples != 3 || p.ResourceContainers != 3 {
		t.Fatal("sum included stopped/deleted/other containers", p)
	}
}

func TestComposeConditionsAndProjectStatesDoNotInventHealth(t *testing.T) {
	for _, tc := range []struct{ state, status, condition, project string }{
		{"running", "Up 1 hour", "running", "running"},
		{"running", "Up 1 hour (healthy)", "healthy", "running"},
		{"running", "Up 1 hour (unhealthy)", "unhealthy", "attention"},
		{"running", "Up 1 second (health: starting)", "starting", "starting"},
		{"exited", "Exited (0) 1 hour ago", "stopped", "stopped"},
		{"exited", "Exited (137) 1 hour ago", "failed", "attention"},
		{"paused", "Up 1 hour (Paused)", "paused", "paused"},
		{"restarting", "Restarting (1)", "restarting", "attention"},
		{"dead", "Dead", "dead", "attention"},
		{"created", "Created", "created", "stopped"},
		{"", "", "unknown", "unknown"},
	} {
		row := Container{ID: "id", Project: "site", State: tc.state, Status: tc.status}
		p := ComposeProjects([]Container{row}, nil)[0]
		if ContainerCondition(row) != tc.condition || p.State() != tc.project {
			t.Fatal(tc, ContainerCondition(row), p.State())
		}
	}
	rows := []Container{{ID: "a", Project: "p", State: "running"}, {ID: "b", Project: "p", State: "exited"}}
	if p := ComposeProjects(rows, nil)[0]; p.State() != "partial" || p.CPUSamples != 0 || p.ResourceContainers != 1 {
		t.Fatal(p)
	}
	rows[1].State = "running"
	metrics := map[string]ContainerStats{"a": {CPU: math.NaN(), CPUValid: true, MemoryValid: true, Memory: 123}}
	if p := ComposeProjects(rows, metrics)[0]; p.CPUSamples != 0 || p.MemorySamples != 1 || p.CPU != 0 {
		t.Fatal("bad samples looked valid", p)
	}
}

func TestComposeDetectionFromInventoryLabelsAndStats(t *testing.T) {
	text := `@@DOCKER_STATUS
ready
@@DOCKER_CONTAINERS
{"ID":"` + strings.Repeat("a", 64) + `","Names":"site-web-1","State":"running","Status":"Up 1 hour (healthy)","Project":"site","Service":"web","ComposeDir":"/srv/site","ComposeFiles":"/srv/site/compose.yaml"}
{"ID":"` + strings.Repeat("b", 64) + `","Names":"site-db-1","State":"running","Status":"Up 1 hour (unhealthy)","Project":"site","Service":"db","ComposeDir":"/srv/site","ComposeFiles":"/srv/site/compose.yaml"}
`
	d := ParseInventory(text).Docker
	if d == nil || len(d.Containers) != 2 || d.Containers[0].ComposeDir != "/srv/site" {
		t.Fatal(d)
	}
	p := ComposeProjects(d.Containers, nil)
	if len(p) != 1 || p[0].Services != 2 || p[0].Faults != 1 || p[0].Containers[0].Service != "db" {
		t.Fatal(p)
	}
	for _, label := range []string{"com.docker.compose.project", "com.docker.compose.service", "com.docker.compose.project.working_dir", "com.docker.compose.project.config_files"} {
		if !strings.Contains(DockerCommand, `.Label "`+label+`"`) {
			t.Fatal("required detection label not collected", label)
		}
	}
}
