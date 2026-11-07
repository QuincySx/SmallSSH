//go:build preview_qa && alva_prototype

package desktop

// Throwaway visual study: three workbench layouts and four companion screens.
import (
	"context"
	"embed"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/monitor"
	"sshclient/internal/snippet"
)

//go:embed testdata/alva-prototype/lucide/*.svg
var alvaProtoSVGFiles embed.FS

var alvaProtoSVGCache = map[string]*ui.SVG{}

func alvaProtoSVG(name string) *ui.SVG {
	if icon := alvaProtoSVGCache[name]; icon != nil {
		return icon
	}
	data, err := alvaProtoSVGFiles.ReadFile("testdata/alva-prototype/lucide/" + name + ".svg")
	if err != nil {
		panic(err)
	}
	icon := ui.MustParseSVG(data)
	alvaProtoSVGCache[name] = icon
	return icon
}

type alvaProtoPalette struct {
	canvas, sidebar, surface, subtle, hover, text, secondary, border, accent, accentSoft, control, success, danger, onAccent ui.Color
}

const alvaProtoWorkspaceGap float32 = 8
const alvaProtoSidebarWidth float32 = 204
const alvaProtoCompactSidebarWidth float32 = 32

func alvaProtoColors(dark bool) alvaProtoPalette {
	if dark {
		return alvaProtoPalette{ui.Hex("#14161B"), ui.Hex("#181B21"), ui.Hex("#1C1F26"), ui.Hex("#252932"), ui.Hex("#2C313C"), ui.Hex("#EAECF3"), ui.Hex("#A6ADBA"), ui.Hex("#303640"), ui.Hex("#AAA1FF"), ui.Hex("#2C2945"), ui.Hex("#929AA9"), ui.Hex("#32704F"), ui.Hex("#AC4050"), ui.Hex("#231F46")}
	}
	return alvaProtoPalette{ui.Hex("#F3F4F7"), ui.Hex("#F3F4F7"), ui.Hex("#FFFFFF"), ui.Hex("#F5F6F8"), ui.Hex("#E9EBF0"), ui.Hex("#272A34"), ui.Hex("#626B7A"), ui.Hex("#E2E5EB"), ui.Hex("#6555CD"), ui.Hex("#EFEDF9"), ui.Hex("#929AA9"), ui.Hex("#32704F"), ui.Hex("#AC4050"), ui.Hex("#FFFFFF")}
}

func alvaProtoTheme(c *ui.Context, p alvaProtoPalette, dark bool) {
	t := ui.LightTheme()
	if dark {
		t = ui.DarkTheme()
	}
	t.Background, t.Surface, t.SurfaceHover, t.Border = p.canvas, p.surface, p.hover, p.border
	t.Text, t.TextMuted, t.Accent, t.AccentText = p.text, p.secondary, p.accent, p.onAccent
	t.AccentHover, t.AccentPressed = p.accent.Mix(p.text, .12), p.accent.Mix(p.text, .22)
	t.Success, t.Danger, t.FontSize, t.Spacing, t.Radius = p.success, p.danger, 13, 4, 8
	c.SetTheme(t)
}

func alvaProtoText(c *ui.Context, text string, size float32, color ui.Color) ui.Element {
	return ui.Text(c, text).FontSize(size).TextColor(color)
}

func alvaProtoMakeButton(c *ui.Context, p alvaProtoPalette, text, variant, icon string, disabled, busy bool) ui.Element {
	original := c.Theme()
	theme := *original
	theme.Surface, theme.Border = p.subtle, ui.Transparent
	theme.SurfaceHover, theme.SurfacePressed = p.hover, p.hover.Mix(p.text, .08)
	if variant == "ghost" {
		theme.Surface = ui.Transparent
	}
	c.SetTheme(&theme)
	var button ui.Element
	if variant == "solid" {
		button = ui.PrimaryButton(c, "")
	} else {
		button = ui.Button(c, "")
	}
	button.Height(32).Shrink(0).Padding(0, 12).Gap(8).Radius(8).FontSize(13).FontWeight(500).Label(text).Disabled(disabled || busy)
	button.Children(func() {
		if busy {
			ui.Spinner(c).Size(16, 16)
		} else if icon != "" {
			ui.Icon(c, alvaProtoSVG(icon)).Size(18, 18)
		}
		if text != "" {
			ui.Text(c, text).SingleLine()
		}
	})
	c.SetTheme(original)
	return button
}

func alvaProtoButton(c *ui.Context, p alvaProtoPalette, text string, primary bool) {
	variant := "soft"
	if primary {
		variant = "solid"
	}
	alvaProtoMakeButton(c, p, text, variant, "", false, false)
}

func alvaProtoHeader(c *ui.Context, p alvaProtoPalette, caption string) {
	ui.Row(c).FillWidth().Height(36).Shrink(0).Padding(0, 12).Gap(12).AlignItems(ui.Center).Background(p.sidebar).Children(func() {
		alvaProtoText(c, caption, 13, p.text).FontWeight(600)
	})
}

func alvaProtoIcon(c *ui.Context, p alvaProtoPalette, icon, label string, color ui.Color) ui.Element {
	original := c.Theme()
	theme := *original
	theme.Surface, theme.Border, theme.Text = ui.Transparent, ui.Transparent, color
	theme.SurfaceHover, theme.SurfacePressed = p.hover, p.hover.Mix(p.text, .08)
	c.SetTheme(&theme)
	button := ui.Button(c, "").Size(32, 32).Shrink(0).Padding(0).Radius(8).Label(label).Tooltip(label).Children(func() {
		ui.Icon(c, alvaProtoSVG(icon)).Size(18, 18)
	})
	c.SetTheme(original)
	return button
}

