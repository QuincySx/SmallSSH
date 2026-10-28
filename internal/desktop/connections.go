package desktop

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"sshclient/internal/desktop/components"
	"strconv"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"sshclient/internal/broker"
	"sshclient/internal/client"
	"sshclient/internal/sshconn"
	"sshclient/internal/vault"
)

type connectionEditor struct {
	sequence         uint64
	returnPage       int
	cancel           context.CancelFunc
	phase, message   string
	errors           map[string]string
	authLabel        string
	replace, connect bool
	review           bool
	identity         *sshconn.HostIdentity
	confirmed        bool
	trustedTarget    string
	keyInfo          *sshconn.KeyInfo
	agentKeys        []sshconn.KeyInfo
	focusPolicy      bool
}

func (a *app) postUI(fn func()) {
	update := func() {
		fn()
		a.invalidateWindows()
	}
	if a.win != nil {
		a.win.Update(update)
	} else if a.dispatch != nil {
		a.dispatch(update)
	}
}
func (a *app) resetConnectionEditor() {
	sequence := a.connection.sequence + 1
	if a.connection.cancel != nil {
		a.connection.cancel()
	}
	a.connection = connectionEditor{sequence: sequence, errors: map[string]string{}, authLabel: "服务器密码", connect: true}
	a.form.Password = ""
	a.form.PrivateKey = ""
	a.form.KeyPassphrase = ""
	a.keyFile = ""
	a.ownerPass = ""
}
func (a *app) cancelConnectionEditor() {
	if a.connection.phase == "save" {
		a.connection.message = tr(nil, "正在提交加密保存，请稍候")
		return
	}
	page := a.connection.returnPage
	if page != 3 && page != 4 {
		page = 1
	}
	a.resetConnectionEditor()
	a.details = nil
	a.page = page
}

func (a *app) requestCloseConnection() {
	if a.connection.phase != "" || !a.connectionHasChanges() {
		a.cancelConnectionEditor()
		return
	}
	a.navigate(func() {})
}
func authLabel(kind string) string {
	switch kind {
	case "key":
		return "已有私钥"
	case "agent":
		return "SSH 密钥代理"
	}
	return "服务器密码"
}
func authKind(label string) string {
	switch label {
	case "已有私钥":
		return "key"
	case "SSH 密钥代理":
		return "agent"
	}
	return "password"
}
func targetID(host string, port int) string { return net.JoinHostPort(host, strconv.Itoa(port)) }
func (a *app) connectionField(c *ui.Context, key, label string, value *string, secret bool, description string) {
	ui.Field(c, label, func() {
		control := ui.TextInput(c, value).FillWidth().Label(label + tr(c, "输入"))
		if key == "name" {
			control.AutoFocus()
		}
		if secret {
			control.Password()
		}
		if control.Changed() {
			delete(a.connection.errors, key)
			if key == "key" {
				a.connection.keyInfo = nil
			}
		}
	}).Description(description).Error(a.connection.errors[key])
}

func (a *app) connectionView(c *ui.Context) {
	if a.page != 0 || !a.online {
		return
	}
	title := tr(c, "新建连接")
	if a.details != nil {
		title = tr(c, "编辑连接")
	}
	review := a.connection.review && a.connection.identity != nil
	height := float32(480)
	if a.form.Auth != "password" || a.details != nil || a.form.Fingerprint != "" {
		height = 580
	}
	if review {
		title, height = tr(c, "确认服务器身份"), 380
	}
	open := true
	components.Dialog(c, &open, components.DialogOptions{
		Title: title, Width: 640, Height: height,
		Close: a.requestCloseConnection, CloseDisabled: a.connection.phase == "save",
	}, func() {
		if a.page != 0 {
			return
		}
		if review {
			a.connectionIdentityView(c)
		} else {
			a.connectionForm(c)
		}
	}, func() {
		if a.page == 0 {
			a.connectionFooter(c, review)
		}
	})
	if !open && a.page == 0 {
		a.requestCloseConnection()
	}
}

