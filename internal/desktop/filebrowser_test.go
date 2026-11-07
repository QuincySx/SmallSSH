package desktop

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"sshclient/internal/snippet"
	"sshclient/internal/sshconn"
	"sshclient/internal/testserver"
	"sshclient/internal/vault"
)

func TestTwoBrowsersKeepIndependentLocationsAndIgnoreStaleNavigation(t *testing.T) {
	a := &app{}
	a.ensurePanes()
	left, right := a.localFilePane.browser(), a.primaryFilePane.browser()
	left.dir = "/local/project"
	right.seq = 2
	a.applyDirectory(right, false, "/srv/new", []sshconn.File{{Name: "new.txt"}}, 2, true)
	right.chosen.Add("new.txt")
	a.applyDirectory(right, false, "/old", []sshconn.File{{Name: "old.txt"}}, 1, true)
	if left.dir != "/local/project" || right.dir != "/srv/new" || !right.chosen.Has("new.txt") {
		t.Fatal("a stale request or the other pane changed the active location")
	}
	right.chosen.Clear()
	right.selected = 0
	if len(chosenFiles(right)) != 0 {
		t.Fatal("focus was treated as a selection after deselecting")
	}
	children := treeChildren(right, false, "/srv")
	if len(children) != 1 || children[0] != "/srv/new" {
		t.Fatal(children)
	}
	a.primaryFilePane.endpoint = "other"
	a.primaryFilePane.browser().dir = "/other"
	a.primaryFilePane.endpoint = localEndpoint
	if a.primaryFilePane.browser().dir != "/srv/new" {
		t.Fatal("lost per-server directory")
	}
}
func TestLocalEditingDetectsChangesAndNeverClobbersSaveAs(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, "config.txt")
	original := []byte("before\n")
	os.WriteFile(name, original, 0600)
	if e := saveLocalText(name, []byte("after\n"), textHash(original), false); e != nil {
		t.Fatal(e)
	}
	if e := saveLocalText(name, []byte("clobber"), textHash(original), false); e == nil {
		t.Fatal("external changes overwritten")
	}
	if e := saveLocalText(name, []byte("clobber"), "", true); e == nil {
		t.Fatal("save-as overwrote an existing file")
	}
	copy := filepath.Join(dir, "copy.txt")
	if e := saveLocalText(copy, []byte("new\n"), "", true); e != nil {
		t.Fatal(e)
	}
	got, _ := os.ReadFile(name)
	if string(got) != "after\n" {
		t.Fatal(string(got))
	}
	if e := mutateLocal("remove", dir, "", 0); e == nil {
		t.Fatal("nonempty directory was recursively deleted")
	}
}