// The same visual frame is hosted by standalone previews and the dialog.
func alvaProtoFrame(c *ui.Context, p alvaProtoPalette, title, target string, actions, body, footer func()) {
	ui.Column(c).Fill().Background(p.surface).Children(func() {
		ui.Row(c).FillWidth().Height(48).Shrink(0).Padding(0, 12).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
				alvaProtoText(c, title, 14, p.text).FontWeight(600).SingleLine()
				if target != "" {
					alvaProtoText(c, target, 11, p.secondary).SingleLine()
				}
			})
			if actions != nil {
				actions()
			}
			alvaProtoIcon(c, p, "x", "关闭"+title, p.secondary)
		})
		ui.Column(c).FillWidth().Grow(1).MinHeight(0).Children(body)
		if footer != nil {
			footer()
		}
	})
}

func alvaProtoWindow(c *ui.Context, p alvaProtoPalette, title, target string, actions, body, footer func()) {
	ui.Column(c).Fill().Background(p.canvas).Children(func() {
		alvaProtoFrame(c, p, title, target, actions, body, footer)
	})
}

func alvaProtoWorkbenchHeader(c *ui.Context, p alvaProtoPalette, variant string) {
	ui.Row(c).FillWidth().Height(32+2*alvaProtoWorkspaceGap).Shrink(0).Padding(0, alvaProtoWorkspaceGap, 0, 0).Gap(alvaProtoWorkspaceGap).AlignItems(ui.Center).Background(p.sidebar).Children(func() {
		ui.Row(c).Width(alvaProtoWorkspaceGap+alvaProtoSidebarWidth).Height(32+2*alvaProtoWorkspaceGap).Padding(0, 0, 0, 78).Gap(4).AlignItems(ui.Center).Children(func() {
			alvaProtoIcon(c, p, "panel-left", "切换服务器侧栏", p.secondary)
		})
		tabBackground := p.surface
		if strings.HasPrefix(variant, "files-") {
			tabBackground = p.sidebar
		}
		ui.Row(c).Height(32).MinWidth(0).Padding(0, 12).Gap(8).Radius(8).Background(tabBackground).AlignItems(ui.Center).Children(func() {
			alvaProtoText(c, "●", 11, p.success)
			alvaProtoText(c, "Web 生产环境", 13, p.text).FontWeight(600).MinWidth(0).SingleLine()
			alvaProtoIcon(c, p, "x", "关闭终端标签", p.secondary)
		})
		if strings.HasPrefix(variant, "files-") {
			ui.Row(c).Height(32).Padding(0, 8).Radius(8).Background(p.surface).AlignItems(ui.Center).Children(func() {
				alvaProtoText(c, "文件", 13, p.text).FontWeight(600)
				alvaProtoIcon(c, p, "x", "关闭文件标签", p.secondary)
			})
		}
		alvaProtoIcon(c, p, "plus", "新建终端", p.secondary)
		ui.Spacer(c).Grow(1)
		alvaProtoIcon(c, p, "download", "全局传输队列 · 2", p.secondary)
		alvaProtoText(c, "2", 11, p.secondary)
		right, bottom := p.secondary, p.accent
		if variant == "c" {
			right, bottom = p.accent, p.secondary
		}
		alvaProtoIcon(c, p, "panel-right", "工具面板放在右侧", right)
		alvaProtoIcon(c, p, "panel-bottom", "工具面板放在底部", bottom)
		alvaProtoIcon(c, p, "settings", "设置", p.secondary)
	})
}

func alvaProtoFileMenu(c *ui.Context, p alvaProtoPalette) {
	ui.Column(c).FillWidth().Padding(8).Gap(4).Children(func() {
		alvaProtoText(c, "更多文件操作", 13, p.text).FontWeight(600)
		for _, item := range []string{"切换来源…", "跟随终端 / 本机 / 已存服务器", "新建文件…", "属性与权限…", "重命名…", "移动…", "向右拆分", "向下拆分", "添加本地窗", "关闭窗", "上传文件", "下载", "删除…"} {
			color := p.text
			if item == "删除…" {
				color = p.danger
			}
			ui.Row(c).FillWidth().Height(32).Padding(0, 8).AlignItems(ui.Center).Children(func() { alvaProtoText(c, item, 12, color).SingleLine() })
		}
	})
}

func alvaProtoToolFrame(c *ui.Context, p alvaProtoPalette, mode string, expanded bool, body func()) {
	ui.Column(c).Fill().Radius(16).Background(p.surface).Border(1, p.border).Clip().Children(func() {
		ui.Row(c).FillWidth().Height(36).Padding(0, 8).Gap(12).AlignItems(ui.Center).Children(func() {
			for _, entry := range []string{"命令", "文件", "服务器状态"} {
				color := p.secondary
				if (mode == "commands" && entry == "命令") || (mode == "status" && entry == "服务器状态") || (mode == "files" && entry == "文件") {
					color = p.accent
				}
				alvaProtoText(c, entry, 13, color).FontWeight(600)
			}
			ui.Spacer(c).Grow(1)
			if expanded {
				alvaProtoIcon(c, p, "minimize-2", "返回停靠工具面板", p.secondary)
			} else {
				alvaProtoIcon(c, p, "maximize-2", "展开为工作区标签", p.secondary)
			}
			alvaProtoIcon(c, p, "ellipsis", "工具面板布局", p.secondary)
		})
		ui.Column(c).FillWidth().Grow(1).MinHeight(0).Children(body)
	})
}

func alvaProtoToolDemo(c *ui.Context, p alvaProtoPalette) {
	ui.Column(c).Fill().Padding(12).Gap(8).Children(func() {
		alvaProtoText(c, "命令分类   ·   常用   ·   运维   ·   自定义", 13, p.text)
		alvaProtoText(c, "命令参数", 14, p.text).FontWeight(600)
		alvaProtoField(c, p, "服务名称", "nginx")
		alvaProtoText(c, "参数历史  ·  最近使用：nginx", 12, p.secondary)
		alvaProtoText(c, "命令预览：systemctl status nginx", 12, p.text).Font("Menlo")
		ui.Row(c).Gap(8).Children(func() { alvaProtoButton(c, p, "填入终端", false); alvaProtoButton(c, p, "发送并执行", true) })
	})
}

