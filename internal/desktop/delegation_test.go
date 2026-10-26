package desktop

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/approval"
	"sshclient/internal/broker"
	"sshclient/internal/client"
	"sshclient/internal/vault"
)

func TestGrantApprovalShowsScopePairingAndRevokesWithoutAnotherPassword(t *testing.T) {
	for _, permission := range []string{"read", "full"} {
		t.Run(permission, func(t *testing.T) {
			h := newConnectionFlowHarness(t)
			if err := h.a.client.SaveConnection(h.ctx, vault.ProfileChange{Profile: h.fixture.Profile()}, h.server.Vault); err != nil {
				t.Fatal(err)
			}
			r, d, err := h.a.client.RequestSession(h.ctx, "fixture", "unverified fixture client", permission, 2*time.Hour, 15*time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()
			h.a.page = 3
			h.a.requests = []approval.Request{r}
			h.a.selectedRequest = r.ID
			h.ui = ui.NewTester(h.a.approvalView, 1100, 760)
			if !h.ui.HasText("允许持续访问服务器？") || !h.ui.HasText(r.Operation.Grant.PairingCode()) || !h.ui.HasText("授权范围") || !h.ui.HasText("授权期限") {
				t.Fatal("grant scope or pairing was hidden", h.ui.Texts())
			}
			if permission == "full" && !h.ui.HasText("可使用此 SSH 账户的全部权限") {
				t.Fatal("full session looked read-only")
			}
			h.click("保险库主密码")
			h.ui.Type(string(h.pass))
			prompt := h.a.inlineApproval
			h.click(approvalConsentLabel(r))
			if !prompt.busy {
				t.Fatal("password action did not start authentication")
			}
			h.wait(func() bool { return !prompt.busy })
			if h.a.ownerPass != "" {
				t.Fatal("approval retained password input")
			}
			status, err := h.a.client.Call(h.ctx, broker.Message{Action: "status", ID: r.ID})
			if err != nil || status.Request == nil || status.Request.Status != "approved" {
				t.Fatal("UI did not approve the session", err, h.a.status)
			}
			delegated := client.Client{Dir: h.dir, Delegation: d}
			child, _, err := delegated.Request(h.ctx, approval.Operation{Host: "fixture", Kind: "list", Path: "/", Client: "fixture"}, time.Minute)
			if err != nil || child.Status != "approved" || child.ParentID != r.ID {
				t.Fatal("child did not inherit approved scope", err)
			}
			h.a.requests = []approval.Request{*status.Request}
			h.a.approvalTab = 1
			h.ui.Frame()
			h.click("撤销并断开")
			h.wait(func() bool { return !h.a.busy })
			if _, _, err = delegated.Request(h.ctx, approval.Operation{Host: "fixture", Kind: "list", Path: "/", Client: "fixture"}, time.Minute); err == nil {
				t.Fatal("revoked session minted child")
			}
			status, err = h.a.client.Call(h.ctx, broker.Message{Action: "status", ID: child.ID})
			if err != nil || status.Request == nil || status.Request.Status != "revoked" {
				t.Fatal("parent revoke did not reach existing child", err)
			}
		})
	}
}
