package desktop

import (
	"fmt"
	"strconv"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/connectcfg"
	"sshclient/internal/desktop/components"
	"sshclient/internal/proxyconfig"
)

func (a *app) savedRouteImport(c *ui.Context, s *savedRouteEditor) {
	if proxyconfig.Supported(s.value.Proxy.Mode) && !s.reimport && len(s.nodes) == 0 {
		ui.Column(c).FillWidth().Padding(12).Gap(6).Radius(8).Border(1, c.Theme().Border).Children(func() {
			ui.Text(c, proxyconfig.Label(s.value.Proxy.Mode)).FontSize(13).Bold().FillWidth()
			ui.Text(c, s.value.Proxy.Address).FontSize(12).FillWidth()
		})
		ui.Row(c).FillWidth().Children(func() {
			if components.Button(c, tr(c, "重新导入节点")).Clicked() {
				s.reimport = true
			}
		})
	}
	if len(s.nodes) == 0 && (!proxyconfig.Supported(s.value.Proxy.Mode) || s.reimport) {
		ui.Field(c, tr(c, "分享链接或节点 JSON"), func() {
			ui.TextArea(c, &s.importText).FillWidth().Height(132).Label(tr(c, "线路导入内容")).
				Placeholder(tr(c, "粘贴链接或 JSON")).Tooltip(tr(c, "支持单个节点、节点数组或 outbounds；仅导入代理节点。"))
		})
		ui.Row(c).FillWidth().Children(func() {
			if components.Button(c, tr(c, "解析节点")).Clicked() {
				nodes, err := proxyconfig.Import(s.importText)
				if err != nil {
					s.error = err.Error()
					s.nodes = nil
				} else {
					s.error, s.importText = "", ""
					s.nodes, s.selectedNode = nodes, "0"
					if len(nodes) == 1 {
						useImportedRoute(s, nodes[0])
					}
				}
			}
		})
	}
	if len(s.nodes) > 0 {
		ui.Text(c, fmt.Sprintf(tr(c, "已提取 %d 个支持的节点，请选择要保存的线路。原始输入已清除。"), len(s.nodes))).FillWidth().FontSize(11).TextColor(c.Theme().TextMuted)
		options := make([]connectionOption, 0, len(s.nodes))
		for i, node := range s.nodes {
			options = append(options, connectionOption{strconv.Itoa(i), node.Name + " · " + proxyconfig.Label(node.Kind) + " · " + node.Address})
		}
		optionSelect(c, "选择导入节点", &s.selectedNode, options)
		ui.Row(c).FillWidth().Gap(8).Children(func() {
			if components.Button(c, tr(c, "使用所选节点")).Clicked() {
				if i, err := strconv.Atoi(s.selectedNode); err == nil && i >= 0 && i < len(s.nodes) {
					useImportedRoute(s, s.nodes[i])
				}
			}
			if components.Button(c, tr(c, "重新粘贴")).Clicked() {
				s.nodes, s.selectedNode = nil, ""
			}
		})
	}
}

func useImportedRoute(s *savedRouteEditor, node proxyconfig.Node) {
	s.value.Proxy = connectcfg.Proxy{Mode: node.Kind, Address: node.Address, Config: node.Config, Password: node.Secret}
	s.value.Relay = ""
	if s.value.Name == "" {
		s.value.Name = node.Name
	}
	s.replacePassword, s.reimport = true, false
	s.nodes, s.importText, s.error = nil, "", ""
}
