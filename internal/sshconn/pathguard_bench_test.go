package sshconn_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"sshclient/internal/sshconn"
	"sshclient/internal/testserver"
)

// The guard is on the path of every mutation an automated caller makes, and it
// resolves each path component and each protected root over SFTP. This measures
// what that costs before anyone optimises it.
func BenchmarkProtectedPathGuard(b *testing.B) {
	if runtime.GOOS == "windows" {
		b.Skip("POSIX path guard fixture")
	}
	server := testserver.New(b)
	client, err := sshconn.Dial(context.Background(), server.Profile())
	if err != nil {
		b.Fatal(err)
	}
	defer client.Close()
	root, err := filepath.EvalSymlinks(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	deep := root
	for i := 0; i < 8; i++ {
		deep = filepath.Join(deep, fmt.Sprintf("level-%d", i))
		if err := os.MkdirAll(deep, 0o700); err != nil {
			b.Fatal(err)
		}
	}
	canonical, err := client.ResolveProtectedRoots(context.Background(), nil)
	if err != nil {
		b.Fatal(err)
	}
	targets := []string{filepath.Join(root, "file"), deep}
	for _, target := range targets {
		label := fmt.Sprintf("components-%d", strings.Count(target, "/"))
		b.Run(label+"-resolving-roots", func(b *testing.B) {
			for range b.N {
				if err := client.CheckProtectedPaths(context.Background(), []string{target}, nil); err != nil {
					b.Fatal(err)
				}
			}
		})
		// What the broker does now: the roots are cached, the path is not.
		b.Run(label+"-cached-roots", func(b *testing.B) {
			for range b.N {
				if err := client.CheckProtectedPathsCanonical(context.Background(), []string{target}, nil, canonical); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
