package desktop

import (
	"context"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"sshclient/docs/design"
	"sshclient/internal/desktop/components"
	"sshclient/internal/identitymodel"
)

type identitySettingsState struct {
	state                                 identitymodel.State
	loaded, busy                          bool
	name, authentication, error, deleting string
	lastAttempt                           time.Time
	sequence                              uint64
	cancel                                context.CancelFunc
}

type managedConnectionKeys struct {
	keys        []identitymodel.Key
	name        string
	auth        string
	creating    bool
	loaded      bool
	unsupported bool
}

func (a *app) managedConnectionForm(c *ui.Context) {
	s := &a.connection.managed
	if runtime.GOOS != "darwin" || s.unsupported {
		ui.Text(c, tr(c, "此设备不支持安全隔区 SSH 密钥。")).FillWidth()
		return
	}
	ui.Text(c, tr(c, "选择或新建一把 Mac 安全隔区密钥，再把公钥添加到服务器。")).FillWidth().Wrap().FontSize(12).TextColor(c.Theme().TextMuted)
	if len(s.keys) != 0 {
		options := []connectionOption{{"", "选择密钥"}}
		names := map[string]int{}
		for _, key := range s.keys {
			names[key.Name]++
		}
		for _, key := range s.keys {
			label := key.Name
			if names[key.Name] > 1 {
				label += " · " + key.Fingerprint
			}
			options = append(options, connectionOption{key.Fingerprint, label})
		}
		optionSelect(c, "SmallSSH 密钥", &a.form.AgentFingerprint, options)
	} else if s.loaded {
		ui.Text(c, tr(c, "暂无 SSH 密钥")).FillWidth().FontSize(12).TextColor(c.Theme().TextMuted)
	}
	ui.Row(c).FillWidth().Gap(8).Wrap().Children(func() {
		if components.Button(c, tr(c, "读取密钥")).Clicked() {
			a.readManagedConnectionKeys(nil)
		}
		if !s.creating && components.Button(c, tr(c, "新建密钥")).Clicked() {
			s.creating = true
		}
		for _, key := range s.keys {
			if key.Fingerprint == a.form.AgentFingerprint {
				if components.Button(c, tr(c, "复制公钥")).Clicked() {
					c.WriteClipboard(strings.TrimSpace(key.PublicKey) + " " + key.Name)
				}
				break
			}
		}
	})
	if s.creating {
		identityCreationFields(c, &s.name, &s.auth, a.connection.phase != "", func(change identitymodel.Change) {
			a.readManagedConnectionKeys(&change)
		}, func() {
			s.creating = false
		})
	}
	if err := a.connection.errors["agent"]; err != "" {
		ui.Text(c, err).FillWidth().Wrap().FontSize(12).TextColor(c.Theme().Danger)
	}
}

func (a *app) readManagedConnectionKeys(change *identitymodel.Change) {
	if a.connection.phase != "" {
		return
	}
	owner, err := a.unlockedOwner()
	if err != nil {
		a.connection.errors["agent"] = err.Error()
		return
	}
	ctx, seq, gen := a.editorOperation("agent")
	cli := a.client
	a.connection.message = tr(nil, "正在读取 SSH 密钥…")
	if change != nil {
		a.connection.message = tr(nil, "正在生成 SSH 密钥…")
	}
	go func() {
		var err error
		if change != nil {
			err = cli.ChangeIdentity(ctx, *change, owner)
		}
		created := change != nil && err == nil
		var state identitymodel.State
		if err == nil {
			state, err = cli.Identities(ctx, owner)
		}
		a.postUI(func() {
			if !a.editorCurrent(seq, gen) || ctx.Err() != nil {
				return
			}
			a.connection.cancel()
			a.connection.cancel = nil
			a.connection.phase = ""
			a.connection.message = ""
			s := &a.connection.managed
			if created {
				s.name, s.creating = "", false
			}
			if err != nil {
				a.connection.errors["agent"] = err.Error()
				return
			}
			delete(a.connection.errors, "agent")
			s.keys, s.loaded, s.unsupported = state.Keys, true, !state.Supported
			if created {
				var matching string
				for _, key := range s.keys {
					if key.Name == change.Name {
						if matching != "" {
							return
						}
						matching = key.Fingerprint
					}
				}
				a.form.AgentFingerprint = matching
			}
		})
	}()
}