func alvaProtoSidebar(c *ui.Context, p alvaProtoPalette, compact bool) {
	width := alvaProtoSidebarWidth
	if compact {
		width = alvaProtoCompactSidebarWidth
	}
	ui.Column(c).Width(width).FillHeight().Shrink(0).Padding(12, 0).Gap(8).Background(p.sidebar).Children(func() {
		if compact {
			alvaProtoIcon(c, p, "plus", "添加服务器", p.secondary)
			alvaProtoIcon(c, p, "search", "搜索服务器", p.secondary)
			for range 3 {
				ui.Row(c).Height(36).FillWidth().Center().Children(func() { ui.Icon(c, alvaProtoSVG("server")).Size(18, 18).TextColor(p.secondary) })
			}
			return
		}
		ui.Row(c).FillWidth().Height(32).AlignItems(ui.Center).Children(func() {
			alvaProtoText(c, "服务器", 14, p.text).FontWeight(600).Grow(1)
			alvaProtoIcon(c, p, "plus", "添加服务器", p.secondary)
		})
		ui.Row(c).FillWidth().Height(32).Padding(0, 8).Gap(8).Radius(8).Background(p.surface).Border(1, p.border).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, alvaProtoSVG("search")).Size(16, 16).TextColor(p.secondary)
			alvaProtoText(c, "搜索服务器", 12, p.secondary)
		})
		alvaProtoText(c, "我的服务器", 11, p.secondary)
		for i, item := range []struct{ name, host string }{{"Web 生产环境", "192.0.2.10"}, {"Staging", "198.51.100.20"}, {"备份服务器", "203.0.113.30"}} {
			bg := p.sidebar
			if i == 0 {
				bg = p.accentSoft
			}
			ui.Row(c).FillWidth().Height(44).Padding(0, 8).Gap(8).Radius(8).Background(bg).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Size(28, 28).Radius(8).Background(p.hover).Center().Children(func() { ui.Icon(c, alvaProtoSVG("server")).Size(16, 16).TextColor(p.accent) })
				ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
					alvaProtoText(c, item.name, 13, p.text).SingleLine()
					alvaProtoText(c, "demo@"+item.host, 11, p.secondary).SingleLine()
				})
			})
		}
	})
}

func alvaProtoTerminal(c *ui.Context, p alvaProtoPalette, height float32) {
	box := ui.Column(c).FillWidth().MinHeight(0).Radius(16).Background(p.subtle).Border(1, p.border).Clip()
	if height > 0 {
		box.Height(height).Shrink(0)
	} else {
		box.Grow(1)
	}
	box.Children(func() {
		ui.Column(c).Grow(1).FillWidth().Padding(12, 16).Gap(8).Children(func() {
			for _, line := range []string{"Last login: Tue Nov  3 09:41:08 from 192.0.2.1", "demo@web:~$ ls -la", "drwxr-xr-x  5 demo demo  4096  Nov  3 09:40  .", "drwxr-xr-x  3 root root  4096  Nov  1 10:12  ..", "drwxr-xr-x  2 demo demo  4096  Nov  3 09:35  projects", "demo@web:~$ ▍"} {
				alvaProtoText(c, line, 12, p.text).Font("Menlo").SingleLine()
			}
		})
	})
}

func alvaProtoFiles(c *ui.Context, p alvaProtoPalette, title, path string, compact bool, menuOpen *bool) {
	box := ui.Column(c).FillWidth().MinWidth(0).MinHeight(0).Grow(1).Background(p.surface).Border(1, p.border).Clip().Label(title + "文件面板")
	tree := []string{"⌄  /", "  ⌄ etc", "    nginx", "  ▸ srv"}
	rows := []struct{ icon, name, info, modified string }{{"folder", "sites-available", "目录", "11月 03日"}, {"folder", "sites-enabled", "目录", "11月 03日"}, {"file-text", "nginx.conf", "2.4 KiB", "11月 02日"}, {"file-text", "mime.types", "5.2 KiB", "10月 28日"}}
	if title == "本机" {
		tree = []string{"⌄  /", "  ⌄ SmallSSH-QA", "    ▸ uploads", "    ▸ reports"}
		rows = []struct{ icon, name, info, modified string }{{"folder", "uploads", "目录", "11月 05日"}, {"folder", "reports", "目录", "11月 04日"}, {"file-text", "deploy.sh", "1.1 KiB", "11月 03日"}, {"file-text", "readme.txt", "0.8 KiB", "11月 02日"}}
	} else if title == "Staging" {
		tree = []string{"⌄  /", "  ⌄ srv", "    ⌄ app", "      ▸ releases"}
		rows = []struct{ icon, name, info, modified string }{{"folder", "releases", "目录", "11月 05日"}, {"folder", "shared", "目录", "11月 04日"}, {"file-text", "app.env", "1.6 KiB", "11月 03日"}, {"file-text", "deploy.log", "8.0 KiB", "11月 02日"}}
	}
	box.Children(func() {
		ui.Row(c).FillWidth().Height(32).Padding(0, 8).Gap(4).AlignItems(ui.Center).Children(func() {
			alvaProtoText(c, title+" ▾", 12, p.text).MinWidth(0).SingleLine()
			alvaProtoText(c, path, 11, p.secondary).SingleLine().Grow(1).MinWidth(0)
			alvaProtoIcon(c, p, "folder-tree", "切换目录树", p.secondary)
			alvaProtoIcon(c, p, "arrow-up", "上一级目录", p.secondary)
			alvaProtoIcon(c, p, "refresh-cw", "刷新文件", p.secondary)
			if !compact {
				alvaProtoIcon(c, p, "folder-plus", "新建目录", p.secondary)
				alvaProtoIcon(c, p, "upload", "上传文件", p.secondary)
				alvaProtoIcon(c, p, "download", "下载", p.secondary)
			}
			anchor := alvaProtoIcon(c, p, "ellipsis", "更多：新建目录、上传、下载、重命名、权限、拆分、关闭窗", p.secondary)
			if menuOpen != nil {
				if anchor.Clicked() {
					*menuOpen = !*menuOpen
				}
				ui.PopoverBase(c, anchor, menuOpen, func(panel ui.Element) {
					panel.Width(240).Radius(12).Background(p.surface).Border(1, p.border).Children(func() { alvaProtoFileMenu(c, p) })
				})
			}
		})
		ui.Row(c).FillWidth().Grow(1).MinHeight(0).Children(func() {
			ui.Column(c).Width(108).FillHeight().Shrink(0).Padding(8).Gap(8).Background(p.subtle).Children(func() {
				for _, dir := range tree {
					alvaProtoText(c, dir, 11, p.secondary).SingleLine()
				}
			})
			ui.Column(c).Grow(1).FillHeight().MinWidth(0).Children(func() {
				ui.Row(c).FillWidth().Height(28).Padding(0, 8).Background(p.subtle).Children(func() {
					alvaProtoText(c, "名称", 11, p.secondary).Grow(1)
					if !compact {
						alvaProtoText(c, "大小         修改时间", 11, p.secondary)
					}
				})
				for _, row := range rows {
					ui.Row(c).FillWidth().Height(28).Padding(0, 8).Gap(8).AlignItems(ui.Center).Children(func() {
						ui.Icon(c, alvaProtoSVG(row.icon)).Size(16, 16).TextColor(p.secondary)
						alvaProtoText(c, row.name, 12, p.text).Grow(1).MinWidth(0).SingleLine()
						if !compact {
							alvaProtoText(c, row.info+"    "+row.modified, 11, p.secondary).SingleLine()
						}
					})
				}
			})
		})
		ui.Row(c).FillWidth().Height(28).Shrink(0).Children(func() {
			ui.Box(c).Width(108).FillHeight().Shrink(0).Background(p.subtle)
			ui.Row(c).Grow(1).FillHeight().Padding(0, 8).Justify(ui.End).AlignItems(ui.Center).Children(func() { alvaProtoText(c, "2 个文件 · 2 个目录 · 已选 0 项", 11, p.secondary).SingleLine() })
		})
	})
}

