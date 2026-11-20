package desktop

import (
	"context"
	"net"
	"strconv"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestConnectionProbeFailureShowsCauseAndKeepsDraft(t *testing.T) {
	h := newConnectionFlowHarness(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err = listener.Close(); err != nil {
		t.Fatal(err)
	}
	h.a.form.Name = "Local connection"
	h.a.form.Host = "127.0.0.1"
	h.a.form.User = "fixture"
	h.a.form.Auth = "password"
	h.a.form.Password = "isolated draft credential"
	h.a.port = strconv.Itoa(port)
	h.ui = ui.NewTester(h.a.view, 1100, 900)
	h.saveOnly()
	id := h.a.connections[0].ID
	if h.a.selected != "" || h.fixture.Auths.Load() != 0 {
		t.Fatal("saving activated a connection")
	}
	h.a.openShellForHostAfter(id, true, nil)
	h.wait(func() bool { return len(h.a.sessions) > 0 && h.a.sessions[0].connectionState == "error" })
	message := h.a.sessions[0].connectionError
	if !strings.Contains(message, "服务器拒绝连接") {
		t.Fatal("explicit connect hid network failure", message)
	}
	if len(h.server.Vault.Summaries()) != 1 || h.a.prompt.open || h.fixture.Auths.Load() != 0 {
		t.Fatal("failed connect altered saved configuration or authenticated")
	}

}

func TestConnectionProbeErrorTextIsBoundedAndEscaped(t *testing.T) {
	for _, width := range []int{560, 1100} {
		a := &app{ctx: context.Background(), online: true}
		a.newConnection()
		message := "未能读取 SSH 主机身份 127.0.0.1:22（SSH 握手）: \n\x1b[2J\u202e" + strings.Repeat("x", 4096) + "undisplayed-error-tail"
		a.connection.errors["trust"] = message
		tt := ui.NewTester(a.view, width, 900)
		expected := reviewText(string([]rune(message)[:1024]) + "…")
		if !tt.HasText(expected) || tt.HasText("undisplayed-error-tail") || a.connection.errors["trust"] != message {
			t.Fatal("connection error display lost its safe bound or changed the original")
		}
		if strings.ContainsAny(strings.Join(tt.Texts(), ""), "\x1b\u202e") {
			t.Fatal("server controls reached the connection form")
		}
		bounds, ok := tt.Find(expected)
		if !ok || bounds.H < 12 || bounds.H > 60 {
			t.Fatal("network error display did not have a bounded readable height", width, bounds)
		}
		for _, label := range []string{"取消", "保存连接"} {
			bounds, ok := tt.Find(label)
			if !ok || bounds.H < 30 || bounds.X < 0 || bounds.Y < 0 || bounds.X+bounds.W > float32(width+1) || bounds.Y+bounds.H > 901 {
				t.Fatal("long network error pushed connection controls out of view", width, label, bounds)
			}
		}
	}
}
