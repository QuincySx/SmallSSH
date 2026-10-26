//go:build terminal_integration

package desktop

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/approval"
	"sshclient/internal/broker"
	"sshclient/internal/client"
	"sshclient/internal/desktop/components"
	"sshclient/internal/localipc"
	"sshclient/internal/testserver"
	"sshclient/internal/vault"
)

func TestConnectionUISaveAndConnectCreatesWorkingTerminal(t *testing.T) {
	f := testserver.New(t)
	dir, err := os.MkdirTemp("", "conn-terminal-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	if err = localipc.Prepare(dir); err != nil {
		t.Fatal(err)
	}
	pass := []byte("full UI and terminal test password")
	v, err := vault.New(pass)
	if err != nil {
		t.Fatal(err)
	}
	if err = vault.Save(broker.VaultPath(dir), v, true); err != nil {
		t.Fatal(err)
	}
	v.Close()
	server, err := broker.Open(dir, pass)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	updates := make(chan func(), 64)
	a := &app{ctx: ctx, dir: dir, client: client.Client{Dir: dir}, online: true, server: server, dispatch: func(fn func()) { updates <- fn }}
	a.newConnection()
	a.form = f.Profile()
	a.form.ID = "new-ui-terminal"
	a.port = strconv.Itoa(a.form.Port)
	a.connection.trustedTarget = targetID(a.form.Host, a.form.Port)
	a.saveProfile()
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	for len(a.sessions) == 0 {
		select {
		case update := <-updates:
			update()
		case <-deadline.C:
			t.Fatal("save and connect did not open terminal", a.connection.errors)
		}
	}
	term := a.sessions[0].term
	defer term.Close()
	term.Send([]byte("saved connection · 中文 echo"))
	for !strings.Contains(term.Text(), "saved connection · 中文 echo") {
		select {
		case update := <-updates:
			update()
		case <-time.After(10 * time.Millisecond):
		case <-deadline.C:
			t.Fatal("saved terminal did not echo", term.Text())
		}
	}
	if a.page != 1 || a.form.Password != "" || a.ownerPass != "" {
		t.Fatal("success retained secrets or did not enter workspace")
	}
	if f.Auths.Load() != 1 {
		t.Fatal("save and connect required another SSH login")
	}
	if a.connection.cancel != nil {
		t.Fatal("editor still owns the active terminal cancellation")
	}
	a.newConnection()
	a.cancelConnectionEditor()
	select {
	case <-term.Done():
		t.Fatal("opening another form cancelled the existing terminal")
	default:
	}
}

func (h *connectionFlowHarness) expectUITerminal() {
	h.t.Helper()
	h.wait(func() bool { return len(h.a.sessions) > 0 || h.a.connection.phase == "" })
	if len(h.a.sessions) != 1 || h.a.page != 1 {
		h.t.Fatal("save and connect did not open one terminal", h.a.connection.errors)
	}
	term := h.a.sessions[0].term
	marker := "UI login passed · 中文 ' $ ` \\"
	term.Send([]byte(marker))
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	for !strings.Contains(term.Text(), marker) {
		select {
		case update := <-h.updates:
			update()
		case <-time.After(5 * time.Millisecond):
		case <-timer.C:
			h.t.Fatal("terminal did not echo after UI login")
		}
	}
	if h.a.form.Password != "" || h.a.form.KeyPassphrase != "" || h.a.ownerPass != "" || h.a.keyFile != "" {
		h.t.Fatal("successful login retained secret fields")
	}
}

func TestConnectionFormRetriesWrongPasswordWithoutDuplicatingConnection(t *testing.T) {
	h := newConnectionFlowHarness(t)
	h.fillTarget()
	h.input("服务器密码", "deliberately incorrect test password")
	h.confirmIdentity()
	id := h.a.form.ID
	h.click("信任并连接")
	h.idle()
	if len(h.a.sessions) != 0 || h.a.page != 0 || !h.ui.HasText("服务器未接受登录凭据") || !h.ui.HasText("连接已保存；登录未成功") {
		t.Fatal("authentication failure did not return to editable form", h.a.connection.errors)
	}
	h.input("服务器密码", h.fixture.Profile().Password)
	h.click("保存并连接")
	h.expectUITerminal()
	if h.a.sessions[0].host != id || len(h.a.connections) != 1 {
		t.Fatal("retry duplicated saved connection")
	}
	if h.stored(id).Password != h.fixture.Profile().Password {
		t.Fatal("retry did not replace saved password")
	}
}