func (a *app) cancelIdentitySettings() {
	if a.identities.cancel != nil {
		a.identities.cancel()
	}
	a.identities = identitySettingsState{sequence: a.identities.sequence + 1}
}

// Only public metadata crosses into the desktop. Rendering never starts the
// native agent, touches Keychain, or requests system authentication.
func (a *app) refreshIdentities(change *identitymodel.Change) {
	if a.identities.busy {
		return
	}
	owner, err := a.unlockedOwner()
	if err != nil {
		a.identities.error = err.Error()
		return
	}
	ctx, cancel := context.WithTimeout(a.ctx, 2*time.Minute)
	a.identities.lastAttempt = time.Now()
	a.identities.sequence++
	seq, gen, cli := a.identities.sequence, a.generation, a.client
	a.identities.busy, a.identities.error, a.identities.cancel = true, "", cancel
	go func() {
		defer cancel()
		if change != nil {
			err = cli.ChangeIdentity(ctx, *change, owner)
		}
		var state identitymodel.State
		if err == nil {
			state, err = cli.Identities(ctx, owner)
		}
		a.postUI(func() {
			if seq != a.identities.sequence || gen != a.generation {
				return
			}
			a.identities.busy, a.identities.cancel = false, nil
			if err != nil {
				a.identities.error = err.Error()
				return
			}
			a.identities.state, a.identities.loaded = state, true
			if change != nil && change.Action == "create" {
				a.identities.name = ""
			}
		})
	}()
}

func (a *app) identitySettings(c *ui.Context) {
	s := &a.identities
	if s.error != "" {
		ui.Text(c, reviewText(s.error)).FillWidth().FontSize(12).TextColor(c.Theme().Danger)
	}
	ui.Row(c).FillWidth().Gap(8).Wrap().Children(func() {
		if components.Button(c, tr(c, "刷新密钥")).Disabled(s.busy).Clicked() {
			a.refreshIdentities(nil)
		}
		if components.Button(c, tr(c, "查看待处理请求")).Disabled(s.busy || !s.loaded || !s.state.Supported).Clicked() {
			a.refreshIdentities(&identitymodel.Change{Action: "pending"})
		}
		if components.Button(c, tr(c, "结束限时授权")).Disabled(s.busy || !s.loaded || !s.state.Supported).Clicked() {
			a.refreshIdentities(&identitymodel.Change{Action: "clear"})
		}
	})
	if !s.loaded {
		if s.busy {
			ui.Text(c, tr(c, "正在读取 SSH 密钥…")).FontSize(12)
		}
		return
	}
	if !s.state.Supported {
		ui.Text(c, tr(c, "此设备不支持安全隔区 SSH 密钥。")).FillWidth()
		return
	}
	ui.Column(c).FillWidth().Padding(12).Gap(8).Radius(8).Border(1, c.Theme().Border).Label(tr(c, "创建 SSH 密钥")).Children(func() {
		ui.Text(c, tr(c, "新建密钥")).FontSize(14).Bold()
		identityCreationFields(c, &s.name, &s.authentication, s.busy, func(change identitymodel.Change) {
			a.refreshIdentities(&change)
		}, nil)
	})
	for _, key := range s.state.Keys {
		c.Key(key.ID)
		ui.Column(c).FillWidth().Padding(12).Gap(8).Radius(8).Border(1, c.Theme().Border).Label(tr(c, "SSH 密钥：") + key.Name).Children(func() {
			ui.Text(c, key.Name).FillWidth().FontSize(13).Bold()
			ui.Text(c, key.Fingerprint).FillWidth().Selectable().FontSize(11).Label(tr(c, "SSH 密钥指纹：") + key.Name)
			ui.Text(c, tr(c, identityAuthenticationLabel(key.Authentication))).FillWidth().FontSize(12).TextColor(c.Theme().TextMuted)
			ui.Row(c).FillWidth().Gap(8).Wrap().Children(func() {
				if components.Button(c, tr(c, "复制公钥")).Label(tr(c, "复制公钥：") + key.Name).Clicked() {
					c.WriteClipboard(strings.TrimSpace(key.PublicKey) + " " + key.Name)
				}
				if key.Authentication != "none" {
					components.Button(c, tr(c, "限时授权")).Disabled(s.busy).Label(tr(c, "密钥限时授权：") + key.Name).Menu(func(menu *ui.Menu) {
						for _, duration := range []struct {
							seconds int
							label   string
						}{{60, "1 分钟"}, {300, "5 分钟"}, {3600, "1 小时"}, {86400, "24 小时"}} {
							if menu.Item(tr(c, duration.label)).Chosen() {
								a.refreshIdentities(&identitymodel.Change{Action: "persist", ID: key.ID, Seconds: duration.seconds})
							}
						}
					})
				}
				if components.Button(c, tr(c, "删除"), components.ButtonOptions{Variant: components.Quiet}).TextColor(c.Theme().Danger).Disabled(s.busy).Label(tr(c, "删除 SSH 密钥：") + key.Name).Clicked() {
					s.deleting = key.ID
				}
			})
			if s.deleting == key.ID {
				ui.Text(c, tr(c, "删除后无法恢复。已部署的公钥需从服务器移除。")).FillWidth().FontSize(12)
				components.ActionGroup(c, ui.End, func() {
					if components.Button(c, tr(c, "取消删除")).Clicked() {
						s.deleting = ""
					}
					if components.Button(c, tr(c, "确认删除密钥"), components.ButtonOptions{Variant: components.Destructive}).Disabled(s.busy).Clicked() {
						s.deleting = ""
						a.refreshIdentities(&identitymodel.Change{Action: "delete", ID: key.ID})
					}
				})
			}
		})
	}
	if len(s.state.Keys) == 0 {
		ui.Text(c, tr(c, "暂无 SSH 密钥")).FontSize(12).TextColor(c.Theme().TextMuted)
	}
	ui.Column(c).FillWidth().Padding(12).Gap(8).Radius(8).Border(1, c.Theme().Border).Children(func() {
		ui.Text(c, tr(c, "外部 SSH 客户端")).FontSize(14).Bold()
		ui.Text(c, s.state.Socket).FillWidth().FontSize(11).Selectable().Label(tr(c, "SSH 代理套接字"))
		ui.Row(c).FillWidth().Children(func() {
			if components.Button(c, tr(c, "复制 SSH_AUTH_SOCK")).Clicked() {
				c.WriteClipboard(s.state.Socket)
			}
		})
		label := "开启签名通知"
		if s.state.Notifications == "enabled" {
			label = "签名通知已开启"
		}
		if s.state.Notifications == "denied" {
			label = "通知已被系统关闭"
		}
		ui.Row(c).FillWidth().Children(func() {
			if components.Button(c, tr(c, label)).Disabled(s.busy || s.state.Notifications == "enabled" || s.state.Notifications == "denied").Clicked() {
				a.refreshIdentities(&identitymodel.Change{Action: "notifications"})
			}
		})
	})
}

