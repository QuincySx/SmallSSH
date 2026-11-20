package desktop

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/desktop/components"
	"sshclient/internal/snippet"
)

func (a *app) snippetRunDialog(c *ui.Context) {
	state := a.prompt.snippet
	if state == nil {
		return
	}
	_, windowHeight := c.Size()
	components.Dialog(c, &a.prompt.open, components.DialogOptions{
		Title: tr(c, "发送常用命令"), Width: 680,
	}, func() {
		ui.Scroll(c).FillWidth().MaxHeight(max(80, windowHeight-320)).Children(func() {
			ui.Column(c).FillWidth().Gap(14).Children(func() {
				ui.Column(c).FillWidth().Padding(12).Gap(8).Radius(8).Background(c.Theme().Surface).Border(1, c.Theme().Border).Label(tr(c, "命令模板")).Children(func() {
					ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
						ui.Text(c, reviewText(state.command.Name)).FontSize(15).Bold().Grow(1)
						if components.Button(c, tr(c, "编辑命令"), components.ButtonOptions{Variant: components.Quiet}).Clicked() {
							a.prompt = promptState{}
							a.editSnippet(state.command)
						}
					})
					ui.Text(c, reviewText(snippet.PromptBody(state.command, state.system))).Font("monospace").FontSize(13).FillWidth().Selectable()
				})
				if len(state.command.Variants) > 0 {
					ui.Field(c, tr(c, "系统"), func() {
						if snippetSystemSelect(c, &state.system, snippet.Options(state.command), tr(c, "执行系统")).FillWidth().Changed() {
							state.values = nil
							a.fillSnippetValues(state)
							state.error = ""
						}
					})
				}
				for i, arg := range snippet.ArgsFor(state.command, state.system) {
					if arg.SkipPrompt {
						continue
					}
					if i >= len(state.values) {
						state.values = append(state.values, arg.Preset)
					}
					label := fmt.Sprintf("$%d · %s", i+1, snippetTypeLabel(c, arg.Type))
					if arg.Name != "" {
						label = fmt.Sprintf("%s（$%d）", arg.Name, i+1)
					}
					ui.Field(c, label, func() {
						ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
							if ui.TextInput(c, &state.values[i]).Grow(1).MinWidth(80).Height(components.FormHeight).
								Label(fmt.Sprintf(tr(c, "参数 %d 输入"), i+1)).Changed() {
								state.error = ""
							}
							a.snippetHistorySelect(c, state, i, arg)
						})
					})
				}
			})
		})
	}, func() {
		ui.Column(c).FillWidth().Gap(12).Children(func() {
			preview, err := snippet.Render(state.command, state.system, state.values)
			if err != nil {
				preview, _ = snippet.BodyFor(state.command, state.system)
			}
			ui.Column(c).FillWidth().Gap(6).Children(func() {
				ui.Text(c, tr(c, "实际发送")).FontSize(12).TextColor(c.Theme().TextMuted)
				ui.Scroll(c).FillWidth().MaxHeight(84).Radius(6).Background(c.Theme().Surface).Border(1, c.Theme().Border).Label(tr(c, "命令预览")).Children(func() {
					ui.Text(c, reviewText(preview)).Padding(10, 12).FillWidth().Selectable().FontSize(12).Font("monospace")
				})
			})
			if state.error != "" {
				ui.Text(c, reviewText(state.error)).FillWidth().FontSize(12).TextColor(c.Theme().Danger).Label(tr(c, "命令错误"))
			}
			ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Text(c, tr(c, "发送到")).FontSize(12).TextColor(c.Theme().TextMuted)
				a.snippetTargetSelect(c, state)
			})
			ask := !state.skip
			if ui.Checkbox(c, &ask, tr(c, "发送时询问")).Tooltip(tr(c, "关闭后仍可能需要填写参数或选择系统")).Changed() {
				state.skip = !ask
			}
			ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
				if components.Button(c, tr(c, "取消")).Clicked() {
					a.prompt = promptState{}
				}
				body, err := snippet.Render(state.command, state.system, state.values)
				fill := components.Button(c, tr(c, "填入终端")).Disabled(err != nil || strings.ContainsAny(body, "\r\n"))
				if strings.ContainsAny(body, "\r\n") {
					fill.Tooltip(tr(c, "多行命令请使用发送并执行"))
				}
				if fill.Clicked() {
					a.submitSnippetRun(state, false)
				}
				if components.Button(c, tr(c, "发送并执行"), components.ButtonOptions{Variant: components.Primary}).Clicked() {
					a.submitSnippetRun(state, true)
				}
			})
		})
	})
	if !a.prompt.open {
		a.prompt = promptState{}
	}
}

func snippetTargetLabel(state *snippetRunState) string {
	if state.target == nil {
		return state.label
	}
	label := sessionDisplayName(nil, state.target)
	if state.target.target.Endpoint != "" {
		label += " · " + state.target.target.Endpoint
	}
	return reviewText(label)
}

func (a *app) snippetTargetSelect(c *ui.Context, state *snippetRunState) {
	parts := ui.SelectBase(c, &state.target)
	theme := c.Theme()
	parts.Trigger.Grow(1).MinWidth(0).Height(34).Padding(6, 10).Radius(theme.Radius).Background(theme.Surface).Border(1, theme.Border).
		Label(tr(c, "命令目标会话")).Tooltip(snippetTargetLabel(state)).Children(func() { ui.Text(c, snippetTargetLabel(state)+" ⌄").FontSize(12).SingleLine() })
	parts.Popup(func(panel ui.Element) {
		panel.Width(360).Padding(4).MaxHeight(240).Background(theme.Background).Border(1, theme.Border).Radius(theme.Radius).Children(func() {
			ui.Scroll(c).FillWidth().MaxHeight(220).Children(func() {
				ui.Column(c).FillWidth().Children(func() {
					count := 0
					for _, target := range a.sessions {
						if target.closed || target.term == nil {
							continue
						}
						count++
						item := parts.Item(target).Padding(8).Radius(theme.Radius)
						if item.Highlighted() {
							item.Background(theme.Surface)
						}
						item.Tooltip(snippetTargetLabel(&snippetRunState{target: target})).Children(func() { ui.Text(c, snippetTargetLabel(&snippetRunState{target: target})).SingleLine().FontSize(12) })
					}
					if count == 0 {
						ui.Text(c, tr(c, "没有可发送的终端会话")).Padding(8).FontSize(12)
					}
				})
			})
		})
	})
	if parts.Trigger.Changed() {
		state.label = snippetTargetLabel(state)
		a.prompt.target = state.label
		state.system = snippet.ChooseSystem(state.command, a.snippetDetectedSystem(state.target))
		state.values = nil
		a.fillSnippetValues(state)
		state.error = ""
	}
}
