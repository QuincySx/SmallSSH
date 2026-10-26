package desktop

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/sshconn"
	"sshclient/internal/transfer"
)

func TestFileHeadersFollowTheTreeDividerAndPathStillNavigates(t *testing.T) {
	a := fileFollowDemo()
	b := a.primaryFilePane.browser()
	a.applyDirectory(b, false, "/srv/next", nil, b.seq, false)
	tt := ui.NewTester(a.fileArea, 960, 420)
	if err := tt.Click("目录路径"); err != nil || !tt.Focused("目录路径") {
		t.Fatal("initial path focus failed", err)
	}
	check := func() (ui.Rect, ui.Rect) {
		t.Helper()
		source, sourceOK := tt.Find("文件位置")
		address, addressOK := tt.Find("目录路径")
		tree, treeOK := tt.Find("目录树")
		files, filesOK := tt.Find("文件列表")
		name, nameOK := tt.Find("名称")
		if !sourceOK || !addressOK || !treeOK || !filesOK || !nameOK {
			t.Fatal("missing file header or content")
		}
		if math.Abs(float64(source.X-tree.X)) > 1 || math.Abs(float64(source.W-tree.W)) > 1 || math.Abs(float64(address.X-files.X)) > 1 {
			t.Fatal("headers and contents use different column boundaries", source, tree, address, files)
		}
		if math.Abs(float64(address.X+8-name.X)) > 1 || math.Abs(float64(source.Y+source.H/2-address.Y-address.H/2)) > 1 {
			t.Fatal("path and name inset or header baselines differ", source, address, name)
		}
		return tree, files
	}
	tree, files := check()
	x, y := files.X-.5, tree.Y+40
	tt.Press(x, y)
	tt.Move(x+48, y)
	tt.Release(x+48, y)
	_, moved := check()
	if moved.X < files.X+40 {
		t.Fatal("divider did not move", files, moved)
	}
	if err := tt.Click("目录路径"); err != nil || !tt.Focused("目录路径") {
		t.Fatal("path focus failed after divider drag", err)
	}
	tt.SetSize(420, 300)
	tt.Frame()
	check()
	if err := tt.Click("目录路径"); err != nil {
		t.Fatal(err)
	}
	if !tt.Focused("目录路径") {
		t.Fatal("path input did not receive focus after resizing")
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("/srv/next")
	if b.address != "/srv/next" {
		t.Fatal("typing did not update path", b.address)
	}
	tt.Key(0, ui.KeyEnter)
	if b.dir != "/srv/next" || b.address != "/srv/next" {
		t.Fatal("header layout broke path navigation", b.dir, b.address)
	}
	check()
}

func TestFileWindowLayoutsKeepIndependentTargetsAndSelections(t *testing.T) {
	a := fileFollowDemo()
	a.ensureFileLayout()
	first := a.primaryFilePane
	second := a.splitFilePane(first, false, true)
	third := a.splitFilePane(first, true, false)
	fourth := a.splitFilePane(second, true, false)
	a.chooseFileEndpoint(third, "c")
	a.applyDirectory(third.browser(), false, "/srv/c", []sshconn.File{{Name: "selected.txt"}}, third.browser().seq, true)
	panes := a.fileLayout.panes()
	if len(panes) != 4 || first == third || first.states == nil || third.states["a"] == first.states["a"] {
		t.Fatal("split windows share mutable browser state")
	}
	tt := ui.NewTester(a.fileArea, 1200, 800)
	if err := tt.Click("selected.txt"); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"horizontal", "vertical", "grid"} {
		a.arrangeFilePanes(mode)
		tt.Frame()
		rects := make([]ui.Rect, 4)
		for i, p := range panes {
			var ok bool
			rects[i], ok = tt.Find(a.filePaneLabel(p))
			if !ok || rects[i].W <= 40 || rects[i].H <= 40 {
				t.Fatal("window lost after rearranging", mode, rects)
			}
			for j := 0; j < i; j++ {
				other := rects[j]
				if rects[i].X < other.X+other.W && other.X < rects[i].X+rects[i].W && rects[i].Y < other.Y+other.H && other.Y < rects[i].Y+rects[i].H {
					t.Fatal("windows overlap", mode, rects)
				}
			}
		}
		if mode == "horizontal" && rects[3].X <= rects[0].X || mode == "vertical" && rects[3].Y <= rects[0].Y || mode == "grid" && (rects[1].X <= rects[0].X || rects[2].Y <= rects[0].Y) {
			t.Fatal("arrangement does not match menu label", mode, rects)
		}
		if third.endpoint != "c" || third.follow || third.browser().dir != "/srv/c" || !third.browser().chosen.Has("selected.txt") || a.fileLayout.panes()[0] != first || a.fileLayout.panes()[3] != fourth {
			t.Fatal("rearranging changed browser identity, directory or selection", mode, third.endpoint, third.follow, third.browser().dir, third.browser().chosen.Has("selected.txt"))
		}
	}
	a.active = 1
	a.syncFollowingFilePanes()
	if first.endpoint != "b" || !first.follow || third.endpoint != "c" || second.endpoint != localEndpoint {
		t.Fatal("following one window redirected independent windows")
	}
}

