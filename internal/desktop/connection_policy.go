package desktop

import (
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/accesspolicy"
	"sshclient/internal/desktop/components"
)

func (a *app) connectionPolicyForm(c *ui.Context) {
	ui.Field(c, tr(c, "AI 访问"), func() {
		ui.Column(c).Key("connection-ai-policy").FillWidth().Gap(8).Children(func() {
			enabled := !a.form.Policy.Disabled
			control := ui.Checkbox(c, &enabled, tr(c, "允许 AI 访问此服务器"))
			if a.connection.focusPolicy {
				control.Focus()
				control.ScrollIntoView()
				a.connection.focusPolicy = false
			}
			if control.Changed() {
				a.form.Policy.Disabled = !enabled
			}
			if !enabled {
				ui.Text(c, tr(c, "AI 请求会直接拒绝并记入历史")).FillWidth().FontSize(11).TextColor(c.Theme().TextMuted)
				return
			}
			protected := !a.form.Policy.Unrestricted
			ui.Row(c).FillWidth().MinWidth(0).Gap(8).AlignItems(ui.Center).Children(func() {
				if ui.Checkbox(c, &protected, tr(c, "保护关键路径")).Grow(1).Changed() {
					if protected {
						a.form.Policy.Unrestricted = false
					} else {
						a.confirmUnrestrictedPolicy()
					}
				}
				if components.Button(c, tr(c, "保护路径…")).Disabled(a.form.Policy.Unrestricted).Clicked() {
					a.editProtectedPaths()
				}
			})
			if !a.form.Policy.Unrestricted {
				ui.Text(c, tr(c, "保护开启时，AI 不开放任意命令和终端")).FillWidth().FontSize(11).TextColor(c.Theme().TextMuted)
				ui.Text(c, tr(c, "仅开放受控文件操作；属性和续传写入关闭")).FillWidth().FontSize(11).TextColor(c.Theme().TextMuted)
			} else {
				ui.Text(c, tr(c, "保护已关闭 · AI 可使用账户全部权限")).FillWidth().FontSize(11).TextColor(c.Theme().Warning)
			}
		})
	})
}

func (a *app) confirmUnrestrictedPolicy() {
	seq, gen, id := a.connection.sequence, a.generation, a.form.ID
	a.ask(tr(nil, "关闭关键路径保护？"), a.form.Name,
		tr(nil, "AI 将可使用此 SSH 账户的全部权限，包括任意命令、终端和关键文件修改。"), "", "", tr(nil, "关闭保护"), func(string) {
			if a.editorCurrent(seq, gen) && a.form.ID == id {
				a.form.Policy.Unrestricted = true
			}
		})
	a.prompt.danger = true
}

func protectedPathsInput(value string) (accesspolicy.Policy, error) {
	policy := accesspolicy.Policy{}
	for _, line := range strings.Split(value, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			policy.ProtectedPaths = append(policy.ProtectedPaths, line)
		}
	}
	return policy, policy.Validate()
}

func (a *app) editProtectedPaths() {
	seq, gen, id := a.connection.sequence, a.generation, a.form.ID
	a.ask(tr(nil, "保护路径"), a.form.Name, tr(nil, "每行一个绝对路径；留空使用默认保护路径。"), tr(nil, "保护路径输入"),
		strings.Join(a.form.Policy.Paths(), "\n"), tr(nil, "应用"), func(value string) {
			if a.editorCurrent(seq, gen) && a.form.ID == id {
				policy, err := protectedPathsInput(value)
				if err == nil && !slices.Equal(policy.ProtectedPaths, a.form.Policy.Paths()) {
					a.form.Policy.ProtectedPaths = policy.ProtectedPaths
				}
			}
		})
	a.prompt.multiline = true
	a.prompt.validate = func(value string) error {
		_, err := protectedPathsInput(value)
		return err
	}
}