func (a *app) connectionForm(c *ui.Context) {
	e := &a.connection
	if e.errors == nil {
		e.errors = map[string]string{}
	}
	if e.authLabel == "" {
		e.authLabel = authLabel(a.form.Auth)
	}
	ui.Column(c).FillWidth().Gap(12).Children(func() {
		if a.isTestVault() {
			ui.Text(c, tr(c, "测试保险库：只使用本机测试凭据")).FontSize(12)
		}
		ui.Form(c, func() {
			a.connectionField(c, "name", tr(c, "名称"), &a.form.Name, false, "")
			beforeHost, beforePort := a.form.Host, a.port
			ui.Field(c, tr(c, "服务器地址"), func() {
				ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
					if ui.TextInput(c, &a.form.Host).Grow(1).MinWidth(100).Height(34).Label(tr(c, "服务器地址输入")).Placeholder(tr(c, "IP 地址或域名")).Changed() {
						delete(e.errors, "host")
					}
					ui.Text(c, tr(c, "端口")).TextColor(c.Theme().TextMuted)
					if ui.TextInput(c, &a.port).Width(80).Height(34).Label(tr(c, "端口输入")).Changed() {
						delete(e.errors, "port")
					}
				})
			}).Error(e.errors["host"]).Error(e.errors["port"])
			a.connectionGroupField(c)
			a.connectionField(c, "user", tr(c, "用户名"), &a.form.User, false, "")
			ui.Field(c, tr(c, "登录方式"), func() {
				if sourceSelect(c, &e.authLabel, []string{"服务器密码", "已有私钥", "SSH 密钥代理"}).FillWidth().Label(tr(c, "登录方式选择")).Changed() {
					a.form.Auth = authKind(e.authLabel)
					a.form.Password = ""
					a.form.PrivateKey = ""
					a.form.KeyPassphrase = ""
					a.keyFile = ""
					e.keyInfo = nil
					e.replace = true
				}
			})
			keep := a.details != nil && !e.replace && a.form.Auth == a.details.Auth
			if a.details != nil && a.form.Auth == a.details.Auth {
				ui.Field(c, tr(c, "已保存的登录信息"), func() {
					if ui.Checkbox(c, &e.replace, tr(c, "更换登录凭据")).Tooltip(tr(c, "未更换时保留已保存的密码或私钥")).Changed() && !e.replace {
						a.form.Password = ""
						a.form.PrivateKey = ""
						a.form.KeyPassphrase = ""
						a.keyFile = ""
						e.keyInfo = nil
					}
				})
				keep = !e.replace
			}
			if !keep {
				switch a.form.Auth {
				case "key":
					ui.Field(c, tr(c, "私钥文件"), func() {
						ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
							if ui.TextInput(c, &a.keyFile).Grow(1).Label(tr(c, "私钥文件输入")).Placeholder(tr(c, "选择文件，或粘贴本机完整路径")).
								Tooltip(tr(c, "支持 OpenSSH / PEM；加密保存在本机，不上传私钥")).Changed() {
								e.keyInfo = nil
								delete(e.errors, "key")
							}
							if components.Button(c, tr(c, "选择私钥文件…")).Clicked() {
								a.openPrivateKeyPicker()
							}
						})
					}).Error(e.errors["key"])
					a.connectionField(c, "key", tr(c, "私钥口令"), &a.form.KeyPassphrase, true, tr(c, "仅在私钥文件已加密时填写"))
					ui.Field(c, tr(c, "私钥检查"), func() {
						if components.Button(c, tr(c, "检查私钥")).Disabled(a.keyFile == "").Clicked() {
							a.checkPrivateKey()
						}
						if e.keyInfo != nil {
							ui.Text(c, e.keyInfo.Algorithm+" · "+e.keyInfo.Fingerprint).FontSize(11).Selectable()
						}
					})
				case "agent":
					ui.Field(c, tr(c, "代理身份"), func() {
						if len(e.agentKeys) > 0 {
							selectKey := ui.SelectBase(c, &a.form.AgentFingerprint)
							label := tr(c, "选择代理中的密钥")
							for _, key := range e.agentKeys {
								if key.Fingerprint == a.form.AgentFingerprint {
									label = key.Algorithm + " · " + key.Fingerprint
								}
							}
							selectKey.Trigger.FillWidth().Label(tr(c, "选择代理身份")).Children(func() { ui.Text(c, label).SingleLine() })
							selectKey.Popup(func(panel ui.Element) {
								panel.Padding(5).Children(func() {
									for _, key := range e.agentKeys {
										selectKey.Item(key.Fingerprint).Padding(6).Children(func() { ui.Text(c, key.Comment+" · "+key.Algorithm+" · "+key.Fingerprint) })
									}
								})
							})
						}
						if components.Button(c, tr(c, "读取代理密钥")).Tooltip(tr(c, "只读取公钥供选择，私钥由密钥代理保管")).Clicked() {
							a.readAgentIdentities()
						}
					}).Error(e.errors["agent"])
				default:
					a.connectionField(c, "password", tr(c, "服务器密码"), &a.form.Password, true, "")
				}
			}
			if beforeHost != a.form.Host || beforePort != a.port {
				a.invalidateHostIdentity()
			}
			if a.form.Fingerprint != "" {
				ui.Field(c, tr(c, "服务器身份"), func() {
					ui.Text(c, tr(c, "已信任 · ")+a.form.Fingerprint).FontSize(11).Selectable()
					if components.Button(c, tr(c, "重新核对服务器身份")).Clicked() {
						a.invalidateHostIdentity()
						delete(e.errors, "trust")
						delete(e.errors, "form")
					}
				})
			}
			a.connectionPolicyForm(c)

		}).FillWidth().Disabled(e.phase != "")
	})
}

