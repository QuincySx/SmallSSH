package tasks

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConcurrentTaskLogsKeepSeparateEvidence(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "go.mod"), []byte("module logfixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(directory)
	first, firstPath, err := openLog("test", true)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if _, err := first.file.WriteString("first task evidence"); err != nil {
		t.Fatal(err)
	}
	second, secondPath, err := openLog("test", true)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if _, err := second.file.WriteString("second task evidence"); err != nil {
		t.Fatal(err)
	}
	if firstPath == secondPath {
		t.Fatal("tasks launched together share a log")
	}
	for path, want := range map[string]string{firstPath: "first task evidence", secondPath: "second task evidence"} {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != want {
			t.Fatal("one task overwrote another's evidence", path, err)
		}
	}
}
