package desktop

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/desktop/components"
	"sshclient/internal/snippet"
	"sshclient/internal/vault"
)

// The command filter uses values no owner-typed group can take.
const (
	snippetAllGroups  = "\x00all"
	snippetNoGroup    = "\x00none"
	snippetRPCTimeout = 30 * time.Second
)

type snippetRunState struct {
	command     snippet.Command
	target      *session
	label       string
	system      string
	values      []string
	histories   [][]string
	historyLoad uint64
	error       string
	skip        bool
}

func (a *app) snippetGroups() []string {
	seen := map[string]bool{}
	for _, command := range a.snippets {
		if group := strings.TrimSpace(command.Group); group != "" {
			seen[group] = true
		}
	}
	groups := make([]string, 0, len(seen))
	for group := range seen {
		groups = append(groups, group)
	}
	sort.Strings(groups)
	return groups
}

func (a *app) hasUngroupedSnippets() bool {
	for _, command := range a.snippets {
		if strings.TrimSpace(command.Group) == "" {
			return true
		}
	}
	return false
}

// snippetGroupChoices puts the two built-in filters before owner groups. Keys
// are stable; labels are localized for the popup.
func (a *app) snippetGroupChoices(c *ui.Context) (keys, labels []string) {
	keys = append(keys, snippetAllGroups)
	labels = append(labels, tr(c, "全部"))
	if a.hasUngroupedSnippets() {
		keys = append(keys, snippetNoGroup)
		labels = append(labels, tr(c, "未分组"))
	}
	for _, group := range a.snippetGroups() {
		keys = append(keys, group)
		labels = append(labels, group)
	}
	return keys, labels
}

func (a *app) snippetGroupLabel(c *ui.Context, key string) string {
	switch key {
	case snippetAllGroups:
		return tr(c, "全部")
	case snippetNoGroup:
		return tr(c, "未分组")
	}
	return reviewText(key)
}

func (a *app) newSnippet() {
	group := a.snippetGroup
	if group == snippetAllGroups || group == snippetNoGroup {
		group = ""
	}
	a.snippetDraft++
	a.snippetForm = snippet.Command{ID: vault.ShortID(), Group: group}
	a.snippetError = ""
	a.snippetOpen = true
	a.snippetDetached = a.toolsWindowFocused()
	a.invalidateWindows()
}

func (a *app) editSnippet(command snippet.Command) {
	a.snippetDraft++
	a.snippetForm = command.Clone()
	a.snippetError = ""
	a.snippetOpen = true
	a.snippetDetached = a.toolsWindowFocused()
	a.invalidateWindows()
}