func TestFileOperationSnapshotsParentAndUpdatesTree(t *testing.T) {
	a := &app{ctx: context.Background(), online: true, selected: "web", sessions: []*session{{host: "web"}}}
	a.ensurePanes()
	p := a.primaryFilePane
	b := p.browser()
	b.dir = "/srv/site"
	b.cache[b.dir] = []sshconn.File{{Name: "config.txt"}}
	b.chosen.Add("config.txt")
	a.fileOperation(p, "mkdir")
	if a.prompt.target != "web · /srv/site" {
		t.Fatal("new folder targeted a selected file", a.prompt.target)
	}
	updateMutationCache(b, false, b.dir, "mkdir", "/srv/site/new-folder", "", 0)
	if children := treeChildren(b, false, b.dir); len(children) != 1 || children[0] != "/srv/site/new-folder" {
		t.Fatal(children)
	}
	updateMutationCache(b, false, b.dir, "rename", "/srv/site/config.txt", "/srv/site/renamed.txt", 0)
	for _, f := range b.cache[b.dir] {
		if f.Name == "config.txt" {
			t.Fatal("old name remained")
		}
	}
}
func TestTerminalFirstLayoutAndCommandsPanelWithoutATerminal(t *testing.T) {
	a := &app{ctx: context.Background(), online: true, page: 1, selected: "web", groups: map[string]*bool{}, sidebarWidth: 210, terminalShare: 72, edit: &editor.State{Buffer: editor.NewBuffer("")}, sessions: []*session{{host: "web", name: "Web"}, {host: "other", name: "Other"}}, connections: []vault.Summary{{ID: "web", Name: "Web", Host: "127.0.0.1", User: "user"}}}
	tt := ui.NewTester(a.view, 1280, 900)
	for _, label := range []string{"目录路径", "文件窗口菜单", "工具面板布局"} {
		if _, ok := tt.Find(label); !ok {
			t.Fatal(label, tt.Texts())
		}
	}
	if err := tt.Click("文件窗口菜单"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("向右拆分"); err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{"窗口1目录路径", "窗口2目录路径"} {
		if _, ok := tt.Find(label); !ok {
			t.Fatal(label)
		}
	}
	a.snippets = []snippet.Command{{ID: "uptime", Name: "运行时间", Group: "运维", Body: "uptime"}}
	a.runSnippet(a.snippets[0])
	if a.prompt.snippet != nil || a.status != "请先打开终端" {
		t.Fatal("a command was prepared without a terminal", a.status)
	}
	a.showTool(toolCommands)
	tt.Frame()
	for _, label := range []string{"新建常用命令", "运行时间", "全部"} {
		if !tt.HasText(label) {
			t.Fatal(label, tt.Texts())
		}
	}
	a.showTool(toolFiles)
	a.fileExpanded = true
	tt.Frame()
	if tt.HasText("终端区域") {
		t.Fatal("tool panel did not expand")
	}
	a.fileExpanded = false
	tt.SetSize(1000, 760)
	tt.SetDark(true)
	if !tt.HasText("窗口1目录路径") {
		t.Fatal("file pane lost at minimum size")
	}
}

func TestDirectoryWithoutPermissionShowsTheRefusalInsteadOfStaleRows(t *testing.T) {
	a := &app{}
	a.ensurePanes()
	p := a.primaryFilePane
	b := p.browser()
	b.dir = "/srv/denied"
	b.address = "/srv/denied"
	b.cache[b.dir] = []sshconn.File{{Name: "old.txt"}}
	b.loadError = map[string]string{}
	b.loadError[b.dir] = friendlyDirectoryError(errors.New("sftp: permission denied"))
	tt := ui.NewTester(func(c *ui.Context) { a.fileList(c, p, b) }, 640, 400)
	if tt.HasText("old.txt") {
		t.Fatal("a refused directory kept showing its old contents", tt.Texts())
	}
	if !tt.HasText("没有权限访问此目录") {
		t.Fatal("the pane did not explain the refusal", tt.Texts())
	}
	for _, label := range []string{"重试", "上一级"} {
		if _, ok := tt.Find(label); !ok {
			t.Fatal("retry affordance missing", label)
		}
	}
}

func TestFailedDirectoryNavigationShowsTheTargetAndCanRetry(t *testing.T) {
	updates := make(chan func(), 8)
	a := &app{ctx: context.Background(), online: true, dispatch: func(fn func()) { updates <- fn }}
	a.ensurePanes()
	p, root := a.primaryFilePane, t.TempDir()
	b := p.browser()
	a.applyDirectory(b, true, root, []sshconn.File{{Name: "previous-directory.txt"}}, b.seq, true)
	b.chosen.Add("previous-directory.txt")
	b.selected, b.anchor = 0, 0
	blocked := filepath.Join(root, "unavailable")
	if err := os.WriteFile(blocked, nil, 0600); err != nil {
		t.Fatal(err)
	}
	wait := func() {
		t.Helper()
		select {
		case update := <-updates:
			update()
		case <-time.After(5 * time.Second):
			t.Fatal("directory request did not finish")
		}
	}
	a.navigateDirectory(p, blocked)
	wait()
	if b.dir != blocked || b.address != blocked || b.chosen.Len() != 0 || b.selected != -1 || b.anchor != -1 {
		t.Fatal("failed navigation retained the previous directory or selection", b.dir, b.address)
	}
	tt := ui.NewTester(func(c *ui.Context) { a.fileList(c, p, b) }, 640, 400)
	if tt.HasText("previous-directory.txt") || !tt.HasText("这不是目录") {
		t.Fatal("failed navigation hid its error behind old rows", tt.Texts())
	}
	if err := os.Remove(blocked); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(blocked, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(blocked, "new.txt"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("重试"); err != nil {
		t.Fatal(err)
	}
	wait()
	tt.Frame()
	if b.dir != blocked || b.loadError[blocked] != "" || !tt.HasText("new.txt") {
		t.Fatal("retry did not recover the failed directory", b.dir, b.loadError, tt.Texts())
	}
	a.navigateDirectory(p, root)
	tt.Frame()
	if b.dir != root || !tt.HasText("previous-directory.txt") {
		t.Fatal("failed navigation destroyed the previous directory cache", b.dir, tt.Texts())
	}
}

func TestBackgroundAndCanceledDirectoryErrorsDoNotReplaceCurrentNavigation(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		updates := make(chan func(), 8)
		a := &app{ctx: context.Background(), online: true, dispatch: func(fn func()) { updates <- fn }}
		a.ensurePanes()
		p, root := a.primaryFilePane, t.TempDir()
		b := p.browser()
		a.applyDirectory(b, true, root, []sshconn.File{{Name: "keep.txt"}}, b.seq, true)
		blocked := filepath.Join(root, "missing")
		a.loadDirectory(p, blocked, canceled)
		var update func()
		select {
		case update = <-updates:
		case <-time.After(5 * time.Second):
			t.Fatal("directory request did not finish")
		}
		if canceled {
			a.navigateDirectory(p, root)
		}
		b.chosen.Add("keep.txt")
		update()
		if b.dir != root || b.address != root || !b.chosen.Has("keep.txt") {
			t.Fatal("a background or canceled failure replaced the active directory", canceled, b.dir, b.address)
		}
		tt := ui.NewTester(func(c *ui.Context) { a.fileList(c, p, b) }, 640, 400)
		if !tt.HasText("keep.txt") || tt.HasText("目录不存在") {
			t.Fatal("another directory's error replaced the visible listing", canceled, tt.Texts())
		}
	}
}

func TestBatchDeleteSelectionAndCapturedLocalEndpoint(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "empty"), 0700); err != nil {
		t.Fatal(err)
	}
	updates := make(chan func(), 32)
	a := &app{ctx: context.Background(), online: true, dispatch: func(fn func()) { updates <- fn }}
	a.ensurePanes()
	p := a.localFilePane
	p.treeHidden = true
	b := p.browser()
	a.applyDirectory(b, true, dir, []sshconn.File{{Name: "empty", Directory: true}, {Name: "a.txt"}, {Name: "b.txt"}, {Name: "c.txt"}}, b.seq, true)
	tt := ui.NewTester(func(c *ui.Context) { a.browserPane(c, p) }, 800, 600)
	if err := tt.Click("a.txt"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ClickWith(ui.Shift, "c.txt"); err != nil {
		t.Fatal(err)
	}
	if b.chosen.Len() != 3 {
		t.Fatal("shift did not extend selection", b.chosen.Len())
	}
	if err := tt.ClickWith(ui.Cmd, "empty"); err != nil {
		t.Fatal(err)
	}
	if b.chosen.Len() != 4 {
		t.Fatal("modifier did not add folder", b.chosen.Len())
	}
	if err := tt.RightClick("b.txt"); err != nil {
		t.Fatal(err)
	}
	if b.chosen.Len() != 4 {
		t.Fatal("right click discarded selected files", b.chosen.Len())
	}
	if err := tt.Click("删除"); err != nil {
		t.Fatal(err)
	}
	if !a.prompt.open || !strings.Contains(a.prompt.message, "4") || !strings.Contains(a.prompt.message, "empty") || !strings.Contains(a.prompt.message, "b.txt") {
		t.Fatal("confirmation did not identify all selections", a.prompt)
	}
	confirm := a.prompt.confirm
	a.prompt = promptState{}
	p.endpoint = "different-server"
	confirm("")
	pumpFileUpdates(t, updates, func() bool { return !a.busy })
	for _, name := range []string{"a.txt", "b.txt", "c.txt", "empty"} {
		if _, err := os.Lstat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("%s remained: %v", name, err)
		}
	}
	if len(b.cache[dir]) != 0 || !strings.Contains(a.status, "4/4") {
		t.Fatal("cache or result did not reflect deletions", b.cache[dir], a.status)
	}
}

