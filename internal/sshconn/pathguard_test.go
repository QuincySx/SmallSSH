package sshconn_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"sshclient/internal/accesspolicy"
	"sshclient/internal/sshconn"
	"sshclient/internal/testserver"
)

func TestRemotePathGuard(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX path guard fixture")
	}
	server := testserver.New(t)
	client, err := sshconn.Dial(context.Background(), server.Profile())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	// Use the real temporary directory so platform convenience symlinks are not
	// accidentally accepted as mutation target ancestors.
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	critical, safe := filepath.Join(dir, "critical"), filepath.Join(dir, "safe")
	for _, p := range []string{critical, safe} {
		if err := os.Mkdir(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	alias := filepath.Join(dir, "alias")
	if err := os.Symlink(critical, alias); err != nil {
		t.Fatal(err)
	}
	protected := []string{critical}
	for _, target := range []string{critical, filepath.Join(critical, "child"), dir, alias, filepath.Join(alias, "child"), filepath.Join(safe, "..", "critical"), "relative", "/"} {
		if err := client.CheckProtectedPaths(context.Background(), []string{target}, protected); !errors.Is(err, accesspolicy.ErrProtected) {
			t.Errorf("accepted %s: %v", target, err)
		}
	}
	for _, target := range []string{safe, filepath.Join(safe, "new"), filepath.Join(safe, "new", "child"), critical + "2"} {
		if err := client.CheckProtectedPaths(context.Background(), []string{target}, protected); err != nil {
			t.Errorf("rejected %s: %v", target, err)
		}
	}
	// Both rename source and destination must be passed to the same guard.
	if err := client.CheckProtectedPaths(context.Background(), []string{safe, filepath.Join(alias, "new")}, protected); !errors.Is(err, accesspolicy.ErrProtected) {
		t.Fatal("rename alias destination accepted", err)
	}
	// A protected root expressed via a symlink must protect its real path too.
	if err := client.CheckProtectedPaths(context.Background(), []string{filepath.Join(critical, "new")}, []string{alias}); !errors.Is(err, accesspolicy.ErrProtected) {
		t.Fatal("protected alias not resolved", err)
	}
	if err := client.CheckProtectedPaths(context.Background(), []string{safe}, []string{filepath.Join(critical, "missing", "child")}); err != nil {
		t.Fatal("missing protected descendant could not be resolved", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := client.CheckProtectedPaths(cancelled, []string{safe}, protected); !errors.Is(err, context.Canceled) || !errors.Is(err, accesspolicy.ErrProtected) {
		t.Fatal("cancelled resolution did not fail closed", err)
	}
}

// A link target must retain filesystem traversal semantics. Lexically cleaning
// jump/../critical would point at aliasdir/critical, while the remote kernel
// resolves jump first and therefore reaches actual/critical.
func TestRemotePathGuardRejectsDotDotInProtectedLinkTargets(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX path guard fixture")
	}
	server := testserver.New(t)
	client, err := sshconn.Dial(context.Background(), server.Profile())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	aliasDir, actualDir := filepath.Join(dir, "aliasdir"), filepath.Join(dir, "actual")
	for _, p := range []string{aliasDir, actualDir, filepath.Join(actualDir, "base"), filepath.Join(actualDir, "critical")} {
		if err := os.Mkdir(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	jump := filepath.Join(aliasDir, "jump")
	if err := os.Symlink(filepath.Join(actualDir, "base"), jump); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, target string }{
		{"relative", "jump/../critical"},
		{"absolute", jump + "/../critical"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			alias := filepath.Join(aliasDir, tc.name)
			if err := os.Symlink(tc.target, alias); err != nil {
				t.Fatal(err)
			}
			real, err := filepath.EvalSymlinks(alias)
			if err != nil || real != filepath.Join(actualDir, "critical") {
				t.Fatal("fixture did not traverse symlink before parent directory", real, err)
			}
			target := filepath.Join(real, "new")
			if err := client.CheckProtectedPaths(context.Background(), []string{target}, []string{alias}); !errors.Is(err, accesspolicy.ErrProtected) {
				t.Fatalf("protected alias allowed canonical descendant %s: %v", target, err)
			}
		})
	}
}
