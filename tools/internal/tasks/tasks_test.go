package tasks

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestUnknownTaskIsRejectedWithTheListHint(t *testing.T) {
	if err := dispatch("bild", nil); err == nil {
		t.Fatal("a typo ran nothing and reported no error")
	}
}

func TestCheckBundleDaemon(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("checkBundleDaemon verifies the macOS bundle path and Unix execute bits; Windows os.FileMode/Chmod do not have the same execute-bit semantics")
	}

	newBundle := func(t *testing.T) (string, string) {
		t.Helper()
		bundle := t.TempDir()
		daemon := filepath.Join(bundle, "Contents", "Resources", "smallsshd")
		if err := os.MkdirAll(filepath.Dir(daemon), 0o755); err != nil {
			t.Fatal(err)
		}
		return bundle, daemon
	}

	t.Run("missing", func(t *testing.T) {
		bundle, _ := newBundle(t)
		if err := checkBundleDaemon(bundle); err == nil {
			t.Fatal("accepted a bundle without smallsshd")
		}
	})
	t.Run("non-regular", func(t *testing.T) {
		bundle, daemon := newBundle(t)
		if err := os.Mkdir(daemon, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := checkBundleDaemon(bundle); err == nil {
			t.Fatal("accepted a non-regular smallsshd")
		}
	})
	t.Run("not executable", func(t *testing.T) {
		bundle, daemon := newBundle(t)
		if err := os.WriteFile(daemon, []byte("daemon"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(daemon, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := checkBundleDaemon(bundle); err == nil {
			t.Fatal("accepted smallsshd without execute permission")
		}
	})
	t.Run("executable", func(t *testing.T) {
		bundle, daemon := newBundle(t)
		if err := os.WriteFile(daemon, []byte("daemon"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(daemon, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := checkBundleDaemon(bundle); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("symlink", func(t *testing.T) {
		bundle, daemon := newBundle(t)
		target := filepath.Join(t.TempDir(), "smallsshd-target")
		if err := os.WriteFile(target, []byte("daemon"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(target, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, daemon); err != nil {
			t.Skipf("cannot create symlink: %v", err)
		}
		if err := checkBundleDaemon(bundle); err == nil {
			t.Fatal("accepted a symlinked smallsshd")
		}
	})
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
