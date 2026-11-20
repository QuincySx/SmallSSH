package desktop

import (
	"context"
	"testing"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/connectcfg"
)

func TestConnectionSectionsKeepDraftAndActionsVisible(t *testing.T) {
	for _, dimensions := range [][2]int{{560, 760}, {1000, 760}} {
		a := &app{ctx: context.Background(), online: true}
		a.newConnection()
		tt := ui.NewTester(a.connectionView, dimensions[0], dimensions[1])
		if a.connection.section != connectionGeneral || !tt.Focused("名称输入") {
			t.Fatal("new server did not focus Normal", tt.Texts())
		}
		if err := tt.Click("连接分类：加速与代理"); err != nil {
			t.Fatal(err)
		}
		if a.connectionHasChanges() || !tt.HasText("网络代理") || tt.HasText("服务器地址输入") || tt.HasText("字符编码选择") {
			t.Fatal("category selection changed the draft or combined pages", tt.Texts())
		}
		// Use synthetic draft values; switching must not run a probe, access
		// credentials or discard fields that are not currently rendered.
		a.form.Name, a.form.Host, a.form.User, a.form.Password = "draft server", "192.0.2.10", "example", "synthetic draft password"
		a.form.Network.Proxy = connectcfg.Proxy{Mode: "socks5", Address: "127.0.0.1:1080", User: "demo", Password: "synthetic proxy password"}
		for _, title := range []string{"终端设置", "访问策略", "常规", "加速与代理"} {
			if err := tt.Click("连接分类：" + title); err != nil {
				t.Fatal(err)
			}
			left, leftOK := tt.Find("连接分类")
			right, rightOK := tt.Find("连接分类内容")
			save, saveOK := tt.Find("保存连接")
			if !leftOK || !rightOK || !saveOK || right.X < left.X+left.W || save.Y+save.H > float32(dimensions[1]) {
				t.Fatal("split navigation or fixed action escaped", left, right, save)
			}
			if a.form.Password != "synthetic draft password" || a.form.Network.Proxy.Password != "synthetic proxy password" {
				t.Fatal("switch discarded credentials")
			}
		}
		navigation, _ := tt.Find("连接分类")
		button, _ := tt.Find("连接分类：常规")
		footer, _ := tt.Find("保存连接")
		content, _ := tt.Find("连接分类内容")
		tt.Scroll(content.X+content.W/2, content.Y+content.H/2, 0, 1000)
		tt.Frame()
		leftAfter, _ := tt.Find("连接分类")
		buttonAfter, _ := tt.Find("连接分类：常规")
		footerAfter, _ := tt.Find("保存连接")
		if navigation != leftAfter || button != buttonAfter || footer != footerAfter {
			t.Fatal("content scroll moved navigation or save")
		}
		if a.connection.phase != "" || a.prompt.open {
			t.Fatal("switching triggered an operation")
		}
	}
}

func TestConnectionSectionKeyboardAndValidationRevealTheRelevantPage(t *testing.T) {
	a := &app{ctx: context.Background(), online: true}
	a.newConnection()
	tt := ui.NewTester(a.connectionView, 900, 760)
	if err := tt.Click("连接分类：常规"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyDown)
	if a.connection.section != connectionGeneral {
		t.Fatal("arrow key applied a category before activation")
	}
	tt.Key(0, ui.KeyEnter)
	if a.connection.section != connectionNetwork || !tt.Focused("连接分类：加速与代理") {
		t.Fatal("keyboard category activation failed", tt.Texts())
	}
	if err := tt.Click("保存连接"); err != nil {
		t.Fatal(err)
	}
	if a.connection.section != connectionGeneral || !tt.HasText("请检查服务器地址和端口") {
		t.Fatal("validation left missing fields hidden", tt.Texts())
	}
	a.form.Name, a.form.Host, a.form.User, a.form.Password = "valid draft", "192.0.2.10", "example", "synthetic password"
	a.form.Network.Proxy = connectcfg.Proxy{Mode: "socks5", Address: "invalid-address"}
	if err := tt.Click("保存连接"); err != nil {
		t.Fatal(err)
	}
	if a.connection.section != connectionNetwork || a.connection.phase != "" {
		t.Fatal("invalid network options did not reveal their page", a.connection.errors)
	}
	a.form.Network.Proxy = connectcfg.Proxy{}
	a.form.Terminal.Encoding = "unsupported-encoding"
	if err := tt.Click("保存连接"); err != nil {
		t.Fatal(err)
	}
	if a.connection.section != connectionTerminal || a.connection.phase != "" {
		t.Fatal("invalid terminal options did not reveal their page", a.connection.errors)
	}
	a.form.Terminal.Encoding = ""
	a.form.Network.KeepaliveSeconds = -1
	if err := tt.Click("连接分类：加速与代理"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("保存连接"); err != nil {
		t.Fatal(err)
	}
	if a.connection.section != connectionTerminal || a.connection.phase != "" {
		t.Fatal("keepalive error revealed the wrong page")
	}
	a.connection.phase = "save"
	tt.Frame()
	if err := tt.Click("连接分类：常规"); err != nil {
		t.Fatal(err)
	}
	if a.connection.section != connectionTerminal {
		t.Fatal("category navigation changed a pending atomic save")
	}
}