func TestBatchDeleteRemoteReportsNonemptyDirectoryAndPreservesTarget(t *testing.T) {
	h := newConnectionFlowHarness(t)
	profile := h.fixture.Profile()
	if err := h.a.client.SaveConnection(h.ctx, vault.ProfileChange{Profile: profile}, h.server.Vault); err != nil {
		t.Fatal(err)
	}
	h.a.connections = h.server.Vault.Summaries()
	dir := t.TempDir()
	for _, name := range []string{"empty", "not-empty"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "not-empty", "keep.txt"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	h.a.ensurePanes()
	pane := h.a.primaryFilePane
	pane.endpoint = profile.ID
	b := pane.browser()
	remoteDir := testserver.RemotePath(dir)
	entries := []sshconn.File{{Name: "a.txt"}, {Name: "empty", Directory: true}, {Name: "not-empty", Directory: true}}
	h.a.applyDirectory(b, false, remoteDir, append([]sshconn.File(nil), entries...), b.seq, true)
	for _, file := range entries {
		b.chosen.Add(file.Name)
	}
	h.a.fileOperationAt(pane, remoteDir, entries, "remove")
	if !h.a.prompt.open || !strings.Contains(h.a.prompt.message, "3") {
		t.Fatal("batch confirmation missing", h.a.prompt)
	}
	confirm := h.a.prompt.confirm
	h.a.prompt = promptState{}
	pane.endpoint = "different-server"
	entries[0].Name = "not-empty"
	confirm("")
	h.wait(func() bool { return !h.a.busy })
	for _, name := range []string{"a.txt", "empty"} {
		if _, err := os.Lstat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("%s not removed: %v; %s", name, err, h.a.status)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "not-empty", "keep.txt")); err != nil {
		t.Fatal("non-empty directory changed", err)
	}
	if len(b.cache[remoteDir]) != 1 || b.cache[remoteDir][0].Name != "not-empty" || !b.cache[remoteDir][0].Directory || b.chosen.Len() != 1 || !b.chosen.Has("not-empty") || b.selected != 0 || !strings.Contains(h.a.status, "2/3") || !(strings.Contains(h.a.status, "目录不是空的") || strings.Contains(h.a.status, "无法删除目录，请检查目录是否为空及权限")) {
		t.Fatal("partial result was not reported or reconciled", b.cache[remoteDir], h.a.status)
	}
}