func TestConnectionFormImportedKeysSaveAndConnect(t *testing.T) {
	for _, encrypted := range []bool{false, true} {
		name := "plain"
		if encrypted {
			name = "encrypted"
		}
		t.Run(name, func(t *testing.T) {
			h := newConnectionFlowHarness(t)
			h.fillTarget()
			h.selectAuth("已有私钥")
			path, _, phrase := generatedKeyFile(t, h.fixture, encrypted)
			h.a.keyPicker = func() ([]string, error) { return []string{path}, nil }
			h.input("私钥口令", phrase)
			h.click("选择私钥文件…")
			h.idle()
			if h.a.connection.keyInfo == nil {
				t.Fatal("key selection did not complete", h.a.connection.errors)
			}
			h.confirmIdentity()
			h.click("信任并连接")
			h.expectUITerminal()
			id := h.a.sessions[0].host
			h.a.selected = id
			h.a.editConnection()
			h.ui.Frame()
			if h.a.keyFile != "" || h.a.form.PrivateKey != "" || !h.ui.HasText("更换登录凭据") {
				t.Fatal("editing exposed stored private key")
			}
			h.click("取消")
		})
	}
}

func TestConnectionFormChangedHostIdentityRequiresExplicitRecheck(t *testing.T) {
	h := newConnectionFlowHarness(t)
	p := h.fixture.Profile()
	p.Fingerprint = "SHA256:previous-server-identity-before-reinstallation"
	owner, err := vault.Read(broker.VaultPath(h.dir), h.pass)
	if err != nil {
		t.Fatal(err)
	}
	err = h.a.client.SaveConnection(h.ctx, vault.ProfileChange{Profile: p}, owner)
	owner.Close()
	if err != nil {
		t.Fatal(err)
	}
	h.a.connections = h.server.Vault.Summaries()
	h.a.selected = p.ID
	h.a.editConnection()
	h.ui.Frame()
	h.click("保存后连接")
	h.click("保存并连接")
	h.idle()
	if h.fixture.Auths.Load() != 0 || !h.ui.HasText("服务器身份与已保存记录不同") || len(h.a.sessions) != 0 {
		t.Fatal("changed identity was authenticated or missing recovery", h.a.connection.errors)
	}
	h.click("重新核对服务器身份")
	h.confirmIdentity()
	if h.fixture.Auths.Load() != 0 {
		t.Fatal("rechecking identity sent login credentials")
	}
	h.click("信任并连接")
	h.expectUITerminal()
	if h.stored(p.ID).Fingerprint != h.fixture.Profile().Fingerprint {
		t.Fatal("verified identity was not saved")
	}
}

