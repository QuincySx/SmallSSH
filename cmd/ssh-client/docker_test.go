package main

import (
	"strings"
	"testing"
)

func TestDockerCLIValidatesTypedRequestsBeforeAuthorization(t *testing.T) {
	id := strings.Repeat("a", 64)
	op, err := dockerOperation("AI", []string{"logs", "--engine", "engine", "--tail", "1000", "host", id})
	if err != nil || op.Kind != "docker-logs" || op.Docker.Tail != 1000 || !op.Docker.Follow {
		t.Fatal(op, err)
	}
	if _, err = dockerOperation("AI", []string{"terminal", "--engine", "engine", "--shell", "sh;id", "host", id}); err == nil {
		t.Fatal("arbitrary shell accepted")
	}
	args := []string{"update", "--engine", "engine", "--container", id, "--directory", "/srv/site", "--files", "/srv/site/compose.yaml", "--services", "web,api", "--hash", strings.Repeat("0", 64), "host", "site"}
	op, err = dockerOperation("AI", args)
	if err != nil || op.Compose.Services != "api,web" || op.Compose.ConfigHash == "" {
		t.Fatal(op, err)
	}
	if _, err = dockerOperation("AI", []string{"update", "host", "site"}); err == nil {
		t.Fatal("unreviewed update accepted")
	}
}