func (a *app) isTestVault() bool { return filepath.Base(a.dir) == "ssh-client-workbench-preview-qa" }

// Only the in-process human desktop holds this session. Public CLI requests
// cannot obtain it; they still need their own explicit owner approval.
func (a *app) unlockedOwner() (*vault.Session, error) {
	if !a.online || a.server == nil {
		return nil, errors.New(tr(nil, "保险库已锁定，请重新解锁后继续"))
	}
	return a.server.Vault, nil
}

func (a *app) connectionGroups() []string {
	seen := map[string]bool{}
	for _, d := range a.connections {
		if g := strings.TrimSpace(d.Group); g != "" {
			seen[g] = true
		}
	}
	groups := make([]string, 0, len(seen))
	for g := range seen {
		groups = append(groups, g)
	}
	sort.Strings(groups)
	return groups
}

func (a *app) connectionGroupField(c *ui.Context) {
	ui.Field(c, tr(c, "分组"), func() {
		t := c.Theme()
		var cb ui.ComboboxParts
		field := ui.Row(c).FillWidth().Height(34).Radius(t.Radius).Background(t.Surface).Border(1, t.Border).AlignItems(ui.Center).Children(func() {
			cb = ui.ComboboxBase(c, &a.form.Group)
			cb.Input.Grow(1).Height(32).Padding(6, 10).Label(tr(c, "分组输入")).Placeholder(tr(c, "选择已有分组，或输入新分组"))
			if components.IconButton(c, "⌄", tr(c, "展开分组")).Size(30, 30).TextColor(t.TextMuted).Clicked() {
				cb.SetOpen(!cb.Open())
				cb.Input.Focus()
			}
		})
		if cb.Input.Focused() {
			field.Border(1, t.Accent)
		}
		if cb.Input.Changed() {
			delete(a.connection.errors, "group")
		}
		groups := a.connectionGroups()
		cb.Popup(func(panel ui.Element) {
			panel.Background(t.Background).Border(1, t.Border).Radius(t.Radius).MaxHeight(220).Children(func() {
				ui.Scroll(c).MaxHeight(210).Children(func() {
					ui.Column(c).FillWidth().Padding(4).Children(func() {
						query := strings.TrimSpace(a.form.Group)
						exact := false
						item := func(value, label string) {
							row := cb.Item(value).Padding(8, 10).Radius(t.Radius).Label(tr(c, "分组：") + label)
							if row.Highlighted() {
								row.Background(t.Accent).TextColor(t.AccentText)
							}
							row.Children(func() { ui.Text(c, label) })
						}
						item("", tr(c, "未分组"))
						for _, group := range groups {
							exact = exact || group == query
							if !cb.Filtering() || strings.Contains(strings.ToLower(group), strings.ToLower(query)) {
								item(group, group)
							}
						}
						if query != "" && !exact {
							item(query, tr(c, "新建分组「")+query+"」")
						}
					})
				})
			})
		})
		if value, chosen := cb.Chosen(); chosen {
			a.form.Group = value
			delete(a.connection.errors, "group")
		}
	}).Error(a.connection.errors["group"])
}

