package tasks

import (
	"bytes"
	"fmt"
	"os/exec"
	"sshclient/tools/internal/project"
	"strings"
)

func verifyFrontendDependencies(out *tee) error {
	for _, entry := range []string{"./cmd/desktop", "./cmd/ssh-client"} {
		cmd := exec.Command("go", "list", "-mod=vendor", "-tags", project.RuntimeTags, "-deps", "-f", "{{.ImportPath}}", entry)
		var result bytes.Buffer
		cmd.Stdout = &result
		cmd.Stderr = out
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("前端依赖图检查 %s: %w", entry, err)
		}
		for _, path := range strings.Fields(result.String()) {
			if forbiddenFrontendDependency(path) {
				return fmt.Errorf("前端 %s 不得编入连接后端: %s", entry, path)
			}
		}
		fmt.Fprintf(out, "== %s 依赖图通过：无 broker/SSH/代理运行时\n", entry)
	}
	return nil
}
func forbiddenFrontendDependency(path string) bool {
	// This standalone WebSocket codec bridges noVNC to a fixed loopback
	// tunnel; it has no SSH or proxy core dependency. All other sagernet
	// packages remain backend-only.
	if path == "github.com/sagernet/ws" || path == "github.com/sagernet/ws/wsutil" {
		return false
	}
	return path == "sshclient/internal/broker" || path == "sshclient/internal/identityagent" || path == "sshclient/internal/service" || path == "sshclient/internal/sshconn" || path == "sshclient/internal/netroute" || strings.HasPrefix(path, "github.com/sagernet/") || strings.HasPrefix(path, "github.com/anytls/")
}
