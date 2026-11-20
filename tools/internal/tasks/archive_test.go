package tasks

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestArchivePreservesBundleFilesAndSymlinks(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "bundle")
	if err := os.MkdirAll(filepath.Join(source, "cli"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "cli", "smallssh"), []byte("executable"), 0755); err != nil {
		t.Fatal(err)
	}
	hasLink := runtime.GOOS != "windows"
	if hasLink {
		if err := os.Symlink("cli/smallssh", filepath.Join(source, "ssh-client")); err != nil {
			t.Fatal(err)
		}
	}
	destination := filepath.Join(directory, "dist", "bundle.tar.gz")
	if err := archiveDirectory(source, destination); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	compressed, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer compressed.Close()
	archive := tar.NewReader(compressed)
	foundExecutable, foundLink := false, false
	for {
		header, err := archive.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		switch header.Name {
		case "cli/smallssh":
			data, err := io.ReadAll(archive)
			if err != nil || string(data) != "executable" || header.Typeflag != tar.TypeReg {
				t.Fatalf("lost executable: %+v, %q, %v", header, data, err)
			}
			if runtime.GOOS != "windows" && header.Mode&0111 == 0 {
				t.Fatal("lost execute bits")
			}
			foundExecutable = true
		case "ssh-client":
			if header.Typeflag != tar.TypeSymlink || header.Linkname != "cli/smallssh" || header.Size != 0 {
				t.Fatalf("followed or changed symlink: %+v", header)
			}
			foundLink = true
		}
	}
	if !foundExecutable || hasLink && !foundLink {
		t.Fatal("missing bundle entries")
	}
}

func TestArchiveFailurePreservesExistingOutput(t *testing.T) {
	directory := t.TempDir()
	destination := filepath.Join(directory, "bundle.tar.gz")
	if err := os.WriteFile(destination, []byte("prior archive"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := archiveDirectory(filepath.Join(directory, "missing"), destination); err == nil {
		t.Fatal("accepted missing bundle")
	}
	data, err := os.ReadFile(destination)
	if err != nil || string(data) != "prior archive" {
		t.Fatal("changed prior archive after failure", err)
	}
}
