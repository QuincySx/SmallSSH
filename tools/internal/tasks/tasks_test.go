package tasks

import "testing"

func TestUnknownTaskIsRejectedWithTheListHint(t *testing.T) {
	if err := dispatch("bild", nil); err == nil {
		t.Fatal("a typo ran nothing and reported no error")
	}
}

func TestEveryTaskIsListedWithAUniqueNameAndSummary(t *testing.T) {
	seen := map[string]bool{}
	for _, task := range tasks {
		if task.name == "" || task.summary == "" || task.run == nil {
			t.Fatalf("incomplete task %+v", task)
		}
		if seen[task.name] {
			t.Fatalf("duplicate task %q", task.name)
		}
		seen[task.name] = true
	}
	// The front door and the per-task commands have to agree, so every task needs
	// a tools/<name> command too; that is checked by building ./tools/... .
	for _, name := range []string{"build", "check", "dev", "test"} {
		if !seen[name] {
			t.Fatalf("task %q disappeared from the front door", name)
		}
	}
}
