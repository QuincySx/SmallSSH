package desktop

import (
	"context"
	"testing"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/desktop/components"
)

func TestConnectionDensityDialogAndFormControls(t *testing.T) {
	for _, width := range []int{560, 1100} {
		for _, dark := range []bool{false, true} {
			a := &app{ctx: context.Background(), online: true}
			a.newConnection()
			tt := ui.NewTester(a.connectionView, width, 760)
			tt.SetDark(dark)
			dialog, d := tt.Find("新建连接对话框")
			left, l := tt.Find("连接分类")
			right, r := tt.Find("连接分类内容")
			footer, f := tt.Find("保存并连接")
			if !d || !l || !r || !f || dialog.W >= 900 || dialog.H >= 720 || dialog.X < 0 || dialog.X+dialog.W > float32(width) || dialog.Y < 0 || dialog.Y+dialog.H > 760 || right.X < left.X+left.W || footer.Y < dialog.Y || footer.Y+footer.H > dialog.Y+dialog.H {
				t.Fatalf("width=%d dark=%v dialog=%v left=%v right=%v footer=%v", width, dark, dialog, left, right, footer)
			}
		}
	}
	a := &app{online: true}
	a.newConnection()
	tt := ui.NewTester(a.connectionGeneralForm, 650, 1100)
	for _, label := range []string{"名称输入", "服务器地址输入", "端口输入", "用户名输入", "登录方式选择"} {
		r, ok := tt.Find(label)
		if !ok || r.H != components.FormHeight {
			t.Fatalf("%s height: %v", label, r)
		}
	}
	for _, label := range []string{"分组输入", "服务器密码输入"} {
		r, ok := tt.Find(label)
		if !ok || r.H != components.FormHeight-2 {
			t.Fatalf("%s inner height: %v", label, r)
		}
	}
	name, _ := tt.Find("名称输入")
	user, _ := tt.Find("用户名输入")
	if name.X != user.X {
		t.Fatal("sections lost their shared field alignment", name, user)
	}
	if err := tt.Click("名称输入"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyTab)
	if !tt.Focused("服务器地址输入") {
		t.Fatal("section heading added a tab stop")
	}
}

func TestConnectionDensityEnglishDraftAndIndependentScroll(t *testing.T) {
	a := &app{ctx: context.Background(), online: true}
	a.newConnection()
	widths := make(map[string]float32)
	tt := ui.NewTester(func(c *ui.Context) {
		for _, section := range connectionSections {
			label := tr(c, section.title)
			widths[label], _ = c.MeasureText(0, ui.Span{Text: label, Size: 13})
		}
		a.connectionView(c)
	}, 560, 480)
	tt.SetLocale("en")
	tt.SetDark(true)
	for _, label := range []string{"Normal", "Acceleration", "Terminal", "Access rules"} {
		text, ok := tt.Find(label)
		if !ok || text.W+0.5 < widths[label] {
			t.Fatalf("English navigation text %q is clipped: %v, natural width %v", label, text, widths[label])
		}
	}
	if !tt.HasText("Connection details") || !tt.HasText("Login identity") {
		t.Fatal("English section headings missing", tt.Texts())
	}
	if err := tt.Click("Name input"); err != nil {
		t.Fatal(err)
	}
	tt.Type("synthetic draft")
	content, ok := tt.Find("Connection category content")
	if !ok {
		t.Fatal("missing category content")
	}
	tt.Scroll(content.X+content.W/2, content.Y+content.H/2, 0, 1000)
	tt.Frame()
	scroll := a.connection.sectionScrolls[connectionSectionIndex(connectionGeneral)].Y
	if scroll <= 0 {
		t.Fatal("general section did not scroll")
	}
	if err := tt.Click("Connection category: Acceleration"); err != nil {
		t.Fatal(err)
	}
	if a.connection.sectionScrolls[connectionSectionIndex(connectionNetwork)].Y != 0 {
		t.Fatal("network inherited the general scroll")
	}
	if err := tt.Click("Connection category: Normal"); err != nil {
		t.Fatal(err)
	}
	if a.form.Name != "synthetic draft" || a.connection.sectionScrolls[connectionSectionIndex(connectionGeneral)].Y != scroll {
		t.Fatal("category switch lost draft or scroll position")
	}
}