func (a *app) connectionActionLabel(review bool) string {
	if review {
		if a.connection.connect {
			return tr(nil, "信任并连接")
		}
		return tr(nil, "信任并保存")
	}
	if a.connection.connect {
		return tr(nil, "保存并连接")
	}
	return tr(nil, "保存连接")
}

func (a *app) connectionFooter(c *ui.Context, review bool) {
	e := &a.connection
	ui.Column(c).FillWidth().Gap(8).Children(func() {
		for _, key := range []string{"trust", "form"} {
			if message := e.errors[key]; message != "" {
				// Handshake errors can contain untrusted server text. Bound its
				// display and escape controls without changing the error itself.
				runes := []rune(message)
				if len(runes) > 1024 {
					message = string(runes[:1024]) + "…"
				}
				ui.Text(c, reviewText(message)).MaxLines(3).FontSize(12).TextColor(c.Theme().Danger).FillWidth()
			}
		}
		if e.message != "" {
			ui.Text(c, e.message).FontSize(12).FillWidth()
		}
		components.ActionGroup(c, ui.Start, func() {
			cancelLabel := tr(c, "取消")
			if e.phase == "connect" {
				cancelLabel = tr(c, "取消连接（配置已保存）")
			}
			if components.Button(c, cancelLabel).Height(36).Disabled(e.phase == "save").Clicked() {
				a.cancelConnectionEditor()
			}
			if !review {
				c.Key("connection-connect-after-save")
				ui.Checkbox(c, &e.connect, tr(c, "保存后连接")).Disabled(e.phase != "")
			}
			if review && components.Button(c, tr(c, "返回修改")).Height(36).Disabled(e.phase != "").Clicked() {
				e.review = false
			}
			ui.Spacer(c).Grow(1)
			label := a.connectionActionLabel(review)
			switch e.phase {
			case "probe":
				label = tr(c, "正在读取服务器身份…")
			case "save":
				label = tr(c, "正在保存…")
			case "connect":
				label = tr(c, "正在连接…")
			}
			if components.Button(c, label, components.ButtonOptions{Variant: components.Primary, Busy: e.phase != ""}).Height(36).Clicked() {
				if review {
					a.trustAndSaveConnection()
				} else {
					a.saveProfile()
				}
			}
		})
	})
}

func (a *app) connectionIdentityView(c *ui.Context) {
	identity := a.connection.identity
	ui.Column(c).FillWidth().Gap(18).Children(func() {
		ui.Text(c, tr(c, "此服务器尚未被信任。")).TextColor(c.Theme().TextMuted)
		ui.Column(c).FillWidth().Padding(20).Gap(12).Radius(10).Background(c.Theme().Surface).Children(func() {
			ui.Text(c, a.form.Name).FontSize(17).Bold()
			ui.Text(c, a.form.User+"@"+targetID(identity.Host, identity.Port)).Selectable()
			ui.Text(c, tr(c, "主机指纹 · ")+identity.Algorithm).TextColor(c.Theme().TextMuted)
			ui.Text(c, identity.Fingerprint).Selectable().FillWidth().Label(tr(c, "待信任服务器指纹"))
		})
		ui.Text(c, tr(c, "请与云控制台或已有可信连接核对指纹，确认后才会登录服务器。")).FillWidth()
	})
}

func (a *app) trustAndSaveConnection() {
	if !a.online || a.connection.phase != "" || a.connection.identity == nil {
		return
	}
	identity := a.connection.identity
	port, err := strconv.Atoi(a.port)
	if err != nil || targetID(strings.TrimSpace(a.form.Host), port) != targetID(identity.Host, identity.Port) {
		a.invalidateHostIdentity()
		a.connection.errors["trust"] = tr(nil, "地址或端口已变化，请重新连接并核对身份")
		return
	}
	a.form.Fingerprint = identity.Fingerprint
	a.connection.trustedTarget = targetID(identity.Host, identity.Port)
	a.connection.confirmed = true
	a.saveProfile()
}

