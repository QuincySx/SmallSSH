package tasks

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDarwinLegacyCLIUsesTheSameExecutableThroughRelativeAlias(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS ad-hoc bundles use relative aliases; other targets retain a copy")
	}
	directory := t.TempDir()
	source := filepath.Join(directory, "cli", "smallssh")
	if err := os.MkdirAll(filepath.Dir(source), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("synthetic executable"), 0755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(directory, "ssh-client")
	if err := stageCLIAlias(target{goos: "darwin", goarch: "arm64"}, source, alias); err != nil {
		t.Fatal(err)
	}
	link, err := os.Readlink(alias)
	if err != nil || link != filepath.Join("cli", "smallssh") || filepath.IsAbs(link) {
		t.Fatal("legacy CLI alias is not portable within the bundle", err)
	}
	a, err := os.Stat(source)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.Stat(alias)
	if err != nil || !os.SameFile(a, b) || !b.Mode().IsRegular() {
		t.Fatal("legacy path does not resolve to the signed CLI executable", err)
	}
}
