package desktop

import (
	"testing"

	"sshclient/internal/vault"
)

func TestEmptyPasswordConnectionDraftAndEditorUI(t *testing.T) {
	h := newConnectionFlowHarness(t)
	h.fillTarget()
	p := h.fixture.Profile()
	h.a.form.Fingerprint = p.Fingerprint
	h.a.connection.trustedTarget = targetID(p.Host, p.Port)
	change, ok := h.a.connectionDraft()
	if !ok || change.KeepCredential || change.Profile.Password != "" || h.a.connection.errors["password"] != "" {
		t.Fatal("empty password draft was rejected", h.a.connection.errors)
	}
	h.ui.Frame()
	if !h.ui.HasText("留空表示使用空密码登录") || h.ui.HasText("请填写服务器登录密码") {
		t.Fatal("empty password guidance or validation is incorrect", h.ui.Texts())
	}

	p.ID = "existing-empty-password"
	p.Name = "Existing connection"
	p.Password = "synthetic saved server password"
	if err := h.a.client.SaveConnection(h.ctx, vault.ProfileChange{Profile: p}, h.server.Vault); err != nil {
		t.Fatal(err)
	}
	h.a.connections = h.server.Vault.Summaries()
	h.a.selected = p.ID
	h.a.editConnection()
	h.ui.Frame()
	change, ok = h.a.connectionDraft()
	if !ok || !change.KeepCredential {
		t.Fatal("editing without replacement did not keep credentials", h.a.connection.errors)
	}
	h.a.connection.replace = true
	change, ok = h.a.connectionDraft()
	if !ok || change.KeepCredential || change.Profile.Password != "" {
		t.Fatal("explicit empty password replacement was not an empty credential", h.a.connection.errors)
	}
}
