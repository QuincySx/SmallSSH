//go:build preview_qa

package desktop

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"sshclient/internal/approval"
	"sshclient/internal/auditlog"
	"sshclient/internal/desktop/components"
	"sshclient/internal/identitymodel"
	"sshclient/internal/localize"
	"sshclient/internal/vault"
)

func authorizationPreviewFixture() *app {
	now := time.Now().UTC()
	a := historyUIFixture()
	a.identities = offlineIdentities()
	a.connections = []vault.Summary{{ID: "offline-web", Name: "Web 生产环境"}}
	a.requests = []approval.Request{{ID: "offline-grant", Nonce: "offline-generation", Status: "approved", Peer: approval.Peer{PID: 1234, Path: "/Applications/Offline QA.app/Contents/MacOS/Offline QA", Instance: "synthetic-instance", CodeID: "synthetic-image"}, Expires: now.Add(time.Hour), Operation: approval.Operation{Kind: "grant", Host: "offline-web", Endpoint: "ops@192.0.2.10:22", Grant: approval.GrantScope{Permission: "guarded", LifetimeSeconds: 7200, IdleSeconds: 900, ReadRoot: "/srv/site", WriteRoot: "/srv/site"}}, Activity: &approval.GrantActivity{ApprovedAt: now.Add(-time.Hour), LastUsed: now, IdleSeconds: 900}}}
	a.identities.state.Leases = []identitymodel.Lease{{ID: "offline-lease", Key: "fixture", Started: now.Add(-time.Hour).UnixMilli(), Expires: now.Add(time.Hour).UnixMilli(), LastUsed: now.UnixMilli(), IdleSeconds: 900}}
	a.approvalHistory.detail = -1
	previousReview := false
	grantGeneration := a.requests[0].Generation()
	a.approvalHistory.events = []auditlog.Event{
		{Time: now, Event: "ended", Kind: "exec", Outcome: "success", Host: "offline-web", HostName: "Web 生产环境", Endpoint: "ops@192.0.2.10:22", Executable: "/Applications/Offline QA.app/Contents/MacOS/Offline QA", PeerPID: 1234, ProcessInstance: "synthetic-instance", ExecutableID: "synthetic-image", Command: "uname -a", RequestID: "offline-operation", ParentID: "offline-grant", ParentGeneration: grantGeneration, MediumReview: &previousReview, OperationHash: "synthetic-operation-hash", RecordHash: "synthetic-record-hash"},
		{Time: now.Add(-time.Minute), Event: "ssh_signed", Kind: "ssh-key-sign", Outcome: "success", Executable: "/usr/bin/ssh", KeyFingerprint: a.identities.state.Keys[0].Fingerprint, Destination: "SSH 登录签名", Authorization: "lease", RequestID: "offline-signature"},
		{Time: now.Add(-time.Hour), Event: "approved", Kind: "grant", Executable: "/Applications/Offline QA.app/Contents/MacOS/Offline QA", Permission: "guarded", HostName: "Web 生产环境", Expires: now.Add(time.Hour), IdleSeconds: 900, RequestID: "offline-grant"},
	}
	a.approvalHistory.summary = &auditlog.Summary{Records: 3, Authorizations: 1, Operations: 1, Signatures: 1, Applications: []auditlog.ApplicationCount{{Application: "/usr/bin/ssh", Signatures: 1}, {Application: "/Applications/Offline QA.app/Contents/MacOS/Offline QA", Authorizations: 1, Operations: 1}}}
	return a
}

func TestAuthorizationCenterOfflineSnapshots(t *testing.T) {
	if os.Getenv("SSH_CLIENT_PREVIEW_QA") != "1" {
		t.Skip("opt-in offline snapshots")
	}
	dir := os.Getenv("SSH_CLIENT_UI_SNAPSHOT_DIR")
	if !filepath.IsAbs(dir) {
		t.Fatal("absolute snapshot directory required")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, locale := range []string{"zh-Hans", "en"} {
		for _, dark := range []bool{false, true} {
			for _, width := range []int{560, 1080, 1440} {
				for _, state := range []string{"active", "active-medium-auto", "audit", "detail", "empty", "error"} {
					name := fmt.Sprintf("authorization-%s-%s-%t-%d", state, locale, dark, width)
					t.Run(name, func(t *testing.T) {
						previous := i18n.Locale()
						i18n.SetLocaleFunc(func() string { return locale })
						defer i18n.SetLocaleFunc(func() string { return previous })
						a := authorizationPreviewFixture()
						a.settingsOpen = true
						a.settingsSection = settingsApprovals
						a.approvalTab = 2
						if state == "active" || state == "active-medium-auto" || state == "empty" {
							a.approvalTab = 1
						}
						if state == "active-medium-auto" {
							review := false
							a.requests[0].Activity.MediumReview = &review
						}
						if state == "detail" {
							a.approvalHistory.detail = 0
						}
						if state == "empty" {
							a.requests = nil
							a.identities.state.Leases = nil
						}
						if state == "error" {
							a.approvalHistory.events = nil
							a.approvalHistory.summary = nil
							a.approvalHistory.next = ""
							a.approvalHistory.err = tr(nil, "无法读取历史，请重试")
						}
						tt := ui.NewTester(func(c *ui.Context) { components.ApplyTheme(c, false); a.settingsView(c) }, width, 800)
						tt.SetLocale(locale)
						tt.SetDark(dark)
						tt.Frame()
						tt.Frame()
						for _, label := range []string{localize.Text(locale, "当前授权"), localize.Text(locale, "审计记录")} {
							if r, ok := tt.Find(label); !ok || r.X < 0 || r.Y < 0 || r.X+r.W > float32(width)+1 {
								t.Fatal("category escaped viewport", label, r)
							}
						}
						f, err := os.Create(filepath.Join(dir, name+".png"))
						if err != nil {
							t.Fatal(err)
						}
						err = png.Encode(f, tt.Image())
						closeErr := f.Close()
						if err != nil || closeErr != nil {
							t.Fatal(err, closeErr)
						}
					})
				}
			}
		}
	}
}