func (a *app) commandsView(c *ui.Context) {
	ui.Column(c).FillWidth().Grow(1).MinHeight(0).Padding(8).Gap(8).Children(func() {
		keys, labels := a.snippetGroupChoices(c)
		if !slices.Contains(keys, a.snippetGroup) {
			a.snippetGroup = snippetAllGroups
		}
		selected := slices.Index(keys, a.snippetGroup)
		if selected < 0 {
			selected, a.snippetGroup = 0, keys[0]
		}
		ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
			tabs := ui.TabsBase(c, &selected, len(keys))
			tabs.List.Grow(1).MinWidth(0).Height(30).Gap(0).AlignItems(ui.Center).Label(tr(c, "命令分组")).Children(func() {
				for i, label := range labels {
					tab := tabs.Tab(i).Height(30).Padding(0, 10).Radius(8).Label(fmt.Sprintf(tr(c, "分组：%s"), label)).
						TextColor(c.Theme().TextMuted)
					if i == selected {
						tab.TextColor(c.Theme().Accent).Background(c.Theme().Accent.Alpha(.10))
					} else if tab.Hovered() {
						tab.TextColor(c.Theme().Text).Background(c.Theme().Border.Alpha(.35))
					}
					tab.Children(func() { components.HeaderLabel(c, label) })
				}
			})
			if tabs.List.Changed() && selected >= 0 && selected < len(keys) {
				a.snippetGroup = keys[selected]
				a.snippetSelected = -1
			}
			search := ui.TextInput(c, &a.snippetSearch).Width(150).Height(30).
				Label(tr(c, "搜索常用命令")).Placeholder(tr(c, "搜索命令"))
			if search.Changed() {
				a.snippetSelected = -1
			}
			if components.IconButton(c, "＋", tr(c, "新建常用命令")).Clicked() {
				a.newSnippet()
			}
		})
		items := a.filteredSnippets()
		if len(items) == 0 {
			ui.Column(c).FillWidth().Grow(1).MinHeight(0).AlignItems(ui.Center).Justify(ui.Center).Gap(8).Children(func() {
				// An empty state offers the action that ends it, rather than
				// leaving the user to find the small ＋ in the toolbar.
				if len(a.snippets) == 0 {
					ui.Text(c, tr(c, "暂无常用命令")).FontSize(11).TextColor(c.Theme().TextMuted)
					if components.Button(c, tr(c, "新建命令")).Clicked() {
						a.newSnippet()
					}
				} else {
					ui.Text(c, tr(c, "没有符合条件的命令")).FontSize(11).TextColor(c.Theme().TextMuted)
					if a.snippetSearch != "" && components.Button(c, tr(c, "清除搜索")).Clicked() {
						a.snippetSearch, a.snippetSelected = "", -1
					}
				}
			})
			return
		}
		a.snippetGrid.Selected = &a.snippetSelected
		a.snippetGrid.Key = func(i int) any {
			if i < len(items) {
				return items[i].ID
			}
			return fmt.Sprintf("empty-%d", i)
		}
		a.snippetGrid.Label = func(i int) string {
			if i < len(items) {
				return items[i].Name
			}
			return ""
		}
		layout := components.ResourceLayout{MinimumWidth: 150, MaximumColumns: 6}
		width := max(0, a.statusPaneWidth(c)-statusScrollGutter-16)
		// Keep the natural column count even when a tab has one command, so a
		// single command never stretches across the whole panel. Empty cells
		// only reserve the column width; they cannot be run.
		columns := components.ResourceColumns(width, layout.MinimumWidth, layout.MaximumColumns)
		renderCount := max(len(items), columns)
		grid := layout.CardGrid(c, &a.snippetGrid, width, renderCount, 60, func(i int) {
			if i < len(items) {
				a.commandCard(c, items[i], i)
				return
			}
			ui.Column(c).FillHeight()
		}).FillWidth().Grow(1).MinHeight(0).Padding(0, statusScrollGutter, 0, 0).Label(tr(c, "常用命令列表"))
		if grid.Submitted() && a.snippetSelected >= 0 && a.snippetSelected < len(items) {
			a.runSnippet(items[a.snippetSelected])
		}
	})
}

// commandCard is a compact tile: the full command belongs in the run dialog,
// not in every row. The grid supplies selection, keyboard navigation and the
// double-click/Enter Submitted event.
func (a *app) commandCard(c *ui.Context, command snippet.Command, index int) {
	target := a.workspaceCommandTarget()
	card := components.Surface(c, tr(c, "命令")+" "+command.Name).FillHeight().Padding(8, 12).Gap(3).Justify(ui.Center)
	if a.snippetSelected == index {
		card.Background(c.Theme().Accent.Alpha(.10))
	}
	card.Children(func() {
		ui.Text(c, reviewText(command.Name)).FontSize(13).Bold().FillWidth().MinWidth(0).SingleLine().Tooltip(command.Name)
		if a.snippetGroup == snippetAllGroups {
			group := strings.TrimSpace(command.Group)
			if group == "" {
				group = tr(c, "未分组")
			}
			ui.Text(c, reviewText(group)).FontSize(10).FillWidth().MinWidth(0).SingleLine().TextColor(c.Theme().TextMuted)
		}
	})
	card.ContextMenu(func(m *ui.Menu) {
		if m.Item(tr(c, "执行")).Chosen() {
			a.runSnippetFor(command, target)
		}
		if m.Item(tr(c, "编辑")).Chosen() {
			a.editSnippet(command)
		}
		if m.Item(tr(c, "删除")).Chosen() {
			a.ask(tr(c, "删除常用命令？"), command.Name, tr(c, "仅删除命令库中的这条记录。"), "", "", tr(c, "删除"), func(string) { a.persistSnippet(command, true, nil) })
		}
		m.Separator()
		if m.Item(tr(c, "执行前确认")).Checked(!command.SkipConfirm).Chosen() {
			command.SkipConfirm = !command.SkipConfirm
			a.persistSnippet(command, false, nil)
		}
	})
}

