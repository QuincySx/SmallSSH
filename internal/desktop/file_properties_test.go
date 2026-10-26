package desktop

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/desktop/components"
	"sshclient/internal/fileattrs"
	"sshclient/internal/sshconn"
)

func testPropertiesInfo() fileattrs.Info {
	return fileattrs.Info{Snapshot: fileattrs.Snapshot{Valid: true, Mode: 0750, UID: 1000, GID: 1000, OwnershipKnown: true},
		Users: []fileattrs.Account{{Name: "root", ID: 0}, {Name: "deploy", ID: 1000}}, Groups: []fileattrs.Account{{Name: "deploy", ID: 1000}, {Name: "www-data", ID: 33}}}
}

func TestTreeMenusSnapshotClickedFolderAndProtectRoot(t *testing.T) {
	a := fileFollowDemo()
	p := a.primaryFilePane
	b := p.browser()
	a.applyDirectory(b, false, "/", []sshconn.File{{Name: "srv", Directory: true}}, b.seq, true)
	a.applyDirectory(b, false, "/srv", []sshconn.File{{Name: "site", Directory: true}}, b.seq, true)
	a.applyDirectory(b, false, "/srv/site", nil, b.seq, true)
	tt := ui.NewTester(a.view, 1100, 820)
	if err := tt.RightClick("site"); err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{"重命名 / 移动", "权限 / 属性", "删除", "新建文件夹", "新建文本文件", "刷新", "跟随当前终端"} {
		if !slices.Contains(tt.Menu(), label) {
			t.Fatal("tree action missing", label, tt.Menu())
		}
	}
	if err := tt.ChooseMenuItem("权限 / 属性"); err != nil {
		t.Fatal(err)
	}
	s := a.prompt.properties
	if s == nil || s.path != "/srv/site" || s.source.endpoint != "a" {
		t.Fatal("wrong properties target")
	}
	s.loaded(testPropertiesInfo())
	tt.Frame()
	if err := tt.Click("用户组 · 写"); err != nil {
		t.Fatal(err)
	}
	s.owner, s.group = "0", "www-data"
	change, err := s.change()
	if err != nil || !change.SetUID || change.UID != 0 || !change.SetGID || change.GID != 33 || change.Mode != 0770 {
		t.Fatal(change, err)
	}
	a.chooseFileEndpoint(p, "b")
	if s.source.endpoint != "a" || s.path != "/srv/site" {
		t.Fatal("form followed a new endpoint")
	}
	if err := tt.Click("取消"); err != nil {
		t.Fatal(err)
	}
	if a.prompt.open || s.ctx.Err() == nil {
		t.Fatal("cancel did not dismiss/cancel metadata")
	}
	a.chooseFileEndpoint(p, "a")
	tt.Frame()
	if err := tt.RightClick("/"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("删除"); err == nil {
		t.Fatal("root delete enabled")
	}
	tt.CloseMenu()
}

func pumpFileUpdates(t *testing.T, updates <-chan func(), done func() bool) {
	t.Helper()
	deadline := time.NewTimer(8 * time.Second)
	defer deadline.Stop()
	for !done() {
		select {
		case fn := <-updates:
			fn()
		case <-deadline.C:
			t.Fatal("file operation timed out")
		}
	}
}

func TestLocalPropertiesSaveUsesOriginalFileAndClosesOnlyOnSuccess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX mode assertion")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	updates := make(chan func(), 64)
	a := &app{ctx: ctx, online: true, page: 1, groups: map[string]*bool{}, dispatch: func(fn func()) { updates <- fn }}
	a.ensurePanes()
	p := a.primaryFilePane
	dir := t.TempDir()
	name := filepath.Join(dir, "config.txt")
	if err := os.WriteFile(name, []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	a.applyDirectory(p.browser(), true, dir, []sshconn.File{{Name: "config.txt", Mode: "-rw-------", Size: 4}}, 0, true)
	a.openFileProperties(p, name)
	s := a.prompt.properties
	pumpFileUpdates(t, updates, func() bool { return !s.loading })
	if !s.info.Valid || s.error != "" {
		t.Fatal(s.info, s.error)
	}
	tt := ui.NewTester(a.view, 1100, 820)
	if err := tt.Click("用户组 · 读"); err != nil {
		t.Fatal(err)
	}
	a.chooseFileEndpoint(p, "another-server")
	if err := tt.Click("保存属性"); err != nil {
		t.Fatal(err)
	}
	pumpFileUpdates(t, updates, func() bool { return !s.saving })
	if a.prompt.open {
		t.Fatal("successful property save kept dialog open", s.error)
	}
	st, err := os.Stat(name)
	if err != nil || st.Mode().Perm() != 0640 {
		t.Fatal(st, err)
	}
	if p.endpoint != "another-server" {
		t.Fatal("saving changed active location")
	}
	a.chooseFileEndpoint(p, localEndpoint)
	a.openFileProperties(p, name)
	s = a.prompt.properties
	pumpFileUpdates(t, updates, func() bool { return !s.loading })
	if err = os.Chmod(name, 0660); err != nil {
		t.Fatal(err)
	}
	s.checks = components.PermissionChecks(0600)
	tt.Frame()
	if err = tt.Click("保存属性"); err != nil {
		t.Fatal(err)
	}
	pumpFileUpdates(t, updates, func() bool { return !s.saving })
	if !a.prompt.open || s.error == "" || s.info.Mode&0777 != 0660 {
		t.Fatal("stale save was not kept for review", s.error, s.info)
	}
	a.closeFileProperties(s)
}

func TestTreeDeleteEmptyLocalDirectoryThroughConfirmation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	updates := make(chan func(), 64)
	a := &app{ctx: ctx, online: true, page: 1, groups: map[string]*bool{}, dispatch: func(fn func()) { updates <- fn }}
	a.ensurePanes()
	p := a.primaryFilePane
	dir := t.TempDir()
	folder := filepath.Join(dir, "empty-folder")
	if err := os.Mkdir(folder, 0700); err != nil {
		t.Fatal(err)
	}
	a.applyDirectory(p.browser(), true, dir, []sshconn.File{{Name: "empty-folder", Directory: true}}, 0, true)
	tt := ui.NewTester(a.view, 1100, 820)
	if err := tt.RightClick("empty-folder"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("删除"); err != nil {
		t.Fatal(err)
	}
	if !a.prompt.open || !strings.Contains(a.prompt.target, folder) {
		t.Fatal("tree delete missing target confirmation", a.prompt.target)
	}
	if err := tt.Click("删除"); err != nil {
		t.Fatal(err)
	}
	pumpFileUpdates(t, updates, func() bool { return !a.busy })
	if _, err := os.Stat(folder); !os.IsNotExist(err) {
		t.Fatal("empty directory not removed", err, a.status)
	}
}

func TestDirectoryMutationRebasesAllMatchingBrowsersAndDropsStaleDescendants(t *testing.T) {
	a := fileFollowDemo()
	a.ensureFileLayout()
	first := a.primaryFilePane
	second := a.splitFilePane(first, false, false)
	other := first.states["b"]
	for _, p := range []*filePane{first, second} {
		b := p.browser()
		b.loadError = map[string]string{"/srv/site/child": "old failure"}
		a.applyDirectory(b, false, "/srv", []sshconn.File{{Name: "site", Directory: true}}, 0, false)
		a.applyDirectory(b, false, "/srv/site", []sshconn.File{{Name: "child", Directory: true}}, 0, false)
		a.applyDirectory(b, false, "/srv/site/child", nil, 0, true)
	}
	a.fileMutationCompleted("a", first.browser(), "/srv", "rename", "/srv/site", "/srv/renamed", 0, true)
	for _, p := range []*filePane{first, second} {
		b := p.browser()
		if b.dir != "/srv/renamed/child" || b.address != b.dir {
			t.Fatal("directory rename left wrong location", b.dir)
		}
		if len(b.loadError) != 0 {
			t.Fatal("old subtree error retained", b.loadError)
		}
		for path := range b.cache {
			if directoryContains(false, "/srv/site", path) {
				t.Fatal("old subtree cache remained", path)
			}
		}
	}
	a.fileMutationCompleted("a", first.browser(), "/srv", "remove", "/srv/renamed", "", 0, true)
	if first.browser().dir != "/srv" || second.browser().dir != "/srv" || other.dir != "/srv/b" {
		t.Fatal("removed-directory fallback affected wrong browser")
	}
}

func TestPropertyDialogSurvivesDockingAndLockCancelsWithoutShowingWindows(t *testing.T) {
	a := fileFollowDemo()
	windows := fakeToolsFactory(a)
	a.detachTools()
	a.renderingToolsWindow = true
	a.openFileProperties(a.primaryFilePane, "/srv/a")
	a.renderingToolsWindow = false
	s := a.prompt.properties
	s.loaded(testPropertiesInfo())
	s.checks = components.PermissionChecks(0770)
	s.owner = "root"
	main := ui.NewTester(a.view, 1000, 760)
	tools := ui.NewTester(a.detachedToolsView, 960, 720)
	if main.HasText("权限 / 属性对话框") || !tools.HasText("权限 / 属性对话框") {
		t.Fatal("properties rendered in the wrong window")
	}
	a.setToolsDock(false)
	(*windows)[0].finishPendingUpdates()
	main.Frame()
	if !main.HasText("权限 / 属性对话框") || a.prompt.properties != s || s.owner != "root" || components.PermissionMode(s.checks) != 0770 || s.ctx.Err() != nil {
		t.Fatal("docking lost the property edit")
	}
	if (*windows)[0].shown != 1 {
		t.Fatal("editing raised the window again")
	}
	a.lock()
	main.Frame()
	if s.ctx.Err() == nil || a.prompt.open || main.HasText("权限 / 属性对话框") {
		t.Fatal("lock retained property operation or UI")
	}
}

func TestPropertyReadCompletionCannotReplaceAnotherDialog(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	updates := make(chan func(), 64)
	a := &app{ctx: ctx, online: true, dispatch: func(fn func()) { updates <- fn }}
	a.ensurePanes()
	first := filepath.Join(t.TempDir(), "first")
	second := filepath.Join(filepath.Dir(first), "second")
	for _, name := range []string{first, second} {
		if err := os.WriteFile(name, nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	a.openFileProperties(a.primaryFilePane, first)
	old := a.prompt.properties
	a.openFileProperties(a.primaryFilePane, second)
	current := a.prompt.properties
	pumpFileUpdates(t, updates, func() bool { return !current.loading })
	if old.ctx.Err() == nil || a.prompt.properties != current || current.info.Name != "second" || current.error != "" {
		t.Fatal("old file read reached a replacement dialog", current.info, current.error)
	}
	a.closeFileProperties(current)
}

func TestDirectoryMoveInvalidatesDestinationOnlyCache(t *testing.T) {
	a := fileFollowDemo()
	a.ensureFileLayout()
	first := a.primaryFilePane
	second := a.splitFilePane(first, false, false)
	b := second.browser()
	clear(b.cache)
	a.applyDirectory(b, false, "/destination", []sshconn.File{{Name: "existing", Directory: true}}, 0, true)
	a.fileMutationCompleted("a", first.browser(), "/source", "rename", "/source/folder", "/destination/folder", 0, true)
	if _, cached := b.cache["/destination"]; cached || b.dir != "/destination" {
		t.Fatal("destination-only cache remained stale or changed location")
	}
}