func TestFileWindowMenusSplitCloseAndKeepOne(t *testing.T) {
	a := fileFollowDemo()
	tt := ui.NewTester(a.fileArea, 1100, 760)
	if err := tt.Click("文件窗口菜单"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("向下拆分"); err != nil {
		t.Fatal(err)
	}
	first, second := a.fileLayout.panes()[0], a.fileLayout.panes()[1]
	if !a.fileLayout.vertical || second.follow || second.endpoint != first.endpoint {
		t.Fatal("down split did not create an independent window")
	}
	if err := tt.Click(a.filePaneLabel(second) + "文件窗口菜单"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("向右拆分"); err != nil {
		t.Fatal(err)
	}
	if len(a.fileLayout.panes()) != 3 || !a.fileLayout.vertical || a.fileLayout.second.vertical {
		t.Fatal("nested splitting did not preserve its parent's direction")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first.browser().loading = map[string]*directoryLoad{"/pending": {ctx: ctx, cancel: cancel}}
	if err := tt.Click(a.filePaneLabel(second) + "文件窗口菜单"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("仅保留此窗口"); err != nil {
		t.Fatal(err)
	}
	if len(a.fileLayout.panes()) != 1 || a.fileLayout.pane != second || ctx.Err() == nil {
		t.Fatal("keeping a window did not cancel discarded browsers")
	}
	if err := tt.Click("文件窗口菜单"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("关闭窗口"); err == nil || len(a.fileLayout.panes()) != 1 {
		t.Fatal("last file window could be closed")
	}
}

func TestFileWindowCloseAndLockCancelEveryPendingBrowser(t *testing.T) {
	a := fileFollowDemo()
	a.arrangeFilePanes("grid")
	panes := a.fileLayout.panes()
	contexts := make([]context.Context, len(panes))
	for i, p := range panes {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		contexts[i] = ctx
		p.browser().loading = map[string]*directoryLoad{"/pending": {ctx: ctx, cancel: cancel}}
	}
	a.closeFilePane(panes[2])
	if contexts[2].Err() == nil || contexts[0].Err() != nil || len(a.fileLayout.panes()) != 3 {
		t.Fatal("closing one window cancelled the wrong browser")
	}
	a.sessions = nil // These fixtures contain metadata, not live terminals.
	a.lock()
	for i, ctx := range contexts {
		if ctx.Err() == nil {
			t.Fatal("lock retained a file request", i)
		}
	}
	if a.fileLayout != nil || a.activeFilePane != nil || a.primaryFilePane != nil {
		t.Fatal("lock retained file window targets")
	}
}

func TestMultipleFileWindowsTransferCompletionAndDropUseExactTargets(t *testing.T) {
	a := fileFollowDemo()
	a.arrangeFilePanes("grid")
	panes := a.fileLayout.panes()
	local, remote := panes[1], panes[3]
	a.chooseFileEndpoint(local, localEndpoint)
	a.chooseFileEndpoint(remote, "c")
	from, to := local.browser(), remote.browser()
	a.applyDirectory(from, true, "/local", []sshconn.File{{Name: "upload-multi.txt"}}, from.seq, true)
	a.applyDirectory(to, false, "/srv/c", nil, to.seq, true)
	tt := ui.NewTester(a.fileArea, 1200, 800)
	source, _ := tt.Find("upload-multi.txt")
	destination, _ := tt.Find(a.filePaneLabel(remote) + "文件列表")
	x, y := source.X+source.W/2, source.Y+source.H/2
	tt.Press(x, y)
	tt.Move(x+12, y)
	tt.Move(destination.X+destination.W/2, destination.Y+destination.H/2)
	tt.Release(destination.X+destination.W/2, destination.Y+destination.H/2)
	if !a.prompt.open || !strings.Contains(a.prompt.target, "/local") || !strings.Contains(a.prompt.target, "/srv/c") {
		t.Fatal("multi-window drop lost its source or destination", a.prompt.target)
	}
	a.chooseFileEndpoint(remote, "b")
	if !strings.Contains(a.prompt.target, "连接 C") || strings.Contains(a.prompt.target, "连接 B") {
		t.Fatal("pending transfer followed a later window location", a.prompt.target)
	}
	a.chooseFileEndpoint(remote, "c")
	a.transferArrived(transfer.Job{Host: "c", Remote: "/srv/c/result.txt", Direction: "upload", Size: 42})
	if len(to.cache["/srv/c"]) != 1 || to.cache["/srv/c"][0].Name != "result.txt" {
		t.Fatal("transfer completion did not update a third or fourth window")
	}
}

func TestFileWindowControlsRemainReachableInSmallGrid(t *testing.T) {
	for _, dark := range []bool{false, true} {
		t.Run(fmt.Sprint(dark), func(t *testing.T) {
			a := fileFollowDemo()
			a.arrangeFilePanes("grid")
			tt := ui.NewTester(a.fileArea, 400, 700)
			tt.SetDark(dark)
			for _, p := range a.fileLayout.panes() {
				for _, suffix := range []string{"文件窗口菜单", "目录路径"} {
					r, ok := tt.Find(a.filePaneLabel(p) + suffix)
					if !ok || r.W < 28 || r.H < 20 || r.X < 0 || r.Y < 0 || r.X+r.W > 400 || r.Y+r.H > 700 {
						t.Fatal("narrow grid lost a window action", suffix, r)
					}
				}
			}
		})
	}
}

func TestFileArrangementMenuKeepsTerminalVisibleAndWindowLimitExplicit(t *testing.T) {
	a := fileFollowDemo()
	tt := ui.NewTester(a.view, 1280, 900)
	before, _ := tt.Find("终端区域")
	for _, arrangement := range []string{"网格排列", "上下排列", "左右排列"} {
		if err := tt.Click(a.paneSide(a.activeFilePane) + "文件窗口菜单"); err != nil {
			t.Fatal(err)
		}
		if err := tt.ChooseMenuItem(arrangement); err != nil {
			t.Fatal(err)
		}
		after, ok := tt.Find("终端区域")
		if !ok || before != after || a.fileExpanded || a.toolsRight || len(a.fileLayout.panes()) != 4 {
			t.Fatal("file arrangement covered the terminal or changed its dock", arrangement, before, after)
		}
	}
	for len(a.fileLayout.panes()) < maxFilePanes {
		a.splitFilePane(a.activeFilePane, false, false)
	}
	tt.Frame()
	if err := tt.Click(a.paneSide(a.activeFilePane) + "文件窗口菜单"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("向下拆分"); err == nil || len(a.fileLayout.panes()) != maxFilePanes {
		t.Fatal("the window limit was bypassed")
	}
}

func TestFilePanelKeepsApplicationAndFileWindowActionsSeparate(t *testing.T) {
	a := fileFollowDemo()
	a.fileExpanded, a.sidebarHidden = true, true
	tt := ui.NewTester(a.fileArea, 560, 500)
	for _, label := range []string{"添加文件窗口", "工作台菜单", "应用菜单", "视图菜单", "收窄连接侧栏", "展开连接侧栏"} {
		if _, found := tt.Find(label); found {
			t.Fatal("file toolbar contains an application-level action", label)
		}
	}
	if err := tt.Click("文件窗口菜单"); err != nil {
		t.Fatal(err)
	}
	menu := strings.Join(tt.Menu(), "\n")
	for _, label := range []string{"向右拆分", "向下拆分", "添加本机窗口", "左右排列", "上下排列", "网格排列", "仅保留此窗口", "关闭窗口"} {
		if !strings.Contains(menu, label) {
			t.Fatal("file window action is no longer reachable", label, menu)
		}
	}
	for _, label := range []string{"授权中心", "安全设置", "立即锁定", "新建终端", "服务器状态"} {
		if strings.Contains(menu, label) {
			t.Fatal("file window menu contains an unrelated action", label, menu)
		}
	}
}

func TestLocalFileWindowMenuKeepsLocationsAndIndependentBrowsers(t *testing.T) {
	a := fileFollowDemo()
	a.ensureFileLayout()
	local := a.localFilePane
	local.browser().dir, local.browser().address = "/local/project", "/local/project"
	local.browser().cache["/local/project"] = []sshconn.File{{Name: "local.txt"}}
	tt := ui.NewTester(a.fileArea, 1000, 600)
	if err := tt.Click("文件窗口菜单"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("添加本机窗口"); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(a.fileLayout.panes(), local) || local.endpoint != localEndpoint || local.browser().dir != "/local/project" {
		t.Fatal("local window did not restore its cached location")
	}
	before := a.fileLayout.panes()
	if err := tt.Click(a.paneSide(local) + "文件窗口菜单"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("添加本机窗口"); err != nil {
		t.Fatal(err)
	}
	var second *filePane
	for _, p := range a.fileLayout.panes() {
		if !slices.Contains(before, p) {
			second = p
		}
	}
	if second == nil || second.endpoint != localEndpoint || second.browser() == local.browser() || second.browser().dir != "/local/project" {
		t.Fatal("additional local window shares browser state or opens a remote source")
	}
	second.browser().chosen.Add("local.txt")
	if local.browser().chosen.Len() != 0 {
		t.Fatal("selecting in one local window changed another")
	}
}

func TestLocalFileWindowRestoresLocalSourceAfterCachedPaneChangesServer(t *testing.T) {
	for _, closed := range []bool{false, true} {
		t.Run(fmt.Sprintf("closed=%v", closed), func(t *testing.T) {
			a := fileFollowDemo()
			a.ensureFileLayout()
			local := a.addLocalFileWindow(a.primaryFilePane)
			local.browser().dir, local.browser().address = "/local/project", "/local/project"
			a.chooseFileEndpoint(local, "c")
			remoteDirectory := local.browser().dir
			if closed {
				a.closeFilePane(local)
			}
			before := a.fileLayout.panes()
			tt := ui.NewTester(a.fileArea, 1000, 600)
			if err := tt.Click(a.paneSide(a.primaryFilePane) + "文件窗口菜单"); err != nil {
				t.Fatal(err)
			}
			if err := tt.ChooseMenuItem("添加本机窗口"); err != nil {
				t.Fatal(err)
			}
			var added *filePane
			for _, p := range a.fileLayout.panes() {
				if !slices.Contains(before, p) {
					added = p
				}
			}
			if added == nil || added == local || added.endpoint != localEndpoint || added.follow || added.browser().dir != "/local/project" {
				t.Fatal("local action reused a remote source or lost the last local directory")
			}
			if local.endpoint != "c" || local.browser().dir != remoteDirectory {
				t.Fatal("restoring local files changed the other window's remote location")
			}
		})
	}
}