func (a *app) filteredSnippets() []snippet.Command {
	query := strings.ToLower(strings.TrimSpace(a.snippetSearch))
	items := make([]snippet.Command, 0, len(a.snippets))
	for _, command := range a.snippets {
		switch a.snippetGroup {
		case snippetAllGroups:
		case snippetNoGroup:
			if strings.TrimSpace(command.Group) != "" {
				continue
			}
		default:
			if command.Group != a.snippetGroup {
				continue
			}
		}
		if query != "" {
			var bodies strings.Builder
			bodies.WriteString(command.Name)
			bodies.WriteString(" ")
			bodies.WriteString(command.Group)
			bodies.WriteString(" ")
			bodies.WriteString(command.Body)
			for _, variant := range command.Variants {
				bodies.WriteString(" ")
				bodies.WriteString(variant.Body)
			}
			if !strings.Contains(strings.ToLower(bodies.String()), query) {
				continue
			}
		}
		items = append(items, command)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Group != items[j].Group {
			return items[i].Group < items[j].Group
		}
		return items[i].Name < items[j].Name
	})
	return items
}

func (a *app) snippetDialog(c *ui.Context) {
	_, windowHeight := c.Size()
	components.Dialog(c, &a.snippetOpen, components.DialogOptions{Title: tr(c, "保存常用命令"), Width: 640}, func() {
		ui.Scroll(c).FillWidth().MaxHeight(min(520, max(120, windowHeight-144))).Children(func() {
			ui.Form(c, func() {
				ui.TextInput(c, &a.snippetForm.Name).Label(tr(c, "命令名称")).Placeholder(tr(c, "命令名称")).FillWidth().AutoFocus()
				ui.Field(c, tr(c, "分类"), func() {
					components.EditableChoice(c, &a.snippetForm.Group, a.snippetGroups(), tr(c, "分类"), false).FillWidth().
						Placeholder(tr(c, "选择已有分类，或输入新分类"))
				})
				ui.Field(c, tr(c, "命令内容"), func() {
					body := ui.TextArea(c, &a.snippetForm.Body).Label(tr(c, "命令内容")).Height(96).FillWidth().
						Placeholder(tr(c, "命令内容，用 $1、$2 表示参数"))
					if body.Changed() {
						a.syncSnippetArgs()
					}
				})
				ask := !a.snippetForm.SkipConfirm
				if ui.Checkbox(c, &ask, tr(c, "发送时询问")).Changed() {
					a.snippetForm.SkipConfirm = !ask
				}
				if len(a.snippetForm.Args) == 0 && snippet.PlaceholderCount(a.snippetForm) > 0 {
					if components.Button(c, tr(c, "识别占位符")).Width(140).Clicked() {
						a.syncSnippetArgs()
						c.Invalidate()
					}
				}
				removeVariant := -1
				for i := range a.snippetForm.Variants {
					variant := &a.snippetForm.Variants[i]
					ui.Field(c, tr(c, "系统"), func() {
						ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
							snippetSystemSelect(c, &variant.OS, snippet.SystemKeys(), fmt.Sprintf(tr(c, "系统选择 %d"), i+1)).Grow(1)
							if components.SVGButton(c, components.CloseIcon, tr(c, "删除系统命令")).Clicked() {
								removeVariant = i
							}
						})
					})
					ui.Field(c, tr(c, "命令内容"), func() {
						body := ui.TextArea(c, &variant.Body).Label(fmt.Sprintf(tr(c, "系统命令 %d"), i+1)).Height(64).FillWidth()
						if body.Changed() {
							a.syncSnippetArgs()
						}
					})
				}
				if removeVariant >= 0 {
					a.snippetForm.Variants = slices.Delete(a.snippetForm.Variants, removeVariant, removeVariant+1)
					if len(a.snippetForm.Args) > 0 {
						a.syncSnippetArgs()
					}
					c.Invalidate()
				}
				if components.Button(c, tr(c, "添加系统命令")).Width(150).Disabled(!a.canAddSnippetVariant()).Clicked() {
					a.addSnippetVariant()
					c.Invalidate()
				}
				if len(a.snippetForm.Args) > 0 {
					ui.Field(c, tr(c, "参数"), func() {
						ui.Column(c).FillWidth().Gap(6).Children(func() {
							for i := range a.snippetForm.Args {
								arg := &a.snippetForm.Args[i]
								ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
									ui.Text(c, fmt.Sprintf("$%d", i+1)).Width(28).FontSize(12)
									ui.TextInput(c, &arg.Name).Width(112).Height(30).
										Label(fmt.Sprintf(tr(c, "参数 %d 名称"), i+1)).Placeholder(tr(c, "名称"))
									snippetTypeSelect(c, &arg.Type, fmt.Sprintf(tr(c, "参数 %d 类型"), i+1)).Width(96)
									ui.TextInput(c, &arg.Preset).Grow(1).MinWidth(80).Height(30).
										Label(fmt.Sprintf(tr(c, "参数 %d 默认值"), i+1)).Placeholder(tr(c, "默认值"))
									fill := !arg.SkipPrompt
									if ui.Checkbox(c, &fill, tr(c, "发送时填写")).Changed() {
										arg.SkipPrompt = !fill
									}
								})
								remember := !arg.NoHistory
								if ui.Checkbox(c, &remember, tr(c, "记录输入历史")).Tooltip(tr(c, "历史随保险库加密保存；敏感参数可关闭记录")).Changed() {
									arg.NoHistory = !remember
								}
							}
						})
					})
				}
			})
			if a.snippetError != "" {
				ui.Text(c, a.snippetError).FillWidth().FontSize(12).TextColor(c.Theme().Danger).Label(tr(c, "命令错误"))
			}
		})
	}, func() {
		if components.Button(c, tr(c, "取消")).Clicked() {
			a.snippetOpen = false
		}
		if components.Button(c, tr(c, "保存命令"), components.ButtonOptions{Variant: components.Primary, Busy: a.busy}).Clicked() {
			a.saveSnippetForm()
		}
	})
	if !a.snippetOpen {
		a.snippetForm, a.snippetError = snippet.Command{}, ""
	}
}