func alvaProtoWorkbench(c *ui.Context, p alvaProtoPalette, variant string, status *app, menuOpen *bool) {
	ui.Column(c).Fill().Background(p.canvas).Children(func() {
		alvaProtoWorkbenchHeader(c, p, variant)
		ui.Row(c).Grow(1).FillWidth().MinHeight(0).Padding(0, alvaProtoWorkspaceGap, alvaProtoWorkspaceGap, alvaProtoWorkspaceGap).Gap(alvaProtoWorkspaceGap).Children(func() {
			alvaProtoSidebar(c, p, variant == "c")
			ui.Column(c).Grow(1).FillHeight().MinWidth(0).MinHeight(0).Gap(alvaProtoWorkspaceGap).Children(func() {
				if variant == "b" {
					ui.Column(c).Grow(1).FillWidth().MinHeight(0).Children(func() {
						alvaProtoToolFrame(c, p, "files", false, func() {
							ui.Row(c).Fill().Gap(alvaProtoWorkspaceGap).Children(func() {
								ui.Column(c).Grow(1).FillHeight().MinWidth(0).Children(func() { alvaProtoFiles(c, p, "Web 生产环境", "/etc/nginx", false, nil) })
								ui.Column(c).Grow(1).FillHeight().MinWidth(0).Children(func() { alvaProtoFiles(c, p, "Staging", "/srv/app", false, nil) })
							})
						})
					})
					alvaProtoTerminal(c, p, 190)
				} else if variant == "c" {
					ui.Row(c).Grow(1).FillWidth().MinHeight(0).Gap(alvaProtoWorkspaceGap).Children(func() {
						ui.Column(c).Grow(1).FillHeight().MinWidth(0).Children(func() { alvaProtoTerminal(c, p, 0) })
						ui.Column(c).Width(320).FillHeight().Shrink(0).Children(func() {
							alvaProtoToolFrame(c, p, "files", false, func() { alvaProtoFiles(c, p, "Web", "/etc/nginx", true, nil) })
						})
					})
				} else if variant == "files-tab" || variant == "files-menu" {
					ui.Column(c).Grow(1).FillWidth().MinHeight(0).Children(func() {
						alvaProtoToolFrame(c, p, "files", true, func() {
							ui.Row(c).Fill().Gap(alvaProtoWorkspaceGap).Children(func() {
								ui.Column(c).Grow(1).FillHeight().MinWidth(0).Children(func() { alvaProtoFiles(c, p, "本机", "/SmallSSH-QA", false, nil) })
								ui.Column(c).Grow(1).FillHeight().MinWidth(0).Children(func() { alvaProtoFiles(c, p, "Web 生产环境", "/etc/nginx", false, menuOpen) })
							})
						})
					})
				} else if variant == "commands" || variant == "status" {
					alvaProtoTerminal(c, p, 0)
					ui.Column(c).FillWidth().Height(312).Shrink(0).Children(func() {
						alvaProtoToolFrame(c, p, variant, false, func() {
							if variant == "status" {
								status.diagnosticsView(c)
							} else {
								alvaProtoToolDemo(c, p)
							}
						})
					})
				} else if strings.HasPrefix(variant, "status-") {
					ui.Column(c).Grow(1).FillWidth().MinHeight(0).Children(func() { alvaProtoToolFrame(c, p, "status", true, func() { status.diagnosticsView(c) }) })
				} else {
					alvaProtoTerminal(c, p, 0)
					ui.Column(c).FillWidth().Height(312).Shrink(0).Children(func() {
						alvaProtoToolFrame(c, p, "files", false, func() { alvaProtoFiles(c, p, "Web 生产环境", "/etc/nginx", false, nil) })
					})
				}
			})
		})
	})
}

func alvaProtoField(c *ui.Context, p alvaProtoPalette, label, value string) {
	ui.Column(c).FillWidth().Gap(4).Children(func() {
		alvaProtoText(c, label, 12, p.secondary)
		ui.Row(c).FillWidth().Height(32).Padding(0, 12).Radius(8).Background(p.surface).Border(1, p.border).AlignItems(ui.Center).Children(func() { alvaProtoText(c, value, 13, p.text).SingleLine() })
	})
}

