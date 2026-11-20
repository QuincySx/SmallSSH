package desktop

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"sshclient/internal/auditlog"
	"sshclient/internal/desktop/components"
)

func (a *app) showRelatedAudit(id, search string) {
	a.selectApprovalCategory(2)
	s := &a.approvalHistory
	s.query = auditlog.Query{Related: id, Search: search}
	s.search = search
	s.application = ""
	s.category = ""
	s.result = ""
	s.period = ""
	s.lastAttempt = time.Time{}
	a.loadApprovalHistory(false)
}

func (a *app) approvalHistoryFilters(c *ui.Context) {
	s := &a.approvalHistory
	ui.Row(c).FillWidth().MinWidth(0).Gap(6).Wrap().Children(func() {
		ui.TextInput(c, &s.search).Grow(1).MinWidth(180).Height(32).Placeholder(tr(c, "应用、服务器、命令、指纹")).Label(tr(c, "搜索审计记录"))
		category := map[string]string{"": "全部类型", "authorization": "授权", "operation": "操作", "ssh": "SSH 密钥", "system": "系统"}[s.category]
		if category == "" {
			category = tr(c, "全部类型")
		}
		components.Button(c, tr(c, category)).Padding(0, 8).Label(tr(c, "审计类型")).Menu(func(m *ui.Menu) {
			for _, v := range []struct{ key, label string }{{"", "全部类型"}, {"authorization", "授权"}, {"operation", "操作"}, {"ssh", "SSH 密钥"}, {"system", "系统"}} {
				if m.Item(tr(c, v.label)).Chosen() {
					s.category = v.key
				}
			}
		})
		appLabel := tr(c, "全部应用")
		if s.application != "" {
			appLabel = filepath.Base(s.application)
		}
		components.Button(c, appLabel).Padding(0, 8).Label(tr(c, "按应用筛选")).Menu(func(m *ui.Menu) {
			if m.Item(tr(c, "全部应用")).Chosen() {
				s.application = ""
			}
			if s.summary != nil {
				for _, app := range s.summary.Applications {
					if app.Application == "其他应用" {
						continue
					}
					label := fmt.Sprintf(tr(c, "%s · 授权 %d / 操作 %d / 签名 %d"), reviewText(app.Application), app.Authorizations, app.Operations, app.Signatures)
					if m.Item(label).Chosen() {
						s.application = app.Application
					}
				}
			}
		})
		result := map[string]string{"": "全部结果", "approved": "已批准", "denied": "已拒绝", "success": "成功", "failed": "失败", "revoked": "已撤销", "expired": "已到期"}[s.result]
		components.Button(c, tr(c, result)).Padding(0, 8).Label(tr(c, "审计结果")).Menu(func(m *ui.Menu) {
			for _, v := range []struct{ key, label string }{{"", "全部结果"}, {"approved", "已批准"}, {"denied", "已拒绝"}, {"success", "成功"}, {"failed", "失败"}, {"revoked", "已撤销"}, {"expired", "已到期"}} {
				if m.Item(tr(c, v.label)).Chosen() {
					s.result = v.key
				}
			}
		})
		period := s.period
		if period == "" {
			period = "全部时间"
		}
		components.Button(c, tr(c, period)).Padding(0, 8).Label(tr(c, "审计时间范围")).Menu(func(m *ui.Menu) {
			for _, v := range []string{"全部时间", "最近 24 小时", "最近 7 天", "最近 30 天"} {
				if m.Item(tr(c, v)).Chosen() {
					s.period = v
				}
			}
		})
		if components.Button(c, tr(c, "筛选")).Padding(0, 8).Disabled(s.loading).Clicked() {
			q := auditlog.Query{Category: s.category, Search: strings.TrimSpace(s.search), Application: s.application, Result: s.result}
			days := map[string]int{"最近 24 小时": 1, "最近 7 天": 7, "最近 30 天": 30}[s.period]
			if days > 0 {
				q.From = time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour)
			}
			s.query = q
			s.lastAttempt = time.Time{}
			a.loadApprovalHistory(false)
		}
		if s.query.Related != "" {
			if components.Button(c, tr(c, "清除关联筛选")).Clicked() {
				s.query.Related = ""
				s.lastAttempt = time.Time{}
				a.loadApprovalHistory(false)
			}
		}
	})
}

// Only an explicit export click opens the native save panel. QA tests exercise
// ExportHistory with synthetic data and never open a system dialog.
func (a *app) exportAuditHistory(format string) {
	s := &a.approvalHistory
	if s.exporting {
		return
	}
	owner, err := a.unlockedOwner()
	if err != nil {
		s.err = err.Error()
		return
	}
	name := "SmallSSH-audit-" + time.Now().Format("20060102-150405") + "." + strings.ToLower(format)
	destination, err := mygo.Dialog.Save(mygo.SaveDialogOptions{Parent: a.win, Title: tr(nil, "导出审计记录"), DefaultPath: name})
	if err != nil {
		s.err = err.Error()
		return
	}
	if destination == "" {
		return
	}
	q, cli, gen, seq := s.query, a.client, a.generation, s.sequence
	s.exporting = true
	ctx, cancel := context.WithTimeout(a.operationContext(), 2*time.Minute)
	go func() {
		defer cancel()
		f, err := os.CreateTemp(filepath.Dir(destination), ".smallssh-audit-*")
		count := 0
		if err == nil {
			defer os.Remove(f.Name())
			count, err = cli.ExportHistory(ctx, f, q, format, owner)
			if err == nil {
				err = f.Sync()
			}
			closeErr := f.Close()
			if err == nil {
				err = closeErr
			}
			if err == nil {
				err = os.Rename(f.Name(), destination)
			}
		}
		a.postUI(func() {
			if gen != a.generation {
				return
			}
			a.approvalHistory.exporting = false
			if err != nil {
				a.approvalHistory.err = tr(nil, "审计导出失败：") + err.Error()
				return
			}
			if seq == a.approvalHistory.sequence {
				a.approvalHistory.err = ""
			}
			a.status = fmt.Sprintf(tr(nil, "已导出 %d 条审计记录"), count)
		})
	}()
}
