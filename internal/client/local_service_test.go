package client

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestDaemonExecutablePath(t *testing.T) {
	hostRoot, err := filepath.Abs(string(filepath.Separator))
	if err != nil {
		t.Fatalf("resolve host root: %v", err)
	}
	hostPath := func(value string) string {
		return filepath.Join(hostRoot, filepath.FromSlash(strings.TrimPrefix(value, "/")))
	}

	tests := []struct {
		name  string
		entry string
		goos  string
		want  string
	}{
		{
			name:  "macOS application bundle",
			entry: "/Applications/SSH Client.app/Contents/MacOS/ssh-client",
			goos:  "darwin",
			want:  "/Applications/SSH Client.app/Contents/Resources/smallsshd",
		},
		{
			name:  "macOS resource command line entry",
			entry: "/Applications/SSH Client.app/Contents/Resources/cli/smallssh",
			goos:  "darwin",
			want:  "/Applications/SSH Client.app/Contents/Resources/smallsshd",
		},
		{
			name:  "macOS legacy resource entry",
			entry: "/Applications/SSH Client.app/Contents/Resources/ssh-client",
			goos:  "darwin",
			want:  "/Applications/SSH Client.app/Contents/Resources/smallsshd",
		},
		{
			name:  "Linux main entry",
			entry: "/opt/ssh-client/ssh-client",
			goos:  "linux",
			want:  "/opt/ssh-client/smallsshd",
		},
		{
			name:  "Linux command line resource entry",
			entry: "/opt/ssh-client/cli/smallssh",
			goos:  "linux",
			want:  "/opt/ssh-client/smallsshd",
		},
		{
			name:  "Windows main entry",
			entry: "/opt/ssh-client/ssh-client.exe",
			goos:  "windows",
			want:  "/opt/ssh-client/smallsshd.exe",
		},
		{
			name:  "Windows command line resource entry",
			entry: "/opt/ssh-client/cli/smallssh.exe",
			goos:  "windows",
			want:  "/opt/ssh-client/smallsshd.exe",
		},
		{
			name:  "compile layout",
			entry: "/work/build/compile/darwin-arm64/ssh-client",
			goos:  "darwin",
			want:  "/work/build/compile/darwin-arm64/smallsshd",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			entry := hostPath(test.entry)
			want := hostPath(test.want)
			got, err := daemonExecutablePath(entry, test.goos)
			if err != nil {
				t.Fatalf("daemonExecutablePath() error = %v", err)
			}
			if got != want {
				t.Fatalf("daemonExecutablePath() = %q, want %q", got, want)
			}
		})
	}
}

func TestDaemonExecutablePathRejectsRelativeEntry(t *testing.T) {
	for _, entry := range []string{"ssh-client", "./ssh-client", ""} {
		if _, err := daemonExecutablePath(entry, "linux"); err == nil {
			t.Fatalf("daemonExecutablePath(%q) succeeded for a relative entry", entry)
		}
	}
}
