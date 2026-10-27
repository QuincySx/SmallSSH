package tasks

import (
	"strings"
	"testing"
)

func TestDevelopmentRestartAlwaysInheritsSilentModeAndIsolatedData(t *testing.T) {
	env := developmentEnv([]string{"PATH=/test", "SSH_WORKBENCH_DEV_BACKGROUND=0", "SSH_WORKBENCH_DEV_DATA_DIR=/wrong"}, "/isolated/development")
	seen := map[string]int{}
	for _, entry := range env {
		seen[strings.SplitN(entry, "=", 2)[0]]++
	}
	if seen["SSH_WORKBENCH_DEV_BACKGROUND"] != 1 || seen["SSH_WORKBENCH_DEV_DATA_DIR"] != 1 {
		t.Fatal("duplicate mode variables")
	}
	joined := strings.Join(env, "\n")
	if !strings.Contains(joined, "SSH_WORKBENCH_DEV_BACKGROUND=1") || !strings.Contains(joined, "SSH_WORKBENCH_DEV_DATA_DIR=/isolated/development") || !strings.Contains(joined, "PATH=/test") {
		t.Fatal("restart could lose silent mode or data isolation")
	}
}