func (a *app) invalidateHostIdentity() {
	a.form.Fingerprint = ""
	a.connection.identity = nil
	a.connection.confirmed = false
	a.connection.review = false
	a.connection.trustedTarget = ""
}
func loadPrivateKey(file, phrase string) (string, *sshconn.KeyInfo, error) {
	st, err := os.Stat(file)
	if err != nil {
		return "", nil, errors.New(tr(nil, "无法打开私钥文件，请重新选择"))
	}
	if !st.Mode().IsRegular() || st.Size() > 1<<20 {
		return "", nil, errors.New(tr(nil, "请选择不超过 1 MiB 的私钥文件"))
	}
	f, err := os.Open(file)
	if err != nil {
		return "", nil, errors.New(tr(nil, "无法打开私钥文件，请重新选择"))
	}
	defer f.Close()
	st, err = f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Size() > 1<<20 {
		return "", nil, errors.New(tr(nil, "请选择不超过 1 MiB 的私钥文件"))
	}
	b, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil || len(b) > 1<<20 {
		wipe(b)
		return "", nil, errors.New(tr(nil, "无法读取完整私钥文件"))
	}
	defer wipe(b)
	raw := string(b)
	info, err := sshconn.InspectPrivateKey(raw, phrase)
	if err != nil {
		return "", nil, err
	}
	return raw, &info, nil
}
func (a *app) editorOperation(phase string) (context.Context, uint64, int) {
	if a.connection.cancel != nil {
		a.connection.cancel()
	}
	a.connection.sequence++
	seq := a.connection.sequence
	ctx, cancel := context.WithCancel(a.operationContext())
	a.connection.cancel = cancel
	a.connection.phase = phase
	return ctx, seq, a.generation
}
func (a *app) editorCurrent(seq uint64, gen int) bool {
	return a.generation == gen && a.connection.sequence == seq && a.page == 0 && a.online
}

// Native dialogs must not wait inside a render callback: nested native events
// may need the same UI frame lock. Return from the frame before opening it.
func (a *app) openPrivateKeyPicker() {
	pick := a.keyPicker
	if pick == nil {
		w := a.win
		pick = func() ([]string, error) {
			return mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: w, Title: tr(nil, "选择已有 SSH 私钥"), ShowHiddenFiles: true})
		}
	}
	a.selectPrivateKey(pick)
}

func (a *app) selectPrivateKey(pick func() ([]string, error)) {
	if a.connection.phase != "" {
		return
	}
	ctx, seq, gen := a.editorOperation("picker")
	a.connection.message = tr(nil, "请选择私钥文件；取消后可继续编辑")
	go func() {
		files, err := pick()
		a.postUI(func() {
			if !a.editorCurrent(seq, gen) || ctx.Err() != nil {
				return
			}
			a.connection.cancel()
			a.connection.cancel = nil
			a.connection.phase = ""
			a.connection.message = ""
			if err != nil {
				a.connection.errors["key"] = tr(nil, "无法打开文件选择器，请重试")
			} else if len(files) > 0 {
				a.keyFile = files[0]
				a.connection.keyInfo = nil
				a.checkPrivateKey()
			}
		})
	}()
}
func (a *app) checkPrivateKey() {
	if a.connection.phase != "" {
		return
	}
	file, phrase := a.keyFile, a.form.KeyPassphrase
	ctx, seq, gen := a.editorOperation("key")
	a.connection.message = tr(nil, "正在检查私钥…")
	go func() {
		_, info, err := loadPrivateKey(file, phrase)
		a.postUI(func() {
			if !a.editorCurrent(seq, gen) || ctx.Err() != nil {
				return
			}
			a.connection.cancel()
			a.connection.cancel = nil
			a.connection.phase = ""
			a.connection.message = ""
			a.connection.keyInfo = info
			if err != nil {
				a.connection.errors["key"] = err.Error()
			} else {
				delete(a.connection.errors, "key")
			}
		})
	}()
}
func (a *app) readAgentIdentities() {
	if a.connection.phase != "" {
		return
	}
	owner, err := a.unlockedOwner()
	if err != nil {
		a.status = err.Error()
		return
	}
	ctx, seq, gen := a.editorOperation("agent")
	cli := a.client
	a.connection.message = tr(nil, "正在读取代理中的公开身份…")
	go func() {
		result, err := cli.CallWithOwner(ctx, broker.Message{Action: "agentIdentities"}, owner)
		a.postUI(func() {
			if !a.editorCurrent(seq, gen) || ctx.Err() != nil {
				return
			}
			a.connection.cancel()
			a.connection.cancel = nil
			a.connection.phase = ""
			a.connection.message = ""
			if err != nil {
				a.connection.errors["agent"] = err.Error()
			} else {
				a.connection.agentKeys = result.Identities
				delete(a.connection.errors, "agent")
			}
		})
	}()
}

