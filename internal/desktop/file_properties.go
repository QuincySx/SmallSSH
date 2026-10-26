package desktop

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/approval"
	"sshclient/internal/client"
	"sshclient/internal/desktop/components"
	"sshclient/internal/fileattrs"
)

type filePropertiesState struct {
	source              *filePane
	path                string
	info                fileattrs.Info
	checks              [9]bool
	owner, group, error string
	loading, saving     bool
	ctx                 context.Context
	cancel              context.CancelFunc
}

func (a *app) cancelFileProperties() {
	if s := a.prompt.properties; s != nil && s.cancel != nil {
		s.cancel()
	}
}

func (a *app) openFileProperties(p *filePane, name string) {
	endpoint, b := p.endpoint, p.browser()
	a.ask(tr(nil, "权限 / 属性"), a.endpointName(endpoint)+" · "+name, "", "", "", tr(nil, "保存"), nil)
	s := &filePropertiesState{source: &filePane{endpoint: endpoint, states: map[string]*directoryBrowser{endpoint: b}}, path: name}
	s.ctx, s.cancel = context.WithCancel(a.operationContext())
	a.prompt.properties = s
	a.readFileProperties(s)
}

func (a *app) currentFileProperties(s *filePropertiesState) bool {
	return a.online && a.prompt.open && a.prompt.properties == s && a.prompt.generation == a.generation && s.ctx.Err() == nil
}

func (s *filePropertiesState) loaded(info fileattrs.Info) {
	s.info = info
	s.checks = components.PermissionChecks(info.Mode)
	s.owner = fileattrs.AccountValue(info.UID, info.Users)
	s.group = fileattrs.AccountValue(info.GID, info.Groups)
	s.loading, s.error = false, ""
}

func (a *app) readFileProperties(s *filePropertiesState) {
	s.loading, s.error = true, ""
	if a.win == nil && a.dispatch == nil {
		return
	} // Offline views never perform IO.
	finish := func(info fileattrs.Info, err error) {
		if !a.currentFileProperties(s) {
			return
		}
		s.loading = false
		if err != nil {
			s.error = err.Error()
			return
		}
		s.loaded(info)
	}
	if s.source.endpoint == localEndpoint {
		go func() {
			info, err := fileattrs.LocalInfo(s.ctx, s.path)
			a.postUI(func() { finish(info, err) })
		}()
		return
	}
	a.beginContext(s.ctx, approval.Operation{Host: s.source.endpoint, Kind: "file-info", Path: s.path}, func(stream *client.Stream) (func(), error) {
		defer stream.Close()
		info, err := stream.ReadFileInfo()
		return func() { finish(info, err) }, nil
	}, func(err error) { finish(fileattrs.Info{}, err) })
}

func (s *filePropertiesState) change() (fileattrs.Change, error) {
	c := fileattrs.Change{Expected: s.info.Snapshot}
	mode := components.PermissionMode(s.checks)
	if mode != s.info.Mode&0777 {
		c.SetMode, c.Mode = true, mode
	}
	if s.info.OwnershipKnown {
		uid, err := fileattrs.ResolveAccount(strings.TrimSpace(s.owner), s.info.Users)
		if err != nil {
			return c, fmt.Errorf(tr(nil, "所属用户：%w"), err)
		}
		gid, err := fileattrs.ResolveAccount(strings.TrimSpace(s.group), s.info.Groups)
		if err != nil {
			return c, fmt.Errorf(tr(nil, "所属组：%w"), err)
		}
		if uid != s.info.UID {
			c.SetUID, c.UID = true, uid
		}
		if gid != s.info.GID {
			c.SetGID, c.GID = true, gid
		}
	}
	return c, nil
}

func (a *app) saveFileProperties(s *filePropertiesState) {
	if !a.currentFileProperties(s) || s.loading || s.saving || !s.info.Valid {
		return
	}
	change, err := s.change()
	if err != nil {
		s.error = err.Error()
		return
	}
	if !change.SetMode && !change.SetUID && !change.SetGID {
		a.closeFileProperties(s)
		return
	}
	if err = change.Validate(); err != nil {
		s.error = err.Error()
		return
	}
	s.saving, s.error = true, ""
	finish := func(info fileattrs.Info, err error) {
		if !a.currentFileProperties(s) {
			return
		}
		s.saving = false
		if info.Valid {
			if len(info.Users) == 0 {
				info.Users = s.info.Users
			}
			if len(info.Groups) == 0 {
				info.Groups = s.info.Groups
			}
			s.info = info
			if !change.SetUID {
				s.owner = fileattrs.AccountValue(info.UID, info.Users)
			}
			if !change.SetGID {
				s.group = fileattrs.AccountValue(info.GID, info.Groups)
			}
			if !change.SetMode {
				s.checks = components.PermissionChecks(info.Mode)
			}
			a.refreshFileMetadata(s.source.endpoint, s.path, info)
		}
		if err != nil {
			s.error = err.Error()
			return
		}
		a.closeFileProperties(s)
	}
	if s.source.endpoint == localEndpoint {
		go func() {
			info, err := fileattrs.SetLocalAttributes(s.ctx, s.path, change)
			a.postUI(func() { finish(info, err) })
		}()
		return
	}
	a.beginContext(s.ctx, approval.Operation{Host: s.source.endpoint, Kind: "file-attributes", Path: s.path, Attributes: change}, func(stream *client.Stream) (func(), error) {
		defer stream.Close()
		info, err := stream.ReadFileInfo()
		return func() { finish(info, err) }, nil
	}, func(err error) { finish(fileattrs.Info{}, err) })
}