func (a *app) canAddSnippetVariant() bool {
	if len(a.snippetForm.Variants) >= snippet.MaxVariants {
		return false
	}
	used := map[string]bool{}
	for _, variant := range a.snippetForm.Variants {
		used[variant.OS] = true
	}
	for _, system := range snippet.SystemKeys() {
		if !used[system] {
			return true
		}
	}
	return false
}

func (a *app) addSnippetVariant() {
	used := map[string]bool{}
	for _, variant := range a.snippetForm.Variants {
		used[variant.OS] = true
	}
	for _, system := range snippet.SystemKeys() {
		if !used[system] {
			a.snippetForm.Variants = append(a.snippetForm.Variants, snippet.Variant{OS: system})
			return
		}
	}
}

func (a *app) syncSnippetArgs() {
	needed := snippet.PlaceholderCount(a.snippetForm)
	if needed > snippet.MaxArgs {
		needed = snippet.MaxArgs
	}
	for len(a.snippetForm.Args) < needed {
		a.snippetForm.Args = append(a.snippetForm.Args, snippet.Arg{})
	}
	if len(a.snippetForm.Args) > needed {
		a.snippetForm.Args = a.snippetForm.Args[:needed]
	}
	for i := range a.snippetForm.Args {
		a.snippetForm.Args[i].Type = snippet.NormalizeType(a.snippetForm.Args[i].Type)
	}
}

func (a *app) saveSnippetForm() {
	if len(a.snippetForm.Args) > 0 {
		a.syncSnippetArgs()
	}
	command := a.snippetForm
	command.Name = strings.TrimSpace(command.Name)
	command.Group = strings.TrimSpace(command.Group)
	variants := make([]snippet.Variant, 0, len(command.Variants))
	for _, variant := range command.Variants {
		if strings.TrimSpace(variant.Body) != "" {
			variants = append(variants, variant)
		}
	}
	command.Variants = variants
	if err := snippet.Validate(command); err != nil {
		a.snippetError = err.Error()
		return
	}
	a.snippetError = ""
	a.persistSnippet(command, false, &snippetEditorSave{sequence: a.snippetDraft, form: a.snippetForm.Clone()})
}

func snippetSystemLabel(c *ui.Context, key string) string {
	switch key {
	case snippet.OSDebian:
		return tr(c, "Debian / Ubuntu（apt）")
	case snippet.OSRedHat:
		return tr(c, "RHEL / Fedora（dnf 或 yum）")
	case snippet.OSSUSE:
		return tr(c, "SUSE（zypper）")
	case snippet.OSArch:
		return tr(c, "Arch（pacman）")
	case snippet.OSAlpine:
		return tr(c, "Alpine（apk）")
	case snippet.OSMacOS:
		return tr(c, "macOS")
	case snippet.OSWindows:
		return tr(c, "Windows")
	}
	return tr(c, "默认")
}

