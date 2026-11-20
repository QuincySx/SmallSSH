package desktop

import (
	"github.com/egoist/mygo/ui"
	"sshclient/docs/design"
	"sshclient/internal/desktop/components"
)

const (
	connectionGeneral  = "general"
	connectionNetwork  = "network"
	connectionTerminal = "terminal"
	connectionAccess   = "access"
)

type connectionSection struct {
	id, title, heading, description string
	icon                            *ui.SVG
	render                          func(*app, *ui.Context)
}

var connectionSections = []connectionSection{
	{connectionGeneral, "常规", "常规（Normal）", "服务器地址、登录信息与备注", components.WorkspaceIcon, (*app).connectionGeneralForm},
	{connectionNetwork, "加速与代理", "加速与代理", "选择默认代理线路或 SSH 跳板机；右键可临时选用线路。", components.TransfersIcon, (*app).connectionNetworkForm},
	{connectionTerminal, "终端设置", "终端设置", "字符编码、退格键与 SSH 保活", components.CodeFileIcon, (*app).connectionTerminalForm},
	{connectionAccess, "访问策略", "访问策略", "设置此服务器的 AI 访问与关键操作保护。", components.ShieldIcon, (*app).connectionPolicyForm},
}

func connectionSectionIndex(id string) int {
	for i, section := range connectionSections {
		if section.id == id {
			return i
		}
	}
	return 0
}

// Navigation and actions stay fixed; each category owns its scroll position.
// Selecting a category only changes presentation, never connection settings.
func (a *app) connectionForm(c *ui.Context) {
	if a.connection.focusPolicy {
		a.connection.section, a.connection.focusName = connectionAccess, false
	}
	index := connectionSectionIndex(a.connection.section)
	a.connection.section = connectionSections[index].id
	sidebarWidth := design.Length("size-dialog-sidebar")
	for _, section := range connectionSections {
		textWidth, _ := c.MeasureText(0, ui.Span{Text: tr(c, section.title), Size: 13})
		// Sidebar padding, icon slot, text margins.
		sidebarWidth = max(sidebarWidth, textWidth+2*(components.DialogSidebarInset+1)+components.NavigationIconSlotSize+components.NavigationTextGap+8)
	}
	ui.Row(c).FillWidth().Grow(1).MinHeight(0).Gap(components.SectionGap).Label(tr(c, "连接分类布局")).Children(func() {
		components.DialogSidebar(c, "connection-sections", tr(c, "连接分类"), sidebarWidth, func() {
			for i, section := range connectionSections {
				if components.NavigationIcon(c, "connection-section:"+section.id, tr(c, section.title), section.icon, i == index, false, "").
					Description(tr(c, section.description)).Label(tr(c, "连接分类：") + tr(c, section.title)).Disabled(a.connection.phase != "").Clicked() {
					index = i
					a.connection.section, a.connection.focusName = section.id, false
				}
			}
		})
		section := connectionSections[index]
		c.Key("connection-content:" + section.id)
		ui.Scroll(c).Grow(1).FillHeight().MinWidth(0).MinHeight(0).TrackScroll(&a.connection.sectionScrolls[index]).Label(tr(c, "连接分类内容")).Children(func() {
			ui.Column(c).FillWidth().Padding(0, 8, 8, 0).Gap(components.FormGap).Children(func() {
				ui.Column(c).FillWidth().Disabled(a.connection.phase != "" || (a.connection.pendingNote != nil && section.id != connectionGeneral)).Children(func() { section.render(a, c) })
			})
		})
	})
}

func (a *app) revealConnectionErrors() {
	section := ""
	for _, key := range []string{"name", "host", "port", "group", "user", "password", "key", "agent", "note"} {
		if a.connection.errors[key] != "" {
			section = connectionGeneral
			break
		}
	}
	if section == "" {
		network := a.form.Network
		network.KeepaliveSeconds, network.KeepaliveFailures = 0, 0
		if a.form.Terminal.Validate() != nil {
			section = connectionTerminal
		} else if network.Validate() != nil {
			section = connectionNetwork
		} else if a.form.Network.Validate() != nil {
			section = connectionTerminal
		} else if a.form.Auth == "pkcs11" && a.form.Hardware.Validate() != nil {
			section = connectionGeneral
		} else if a.connection.errors["policy"] != "" {
			section = connectionAccess
		}
	}
	if section != "" {
		a.connection.focusPaths = section == connectionAccess && a.connection.errors["policy"] != ""
		a.connection.section, a.connection.focusName = section, false
		a.connection.sectionScrolls[connectionSectionIndex(section)] = ui.ScrollState{}
	}
}
