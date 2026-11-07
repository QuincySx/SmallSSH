package broker

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"sshclient/internal/approval"
)

// peerProcess resolves the process the local socket reported. This is the
// kernel's answer, unlike Operation.Client, which the caller writes itself.
func (s *Server) peerProcess(pid int) approval.Peer {
	if pid <= 0 {
		return approval.Peer{}
	}
	path := executablePath(pid)
	return approval.Peer{PID: pid, Path: path, Bundle: bundleIdentifier(path)}
}

// bundleIdentifier names the macOS app a path belongs to, the way an approval
// dialog can show a stable identity instead of an editable name.
func bundleIdentifier(path string) string {
	if runtime.GOOS != "darwin" || path == "" {
		return ""
	}
	const marker = ".app/Contents/MacOS/"
	index := strings.Index(path, marker)
	if index < 0 {
		return ""
	}
	plist := path[:index+len(".app")] + "/Contents/Info.plist"
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/usr/bin/plutil", "-extract", "CFBundleIdentifier", "raw", "-o", "-", plist).Output()
	if err != nil {
		return ""
	}
	id := strings.TrimSpace(string(out))
	if len(id) > 128 {
		return ""
	}
	return id
}

// executablePath is best effort: an unknown path leaves the PID, which still
// shows that two requests came from different processes.
func executablePath(pid int) string {
	switch runtime.GOOS {
	case "windows":
		return windowsProcessPath(uint32(pid))
	case "linux":
		path, _ := os.Readlink("/proc/" + strconv.Itoa(pid) + "/exe")
		return path
	case "darwin":
	default:
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/bin/ps", "-p", strconv.Itoa(pid), "-o", "comm=").Output()
	if err != nil {
		return ""
	}
	path := strings.TrimSpace(string(out))
	if len(path) > 512 {
		path = path[:512]
	}
	return path
}