func alvaProtoGroup(c *ui.Context, p alvaProtoPalette) {
	ui.Column(c).FillWidth().Gap(4).Children(func() {
		alvaProtoText(c, "分组（可选）", 12, p.secondary)
		ui.Row(c).FillWidth().Height(32).Padding(0, 8).Radius(8).Background(p.surface).Border(1, p.border).AlignItems(ui.Center).Children(func() {
			alvaProtoText(c, "输入或选择分组", 12, p.secondary).Grow(1).MinWidth(0).SingleLine()
			ui.Icon(c, alvaProtoSVG("chevron-down")).Size(16, 16).TextColor(p.secondary)
		})
		alvaProtoText(c, "输入新名称可创建 · 未分组 / 我的服务器", 11, p.secondary).SingleLine()
	})
}

func alvaProtoSectionNav(c *ui.Context, p alvaProtoPalette, title string, entries []string, width float32) {
	ui.Column(c).Width(width).FillHeight().Shrink(0).Padding(8).Gap(4).Background(p.sidebar).Children(func() {
		if title != "" {
			alvaProtoText(c, title, 14, p.text).FontWeight(600)
			ui.Box(c).Height(8)
		}
		for i, entry := range entries {
			bg := p.sidebar
			if i == 0 {
				bg = p.accentSoft
			}
			ui.Row(c).FillWidth().Height(32).Padding(0, 8).Radius(8).Background(bg).AlignItems(ui.Center).Children(func() { alvaProtoText(c, entry, 13, p.text).SingleLine() })
		}
	})
}

func alvaProtoConnection(c *ui.Context, p alvaProtoPalette, narrow bool, fixture *app) {
	w, h := c.Size()
	alvaProtoWorkbench(c, p, "a", nil, nil)
	open := true
	ui.DialogBase(c, &open, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, .4))
		width, height := float32(760), float32(560)
		if narrow {
			width, height = 528, 640
		}
		panel.Width(min(width, w-32)).Height(min(height, h-32)).Padding(12).Radius(12).Clip().Background(p.surface).Border(1, p.border).Children(func() {
			alvaProtoFrame(c, p, "新建连接", "", nil, func() { fixture.connectionForm(c) }, func() {
				ui.Row(c).FillWidth().Height(32).Shrink(0).Gap(8).Justify(ui.End).Children(func() { alvaProtoButton(c, p, "取消", false); alvaProtoButton(c, p, "连接", true) })
			})
		})
	})
}

func alvaProtoKey(c *ui.Context, p alvaProtoPalette) {
	alvaProtoWindow(c, p, "安装登录公钥", "Web生产环境 · demo@192.0.2.10", nil, func() {
		ui.Column(c).FillWidth().MaxWidth(680).Padding(12).Gap(12).Children(func() {
			ui.Row(c).FillWidth().Height(40).Gap(16).AlignItems(ui.Center).Children(func() {
				for i, s := range []string{"① 选身份", "② 安装", "③ 独立验证"} {
					color := p.secondary
					if i == 0 {
						color = p.accent
					}
					alvaProtoText(c, s, 13, color)
				}
			})
			ui.Column(c).FillWidth().Padding(12).Gap(8).Radius(12).Background(p.surface).Border(1, p.border).Children(func() {
				alvaProtoText(c, "◉  此服务器专用密钥", 13, p.text)
				alvaProtoText(c, "私钥加密保存在本机。", 12, p.secondary)
				alvaProtoText(c, "○  硬件密钥", 13, p.text)
				alvaProtoText(c, "公钥安装到服务器，私钥留在设备中。", 12, p.secondary)
			})
		})
	}, func() {
		ui.Row(c).FillWidth().Height(56).Shrink(0).Padding(12).Gap(8).Justify(ui.End).Children(func() { alvaProtoButton(c, p, "取消", false); alvaProtoButton(c, p, "生成并继续", true) })
	})
}

func alvaProtoFile(c *ui.Context, p alvaProtoPalette) {
	alvaProtoWindow(c, p, "nginx.conf", "demo@192.0.2.10  ·  /etc/nginx", func() {
		alvaProtoIcon(c, p, "save", "保存文件", p.secondary)
	}, func() {
		ui.Column(c).Grow(1).FillWidth().Padding(12).Gap(8).Background(p.surface).Children(func() {
			for _, line := range []string{"1   user nginx;", "2   worker_processes auto;", "3   error_log /var/log/nginx/error.log;", "4", "5   events {", "6       worker_connections 1024;", "7   }", "8", "9   http {", "10      include /etc/nginx/mime.types;", "11      server {", "12          listen 80;", "13          server_name example.test;", "14          root /srv/www;", "15      }", "16  }"} {
				alvaProtoText(c, line, 13, p.text).Font("Menlo")
			}
		})
	}, func() {
		ui.Row(c).FillWidth().Height(28).Shrink(0).Padding(0, 12).AlignItems(ui.Center).Background(p.subtle).Children(func() {
			alvaProtoText(c, "16 行  ·  行 7，列 1", 11, p.secondary).Grow(1)
			alvaProtoText(c, "UTF-8  ·  LF  ·  2.4 KiB", 11, p.secondary)
		})
	})
}

