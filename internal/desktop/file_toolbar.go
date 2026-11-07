package desktop

import (
	"errors"
	"github.com/egoist/mygo/ui"
	"os"
	"path/filepath"
	"sshclient/internal/desktop/components"
	"sshclient/internal/sshconn"
)

func (a *app) fileToolbar(c *ui.Context, p *filePane, width float32) {
	b := p.browser()
	if width <= 0 {
		// On the first frame the pane has no previous bounds yet.
		width, _ = c.Size()
	}
	compact := width > 0 && width < 720
	tiny := width > 0 && width < 260
	ui.Row(c).FillWidth().Height(36).Shrink(0).MinWidth(0).Gap(2).AlignItems(ui.Center).Children(func() {
		sourceWidth := float32(144)
		if compact {
			sourceWidth = min(112, max(48, width*.25))
		}
		ui.Column(c).Width(sourceWidth).Shrink(0).MinWidth(0).Children(func() { a.fileLocationHeader(c, p) })
		input := ui.TextInput(c, &b.address).Grow(1).MinWidth(28).Height(30).Padding(4, 7).FontSize(components.BodyFont).
			Background(ui.Transparent).Border(1, ui.Transparent).Label(a.paneSide(p) + tr(c, "目录路径")).Tooltip(tr(c, "输入目录路径，按 Enter 打开"))
		if input.Submitted() {
			a.navigateDirectory(p, b.address)
		}
		if !tiny && components.SVGButton(c, components.FolderTreeIcon, tr(c, "显示或隐藏目录树")).Size(28, 28).Shrink(0).Clicked() {
			p.treeHidden = !p.treeHidden
		}
		if !compact && components.SVGButton(c, components.UpIcon, tr(c, "上一级")).Size(28, 28).Shrink(0).
			Disabled(p.endpoint == "" || parentDir(p.endpoint == localEndpoint, b.dir) == b.dir).Clicked() {
			a.navigateDirectory(p, parentDir(p.endpoint == localEndpoint, b.dir))
		}
		if components.SVGButton(c, components.RefreshIcon, a.paneSide(p)+tr(c, "刷新目录")).Size(28, 28).Shrink(0).
			Disabled(p.endpoint == "" || b.loading[b.dir] != nil).Clicked() {
			a.loadDirectory(p, b.dir, true)
		}
		if !compact {
			if components.SVGButton(c, components.FolderPlusIcon, tr(c, "新建文件夹")).Size(28, 28).Shrink(0).Disabled(a.busy || p.endpoint == "").Clicked() {
				a.fileOperationAt(p, b.dir, nil, "mkdir")
			}
			if components.SVGButton(c, components.UploadIcon, tr(c, "上传文件")).Size(28, 28).Shrink(0).Disabled(a.busy || p.endpoint == "" || p.endpoint == localEndpoint).Clicked() {
				a.promptPaneUpload(p.endpoint, b.dir)
			}
			entries := chosenFiles(b)
			if components.SVGButton(c, components.DownloadIcon, tr(c, "下载到本机…")).Size(28, 28).Shrink(0).Disabled(a.busy || p.endpoint == "" || p.endpoint == localEndpoint || len(entries) == 0).Clicked() {
				a.promptDownload(p.endpoint, b.dir, entries)
			}
		}
		components.SVGButton(c, components.MoreIcon, a.paneSide(p)+tr(c, "文件窗口菜单")).Key("file-window-menu").Size(28, 28).Shrink(0).Menu(func(m *ui.Menu) {
			a.activeFilePane = p
			a.fileContextMenu(m, p, b.dir, chosenFiles(b))
			if p.endpoint != "" && p.endpoint != localEndpoint {
				m.Separator()
				if m.Item(tr(c, "上传文件")).Disabled(a.busy).Chosen() {
					a.promptPaneUpload(p.endpoint, b.dir)
				}
			}
			m.Separator()
			if m.Item(tr(c, "显示或隐藏目录树")).Checked(!p.treeHidden).Chosen() {
				p.treeHidden = !p.treeHidden
			}
			a.fileWindowsMenu(m, p)
		})
	})
}

func (a *app) promptPaneUpload(endpoint, dir string) {
	if endpoint == "" || endpoint == localEndpoint {
		return
	}
	a.ask(tr(nil, "上传文件"), a.endpointName(endpoint)+" · "+reviewText(dir), tr(nil, "输入本机普通文件的完整路径，核对后上传到此目录。"), tr(nil, "本机文件完整路径"), "", tr(nil, "继续"), func(name string) {
		a.transferEntries(localEndpoint, filepath.Dir(name), []sshconn.File{{Name: filepath.Base(name)}}, endpoint, dir)
	})
	a.prompt.validate = func(name string) error {
		if !filepath.IsAbs(name) {
			return errors.New(tr(nil, "请填写本机绝对路径"))
		}
		info, err := os.Lstat(name)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return errors.New(tr(nil, "请选择普通文件；不覆盖符号链接"))
		}
		return nil
	}
}