func (a *app) connectionDraft() (vault.ProfileChange, bool) {
	p := a.form
	p.Name = strings.TrimSpace(p.Name)
	p.Host = strings.TrimSpace(p.Host)
	p.User = strings.TrimSpace(p.User)
	p.Group = strings.TrimSpace(p.Group)
	if p.ID == "" {
		p.ID = vault.ShortID()
		a.form.ID = p.ID
	}
	errs := map[string]string{}
	port, err := strconv.Atoi(a.port)
	p.Port = port
	if p.Name == "" {
		errs["name"] = tr(nil, "请填写连接名称")
	}
	if p.User == "" {
		errs["user"] = tr(nil, "请填写 SSH 用户名")
	}
	if err != nil || port < 1 || port > 65535 {
		errs["port"] = tr(nil, "端口应为 1–65535 之间的数字")
	}
	if sshconn.ValidateTarget(p.Host, port) != nil {
		errs["host"] = tr(nil, "请检查服务器地址和端口")
	}
	if len(p.Group) > 128 || strings.ContainsAny(p.Group, "\x00\r\n") {
		errs["group"] = tr(nil, "分组不能超过 128 字节或包含换行")
	}
	keep := a.details != nil && !a.connection.replace && p.Auth == a.details.Auth
	if !keep {
		switch p.Auth {
		case "key":
			if a.keyFile == "" {
				errs["key"] = tr(nil, "请选择已有私钥文件")
			}
		case "agent":
			if p.AgentFingerprint == "" {
				errs["agent"] = tr(nil, "请读取并选择一个代理身份")
			}
		default:
			if p.Password == "" {
				errs["password"] = tr(nil, "请填写服务器登录密码")
			}
		}
	}
	if a.isTestVault() && p.Host != "localhost" {
		ip := net.ParseIP(p.Host)
		if ip == nil || !ip.IsLoopback() {
			errs["host"] = tr(nil, "测试库只用于本机测试；真实服务器请使用正式保险库")
		}
	}
	if a.connection.trustedTarget != targetID(p.Host, port) {
		p.Fingerprint = ""
		a.form.Fingerprint = ""
	}
	a.connection.errors = errs
	return vault.ProfileChange{Profile: p, KeepCredential: keep}, len(errs) == 0
}