func alvaProtoSettings(c *ui.Context, p alvaProtoPalette) {
	ui.Row(c).Fill().Padding(8).Gap(8).Background(p.canvas).Children(func() {
		ui.Column(c).Width(200).FillHeight().Shrink(0).Padding(12).Gap(4).Radius(12).Background(p.surface).Children(func() {
			alvaProtoText(c, "设置", 14, p.text).FontWeight(600)
			ui.Box(c).Height(8)
			for i, item := range []string{"工作区", "授权中心", "外观", "终端与剪贴板", "文件与传输", "安全与解锁", "运行与后台"} {
				bg := p.surface
				if i == 0 {
					bg = p.accentSoft
				}
				ui.Row(c).FillWidth().Height(32).Padding(0, 8).Radius(8).Background(bg).AlignItems(ui.Center).Children(func() { alvaProtoText(c, item, 13, p.text).SingleLine() })
			}
		})
		ui.Column(c).Grow(1).FillHeight().MinWidth(0).Padding(12).Gap(8).Radius(12).Background(p.surface).Children(func() {
			ui.Row(c).FillWidth().Height(32).AlignItems(ui.Center).Children(func() {
				alvaProtoText(c, "工作区", 14, p.text).FontWeight(600).Grow(1)
				alvaProtoIcon(c, p, "x", "关闭设置", p.secondary)
			})
			for _, row := range []struct{ title, detail, value string }{
				{"选中即复制", "选中文本后复制到剪贴板", "开启"},
				{"OSC 52", "写入前询问，拒绝读取", "写入询问 · 读取拒绝"},
				{"自动重连", "连接中断后重新连接", "开启"},
				{"SSH 保活间隔", "定期检查连接状态", "60 秒"},
				{"并发传输", "同时进行的文件传输", "3"},
				{"打开前询问", "文件大于 10 MiB 时先确认是否打开", "询问"},
				{"单文件独立窗口", "在单独窗口中打开文件", "开启"},
			} {
				ui.Row(c).FillWidth().Height(56).Gap(12).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(p.border).Padding(0, 12).Children(func() {
					ui.Column(c).Grow(1).Gap(4).Children(func() {
						alvaProtoText(c, row.title, 13, p.text).FontWeight(600)
						alvaProtoText(c, row.detail, 11, p.secondary)
					})
					alvaProtoText(c, row.value, 12, p.accent)
				})
			}
		})
	})
}

func alvaProtoButtons(c *ui.Context, p alvaProtoPalette) {
	ui.Column(c).Fill().Background(p.canvas).Children(func() {
		ui.Column(c).FillWidth().MaxWidth(840).Padding(24).Gap(16).Children(func() {
			alvaProtoText(c, "按钮组件", 20, p.text).FontWeight(600)
			alvaProtoText(c, "Radix 视觉适配 · Lucide 图标 · MyGo 原生交互", 12, p.secondary)
			ui.Column(c).FillWidth().Padding(24).Gap(16).Radius(16).Background(p.surface).Border(1, p.border).Children(func() {
				alvaProtoText(c, "主要与次要操作", 14, p.text).FontWeight(600)
				ui.Row(c).Gap(8).Children(func() {
					alvaProtoMakeButton(c, p, "连接", "solid", "", false, false)
					alvaProtoMakeButton(c, p, "取消", "soft", "", false, false)
					alvaProtoMakeButton(c, p, "添加服务器", "solid", "plus", false, false)
					alvaProtoMakeButton(c, p, "继续", "soft", "arrow-right", false, false)
				})
				alvaProtoText(c, "工具与选中", 14, p.text).FontWeight(600)
				ui.Row(c).Gap(8).Children(func() {
					alvaProtoIcon(c, p, "settings", "设置", p.secondary)
					alvaProtoIcon(c, p, "panel-bottom", "工具面板放在底部", p.accent)
					alvaProtoIcon(c, p, "search", "搜索", p.secondary)
				})
				alvaProtoText(c, "不可用与处理中", 14, p.text).FontWeight(600)
				ui.Row(c).Gap(8).Children(func() {
					alvaProtoMakeButton(c, p, "连接", "solid", "", true, false)
					alvaProtoMakeButton(c, p, "连接中", "solid", "", false, true)
				})
			})
		})
	})
}

