package desktop

import (
	"github.com/egoist/mygo/ui"
	"testing"
)

// Exercise the real settings shell, including its 200 DIP navigation. Window
// width alone must not decide whether the inner event table can fit.
func TestAuditSettingsUsesFullWidthAndAnAdjacentDetailInspector(t *testing.T) {
	for _, width := range []int{560, 900, 1080, 1440} {
		a := historyUIFixture()
		a.settingsSection = settingsApprovals
		a.approvalHistory.detail = 0
		tt := ui.NewTester(a.settingsView, width, 800)
		tt.Frame()
		tt.Frame()
		body, ok := tt.Find("审计记录区域")
		if !ok || body.X != 228 || body.X+body.W != float32(width-20) {
			t.Fatal("audit content wasted horizontal space", width, body)
		}
		footer, visible := tt.Find("更早记录")
		if !visible || footer.Y+footer.H > 788 {
			t.Fatal("audit footer clipped", width, footer)
		}
		table, ok := tt.Find("访问历史表格")
		if !ok || table.X != body.X || table.H < 160 {
			t.Fatal("records lost full-height viewport", table)
		}
		inspector, shown := tt.Find("审计详情侧栏")
		if width >= 1080 {
			if !shown || inspector.W != 332 || inspector.X != table.X+table.W || inspector.Y != table.Y || inspector.H != table.H {
				t.Fatal("detail is not adjacent to the table", width, table, inspector)
			}
		} else if shown {
			t.Fatal("nested settings navigation was not included in responsive calculation", inspector)
		}
		if err := tt.Click("关闭详情"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		table, _ = tt.Find("访问历史表格")
		if table.W != body.W {
			t.Fatal("closing detail did not restore full width", table, body)
		}
	}
}
