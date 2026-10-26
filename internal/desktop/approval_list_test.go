package desktop

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/approval"
	"sshclient/internal/vault"
)

func TestApprovalListFitsRequesterAndSelectsExactRequest(t *testing.T) {
	for _, dark := range []bool{false, true} {
		a, first := reviewFixture("read")
		second := first
		second.ID = "second-list-request"
		second.Operation.Client = "另一个 AI 客户端"
		second.Operation.Host = "other"
		second.Operation.Endpoint = "deploy@192.0.2.20:2222"
		a.connections = append(a.connections, vault.Summary{ID: "other", Name: "开发服务器"})
		a.requests = []approval.Request{first, second}
		a.selectedRequest = first.ID
		tt := ui.NewTester(a.approvalView, 1000, 760)
		tt.SetDark(dark)
		for _, entry := range []struct {
			request approval.Request
			server  string
		}{{first, "个人服务器"}, {second, "开发服务器"}} {
			detail := operationTitle(entry.request.Operation.Kind) + " · " + entry.server
			row, ok := tt.Find(entry.request.Operation.Client + " · " + detail)
			if !ok || row.H < 56 {
				t.Fatal("request was rendered as a short action button", row)
			}
			for _, label := range []string{entry.request.Operation.Client, detail} {
				text, ok := tt.Find(label)
				if !ok || text.Y < row.Y || text.Y+text.H > row.Y+row.H+1 {
					t.Fatal("request identity or target was clipped", label, row, text)
				}
			}
		}
		a.ownerPass = "previous-confirmation"
		secondLabel := second.Operation.Client + " · " + operationTitle(second.Operation.Kind) + " · 开发服务器"
		if err := tt.Click(secondLabel); err != nil {
			t.Fatal(err)
		}
		if a.selectedRequest != second.ID || a.ownerPass != "" || !tt.HasText(second.Operation.Endpoint) {
			t.Fatal("request selection used the wrong target or retained a password", a.selectedRequest)
		}
		a.selectedRequest = first.ID
		tt.Frame()
		a.ownerPass = "previous-confirmation"
		tt.Key(0, ui.KeyEnter)
		if a.selectedRequest != second.ID || a.ownerPass != "" {
			t.Fatal("keyboard activation did not select its focused request", a.selectedRequest)
		}
	}
}