func TestAlvaStylePrototypeSnapshotsWithoutWindows(t *testing.T) {
	if os.Getenv("SSH_CLIENT_PREVIEW_QA") != "1" {
		t.Skip("opt-in offline visual prototype")
	}
	dir := os.Getenv("SSH_CLIENT_UI_SNAPSHOT_DIR")
	if !filepath.IsAbs(dir) {
		t.Fatal("absolute snapshot directory required")
	}
	buildPath, err := filepath.Abs(filepath.Join("..", "..", "build"))
	if err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(buildPath, dir)
	if err != nil || filepath.IsAbs(rel) || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		t.Fatal("snapshot directory must be a subdirectory of build")
	}
	root, err := os.OpenRoot(buildPath)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := root.MkdirAll(rel, 0700); err != nil {
		t.Fatal(err)
	}
	outRoot, err := root.OpenRoot(rel)
	if err != nil {
		t.Fatal(err)
	}
	defer outRoot.Close()
	write := func(screen, variant string, dark, narrow bool) {
		t.Helper()
		theme := "light"
		if dark {
			theme = "dark"
		}
		name := fmt.Sprintf("alva-%s-%s-%s", screen, variant, theme)
		width, height := 1280, 860
		switch screen {
		case "settings":
			width, height = 1000, 720
		case "key":
			width, height = 720, 420
		case "file":
			width, height = 1100, 760
		case "command-edit":
			width, height = 760, 860
		case "command-run":
			width, height = 720, 760
		}
		if narrow {
			name += "-narrow"
			width, height = 560, 760
		}
		p := alvaProtoColors(dark)
		var connection, command *app
		if screen == "connection" {
			connection = &app{ctx: context.Background(), online: true, page: 1, sidebarWidth: 190, groups: map[string]*bool{}}
			connection.newConnection()
			connection.form.Name, connection.form.Host, connection.form.User = "示例服务器", "192.0.2.10", "demo"
		}
		if screen == "command-edit" || screen == "command-run" {
			command = &app{online: true}
			if screen == "command-edit" {
				command.newSnippet()
				command.snippetForm = snippet.Command{ID: "update", Name: "更新系统", Group: "运维", Body: "sudo apt update && sudo apt upgrade -y $1", Args: []snippet.Arg{{Type: snippet.TypeText, Name: "软件包", Preset: "nginx"}}}
				command.syncSnippetArgs()
			} else {
				cmd := snippet.Command{ID: "inspect", Name: "查看容器", Body: "pct config $1", Args: []snippet.Arg{{Name: "ContainerID", Type: snippet.TypeInt, Preset: "100"}}}
				state := &snippetRunState{command: cmd, label: "示例节点 · demo@192.0.2.10:22", system: snippet.OSDefault}
				command.fillSnippetValues(state)
				state.histories = [][]string{{"101", "102"}}
				command.prompt = promptState{open: true, snippet: state, target: state.label}
			}
		}
		var status *app
		if screen == "status" || strings.HasPrefix(screen, "status-") {
			status = responsiveWorkspaceDemo()
			status.sidebarWidth, status.toolsRight, status.statusMetric = 220, false, ""
			switch screen {
			case "status-processes":
				status.diagnosticTab = 1
			case "status-services":
				status.diagnosticTab = 2
			case "status-system":
				status.diagnosticTab = 3
			case "status-docker":
				status.diagnosticTab = 4
			case "status-ports":
				status.diagnosticTab = 5
				stats := status.sessions[0].stats
				stats.PortSnapshot = &monitor.PortSnapshot{Time: stats.Time, Status: "ready", Source: "ss", Ports: []monitor.Port{
					{Protocol: "TCP", State: "LISTEN", LocalAddress: "0.0.0.0", LocalPort: "443", PeerAddress: "*", PeerPort: "*", Process: "nginx", PID: 445, Owners: []monitor.PortOwner{{Process: "nginx", PID: 445}, {Process: "nginx", PID: 446}}},
					{Protocol: "TCP", State: "LISTEN", LocalAddress: "2001:db8::10", LocalPort: "8443", PeerAddress: "*", PeerPort: "*", Process: "web", PID: 1200},
					{Protocol: "UDP", State: "UNCONN", LocalAddress: "0.0.0.0", LocalPort: "51820", PeerAddress: "*", PeerPort: "*"},
					{Protocol: "TCP", State: "ESTAB", LocalAddress: "192.0.2.10", LocalPort: "22", PeerAddress: "198.51.100.24", PeerPort: "56342", Process: "sshd", PID: 812},
					{Protocol: "TCP", State: "ESTAB", LocalAddress: "2001:db8::10", LocalPort: "443", PeerAddress: "2001:db8::20", PeerPort: "50123", Process: "nginx", PID: 445},
				}}
				if dark {
					stats.PortSnapshot.Ports = stats.PortSnapshot.Ports[3:]
					status.managementState().portScope = portScopeConnections
				} else {
					stats.PortSnapshot.Status = "partial"
					stats.PortSnapshot.ProcessLimited = true
				}
			default:
				status.diagnosticTab = 0
			}
		}
		menuOpen := screen == "files-menu"
		tt := ui.NewTester(func(c *ui.Context) {
			alvaProtoTheme(c, p, dark)
			switch screen {
			case "workbench":
				alvaProtoWorkbench(c, p, variant, nil, nil)
			case "connection":
				alvaProtoConnection(c, p, narrow, connection)
			case "key":
				alvaProtoKey(c, p)
			case "file":
				alvaProtoFile(c, p)
			case "settings":
				alvaProtoSettings(c, p)
			case "buttons":
				alvaProtoButtons(c, p)
			case "command-edit":
				command.snippetDialog(c)
			case "command-run":
				command.promptView(c)
			case "files-tab", "files-menu", "commands", "status", "status-expanded", "status-processes", "status-services", "status-system", "status-docker", "status-ports":
				alvaProtoWorkbench(c, p, screen, status, &menuOpen)
			}
		}, width, height)
		tt.SetLocale("zh-Hans")
		path := filepath.Join(dir, name+".png")
		f, err := outRoot.OpenFile(name+".png", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
		if err != nil {
			t.Fatal(err)
		}
		err = png.Encode(f, tt.Image())
		closeErr := f.Close()
		if err != nil || closeErr != nil {
			t.Fatal(err, closeErr)
		}
		if screen == "files-menu" {
			before, ok := tt.Find("Web 生产环境文件面板")
			if !ok || !menuOpen || !tt.HasText("更多文件操作") {
				t.Fatal("file menu did not open over the Web pane", before)
			}
			tt.Key(0, ui.KeyEscape)
			tt.Frame()
			after, found := tt.Find("Web 生产环境文件面板")
			if menuOpen || tt.HasText("更多文件操作") || !found || after.X != before.X || after.W != before.W {
				t.Fatal("Escape did not dismiss the menu without changing the split", before, after)
			}
			menuOpen = true
			tt.Frame()
			if !tt.HasText("更多文件操作") {
				t.Fatal("file menu did not reopen")
			}
			tt.ClickAt(5, 5)
			tt.Frame()
			if menuOpen || tt.HasText("更多文件操作") {
				t.Fatal("outside click did not dismiss the file menu")
			}
		}
		t.Log(path)
	}
	for _, screen := range []string{"workbench", "connection", "key", "file", "settings", "buttons", "files-tab", "files-menu", "commands", "command-edit", "command-run", "status", "status-expanded", "status-processes", "status-services", "status-system", "status-docker", "status-ports"} {
		write(screen, "a", false, false)
		write(screen, "a", true, false)
	}
	write("workbench", "b", false, false)
	write("workbench", "c", false, false)
	write("connection", "a", false, true)
	path := filepath.Join(dir, "index.html")
	if err := outRoot.WriteFile("index.html", []byte(alvaProtoIndex), 0600); err != nil {
		t.Fatal(err)
	}
	t.Log(path)
}

