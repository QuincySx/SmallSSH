package desktop

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func chooseComposeProjectMenu(tt *ui.Tester, project, action string) error {
	if err := tt.Click("项目菜单 " + project); err != nil {
		return err
	}
	return tt.ChooseMenuItem(action)
}

func TestComposeProjectLifecycleMenusRequireSpecificConfirmationAndCancelIsInert(t *testing.T) {
	for _, tc := range []struct{ project, action, title string }{{"docs", "启动项目…", "启动项目"}, {"blog", "停止项目…", "停止项目"}, {"blog", "重启项目…", "重启项目"}} {
		t.Run(tc.title, func(t *testing.T) {
			a := composeProjectsDemo()
			tt := ui.NewTester(a.view, 1280, 1000)
			if err := tt.Click("Compose 项目 " + tc.project); err != nil {
				t.Fatal(err)
			}
			if err := chooseComposeProjectMenu(tt, tc.project, tc.action); err != nil {
				t.Fatal(err)
			}
			if !a.prompt.open || a.prompt.title != tc.title+"？" || !strings.Contains(a.prompt.target, "192.0.2.10:22") || !strings.Contains(a.prompt.target, tc.project) || !strings.Contains(a.prompt.message, "/srv/"+tc.project+"/compose.yaml") || a.managementState().busy {
				t.Fatal("operation was not reviewed before execution", a.prompt)
			}
			if tc.project == "blog" && !strings.Contains(a.prompt.message, "cache,db,web,worker") {
				t.Fatal("project confirmation dropped a stopped/paused service", a.prompt.message)
			}
			if tc.title == "重启项目" && !strings.Contains(a.prompt.message, "包括当前已停止的服务") {
				t.Fatal("restart scope was not explained")
			}
			if a.prompt.danger != (tc.title != "启动项目") {
				t.Fatal("disruptive action lost its confirmation emphasis")
			}
			if err := tt.Click("取消"); err != nil {
				t.Fatal(err)
			}
			if a.prompt.open || a.managementState().busy || a.managementState().message != "" {
				t.Fatal("cancelling performed an operation")
			}
		})
	}
}

func TestComposeProjectContextMenuSharesActionsAndFiltersDoNotChangeScope(t *testing.T) {
	a := composeProjectsDemo()
	tt := ui.NewTester(a.view, 1280, 1000)
	if err := tt.RightClick("Compose 项目 blog"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("停止项目…"); err != nil {
		t.Fatal(err)
	}
	if !a.prompt.open || !strings.Contains(a.prompt.message, "cache,db,web,worker") {
		t.Fatal(a.prompt)
	}
	tt.Click("取消")
	tt.Click("搜索 Docker")
	tt.Type("example/db")
	tt.Click("Compose 项目 blog")
	if err := chooseComposeProjectMenu(tt, "blog", "停止项目…"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(a.prompt.message, "cache,db,web,worker") {
		t.Fatal("display filter narrowed a whole-project action", a.prompt.message)
	}
	tt.Click("取消")
	a.managementState().busy = true
	tt.Frame()
	if err := chooseComposeProjectMenu(tt, "blog", "重启项目…"); err == nil {
		t.Fatal("a second operation was allowed while busy")
	}
}

func TestComposeProjectStateDisablesMeaninglessStartOrStop(t *testing.T) {
	a := composeProjectsDemo()
	tt := ui.NewTester(a.view, 1280, 1000)
	tt.Click("Compose 项目 docs")
	if err := chooseComposeProjectMenu(tt, "docs", "停止项目…"); err == nil {
		t.Fatal("an entirely stopped project exposed stop")
	}
	if a.prompt.open {
		t.Fatal("disabled action opened confirmation")
	}
}
