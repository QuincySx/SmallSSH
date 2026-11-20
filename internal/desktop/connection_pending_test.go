package desktop

import (
	"context"
	"testing"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/approval"
)

func TestRetryWithoutAnUnlockedVaultKeepsTheCardRetryable(t *testing.T) {
	for _, kind := range []string{"shell", "docker-terminal"} {
		s := &session{host: "a", name: "连接 A", closed: true, connectionState: "error", connectionError: "earlier failure", connectionMismatch: true,
			target: approval.Operation{Host: "a", Kind: kind}}
		a := &app{ctx: context.Background(), online: true, page: 1, sessions: []*session{s}}
		a.ensureTerminalGroups()
		a.retryPendingTerminal(s)
		tt := ui.NewTester(func(c *ui.Context) { a.pendingTerminalView(c, s) }, 640, 400)
		if s.connectionState != "error" || !s.closed || s.connectionMismatch || !tt.HasText("保险库已锁定，请重新解锁后继续") {
			t.Fatal("a refused retry left a card without its reason", kind, s.connectionState, tt.Texts())
		}
		if _, ok := tt.Find("重试连接"); !ok {
			t.Fatal("a refused retry removed the retry button", kind, tt.Texts())
		}
	}
}