func (a *app) saveProfile() {
	if a.connection.phase != "" || !a.online {
		return
	}
	change, ok := a.connectionDraft()
	if !ok {
		return
	}
	if change.Profile.Fingerprint == "" {
		if a.connection.identity != nil {
			a.connection.review = true
			return
		}
		ctx, seq, gen := a.editorOperation("probe")
		cli := a.client
		owner, err := a.unlockedOwner()
		if err != nil {
			a.cancelConnectionEditor()
			a.status = err.Error()
			return
		}
		a.connection.message = tr(nil, "正在读取服务器身份，尚未发送登录凭据…")
		go func() {
			result, err := cli.CallWithOwner(ctx, broker.Message{Action: "probeHost", Target: change.Profile.Host, Port: change.Profile.Port}, owner)
			a.postUI(func() {
				if !a.editorCurrent(seq, gen) || ctx.Err() != nil {
					return
				}
				a.connection.cancel()
				a.connection.cancel = nil
				a.connection.phase = ""
				a.connection.message = ""
				if err != nil {
					a.connection.errors["trust"] = err.Error()
				} else if result.HostIdentity != nil {
					a.connection.identity = result.HostIdentity
					a.connection.review = true
					a.connection.confirmed = false
				}
			})
		}()
		return
	}
	owner, err := a.unlockedOwner()
	if err != nil {
		a.connection.errors["form"] = err.Error()
		return
	}
	ctx, seq, gen := a.editorOperation("save")
	cli := a.client
	file, phrase := a.keyFile, a.form.KeyPassphrase
	connect := a.connection.connect
	a.connection.message = tr(nil, "正在加密保存连接…")
	go func() {
		fail := func(key string, err error) {
			a.postUI(func() {
				if !a.editorCurrent(seq, gen) {
					return
				}
				a.connection.cancel()
				a.connection.cancel = nil
				a.connection.phase = ""
				a.connection.message = ""
				a.connection.errors[key] = err.Error()
				a.connection.review = false
			})
		}
		if !change.KeepCredential && change.Profile.Auth == "key" {
			raw, _, err := loadPrivateKey(file, phrase)
			if err != nil {
				fail("key", err)
				return
			}
			change.Profile.PrivateKey = raw
		}
		if err = cli.SaveConnection(ctx, change, owner); err != nil {
			fail("form", err)
			return
		}
		a.postUI(func() {
			if a.editorCurrent(seq, gen) {
				if cancel := a.connecting[change.Profile.ID]; cancel != nil {
					cancel()
				}
			}
		})
		var stream *client.Stream
		if connect {
			a.postUI(func() {
				if a.editorCurrent(seq, gen) {
					a.connection.phase = "connect"
					a.connection.message = tr(nil, "已加密保存，正在连接终端…")
				}
			})
			stream, err = cli.ConnectWithOwner(ctx, change.Profile.ID, owner)
		}
		a.postUI(func() {
			if !a.editorCurrent(seq, gen) || ctx.Err() != nil {
				if stream != nil {
					stream.Close()
				}
				return
			}
			cancel := a.connection.cancel
			a.connection.cancel = nil
			p := change.Profile
			summary := vault.Summary{ID: p.ID, Name: p.Name, Group: p.Group, Host: p.Host, Port: p.Port, User: p.User, Auth: p.Auth, Fingerprint: p.Fingerprint, AgentFingerprint: p.AgentFingerprint, Policy: p.Policy}
			found := false
			for i, d := range a.connections {
				if d.ID == p.ID {
					a.connections[i] = summary
					found = true
				}
			}
			if !found {
				a.connections = append(a.connections, summary)
			}
			if err != nil {
				cancel()
				a.details = &summary
				a.connection.replace = !change.KeepCredential
				a.connection.review = false
				a.connection.phase = ""
				a.connection.message = tr(nil, "连接已保存；登录未成功，可修改后重试")
				a.connection.errors["form"] = connectionError(err)
				return
			}
			a.form.Password = ""
			a.form.PrivateKey = ""
			a.form.KeyPassphrase = ""
			a.ownerPass = ""
			a.keyFile = ""
			a.connection.phase = ""
			a.connection.message = ""
			a.details = nil
			a.switchHost(p.ID)
			a.page = 1
			if stream != nil {
				a.attachTerminal(stream, p.ID, cancel)
			} else {
				cancel()
				a.status = tr(nil, "连接已加密保存，双击即可连接")
			}
		})
	}()
}

func connectionError(err error) string {
	message := err.Error()
	switch {
	case strings.Contains(message, "主机指纹与已确认记录不一致"):
		return tr(nil, "服务器身份与已保存记录不同，已阻止登录。请先重新核对服务器身份")
	case strings.Contains(message, "unable to authenticate"):
		return tr(nil, "服务器未接受登录凭据，请检查用户名、密码或密钥")
	case errors.Is(err, context.DeadlineExceeded), strings.Contains(message, "timeout"), strings.Contains(message, "deadline exceeded"):
		return tr(nil, "连接超时，请检查服务器地址、端口和网络后重试")
	case strings.Contains(message, "connection refused"):
		return tr(nil, "服务器拒绝连接，请检查 SSH 服务和端口是否开放")
	}
	return message
}
