package desktop

import (
	"context"
	"fmt"
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
	"sshclient/docs/design"
	"sshclient/internal/desktop/components"
)

func TestConnectionGroupChevronIsVisuallyCentered(t *testing.T) {
	for _, dark := range []bool{false, true} {
		a := &app{ctx: context.Background(), online: true}
		a.newConnection()
		tt := ui.NewTester(a.view, 1100, 760)
		tt.SetLocale("zh-Hans")
		tt.SetDark(dark)
		button, found := tt.Find("展开分组")
		field, fieldFound := tt.Find("分组输入")
		size := design.Length("size-native-field-action")
		if !found || !fieldFound || button.W != size || button.H != size || button.Y+button.H/2 != field.Y+field.H/2 {
			t.Fatalf("dark=%t dropdown %v is not centered in field %v at design size %v", dark, button, field, size)
		}

		// The old text arrow passed the button bounds check. Measure the
		// painted arrow inside the button to also catch a baseline shift.
		frame := tt.Image()
		left, top := int(button.X)+4, int(button.Y)+4
		right, bottom := int(button.X+button.W)-4, int(button.Y+button.H)-4
		bg := frame.RGBAAt(left, top)
		minY, maxY := bottom, top-1
		for y := top; y < bottom; y++ {
			for x := left; x < right; x++ {
				ink := frame.RGBAAt(x, y)
				if math.Abs(float64(ink.R)-float64(bg.R)) > 32 {
					minY, maxY = min(minY, y), max(maxY, y)
				}
			}
		}
		inkCenter := float64(minY+maxY+1) / 2
		fieldCenter := float64(field.Y + field.H/2)
		if maxY < minY || math.Abs(inkCenter-fieldCenter) > 1 {
			t.Fatalf("dark=%t chevron ink y=%d..%d, field center %.1f", dark, minY, maxY, fieldCenter)
		}
	}
}

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
			footer, f := tt.Find("保存连接")
			if !d || !l || !r || !f || dialog.W >= 900 || dialog.H >= 720 || dialog.X < 0 || dialog.X+dialog.W > float32(width) || dialog.Y < 0 || dialog.Y+dialog.H > 760 || right.X < left.X+left.W || footer.Y < dialog.Y || footer.Y+footer.H > dialog.Y+dialog.H {
				t.Fatalf("width=%d dark=%v dialog=%v left=%v right=%v footer=%v", width, dark, dialog, left, right, footer)
			}
			cancel, _ := tt.Find("取消")
			if cancel.X < dialog.X+dialog.W/3 || footer.X < cancel.X+cancel.W {
				t.Fatal("dialog actions are split between the left and right edges", cancel, footer)
			}
			choice, _ := tt.Find("连接分类：常规")
			if choice.X-left.X < components.DialogSidebarInset || left.X+left.W-choice.X-choice.W < components.DialogSidebarInset {
				t.Fatal("category touches the frame", left, choice)
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

func TestConnectionFooterWrapsLongActionsInsideTheDialog(t *testing.T) {
	for _, width := range []int{560, 1000} {
		for _, locale := range []string{"zh-Hans", "en"} {
			t.Run(locale+"-"+fmt.Sprint(width), func(t *testing.T) {
				t.Setenv("MYGO_LOCALE", locale)
				a := &app{online: true}
				a.newConnection()
				a.connection.phase = "save"
				var dialogLabel string
				var labels []string
				tt := ui.NewTester(func(c *ui.Context) {
					dialogLabel = tr(c, "新建连接") + tr(c, "对话框")
					labels = []string{tr(c, "取消"), tr(c, "正在保存…")}
					a.connectionView(c)
				}, width, 480)
				tt.SetLocale(locale)
				dialog, ok := tt.Find(dialogLabel)
				if !ok {
					t.Fatal("dialog missing", dialogLabel)
				}
				var actions []ui.Rect
				for _, label := range labels {
					r, found := tt.Find(label)
					if !found || r.X < dialog.X+12 || r.X+r.W > dialog.X+dialog.W-12+0.5 || r.Y+r.H > dialog.Y+dialog.H-12+0.5 {
						t.Fatal("action escaped the dialog", label, r, dialog)
					}
					for _, earlier := range actions {
						if r.X < earlier.X+earlier.W && r.X+r.W > earlier.X && r.Y < earlier.Y+earlier.H && r.Y+r.H > earlier.Y {
							t.Fatal("wrapped actions overlap", earlier, r)
						}
					}
					actions = append(actions, r)
				}
				save := actions[len(actions)-1]
				if math.Abs(float64(save.X+save.W-(dialog.X+dialog.W-12))) > 0.5 {
					t.Fatal("primary action is not aligned to the right", save, dialog)
				}
			})
		}
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
