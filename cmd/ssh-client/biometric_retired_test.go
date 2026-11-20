package main

import (
	"os"
	"strings"
	"testing"
)

// The CLI can be driven from a terminal another process owns, so it must never
// raise a system verification prompt; both old entry points explain where it went.
func TestCLIRefusesSystemVerification(t *testing.T) {
	original := os.Args
	defer func() { os.Args = original }()
	for _, args := range [][]string{
		{"smallssh", "--data-dir", t.TempDir(), "--biometric", "agent"},
		{"smallssh", "--data-dir", t.TempDir(), "--biometric", "approve", "request-id"},
		{"smallssh", "--data-dir", t.TempDir(), "biometric", "enable"},
		{"smallssh", "--data-dir", t.TempDir(), "biometric", "status"},
	} {
		os.Args = args
		code, err := run()
		if code != 2 || err == nil || !strings.Contains(err.Error(), "桌面端") {
			t.Fatal("system verification was not refused", args, code, err)
		}
	}
}
