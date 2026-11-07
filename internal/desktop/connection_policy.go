package desktop

import (
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/accesspolicy"
	"sshclient/internal/desktop/components"
)

// The owner decides whether automation may touch this server at all. The
// consequence of the choice belongs in the approval dialog, not repeated as
// paragraphs under the checkbox.
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
				return
			}
			protected := !a.form.Policy.Unrestricted
			ui.Row(c).FillWidth().MinWidth(0).Gap(8).AlignItems(ui.Center).Children(func() {
				if ui.Checkbox(c, &protected, tr(c, "保护关键操作")).Grow(1).Changed() {
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
		})
	})
}

func (a *app) confirmUnrestrictedPolicy() {
	seq, gen, id := a.connection.sequence, a.generation, a.form.ID
	a.ask(tr(nil, "关闭关键操作保护？"), a.form.Name,
		tr(nil, "AI 将可执行任意命令、使用交互终端并修改关键文件；破坏性命令护栏不再生效。"), "", "", tr(nil, "关闭保护"), func(string) {
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
	a.ask(tr(nil, "保护路径"), a.form.Name, tr(nil, "AI 的文件操作不能读取、下载或修改这些路径。每行一个绝对路径；留空使用默认保护路径。"), tr(nil, "保护路径输入"),
		strings.Join(a.form.Policy.Paths(), "\n"), tr(nil, "应用"), func(value string) {
			if a.editorCurrent(seq, gen) && a.form.ID == id {
				policy, err := protectedPathsInput(value)
				if err == nil && !slices.Equal(policy.ProtectedPaths, a.form.Policy.Paths()) {
					a.form.Policy.ProtectedPaths = policy.ProtectedPaths
				}
			}
		})
	a.prompt.multiline = true
	a.prompt.width = 720
	a.prompt.multilineHeight = 210
	a.prompt.validate = func(value string) error {
		_, err := protectedPathsInput(value)
		return err
	}
}
