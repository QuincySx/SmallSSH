package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/sagernet/ws"
	"github.com/sagernet/ws/wsutil"
	"sshclient/internal/broker"
	"sshclient/internal/client"
	"sshclient/internal/testserver"
	"sshclient/internal/vault"
)

func TestVNCDialogPrefillsHostSeparatesPortAndOffersDirectOrSSH(t *testing.T) {
	a := &app{online: true, connections: []vault.Summary{{ID: "a", Host: "192.0.2.10"}, {ID: "b", Host: "2001:db8::10"}}}
	a.openVNC("a")
	if a.vnc.address != "192.0.2.10" || a.vnc.port != "5900" || a.vnc.mode != "direct" {
		t.Fatal("VNC defaults did not use the selected server", a.vnc)
	}
	tt := ui.NewTester(a.vncDialog, 680, 700)
	for _, label := range []string{"VNC 主机地址", "VNC 端口", "连接方式选择"} {
		if _, ok := tt.Find(label); !ok {
			t.Fatal("missing VNC field", label)
		}
	}
	if _, ok := tt.Find("打开位置选择"); ok {
		t.Fatal("unrequested presentation selector remains")
	}
	if _, ok := tt.Find("VNC 密码"); ok {
		t.Fatal("asked for credentials before negotiating VNC authentication")
	}
	if err := tt.Click("连接方式选择"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("通过 SSH 通道"); err != nil {
		t.Fatal(err)
	}
	if a.vnc.mode != "ssh" {
		t.Fatal("SSH option is unavailable")
	}
	a.vnc.address, a.vnc.port = "127.0.0.1", "5901"
	a.openVNC("a")
	if a.vnc.address != "127.0.0.1" || a.vnc.port != "5901" {
		t.Fatal("reconnect discarded chosen address")
	}
	canceled := false
	a.vnc.pending = &vncRun{cancel: func() { canceled = true }}
	a.openVNC("b")
	if !canceled || a.vnc.pending != nil || a.vnc.address != "2001:db8::10" || a.vnc.port != "5900" || a.vnc.mode != "direct" {
		t.Fatal("switching servers retained another destination or pending request")
	}
}

func TestVNCTargetCombinesIPv4DNSAndIPv6AndValidatesPort(t *testing.T) {
	for _, test := range []struct{ address, port, want string }{
		{"192.0.2.10", "5900", "192.0.2.10:5900"}, {"vnc.example", "5901", "vnc.example:5901"},
		{"2001:db8::1", "5900", "[2001:db8::1]:5900"}, {" [::1] ", " 5902 ", "[::1]:5902"},
	} {
		got, err := vncTarget(test.address, test.port)
		if err != nil || got != test.want {
			t.Fatal(got, err)
		}
	}
	for _, test := range [][2]string{{"", "5900"}, {"192.0.2.1:5900", "5900"}, {"bad host", "5900"}, {"::1", "0"}, {"127.0.0.1", "65536"}, {"127.0.0.1", "vnc"}} {
		if _, err := vncTarget(test[0], test[1]); err == nil {
			t.Fatal("accepted invalid target", test)
		}
	}
}

type fakeVNCView struct {
	ui.NativeWebView
	destroyed int
}

func (v *fakeVNCView) Destroy() { v.destroyed++ }

func TestVNCTabsKeepIndependentViewsAcrossSelectionDragCanvasAndClose(t *testing.T) {
	a, updates, windows := vncUIFixture(t)
	a.vnc.presentation = "tab"
	var views []*fakeVNCView
	a.newVNCView = func(options mygo.WebViewOptions) (vncEmbeddedView, error) {
		if !strings.HasPrefix(options.URL, "http://127.0.0.1:") || len(options.Page.TrustedOrigins) != 0 {
			t.Fatal("embedded viewer origin was not private")
		}
		v := &fakeVNCView{}
		views = append(views, v)
		return v, nil
	}
	for range 2 {
		a.startVNC()
		select {
		case update := <-updates:
			update()
		case <-time.After(10 * time.Second):
			t.Fatal("VNC tab did not open")
		}
	}
	if len(views) != 2 || len(*windows) != 0 || len(a.vnc.runs) != 2 {
		t.Fatal("VNC did not create independent workspace tabs", a.vnc.message)
	}
	first, second := a.vnc.runs[0], a.vnc.runs[1]
	if first.tab == second.tab || first.tab.kind == second.tab.kind {
		t.Fatal("VNC tabs share identity")
	}
	group := first.tab.group
	a.activateWorkspaceTab(group, workspaceTab{tool: first.tab})
	tt := ui.NewTester(func(c *ui.Context) { a.renderWorkspaceTool(c, group.selectedTool) }, 800, 500)
	if tt.WebViewAt(200, 200) != views[0] {
		t.Fatal("selected tab rendered another VNC view")
	}
	a.dropWorkspaceTab(a.workspaceTabPayload(workspaceTab{tool: second.tab}), group, workspaceTab{}, "right")
	if second.tab.group == first.tab.group || second.view != views[1] {
		t.Fatal("moving tab replaced its remote connection")
	}
	a.setWorkspaceCanvas(true)
	a.setWorkspaceCanvas(false)
	if first.view != views[0] || second.view != views[1] {
		t.Fatal("canvas changed live VNC views")
	}
	a.toggleVNCFullscreen(first)
	if a.vnc.fullscreen != first || first.view != views[0] {
		t.Fatal("fullscreen replaced live view")
	}
	fullscreen := ui.NewTester(a.view, 1100, 760)
	if fullscreen.WebViewAt(500, 400) != views[0] {
		t.Fatal("fullscreen did not fill content")
	}
	a.toggleVNCFullscreen(first)
	if a.vnc.fullscreen != nil || first.view != views[0] {
		t.Fatal("leaving fullscreen lost connection")
	}
	a.toggleVNCFullscreen(first)
	a.closeWorkspaceTab(workspaceTab{tool: first.tab})
	if a.vnc.fullscreen != nil {
		t.Fatal("closing fullscreen tab retained presentation")
	}
	if views[0].destroyed != 1 || views[1].destroyed != 0 || len(a.vnc.runs) != 1 {
		t.Fatal("closing VNC tab affected another viewer")
	}
	a.closeVNCs()
	if views[1].destroyed != 1 || len(a.vnc.runs) != 0 {
		t.Fatal("lock cleanup retained embedded viewer")
	}
}

func TestVNCSavedTargetRestoresAndPasswordIsBoundToDestination(t *testing.T) {
	for _, changed := range []bool{false, true} {
		a, updates, _ := vncUIFixture(t)
		value := vault.VNCSettings{Address: "127.0.0.1:5901", Mode: "direct", Password: "synthetic-saved-password"}
		if err := a.client.SaveVNCSettings(a.ctx, a.vnc.host, value, a.client.Owner); err != nil {
			t.Fatal(err)
		}
		a.closeVNCDialog()
		a.vnc.address = ""
		a.openVNC(a.vnc.host)
		select {
		case fn := <-updates:
			fn()
		case <-time.After(5 * time.Second):
			t.Fatal("saved target did not load")
		}
		if a.vnc.port != "5901" || a.vnc.mode != "direct" {
			t.Fatal("saved VNC target not restored")
		}
		if changed {
			a.vnc.port = "5902"
		}
		a.vnc.presentation = "tab"
		address := ""
		a.newVNCView = func(options mygo.WebViewOptions) (vncEmbeddedView, error) {
			address = options.URL
			return &fakeVNCView{}, nil
		}
		a.startVNC()
		select {
		case fn := <-updates:
			fn()
		case <-time.After(5 * time.Second):
			t.Fatal("viewer did not open")
		}
		response, err := http.Get(address + "config.json")
		if err != nil {
			t.Fatal(err)
		}
		var config struct{ Credentials struct{ Password string } }
		err = json.NewDecoder(response.Body).Decode(&config)
		response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if changed && config.Credentials.Password != "" || !changed && config.Credentials.Password != value.Password {
			t.Fatal("password crossed VNC target boundary")
		}
	}
}

func vncUIFixture(t *testing.T) (*app, chan func(), *[]*fakeToolsWindow) {
	t.Helper()
	ssh := testserver.NewWithForwarding(t)
	dir, err := os.MkdirTemp("", "vnc-ui-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	pass := []byte("synthetic VNC UI fixture password")
	source, err := vault.New(pass)
	if err != nil {
		t.Fatal(err)
	}
	p := ssh.Profile()
	source.Put(p)
	if err := vault.Save(vault.Path(dir), source, true); err != nil {
		t.Fatal(err)
	}
	source.Close()
	server, err := broker.Open(dir, pass)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { server.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	updates := make(chan func(), 32)
	a := &app{ctx: ctx, dir: dir, online: true, server: testDesktopSession(server), client: client.Client{Dir: dir, Owner: server.Vault}, connections: []vault.Summary{{ID: p.ID, Name: "VNC 测试服务器", Host: p.Host, Port: p.Port}}, dispatch: func(fn func()) { updates <- fn }}
	var windows []*fakeToolsWindow
	a.newVNCWindow = func(options mygo.WindowOptions, closed func()) toolsWindow {
		w := &fakeToolsWindow{options: options, onClosed: closed, title: options.Title}
		windows = append(windows, w)
		return w
	}
	a.openVNC(p.ID)
	if a.vnc.loading {
		select {
		case update := <-updates:
			update()
		case <-time.After(5 * time.Second):
			t.Fatal("VNC settings lookup did not finish")
		}
	}
	a.vnc.presentation = "window"
	t.Cleanup(a.closeVNCs)
	return a, updates, &windows
}

func TestVNCDirectAndSSHTunnelLockCloseViewerAndSockets(t *testing.T) {
	for _, mode := range []string{"direct", "ssh"} {
		t.Run(mode, func(t *testing.T) {
			a, updates, windows := vncUIFixture(t)
			listener, err := net.Listen("tcp4", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { listener.Close() })
			targetClosed := make(chan struct{})
			go func() {
				defer close(targetClosed)
				conn, err := listener.Accept()
				if err != nil {
					return
				}
				defer conn.Close()
				io.Copy(conn, conn)
			}()
			a.vnc.address, a.vnc.port, err = net.SplitHostPort(listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			a.vnc.mode, a.vnc.viewOnly = mode, true
			if mode == "direct" {
				a.client = client.Client{Dir: t.TempDir()}
			}
			a.startVNC()
			select {
			case update := <-updates:
				update()
			case <-time.After(15 * time.Second):
				t.Fatal("VNC SSH tunnel did not complete")
			}
			if len(*windows) != 1 || a.vnc.open || a.vnc.pending != nil || len(a.vnc.runs) != 1 {
				t.Fatal("VNC did not open its isolated viewer", a.vnc.message)
			}
			w := (*windows)[0]
			endpoint, err := url.Parse(w.options.URL)
			if err != nil || endpoint.Hostname() != "127.0.0.1" || w.shown != 1 || len(w.options.Page.TrustedOrigins) != 0 {
				t.Fatal("VNC window did not remain a local untrusted page", w.options)
			}
			origin := endpoint.Scheme + "://" + endpoint.Host
			endpoint.Path += "socket"
			endpoint.Scheme = "ws"
			dialer := ws.Dialer{Header: ws.HandshakeHeaderHTTP(http.Header{"Origin": []string{origin}})}
			conn, _, _, err := dialer.Dial(context.Background(), endpoint.String())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { conn.Close() })
			conn.SetDeadline(time.Now().Add(5 * time.Second))
			if err := wsutil.WriteClientBinary(conn, []byte("RFB over SSH")); err != nil {
				t.Fatal(err)
			}
			payload, op, err := wsutil.ReadServerData(conn)
			if err != nil || op != ws.OpBinary || string(payload) != "RFB over SSH" {
				t.Fatal("VNC relay did not traverse the real SSH forwarding fixture", string(payload), err)
			}
			a.lock()
			w.finishPendingUpdates()
			if w.closed != 1 || w.hidden != 1 || a.vnc.open || len(a.vnc.runs) != 0 {
				t.Fatal("lock left a VNC viewer open")
			}
			select {
			case <-targetClosed:
			case <-time.After(5 * time.Second):
				t.Fatal("lock leaked the VNC target socket")
			}
			if _, _, err := wsutil.ReadServerData(conn); err == nil {
				t.Fatal("lock left the viewer WebSocket connected")
			}
		})
	}
}

func TestVNCCancelPendingOpenRejectsLateViewer(t *testing.T) {
	a, updates, windows := vncUIFixture(t)
	a.startVNC()
	select {
	case update := <-updates:
		a.closeVNCDialog()
		update()
	case <-time.After(15 * time.Second):
		t.Fatal("VNC setup did not return")
	}
	if len(*windows) != 0 || a.vnc.open || a.vnc.pending != nil || len(a.vnc.runs) != 0 {
		t.Fatal("cancelled VNC setup opened a late viewer")
	}
}

func TestVNCDisconnectedViewerReleasesSlotAndRetainsReconnectTarget(t *testing.T) {
	updates := make(chan func(), 2)
	a := &app{online: true, generation: 3, dispatch: func(fn func()) { updates <- fn }}
	a.vnc.host, a.vnc.address, a.vnc.port, a.vnc.mode = "server", "127.0.0.1", "5901", "ssh"
	var ended *vncRun
	for range 8 {
		_, cancel := context.WithCancel(context.Background())
		run := &vncRun{cancel: cancel}
		a.vnc.runs = append(a.vnc.runs, run)
		if ended == nil {
			ended = run
		}
		t.Cleanup(cancel)
	}
	window := &fakeToolsWindow{onClosed: func() { a.stopVNC(ended) }}
	ended.window = window
	a.finishVNC(ended, a.generation, context.Background(), errors.New("synthetic tunnel disconnect"))
	select {
	case fn := <-updates:
		fn()
	case <-time.After(3 * time.Second):
		t.Fatal("disconnect did not reach UI")
	}
	window.finishPendingUpdates()
	if len(a.vnc.runs) != 7 || window.closed != 1 || window.hidden != 1 || ended.window != nil {
		t.Fatal("disconnected viewer retained its window slot", len(a.vnc.runs), window.closed)
	}
	if !strings.Contains(a.status, "synthetic tunnel disconnect") {
		t.Fatal("disconnect reason was not retained", a.status)
	}
	a.openVNC("server")
	if (a.vnc.address != "127.0.0.1" || a.vnc.port != "5901" || a.vnc.mode != "ssh") || !a.vnc.open {
		t.Fatal("existing reconnect entry lost the selected target")
	}
	// The queued native close callback and a repeated transport completion are harmless.
	a.finishVNC(ended, a.generation, context.Background(), errors.New("late completion"))
	(<-updates)()
	if len(a.vnc.runs) != 7 || window.closed != 1 {
		t.Fatal("repeated completion closed another viewer")
	}
}

func TestVNCOldGenerationCannotCloseCurrentViewer(t *testing.T) {
	updates := make(chan func(), 1)
	a := &app{online: true, generation: 4, dispatch: func(fn func()) { updates <- fn }}
	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	run := &vncRun{cancel: cancel}
	window := &fakeToolsWindow{onClosed: func() { a.stopVNC(run) }}
	run.window = window
	a.vnc.runs = []*vncRun{run}
	a.finishVNC(run, 3, context.Background(), errors.New("stale disconnect"))
	(<-updates)()
	if len(a.vnc.runs) != 1 || window.hidden != 0 || run.window == nil || a.status != "" {
		t.Fatal("stale transport completion changed current owner state")
	}
}

func TestVNCNativeControlsRejectStaleQueuedActions(t *testing.T) {
	for _, kind := range []string{"live", "locked", "generation", "closed", "cancelled"} {
		t.Run(kind, func(t *testing.T) {
			updates := make(chan func(), 1)
			a := &app{online: true, generation: 7, dispatch: func(fn func()) { updates <- fn }}
			run := &vncRun{}
			a.vnc.runs = []*vncRun{run}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			type result struct {
				text string
				err  error
			}
			done := make(chan result, 1)
			calls := 0
			go func() {
				text, err := a.vncControl(ctx, ctx, run, 7, func() (string, error) { calls++; return "synthetic", nil })
				done <- result{text, err}
			}()
			var update func()
			select {
			case update = <-updates:
			case <-time.After(time.Second):
				t.Fatal("action was not queued")
			}
			switch kind {
			case "locked":
				a.online = false
			case "generation":
				a.generation++
			case "closed":
				a.vnc.runs = nil
			case "cancelled":
				cancel()
			}
			update()
			value := <-done
			if kind == "live" {
				if calls != 1 || value.err != nil || value.text != "synthetic" {
					t.Fatal("live action failed", value)
				}
			} else if calls != 0 || value.err == nil || value.text != "" {
				t.Fatal("stale action reached native authority", value)
			}
		})
	}
}
