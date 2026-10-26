package desktop

import (
	"fmt"
	"testing"

	"github.com/egoist/mygo/ui"
)

func terminalHeaderFixture(t *testing.T, count int) (*app, *ui.Tester) {
	t.Helper()
	a := &app{online: true, page: 1, toolsHidden: true}
	for i := range count {
		a.sessions = append(a.sessions, &session{name: fmt.Sprintf("tab-%02d", i), host: "same"})
	}
	a.ensureTerminalGroups()
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Children(func() {
			a.terminalGroupHeader(c, a.singleTerminalGroup, true, true, 450)
		})
	}, 450, 180)
	return a, tt
}

func TestTerminalTabSelectionKeepsHeaderGeometry(t *testing.T) {
	a, tt := terminalHeaderFixture(t, 12)
	labels := []string{"tab-00", "tab-01", "关闭标签：tab-00", "关闭标签：tab-01"}
	before := make(map[string]ui.Rect, len(labels))
	for _, label := range labels {
		r, ok := tt.Find(label)
		if !ok {
			t.Fatal("missing header item", label)
		}
		before[label] = r
	}
	for _, index := range []int{1, 0, 1, 0} {
		if err := tt.Click(a.sessions[index].name); err != nil {
			t.Fatal(err)
		}
		if a.currentTerminal() != a.sessions[index] {
			t.Fatal("click did not select the terminal", index)
		}
		for _, label := range labels {
			r, ok := tt.Find(label)
			if !ok || r != before[label] {
				t.Fatal("tab selection shifted header geometry", label, before[label], r)
			}
		}
	}
}

func TestOverflowTerminalTabRevealSettlesAndKeepsCloseVisible(t *testing.T) {
	a, tt := terminalHeaderFixture(t, 24)
	for _, index := range []int{23, 22, 0, 1, 17, 0} {
		s := a.sessions[index]
		a.activateTerminal(s, false)
		tt.Frame()
		title, ok := tt.Find(s.name)
		if !ok {
			t.Fatal("missing selected title", index)
		}
		close, ok := tt.Find("关闭标签：" + s.name)
		if !ok || title.X < 0 || close.X+close.W > 450-30 || close.W < 24 {
			t.Fatal("selected tab or close button stayed clipped", index, title, close)
		}
		for range 3 {
			tt.Frame()
			nextTitle, _ := tt.Find(s.name)
			nextClose, _ := tt.Find("关闭标签：" + s.name)
			if nextTitle != title || nextClose != close {
				t.Fatal("overflow selection kept jumping after reveal", index, title, nextTitle, close, nextClose)
			}
		}
	}
}
