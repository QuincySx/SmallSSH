package desktop

import (
	"context"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/identitymodel"
)

func offlineIdentities() identitySettingsState {
	return identitySettingsState{loaded: true, state: identitymodel.State{Supported: true, Socket: "/synthetic/SmallSSH/signing/agent.sock", Notifications: "enabled", Keys: []identitymodel.Key{{ID: "fixture", Name: "开发机", Authentication: "presence", Fingerprint: "SHA256:abcdefghijklmnopqrstuvwxyz0123456789ABCDE12", PublicKey: "ecdsa-sha2-nistp256 AAAASYNTHETICOFFLINEONLY"}}}}
}
func TestSSHIdentitySettingsExposePublicMetadataAndExplicitDelete(t *testing.T) {
	for _, width := range []int{720, 1080} {
		a := &app{settingsSection: settingsIdentities, identities: offlineIdentities()}
		tt := settingsTester(a, width, 900)
		if !tt.HasText("签名前系统验证") || !tt.HasText("开发机") {
			t.Fatal("key identity or authentication policy missing")
		}
		tt.SetClipboard("before")
		if err := tt.Click("复制公钥：开发机"); err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(tt.Clipboard(), "AAAASYNTHETICOFFLINEONLY 开发机") {
			t.Fatal("did not copy exactly the public identity")
		}
		if err := tt.Click("删除 SSH 密钥：开发机"); err != nil {
			t.Fatal(err)
		}
		if a.identities.deleting != "fixture" || !tt.HasText("删除后无法恢复。已部署的公钥需从服务器移除。") {
			t.Fatal("deletion skipped review")
		}
		if err := tt.Click("取消删除"); err != nil || a.identities.deleting != "" {
			t.Fatal("delete cancellation failed", err)
		}
		if err := tt.Click("密钥限时授权：开发机"); err != nil {
			t.Fatal(err)
		}
		if len(tt.Menu()) != 4 {
			t.Fatal("timed options diverged from reference", tt.Menu())
		}
	}
}
func TestSSHIdentitySettingsCancelRequestsOnCloseAndCategoryChange(t *testing.T) {
	for _, closeWindow := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		a := &app{settingsSection: settingsIdentities, identities: offlineIdentities()}
		a.identities.cancel = cancel
		a.identities.busy = true
		if closeWindow {
			a.closeSettingsWindow()
		} else {
			a.selectSettingsSection(settingsWorkspace)
		}
		if ctx.Err() == nil || a.identities.busy || a.identities.loaded {
			t.Fatal("native authorization survived leaving its settings context")
		}
	}
}

func TestSSHIdentitySettingsFailureDoesNotKeepLoading(t *testing.T) {
	a := &app{settingsSection: settingsIdentities, identities: identitySettingsState{error: "synthetic key storage failure"}}
	tt := settingsTester(a, 720, 700)
	if !tt.HasText("synthetic key storage failure") || tt.HasText("正在读取 SSH 密钥…") {
		t.Fatal("failed metadata read still claims to be loading")
	}
	if _, ok := tt.Find("刷新密钥"); !ok {
		t.Fatal("failed metadata read lost its retry action")
	}
}

func TestSSHIdentityCreationNamesTheMacLoginPassword(t *testing.T) {
	name, authentication := "QA 密钥", ""
	var created identitymodel.Change
	tt := ui.NewTester(func(c *ui.Context) {
		identityCreationFields(c, &name, &authentication, false, func(change identitymodel.Change) { created = change }, nil)
	}, 360, 240)
	if !tt.HasText(identitySystemAuthentication) {
		t.Fatal("the signature policy did not identify which password macOS may request")
	}
	if err := tt.Click("生成密钥"); err != nil || created.Authentication != "presence" {
		t.Fatal("creating a system-protected SSH key used a different policy", created.Authentication, err)
	}
	if err := tt.Click("签名验证选择"); err != nil {
		t.Fatal(err)
	}
	if bounds, ok := tt.Find("签名验证选项"); !ok || bounds.W != 256 || bounds.H != 104 {
		t.Fatal("the signature verification popup did not match the design size", bounds, ok)
	}
	if err := tt.Click("仅当前指纹"); err != nil || authentication != "biometry" {
		t.Fatal("could not choose current fingerprints from the signing policy popup", authentication, err)
	}
}