func TestUnlockedDesktopDoubleClickConnectsWithoutPasswordAndCloseStopsStream(t *testing.T) {
	h := newConnectionFlowHarness(t)
	p := h.fixture.Profile()
	p.Name = "已保存的服务器"
	if err := h.a.client.SaveConnection(h.ctx, vault.ProfileChange{Profile: p}, h.server.Vault); err != nil {
		t.Fatal(err)
	}
	pending, _, err := h.a.client.Request(h.ctx, approval.Operation{Host: p.ID, Kind: "exec", Command: "printf external", Client: "unverified AI"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	h.a.connections = h.server.Vault.Summaries()
	h.a.page = 1
	h.a.sidebarWidth = 190
	h.a.toolsHidden = true
	h.ui = ui.NewTester(h.a.view, 1000, 760)
	h.click(p.Name)
	if len(h.a.sessions) != 0 || len(h.a.connecting) != 0 {
		t.Fatal("single sidebar click started a connection")
	}
	h.click(p.Name)
	h.wait(func() bool { return len(h.a.sessions) > 0 })
	h.expectUITerminal()
	if h.ui.HasText("服务器密码输入") || h.a.page != 1 || h.a.approvalWin != nil || h.a.ownerPass != "" {
		t.Fatal("direct action left form or required duplicate auth")
	}
	state, err := h.a.client.Call(h.ctx, broker.Message{Action: "status", ID: pending.ID})
	if err != nil || state.Request == nil || state.Request.Status != "pending" {
		t.Fatal("unrelated AI request approved", err)
	}
	term := h.a.sessions[0].term
	if err = h.ui.Click("关闭标签：" + p.Name); err != nil {
		t.Fatal(err)
	}
	if len(h.a.sessions) != 0 {
		r, _ := h.ui.Find("关闭标签：" + p.Name)
		terminalRect, _ := h.ui.Find("Terminal")
		t.Fatal("tab close did not remove session", len(h.a.sessions), h.a.active, r, terminalRect, h.fixture.Auths.Load(), h.ui.Texts())
	}
	select {
	case <-term.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("tab close left SSH terminal running")
	}
}

func TestPendingConnectionDoesNotDiscardNewDraftAndReconnectReplacesOldTab(t *testing.T) {
	h := newConnectionFlowHarness(t)
	p := h.fixture.Profile()
	if err := h.a.client.SaveConnection(h.ctx, vault.ProfileChange{Profile: p}, h.server.Vault); err != nil {
		t.Fatal(err)
	}
	h.a.connections = h.server.Vault.Summaries()
	h.a.selected = p.ID
	h.a.page = 1
	h.a.openShell()
	h.a.openShell() // A repeated gesture during login is coalesced.
	h.a.newConnection()
	h.a.form.Name = "keep my draft"
	h.a.form.Password = "unsaved synthetic credential"
	h.wait(func() bool { return len(h.a.sessions) == 1 })
	if h.a.page != 0 || h.a.form.Name != "keep my draft" || h.a.form.Password == "" || h.fixture.Auths.Load() != 1 {
		t.Fatal("late connection replaced active form or duplicated login")
	}
	h.a.cancelConnectionEditor()
	h.a.selected = p.ID
	h.a.openShell()
	if len(h.a.sessions) != 1 || h.fixture.Auths.Load() != 1 {
		t.Fatal("existing terminal was duplicated")
	}
	old := h.a.sessions[0]
	old.term.Close()
	h.wait(func() bool { return old.closed })
	h.a.openShell()
	h.wait(func() bool { return len(h.a.sessions) == 1 && h.a.sessions[0] != old })
	if h.fixture.Auths.Load() != 2 || h.a.sessions[0].closed {
		t.Fatal("reconnection did not replace old tab")
	}
}

func TestRepeatedConnectionRailDoubleClicksCreateIndependentTerminals(t *testing.T) {
	for _, compact := range []bool{false, true} {
		t.Run(fmt.Sprint(compact), func(t *testing.T) {
			h := newConnectionFlowHarness(t)
			p := h.fixture.Profile()
			p.Name = "重复打开的本机服务器"
			other := p
			other.ID, other.Name = "other-sidebar-server", "仅选中的另一服务器"
			for _, profile := range []vault.Profile{p, other} {
				if err := h.a.client.SaveConnection(h.ctx, vault.ProfileChange{Profile: profile}, h.server.Vault); err != nil {
					t.Fatal(err)
				}
			}
			h.a.connections = h.server.Vault.Summaries()
			h.a.page = 1
			h.a.ensurePanes()
			width := 248
			if compact {
				width = int(components.RailWidth)
			}
			h.ui = ui.NewTester(func(c *ui.Context) { h.a.connectionSidebar(c, compact) }, width, 360)
			const count = 3
			for i := range count {
				if i > 0 {
					h.ui.Advance(time.Second)
				}
				h.click(p.Name)
				h.click(p.Name)
			}
			// Completion is queued through dispatch, so all three gestures
			// arrive before the first login can attach its terminal.
			if len(h.a.sessions) != 0 || len(h.a.pendingShellOpens[p.ID]) != count-1 {
				t.Fatal("double-click gestures were coalesced", len(h.a.sessions), len(h.a.pendingShellOpens[p.ID]), len(h.a.connecting), h.a.selected, h.a.status, h.a.page, h.a.prompt.open)
			}
			h.click(other.Name)
			h.wait(func() bool { return len(h.a.sessions) == count })
			if h.fixture.Auths.Load() != count || h.a.selected != other.ID || h.a.primaryFilePane.endpoint != p.ID || h.a.approvalWin != nil || h.a.ownerPass != "" || h.ui.HasText("服务器密码输入") || len(h.a.connecting) != 0 || len(h.a.pendingShellOpens) != 0 {
				t.Fatal("new terminals changed sidebar selection or required extra authorization", h.fixture.Auths.Load(), h.a.selected, h.a.primaryFilePane.endpoint)
			}
			closed := h.a.sessions[1]
			h.a.closeSessionAt(1)
			h.ui.Advance(time.Second)
			h.click(p.Name)
			h.click(p.Name)
			h.wait(func() bool { return len(h.a.sessions) == count })
			if h.a.sessions[count-1].term == closed.term || h.a.sessions[count-1].requestID == closed.requestID || h.fixture.Auths.Load() != count+1 {
				t.Fatal("reopening a same-server tab reused the closed terminal")
			}
			seenRequests, seenNames := map[string]bool{}, map[string]bool{}
			for i, s := range h.a.sessions {
				if s.host != p.ID || seenRequests[s.requestID] || seenNames[s.name] || s.term == nil {
					t.Fatal("same-host tabs shared a terminal request or visible name")
				}
				seenRequests[s.requestID], seenNames[s.name] = true, true
				marker := fmt.Sprintf("independent-PTY-%d", i)
				s.term.Send([]byte(marker))
				deadline := time.NewTimer(5 * time.Second)
				for !strings.Contains(s.term.Text(), marker) {
					select {
					case update := <-h.updates:
						update()
					case <-time.After(5 * time.Millisecond):
					case <-deadline.C:
						t.Fatal("new terminal did not echo", marker)
					}
				}
				deadline.Stop()
				for _, sibling := range h.a.sessions {
					if sibling != s && (sibling.term == s.term || strings.Contains(sibling.term.Text(), marker)) {
						t.Fatal("input was shared across independent terminal tabs", marker)
					}
				}
			}
		})
	}
}

func TestCancelPendingTerminalOpensDropsQueuedNewTabs(t *testing.T) {
	h := newConnectionFlowHarness(t)
	p := h.fixture.Profile()
	if err := h.a.client.SaveConnection(h.ctx, vault.ProfileChange{Profile: p}, h.server.Vault); err != nil {
		t.Fatal(err)
	}
	h.a.page, h.a.selected = 1, p.ID
	h.a.openShellMode(true)
	h.a.openShellMode(true)
	if len(h.a.pendingShellOpens[p.ID]) != 1 {
		t.Fatal("second explicit new tab did not queue")
	}
	h.a.connecting[p.ID]()
	h.wait(func() bool { return len(h.a.connecting) == 0 })
	if len(h.a.sessions) != 0 || len(h.a.pendingShellOpens) != 0 {
		t.Fatal("canceled request attached or opened a queued terminal")
	}
	h.a.openShellMode(true)
	h.wait(func() bool { return len(h.a.sessions) == 1 })
}

func TestFailedTerminalOpenDropsQueuedRequests(t *testing.T) {
	h := newConnectionFlowHarness(t)
	h.a.page, h.a.selected = 1, "missing-test-connection"
	h.a.openShellMode(true)
	h.a.openShellMode(true)
	h.wait(func() bool { return len(h.a.connecting) == 0 })
	if len(h.a.sessions) != 0 || len(h.a.pendingShellOpens) != 0 || h.a.status == "" {
		t.Fatal("failed login retained or retried queued terminal requests")
	}
}

func TestCancelledTerminalOpenKeepsLaterNewRequest(t *testing.T) {
	h := newConnectionFlowHarness(t)
	p := h.fixture.Profile()
	if err := h.a.client.SaveConnection(h.ctx, vault.ProfileChange{Profile: p}, h.server.Vault); err != nil {
		t.Fatal(err)
	}
	h.a.page, h.a.selected = 1, p.ID
	h.a.openShellMode(true)
	h.a.openShellMode(true)
	h.a.connecting[p.ID]()
	if len(h.a.pendingShellOpens) != 0 {
		t.Fatal("cancel retained an earlier queued request")
	}
	// Retry before the canceled result is dispatched. Its completion must
	// neither discard this later gesture nor remove the retry's cancel entry.
	h.a.openShellMode(true)
	h.wait(func() bool { return len(h.a.sessions) == 1 && len(h.a.connecting) == 0 })
	if len(h.a.pendingShellOpens) != 0 || h.a.sessions[0].closed || h.a.sessions[0].host != p.ID {
		t.Fatal("canceled completion discarded or closed the later terminal")
	}
}