func snippetSystemSelect(c *ui.Context, selected *string, keys []string, label string) ui.Element {
	display := snippetSystemLabel(c, *selected)
	labels := make([]string, len(keys))
	for i, key := range keys {
		labels[i] = snippetSystemLabel(c, key)
	}
	control := ui.Select(c, &display, labels).Label(label)
	if control.Changed() {
		for i, value := range labels {
			if value == display {
				*selected = keys[i]
				break
			}
		}
	}
	return control
}

func snippetTypeLabel(c *ui.Context, kind string) string {
	switch snippet.NormalizeType(kind) {
	case snippet.TypeInt:
		return tr(c, "整数")
	case snippet.TypeNumber:
		return tr(c, "数字")
	}
	return tr(c, "文本")
}

func snippetTypeSelect(c *ui.Context, kind *string, label string) ui.Element {
	keys := []string{snippet.TypeText, snippet.TypeInt, snippet.TypeNumber}
	labels := make([]string, len(keys))
	for i, key := range keys {
		labels[i] = snippetTypeLabel(c, key)
	}
	display := snippetTypeLabel(c, *kind)
	control := ui.Select(c, &display, labels).Label(label)
	if control.Changed() {
		for i, value := range labels {
			if value == display {
				*kind = keys[i]
				break
			}
		}
	}
	return control
}

func (a *app) snippetDetectedSystem(s *session) string {
	if s != nil && s.stats != nil && s.stats.Inventory != nil {
		return snippet.SystemFor(s.stats.Inventory.OS)
	}
	return ""
}

func (a *app) runSnippet(command snippet.Command) {
	a.runSnippetFor(command, a.workspaceCommandTarget())
}

func (a *app) workspaceCommandTarget() *session {
	if a.renderingWorkspaceTool != nil {
		return a.workspaceToolSource(a.renderingWorkspaceTool)
	}
	return a.currentTerminal()
}

func (a *app) runSnippetFor(command snippet.Command, target *session) {
	if target == nil || !a.hasSession(target) || target.term == nil || target.closed {
		a.status = tr(nil, "请先打开终端")
		return
	}
	options := snippet.Options(command)
	if len(options) == 0 {
		a.status = tr(nil, "该命令没有可执行的版本")
		return
	}
	detected := a.snippetDetectedSystem(target)
	system := snippet.ChooseSystem(command, detected)
	if !snippetRunNeedsDialog(command, system, detected, command.SkipConfirm) {
		body, err := snippet.Render(command, system, snippet.PresetValues(command, system))
		if err != nil {
			a.status = err.Error()
			return
		}
		if err = a.sendSnippetCommand(target, body); err != nil {
			a.status = err.Error()
		}
		return
	}
	a.cancelFileProperties()
	a.cancelDockerDialog()
	label := sessionDisplayName(nil, target)
	if target.target.Endpoint != "" {
		label += " · " + reviewText(target.target.Endpoint)
	}
	state := &snippetRunState{command: command.Clone(), target: target, label: label, system: system, skip: command.SkipConfirm}
	a.fillSnippetValues(state)
	a.prompt = promptState{open: true, detached: a.toolsWindowFocused(), title: tr(nil, "执行常用命令"), target: state.label, generation: a.generation, snippet: state}
	a.invalidateWindows()
}

// snippetRunNeedsDialog keeps the double click one step: a dropped
// confirmation only sends commands that need no choice or value. A command
// with only a variant for another detected system still asks once, so it
// cannot run on the wrong system just because the reminder was dropped.
func snippetRunNeedsDialog(command snippet.Command, system, detected string, skip bool) bool {
	if !skip || len(snippet.Options(command)) > 1 {
		return true
	}
	for _, arg := range snippet.ArgsFor(command, system) {
		if !arg.SkipPrompt {
			return true
		}
	}
	return detected != "" && detected != system && strings.TrimSpace(command.Body) == ""
}

func (a *app) fillSnippetValues(state *snippetRunState) {
	args := snippet.ArgsFor(state.command, state.system)
	values := make([]string, len(args))
	for i := range args {
		if i < len(state.values) && state.values[i] != "" {
			values[i] = state.values[i]
		} else {
			values[i] = args[i].Preset
		}
	}
	state.values = values
	a.loadSnippetHistory(state)
}