const identitySystemAuthentication = "macOS 验证（指纹或 Mac 登录密码）"

func identityCreationFields(c *ui.Context, name, authentication *string, busy bool, create func(identitymodel.Change), cancel func()) {
	if *authentication == "" {
		*authentication = "presence"
	}
	ui.TextInput(c, name).FillWidth().Height(components.FormHeight).Placeholder(tr(c, "密钥名称")).Label(tr(c, "SSH 密钥名称"))
	optionSelectHint(c, "签名验证", authentication,
		[]connectionOption{{"presence", identitySystemAuthentication}, {"biometry", "仅当前指纹"}, {"none", "无需验证"}}, "",
		identityAuthenticationMenu)
	row := ui.Row(c).FillWidth().Gap(8).Wrap()
	if cancel != nil {
		row.Justify(ui.End)
	}
	row.Children(func() {
		if cancel != nil && components.Button(c, tr(c, "取消新建密钥")).Clicked() {
			cancel()
		}
		if components.Button(c, tr(c, "生成密钥"), components.ButtonOptions{Variant: components.Primary}).Disabled(busy || strings.TrimSpace(*name) == "").Clicked() {
			create(identitymodel.Change{Action: "create", Name: strings.TrimSpace(*name), Authentication: *authentication})
		}
	})
}

var identityAuthenticationMenu = optionMenuStyle{
	width: 256, inset: design.Length("spacing-space-xs") - design.Length("size-review-divider"),
	rowHeight: design.Length("size-control"),
	rowInset:  design.Length("spacing-space-md"), fontSize: design.TypeSize("type-native-copy"),
}

func identityAuthenticationLabel(policy string) string {
	switch policy {
	case "presence":
		return "签名前系统验证"
	case "biometry":
		return "签名前验证当前指纹"
	default:
		return "无需签名前验证"
	}
}
