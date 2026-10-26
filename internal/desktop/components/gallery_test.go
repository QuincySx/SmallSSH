package components

import (
	"cmp"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// Opt-in, offline component gallery. It never starts MyGo's application loop,
// opens a window, reads the user's files or makes a network connection.
func TestComponentGallerySnapshotsWithoutWindows(t *testing.T) {
	if os.Getenv("SSH_CLIENT_PREVIEW_QA") != "1" {
		t.Skip("opt-in offline gallery")
	}
	dir := os.Getenv("SSH_CLIENT_UI_SNAPSHOT_DIR")
	if !filepath.IsAbs(dir) {
		t.Fatal("absolute snapshot directory required")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, variant := range []string{"light", "dark", "dialog", "compact"} {
		t.Run(variant, func(t *testing.T) {
			selected := 1
			treeState := ui.ListState{Selected: &selected}
			fileState := ui.ListState{}
			order := ui.SortOrder{Column: "cpu", Descending: true}
			processState := ui.ListState{Sort: &order}
			open := variant == "dialog"
			value := "../assets"
			entries := []FileEntry{
				{Name: "assets", Directory: true},
				{Name: "index.html", Bytes: 2048, Modified: time.Date(2026, 10, 20, 10, 0, 0, 0, time.Local)},
				{Name: "nginx.conf", Bytes: 876},
				{Name: "README.md", Bytes: 1240},
			}
			view := func(c *ui.Context) {
				ApplyTheme(c, false)
				ui.Column(c).Fill().Padding(24).Gap(18).Background(c.Theme().Background).Children(func() {
					ui.Text(c, "基础组件").FontSize(24).Bold()
					ui.Text(c, "现有页面共用的按钮、弹窗、目录树与列表").TextColor(c.Theme().TextMuted)
					ui.Column(c).FillWidth().Gap(10).Children(func() {
						ui.Text(c, "按钮与状态").Bold()
						ActionGroup(c, ui.Start, func() {
							Button(c, "普通操作")
							PrimaryButton(c, "主要操作")
							Button(c, "删除", ButtonOptions{Variant: Destructive})
							Button(c, "次要操作", ButtonOptions{Variant: Quiet})
							IconButton(c, "↻", "刷新")
						})
						ActionGroup(c, ui.Start, func() {
							Button(c, "暂不可用", ButtonOptions{Disabled: true})
							Button(c, "正在保存…", ButtonOptions{Variant: Primary, Busy: true})
							Button(c, "删除不可用", ButtonOptions{Variant: Destructive, Disabled: true})
						})
					})
					ui.Column(c).FillWidth().Gap(8).Children(func() {
						ui.Text(c, "按钮组 · 方向键切换焦点").Bold()
						ButtonGroup(c, func() {
							Button(c, "保存")
							Button(c, "另存为")
							Button(c, "撤销", ButtonOptions{Disabled: true})
						}).Label("文件操作")
					})
					ui.Text(c, "文件窗口 · 虚拟目录树与文件列表").Bold()
					ui.Row(c).FillWidth().Height(250).Gap(1).Border(1, c.Theme().Border).Children(func() {
						ui.Column(c).Width(180).FillHeight().Children(func() {
							rows := []TreeItem{{ID: "/", Name: "/", Parent: -1, Branch: true, Expanded: true}, {ID: "/srv", Name: "srv", Depth: 1, Parent: 0, Branch: true, Expanded: true}, {ID: "/srv/site", Name: "site", Depth: 2, Parent: 1, Branch: true}, {ID: "/etc", Name: "etc", Depth: 1, Parent: 0, Branch: true}}
							DirectoryTree(c, &treeState, len(rows), func(i int) TreeItem { return rows[i] }, TreeOptions{Label: "目录树"})
						})
						ui.Column(c).Grow(1).FillHeight().Children(func() {
							FileList(c, &fileState, len(entries), func(i int) FileEntry { return entries[i] }, FileListOptions{Label: "文件列表", ShowModified: true})
						})
					})
					ui.Text(c, "可排序表格 · 点表头切换顺序").Bold()
					rows := []struct {
						name string
						cpu  float64
					}{{"sshd", 1.2}, {"nginx", 0.4}, {"sub-store", 2.8}}
					slices.SortStableFunc(rows, func(a, b struct {
						name string
						cpu  float64
					}) int {
						v := strings.Compare(a.name, b.name)
						if order.Column == "cpu" {
							v = cmp.Compare(a.cpu, b.cpu)
						}
						if order.Descending {
							return -v
						}
						return v
					})
					processState.Key = func(i int) any { return rows[i].name }
					Table(c, &processState, []ui.TableColumn{{ID: "name", Title: "进程", Sortable: true}, {ID: "cpu", Title: "CPU %", Width: 100, Align: ui.End, Sortable: true}}, len(rows), func(i, col int) {
						text := rows[i].name
						if col == 1 {
							text = fmt.Sprintf("%.1f", rows[i].cpu)
						}
						CellText(c, text)
					}).FillWidth().Height(130).Label("进程列表")
					ui.Text(c, "弹窗操作区").Bold()
					ActionGroup(c, ui.End, func() { Button(c, "取消"); PrimaryButton(c, "保存") })
				})
				Dialog(c, &open, DialogOptions{Title: "新建文件夹", Target: "/srv/site"}, func() {
					ui.Field(c, "文件夹名称", func() { ui.TextInput(c, &value).FillWidth().Label("文件夹名称").AutoFocus() }).Error("请输入有效名称，不要包含路径分隔符")
				}, func() { Button(c, "取消"); PrimaryButton(c, "创建") })
			}
			width, height := 1000, 940
			if variant == "compact" {
				width, height = 760, 940
			}
			tt := ui.NewTester(view, width, height)
			tt.SetDark(variant == "dark")
			if _, ok := tt.Find("文件列表"); !ok && variant != "dialog" {
				t.Fatal("missing file list")
			}
			f, err := os.Create(filepath.Join(dir, "components-"+variant+".png"))
			if err != nil {
				t.Fatal(err)
			}
			err = png.Encode(f, tt.Image())
			closeErr := f.Close()
			if err != nil {
				t.Fatal(err)
			}
			if closeErr != nil {
				t.Fatal(closeErr)
			}
		})
	}
}