func (a *app) executeSnippetRun(state *snippetRunState) {
	a.submitSnippetRun(state, true)
}

func (a *app) submitSnippetRun(state *snippetRunState, execute bool) {
	body, err := snippet.Render(state.command, state.system, state.values)
	if err != nil {
		state.error = err.Error()
		return
	}
	if !execute && strings.ContainsAny(body, "\r\n") {
		state.error = tr(nil, "多行命令请使用发送并执行")
		return
	}
	if execute && state.skip != state.command.SkipConfirm && a.busy {
		state.error = tr(nil, "正在保存其他更改，请稍后重试")
		return
	}
	if err = a.sendSnippetInput(state.target, body, execute); err != nil {
		state.error = err.Error()
		return
	}
	a.recordSnippetHistory(state)
	if execute && state.skip != state.command.SkipConfirm {
		command := state.command.Clone()
		command.SkipConfirm = state.skip
		a.persistSnippet(command, false, nil)
	}
	a.prompt = promptState{}
}

func (a *app) sendSnippetCommand(target *session, body string) error {
	return a.sendSnippetInput(target, body, true)
}

func (a *app) sendSnippetInput(target *session, body string, execute bool) error {
	if target == nil || a.sessionIndex(target) < 0 || target.closed || target.term == nil {
		return errors.New(tr(nil, "原终端已关闭，命令未发送"))
	}
	select {
	case <-target.term.Done():
		return errors.New(tr(nil, "终端已断开，请重新连接"))
	default:
	}
	body = strings.ReplaceAll(body, "\r\n", "\n")
	if execute {
		body += "\r"
	}
	target.term.Send([]byte(body))
	a.status = tr(nil, "命令已发送到 ") + sessionDisplayName(nil, target)
	if !execute {
		a.status = tr(nil, "命令已填入终端：") + sessionDisplayName(nil, target)
	}
	return nil
}

// snippetEditorSave identifies the exact editor instance and content that
// requested a save, so a late completion cannot close a newer draft.
type snippetEditorSave struct {
	sequence uint64
	form     snippet.Command
}

func (a *app) persistSnippet(command snippet.Command, remove bool, editor *snippetEditorSave) {
	if a.busy || !a.online {
		return
	}
	owner, err := a.unlockedOwner()
	if err != nil {
		return
	}
	cli, gen, ctx, load := a.client, a.generation, a.operationContext(), a.snippetLoad
	command = command.Clone()
	a.busy = true
	go func() {
		saveCtx, cancel := context.WithTimeout(ctx, snippetRPCTimeout)
		err := cli.SaveSnippet(saveCtx, command, remove, owner)
		cancel()
		if err != nil {
			a.postUI(func() {
				if a.generation != gen {
					return
				}
				a.busy = false
				if load == a.snippetLoad {
					a.status = err.Error()
				}
			})
			return
		}
		listCtx, cancel := context.WithTimeout(ctx, snippetRPCTimeout)
		commands, listErr := cli.ListSnippets(listCtx, owner)
		cancel()
		a.postUI(func() {
			if a.generation != gen {
				return
			}
			a.busy = false
			if load != a.snippetLoad {
				return
			}
			a.snippetLoad++
			if editor != nil && editor.sequence == a.snippetDraft && reflect.DeepEqual(editor.form, a.snippetForm.Clone()) {
				a.snippetOpen = false
				a.snippetForm, a.snippetError = snippet.Command{}, ""
			}
			if listErr != nil {
				a.status = tr(nil, "常用命令已保存，但列表刷新失败：") + listErr.Error()
				return
			}
			a.snippets = commands
			a.status = tr(nil, "常用命令已加密保存")
		})
	}()
}

func (a *app) loadSnippets() {
	if !a.online || a.server == nil || (a.win == nil && a.dispatch == nil) {
		return
	}
	owner, err := a.unlockedOwner()
	if err != nil {
		return
	}
	cli, gen, ctx := a.client, a.generation, a.operationContext()
	a.snippetLoad++
	load := a.snippetLoad
	go func() {
		listCtx, cancel := context.WithTimeout(ctx, snippetRPCTimeout)
		commands, err := cli.ListSnippets(listCtx, owner)
		cancel()
		a.postUI(func() {
			if a.generation != gen || load != a.snippetLoad {
				return
			}
			if err != nil {
				a.status = tr(nil, "无法加载常用命令：") + err.Error()
				return
			}
			a.snippets = commands
		})
	}()
}