const alvaProtoIndex = `<!doctype html>
<html lang="zh-Hans"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>SmallSSH × Alva · 离线视觉原型</title><style>
:root{font-family:-apple-system,BlinkMacSystemFont,"PingFang SC",sans-serif;color:#272a34;background:#f3f4f7}*{box-sizing:border-box}body{margin:0}header{padding:20px 24px;background:#fff;border-bottom:1px solid #e2e5eb}h1{font-size:18px;margin:0 0 6px}p{margin:0;color:#626b7a;font-size:13px}.controls{display:flex;gap:12px;align-items:center;flex-wrap:wrap;margin-top:16px}label{font-size:12px;color:#626b7a}select{margin-left:6px;padding:6px 8px;border:1px solid #929aa9;background:#fff;border-radius:8px;color:#272a34}main{padding:24px 24px 90px;text-align:center}img{display:block;margin:auto;max-width:100%;height:auto;border:1px solid #e2e5eb;border-radius:12px;box-shadow:0 8px 24px #272a3414}.switcher{position:fixed;bottom:16px;left:50%;transform:translateX(-50%);display:flex;align-items:center;gap:14px;background:#272a34;color:white;border-radius:24px;padding:6px 8px;box-shadow:0 4px 18px #0003;font-size:13px;white-space:nowrap}.switcher button{border:0;background:#ffffff20;color:white;border-radius:50%;width:32px;height:32px;font-size:18px;cursor:pointer}#state{min-width:160px;text-align:center}
</style></head><body><header><h1>SmallSSH × Alva · 离线视觉原型</h1><p>视觉原型 · 合成数据 · 无真实操作。Radix 视觉适配 + Lucide，未直接安装 React 库；窗口外观为原型，数据为合成示例，不进行真实连接或密钥操作。</p><div class="controls"><label>界面<select id="screen"></select></label><label>工作台方案<select id="variant"></select></label><label>主题<select id="theme"></select></label><label>连接视图<select id="size"></select></label></div></header><main><img id="preview" alt="SmallSSH 离线视觉原型"></main><div class="switcher"><button id="prev" aria-label="上一个">←</button><span id="state"></span><button id="next" aria-label="下一个">→</button></div><script>
const screens={workbench:'工作台',connection:'新建连接',key:'服务器密钥',file:'独立文件',settings:'设置',buttons:'按钮组件','files-tab':'文件大标签','files-menu':'文件操作菜单',commands:'命令面板','command-edit':'命令编辑','command-run':'命令运行参数',status:'服务器状态（停靠）','status-expanded':'状态概览','status-processes':'状态进程','status-services':'状态自启服务','status-system':'状态系统','status-docker':'状态 Docker','status-ports':'状态端口'};
const variants={a:'A · 终端优先',b:'B · 文件并排',c:'C · 紧凑侧栏'};
const themes={light:'浅色',dark:'深色'},sizes={wide:'标准',narrow:'窄窗'};
const states=[['workbench','a','light','wide'],['workbench','b','light','wide'],['workbench','c','light','wide'],['workbench','a','dark','wide'],['connection','a','light','wide'],['connection','a','dark','wide'],['connection','a','light','narrow'],['key','a','light','wide'],['key','a','dark','wide'],['file','a','light','wide'],['file','a','dark','wide'],['settings','a','light','wide'],['settings','a','dark','wide'],['buttons','a','light','wide'],['buttons','a','dark','wide'],['files-tab','a','light','wide'],['files-tab','a','dark','wide'],['files-menu','a','light','wide'],['files-menu','a','dark','wide'],['commands','a','light','wide'],['commands','a','dark','wide'],['command-edit','a','light','wide'],['command-edit','a','dark','wide'],['command-run','a','light','wide'],['command-run','a','dark','wide'],['status','a','light','wide'],['status','a','dark','wide'],['status-expanded','a','light','wide'],['status-expanded','a','dark','wide'],['status-processes','a','light','wide'],['status-processes','a','dark','wide'],['status-services','a','light','wide'],['status-services','a','dark','wide'],['status-system','a','light','wide'],['status-system','a','dark','wide'],['status-docker','a','light','wide'],['status-docker','a','dark','wide'],['status-ports','a','light','wide'],['status-ports','a','dark','wide']];
const screen=document.getElementById('screen'),variant=document.getElementById('variant'),theme=document.getElementById('theme'),size=document.getElementById('size'),preview=document.getElementById('preview'),state=document.getElementById('state');
function options(el,values){for(const [value,label] of Object.entries(values)){const o=document.createElement('option');o.value=value;o.textContent=label;el.appendChild(o)}}
options(screen,screens);options(variant,variants);options(theme,themes);options(size,sizes);
const q=new URLSearchParams(location.search);screen.value=Object.hasOwn(screens,q.get('screen'))?q.get('screen'):'workbench';variant.value=Object.hasOwn(variants,q.get('variant'))?q.get('variant'):'a';theme.value=Object.hasOwn(themes,q.get('theme'))?q.get('theme'):'light';size.value=q.get('size')==='narrow'?'narrow':'wide';
function currentIndex(){return states.findIndex(s=>s[0]===screen.value&&s[1]===variant.value&&s[2]===theme.value&&s[3]===size.value)}
function render(){if(screen.value!=='workbench'||theme.value==='dark')variant.value='a';if(screen.value!=='connection'||theme.value==='dark')size.value='wide';variant.disabled=screen.value!=='workbench'||theme.value==='dark';size.disabled=screen.value!=='connection'||theme.value==='dark';const suffix=size.value==='narrow'?'-narrow':'';preview.src='alva-'+screen.value+'-'+variant.value+'-'+theme.value+suffix+'.png';state.textContent=(currentIndex()+1)+'/'+states.length+' · '+screens[screen.value]+' · '+variants[variant.value]+' · '+themes[theme.value]+(size.value==='narrow'?' · 窄窗':'');const next=new URL(location.href);next.searchParams.set('screen',screen.value);next.searchParams.set('variant',variant.value);next.searchParams.set('theme',theme.value);if(size.value==='narrow')next.searchParams.set('size','narrow');else next.searchParams.delete('size');history.replaceState(null,'',next)}
function step(delta){const next=states[(currentIndex()+delta+states.length)%states.length];[screen.value,variant.value,theme.value,size.value]=next;render()}
for(const el of [screen,variant,theme,size])el.addEventListener('change',render);document.getElementById('prev').addEventListener('click',()=>step(-1));document.getElementById('next').addEventListener('click',()=>step(1));document.addEventListener('keydown',e=>{const el=document.activeElement;if(el&&(el.matches('input,textarea,select')||el.isContentEditable))return;if(e.key==='ArrowLeft'||e.key==='ArrowRight'){e.preventDefault();step(e.key==='ArrowLeft'?-1:1)}});render();
</script></body></html>`
