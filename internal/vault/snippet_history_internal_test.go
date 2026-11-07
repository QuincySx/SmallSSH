package vault

import (
	"strings"
	"testing"

	"sshclient/internal/snippet"
)

func TestParameterHistoryGlobalBudgetEvictsOldestContexts(t *testing.T) {
	commands := []snippet.Command{{ID: "test"}}
	for i := 0; i < 900; i++ {
		commands[0].History = append(commands[0].History, snippet.InputHistory{Updated: int64(i), Target: strings.Repeat("a", 64), Definition: strings.Repeat("b", 64), Values: []string{strings.Repeat("x", 512), strings.Repeat("y", 512), strings.Repeat("z", 512)}})
	}
	trimParameterHistory(commands)
	if len(commands[0].History) > 512 || len(commands[0].History) == 0 || commands[0].History[0].Updated == 0 || commands[0].History[len(commands[0].History)-1].Updated != 899 {
		t.Fatal("history eviction did not preserve the newest bounded contexts")
	}
	size := 0
	for _, h := range commands[0].History {
		size += len(h.Target) + len(h.Definition) + 128
		for _, value := range h.Values {
			size += len(value)
		}
	}
	if size > 1<<20 {
		t.Fatal("history input exceeded the global budget", size)
	}
}

func TestSealRefusesFilesThatCannotBeReopened(t *testing.T) {
	s := &Session{header: Header{Version: Version, Salt: make([]byte, 16), ID: make([]byte, 16)}, key: make([]byte, 32), profiles: []Profile{{Password: strings.Repeat("x", 25<<20)}}}
	if data, err := s.Seal(); err == nil || data != nil {
		t.Fatal("oversized encrypted vault could be written but not reopened")
	}
}