func (a *app) closeFileProperties(s *filePropertiesState) {
	if a.prompt.properties != s {
		return
	}
	if s.cancel != nil {
		s.cancel()
	}
	a.prompt = promptState{}
	a.invalidateWindows()
}

func (a *app) filePropertiesView(c *ui.Context) {
	s := a.prompt.properties
	if s == nil {
		return
	}
	readonly := parentDir(s.source.endpoint == localEndpoint, s.path) == s.path || s.info.Type != 0 && os.FileMode(s.info.Type) != os.ModeDir
	disabled := s.loading || s.saving || !s.info.Valid || readonly
	components.Dialog(c, &a.prompt.open, components.DialogOptions{Title: tr(c, "权限 / 属性"), Target: reviewText(a.prompt.target), Width: 540, Height: 480,
		Close: func() { a.closeFileProperties(s) }, CloseDisabled: s.saving}, func() {
		ui.Column(c).FillWidth().Gap(16).Children(func() {
			if s.loading {
				ui.Row(c).Gap(8).Children(func() { ui.Spinner(c).Size(14, 14); ui.Text(c, tr(c, "正在读取属性…")).FontSize(12) })
				return
			}
			if s.info.Valid {
				components.PermissionMatrix(c, &s.checks, disabled)
				ui.Row(c).FillWidth().Gap(12).Children(func() {
					for _, field := range []struct {
						label    string
						value    *string
						accounts []fileattrs.Account
					}{{tr(c, "所属用户"), &s.owner, s.info.Users}, {tr(c, "所属组"), &s.group, s.info.Groups}} {
						ui.Column(c).Grow(1).MinWidth(0).Gap(6).Children(func() {
							ui.Text(c, field.label).FontSize(12)
							options := make([]string, 0, len(field.accounts))
							for _, account := range field.accounts {
								options = append(options, account.Name)
							}
							components.EditableChoice(c, field.value, options, field.label, disabled || !s.info.OwnershipKnown).
								Tooltip(tr(c, "选择账户名称，或输入数字 UID/GID"))
						})
					}
				})
				if s.info.OwnershipKnown {
					ui.Text(c, fmt.Sprintf("UID %d · GID %d", s.info.UID, s.info.GID)).FontSize(11).TextColor(c.Theme().TextMuted)
				}
				if s.info.Mode&07000 != 0 {
					ui.Text(c, fmt.Sprintf(tr(c, "特殊权限 %04o · 仅调整读、写、执行位"), s.info.Mode&07000)).FontSize(11).TextColor(c.Theme().TextMuted)
				}
				if readonly {
					ui.Text(c, tr(c, "根目录、符号链接及特殊文件仅查看属性")).FontSize(12).TextColor(c.Theme().TextMuted)
				}
				if s.info.Note != "" {
					ui.Text(c, s.info.Note).FontSize(11).TextColor(c.Theme().TextMuted)
				}
			}
			if s.error != "" {
				ui.Text(c, reviewText(s.error)).FillWidth().FontSize(12).TextColor(c.Theme().Danger).Label(tr(c, "属性错误"))
			}
		})
	}, func() {
		if s.error != "" && !s.loading && components.Button(c, tr(c, "重新读取")).Disabled(s.saving).Clicked() {
			a.readFileProperties(s)
		}
		if components.Button(c, tr(c, "取消"), components.ButtonOptions{Disabled: s.saving}).Clicked() {
			a.closeFileProperties(s)
			return
		}
		if components.PrimaryButton(c, tr(c, "保存属性")).Disabled(disabled).Clicked() {
			a.saveFileProperties(s)
		}
	})
	if !a.prompt.open {
		if s.saving {
			a.prompt.open = true
			a.invalidateWindows()
		} else {
			a.closeFileProperties(s)
		}
	}
}

func (a *app) refreshFileMetadata(endpoint, name string, info fileattrs.Info) {
	local := endpoint == localEndpoint
	for _, p := range a.allFilePanes() {
		if b := p.states[endpoint]; b != nil {
			dir, base := parentDir(local, name), baseName(local, name)
			fs := b.cache[dir]
			for i := range fs {
				if fs[i].Name == base {
					mode := fileattrs.FileMode(info.Mode) | os.FileMode(info.Type)
					fs[i].Mode = mode.String()
				}
			}
		}
	}
}
