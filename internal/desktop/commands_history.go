package desktop

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/desktop/components"
	"sshclient/internal/snippet"
)

const snippetHistoryRPCTimeout = 10 * time.Second

func snippetHistoryTarget(target *session) string {
	if target == nil {
		return ""
	}
	data, _ := json.Marshal([]any{target.host, target.target.Endpoint, target.target.HostFingerprint, target.target.Kind, target.target.Docker})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (a *app) loadSnippetHistory(state *snippetRunState) {
	state.histories = make([][]string, len(state.values))
	client := a.client
	if state.target == nil {
		return
	}
	owner, err := a.unlockedOwner()
	if err != nil {
		return
	}
	ctx := a.operationContext()
	command := state.command.Clone()
	commandID := command.ID
	target := snippetHistoryTarget(state.target)
	system := state.system
	args := snippet.ArgsFor(command, system)
	state.historyLoad++
	load := state.historyLoad
	loaded := make([][]string, len(args))
	a.queueSnippetHistory(func() error {
		ctx, cancel := context.WithTimeout(ctx, snippetHistoryRPCTimeout)
		defer cancel()
		for i, arg := range args {
			if arg.NoHistory {
				continue
			}
			values, err := client.ParameterHistory(ctx, commandID, target, system, i, owner)
			if err != nil {
				return err
			}
			loaded[i] = values
		}
		return nil
	}, func(err error) {
		if err != nil || a.prompt.snippet != state || state.command.ID != commandID || snippetHistoryTarget(state.target) != target || state.system != system || state.historyLoad != load {
			return
		}
		state.histories = loaded
	})
}

// One FIFO per unlocked UI session preserves send/clear ordering. Jobs from a
// locked session drain without writing; callbacks cannot update the next one.
func (a *app) queueSnippetHistory(write func() error, done func(error)) bool {
	if a.snippetHistoryWrites == nil {
		queue := make(chan func(), 32)
		a.snippetHistoryWrites = queue
		go func() {
			for job := range queue {
				job()
			}
		}()
	}
	ctx, gen := a.operationContext(), a.generation
	job := func() {
		if ctx.Err() != nil {
			return
		}
		err := write()
		a.postUI(func() {
			if gen == a.generation {
				done(err)
			}
		})
	}
	select {
	case a.snippetHistoryWrites <- job:
		return true
	default:
		return false
	}
}

func (a *app) stopSnippetHistoryWrites() {
	if a.snippetHistoryWrites != nil {
		close(a.snippetHistoryWrites)
		a.snippetHistoryWrites = nil
	}
}

func (a *app) recordSnippetHistory(state *snippetRunState) {
	client := a.client
	if state.target == nil || len(state.values) == 0 {
		return
	}
	owner, err := a.unlockedOwner()
	if err != nil {
		return
	}
	ctx := a.operationContext()
	command, values := state.command.Clone(), append([]string(nil), state.values...)
	target, system := snippetHistoryTarget(state.target), state.system
	if !a.queueSnippetHistory(func() error {
		ctx, cancel := context.WithTimeout(ctx, snippetHistoryRPCTimeout)
		defer cancel()
		return client.SaveParameterHistory(ctx, command, target, system, values, -1, owner)
	}, func(err error) {
		if err != nil {
			a.status = tr(nil, "命令已发送，但参数历史未保存：") + err.Error()
		}
	}) {
		a.status = tr(nil, "命令已发送，参数历史队列已满")
	}
}

func (a *app) clearSnippetHistory(state *snippetRunState, parameter int) {
	client := a.client
	if state.target == nil {
		return
	}
	owner, err := a.unlockedOwner()
	if err != nil {
		return
	}
	ctx := a.operationContext()
	command := state.command.Clone()
	target, system := snippetHistoryTarget(state.target), state.system
	state.historyLoad++
	if !a.queueSnippetHistory(func() error {
		ctx, cancel := context.WithTimeout(ctx, snippetHistoryRPCTimeout)
		defer cancel()
		return client.SaveParameterHistory(ctx, command, target, system, nil, parameter, owner)
	}, func(err error) {
		if a.prompt.snippet != state || target != snippetHistoryTarget(state.target) || state.system != system {
			return
		}
		if err != nil {
			state.error = tr(nil, "参数历史未清空：") + err.Error()
			return
		}
		state.histories[parameter] = nil
	}) {
		state.error = tr(nil, "参数历史正在保存，请稍后再试")
	}
}

func (a *app) snippetHistorySelect(c *ui.Context, state *snippetRunState, parameter int, arg snippet.Arg) {
	choice := ""
	parts := ui.SelectBase(c, &choice)
	theme := c.Theme()
	parts.Trigger.Height(components.FormHeight).Width(84).Padding(0, 10).Radius(theme.Radius).
		Background(theme.Surface).Border(1, theme.Border).Label(fmt.Sprintf(tr(c, "参数 %d 历史"), parameter+1)).Disabled(arg.NoHistory).
		Children(func() { ui.Text(c, tr(c, "历史")+" ⌄").FontSize(12) })
	if arg.NoHistory {
		parts.Trigger.Tooltip(tr(c, "此参数不记录历史"))
	} else {
		parts.Trigger.Tooltip(tr(c, "历史仅记录已发送的单行输入，最多 512 字节"))
	}
	parts.Popup(func(panel ui.Element) {
		panel.Width(280).MaxHeight(340).Padding(6).Background(theme.Background).Border(1, theme.Border).Radius(theme.Radius).Children(func() {
			ui.Column(c).FillWidth().Gap(4).Children(func() {
				ui.Text(c, tr(c, "当前服务器 · 最近 10 次输入")).FontSize(11).TextColor(theme.TextMuted)
				var values []string
				if parameter < len(state.histories) {
					values = state.histories[parameter]
				}
				ui.Scroll(c).FillWidth().MaxHeight(250).Children(func() {
					ui.Column(c).FillWidth().Children(func() {
						if len(values) == 0 {
							ui.Text(c, tr(c, "暂无历史，发送后会记录")).Padding(8).FontSize(12).TextColor(theme.TextMuted)
						}
						for _, value := range values {
							item := parts.Item(value).Padding(7, 8).Radius(theme.Radius)
							if item.Highlighted() {
								item.Background(theme.Surface)
							}
							item.Children(func() {
								ui.Text(c, reviewText(value)).SingleLine().Tooltip(reviewText(value)).Font("monospace").FontSize(12)
							})
						}
					})
				})
				if len(values) > 0 {
					parts.Item("\x00clear").Padding(7, 8).Children(func() { ui.Text(c, tr(c, "清空此参数历史")).FontSize(12).TextColor(theme.Danger) })
				}
			})
		})
	})
	if parts.Trigger.Changed() {
		if choice == "\x00clear" {
			a.clearSnippetHistory(state, parameter)
		} else if choice != "" {
			state.values[parameter] = choice
			state.error = ""
		}
	}
}
