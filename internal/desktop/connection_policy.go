package desktop

import (
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/accesspolicy"
)

// The owner decides whether automation may touch this server at all. The
// consequence of the choice belongs in the approval dialog, not repeated as
// paragraphs under the checkbox.
func (a *app) connectionPolicyForm(c *ui.Context) {
	e := &a.connection
	enabled := !a.form.Policy.Disabled
	ui.Field(c, tr(c, "AI 访问"), func() {
		ui.Column(c).Key("connection-ai-policy").FillWidth().Gap(8).Children(func() {
			control := ui.Checkbox(c, &enabled, tr(c, "允许 AI 访问此服务器"))
			if e.focusPolicy {
				control.Focus()
				control.ScrollIntoView()
				e.focusPolicy = false
			}
			if control.Changed() {
				a.form.Policy.Disabled = !enabled
			}
			if enabled {
				protected := !a.form.Policy.Unrestricted
				if ui.Checkbox(c, &protected, tr(c, "保护关键操作")).Changed() {
					if protected {
						a.form.Policy.Unrestricted = false
					} else {
						a.confirmUnrestrictedPolicy()
					}
				}
			}
		})
	})
	policyError := ""
	if err := a.form.Policy.Validate(); err != nil {
		policyError = err.Error()
	}
	if !enabled && policyError == "" {
		return
	}
	if !e.pathsReady {
		e.protectedPaths = strings.Join(a.form.Policy.Paths(), "\n")
		e.pathsReady = true
	}
	ui.Field(c, tr(c, "保护路径"), func() {
		// A rejected draft stays editable even if access or protection was
		// switched off, so validation never traps an error behind a disabled UI.
		area := ui.TextArea(c, &e.protectedPaths).Height(210).FillWidth().Label(tr(c, "保护路径输入")).
			Placeholder(strings.Join((accesspolicy.Policy{}).Paths(), "\n")).
			Disabled((!enabled || a.form.Policy.Unrestricted) && policyError == "")
		if e.focusPaths {
			area.Focus()
			area.ScrollIntoView()
			e.focusPaths = false
		}
		if area.Changed() {
			a.updateProtectedPaths(e.protectedPaths)
		}
	}).Description(tr(c, "开启关键操作保护时，AI 的文件操作不能读取、下载或修改这些路径。每行一个绝对路径；留空使用默认保护路径。")).Error(policyError)
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

// The textarea keeps the literal draft while the form holds its parsed paths.
// Invalid drafts participate in unsaved-change handling and cannot be saved.
func (a *app) updateProtectedPaths(value string) {
	e := &a.connection
	e.protectedPaths, e.pathsReady = value, true
	policy, err := protectedPathsInput(value)
	if !slices.Equal(policy.ProtectedPaths, a.form.Policy.Paths()) {
		a.form.Policy.ProtectedPaths = policy.ProtectedPaths
	}
	if e.errors == nil {
		e.errors = map[string]string{}
	}
	if err != nil {
		e.errors["policy"] = err.Error()
	} else {
		delete(e.errors, "policy")
	}
}