func TestDeleteKeyConfirmsAllSelectedFiles(t *testing.T) {
	a := &app{}
	a.ensurePanes()
	p := a.primaryFilePane
	p.endpoint = "web"
	p.treeHidden = true
	b := p.browser()
	a.applyDirectory(b, false, "/srv", []sshconn.File{{Name: "a"}, {Name: "b"}}, b.seq, true)
	tt := ui.NewTester(func(c *ui.Context) { a.browserPane(c, p) }, 800, 600)
	if err := tt.Click("a"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ClickWith(ui.Cmd, "b"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyDelete)
	if !a.prompt.open || !strings.Contains(a.prompt.message, "2") || !strings.Contains(a.prompt.message, "a") || !strings.Contains(a.prompt.message, "b") {
		t.Fatal("delete key did not confirm the whole selection", a.prompt)
	}
}

func TestDeleteCompletionPreservesSelectionAfterNavigation(t *testing.T) {
	root := t.TempDir()
	original := filepath.Join(root, "original")
	other := filepath.Join(root, "other")
	for _, dir := range []string{original, other} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "same.txt"), []byte(dir), 0600); err != nil {
			t.Fatal(err)
		}
	}
	updates := make(chan func(), 8)
	a := &app{ctx: context.Background(), online: true, dispatch: func(fn func()) { updates <- fn }}
	a.ensurePanes()
	p := a.localFilePane
	b := p.browser()
	a.applyDirectory(b, true, original, []sshconn.File{{Name: "same.txt"}}, b.seq, true)
	b.chosen.Add("same.txt")
	b.selected = 0
	a.fileOperationAt(p, original, chosenFiles(b), "remove")
	confirm := a.prompt.confirm
	if confirm == nil {
		t.Fatal("missing delete confirmation")
	}
	a.prompt = promptState{}
	confirm("")
	// The worker posts its completion to updates. Change the browser before
	// delivering it, even if the filesystem operation has already finished.
	a.applyDirectory(b, true, other, []sshconn.File{{Name: "same.txt"}}, b.seq, true)
	b.chosen.Add("same.txt")
	b.selected = 0
	pumpFileUpdates(t, updates, func() bool { return !a.busy })
	if b.dir != other || b.selected != 0 || b.chosen.Len() != 1 || !b.chosen.Has("same.txt") {
		t.Fatal("late delete changed the new directory's selection", b.dir, b.selected, b.chosen.Len())
	}
	if len(b.cache[original]) != 0 || len(b.cache[other]) != 1 {
		t.Fatal("late delete updated the wrong directory cache", b.cache[original], b.cache[other])
	}
	if _, err := os.Stat(filepath.Join(other, "same.txt")); err != nil {
		t.Fatal("other directory changed", err)
	}
}

func TestDeleteRejectsRootAndTraversalNames(t *testing.T) {
	a := &app{ctx: context.Background(), online: true}
	a.ensurePanes()
	p := a.primaryFilePane
	p.endpoint = "web"
	for _, name := range []string{"", ".", "..", "/", "../other"} {
		a.prompt = promptState{}
		a.status = ""
		a.fileOperationAt(p, "/", []sshconn.File{{Name: name, Directory: true}}, "remove")
		if a.prompt.open || a.status == "" {
			t.Fatalf("unsafe root/traversal name %q was accepted", name)
		}
	}
}
