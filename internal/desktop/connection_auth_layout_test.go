package desktop

import (
	"context"
	"runtime"
	"strconv"
	"testing"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/connectcfg"
	"sshclient/internal/hardware"
	"sshclient/internal/vault"
)

func TestConnectionAuthSwitchKeepsCommonFieldsStable(t *testing.T) {
	for _, width := range []int{560, 1100} {
		for _, locale := range []string{"zh-Hans", "en"} {
			for _, editing := range []bool{false, true} {
				t.Run(locale+"/"+strconv.Itoa(width)+"/edit="+strconv.FormatBool(editing), func(t *testing.T) {
					a := &app{ctx: context.Background(), online: true}
					a.newConnection()
					if editing {
						a.details = &vault.Summary{Auth: "password"}
					}
					a.form.Name, a.form.Host, a.form.User = "Example", "192.0.2.10", "demo"
					a.form.Password = "synthetic password"
					tt := ui.NewTester(a.connectionView, width, 760)
					tt.SetLocale(locale)
					fields := []string{"名称输入", "服务器地址输入", "端口输入", "分组输入", "用户名输入", "登录方式选择"}
					options := []struct{ label, kind, branch string }{
						{"已有私钥", "key", "私钥文件输入"},
						{"SSH 密钥代理", "agent", "读取代理密钥"},
						{"硬件密钥", "pkcs11", "选择驱动"},
						{"服务器密码", "password", "服务器密码输入"},
					}
					if locale == "en" {
						fields = []string{"Name input", "Server address input", "Port input", "Group input", "Username input", "Login method selection"}
						options = []struct{ label, kind, branch string }{
							{"Existing private key", "key", "Private key file input"},
							{"SSH agent", "agent", "Load agent keys"},
							{"Hardware key", "pkcs11", "Choose driver"},
							{"Server password", "password", "Server password input"},
						}
					}
					before := make(map[string]ui.Rect, len(fields))
					for _, label := range fields {
						r, ok := tt.Find(label)
						if !ok {
							t.Fatal("common field missing", label)
						}
						before[label] = r
					}
					for _, option := range options {
						a.form.Password, a.form.PrivateKey, a.form.KeyPassphrase, a.form.Hardware.PIN = "password", "private key", "passphrase", "1234"
						a.keyFile = "/synthetic/key"
						if err := tt.Click(fields[5]); err != nil {
							t.Fatal(err)
						}
						if err := tt.Click(option.label); err != nil {
							t.Fatal(err)
						}
						if a.form.Auth != option.kind || a.form.Password != "" || a.form.PrivateKey != "" || a.form.KeyPassphrase != "" || a.form.Hardware.PIN != "" || a.keyFile != "" {
							t.Fatal("authentication switch lost its secret-clearing behavior", option.kind)
						}
						if _, ok := tt.Find(option.branch); !ok {
							t.Fatal("authentication branch missing", option.branch)
						}
						for _, label := range fields {
							r, ok := tt.Find(label)
							if !ok || r != before[label] {
								t.Fatalf("%s changed the common field %s: %v -> %v", option.kind, label, before[label], r)
							}
						}
						if a.form.Name != "Example" || a.form.Host != "192.0.2.10" || a.form.User != "demo" {
							t.Fatal("authentication switch changed the connection draft")
						}
					}
				})
			}
		}
	}
}

func TestConnectionAuthHardwareShowsPINOnlyAfterDeviceSelection(t *testing.T) {
	a := &app{}
	a.form.Hardware.Module = "/example/opensc-pkcs11.so"
	a.connection.hardwareKeys = []hardware.Key{{Token: "测试智能卡", Serial: "test-card"}}
	tt := ui.NewTester(a.hardwareForm, 560, 500)
	if tt.HasText("设备 PIN") || !tt.HasText("测试智能卡") {
		t.Fatal("device discovery exposed a PIN or hid the device name")
	}
	if err := tt.Click("选择设备"); err != nil {
		t.Fatal(err)
	}
	if a.form.Hardware.TokenSerial != "test-card" || !tt.HasText("设备 PIN") {
		t.Fatal("the selected device cannot be unlocked")
	}
	a.form.Hardware.PIN, a.form.Hardware.KeyID = "synthetic PIN", "3d"
	if err := tt.Click("更换设备"); err != nil {
		t.Fatal(err)
	}
	if a.form.Hardware != (connectcfg.Hardware{Module: "/example/opensc-pkcs11.so"}) {
		t.Fatal("changing devices kept the prior device's PIN or identity")
	}
}

func TestConnectionAuthCanCreateManagedKeyInsideTheForm(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Secure Enclave is macOS-only")
	}
	a := &app{}
	tt := ui.NewTester(a.managedConnectionForm, 560, 500)
	if err := tt.Click("新建密钥"); err != nil {
		t.Fatal(err)
	}
	if _, ok := tt.Find("SSH 密钥名称"); !ok || a.connection.managed.auth != "presence" {
		t.Fatal("creation is missing a name or the default system authentication")
	}
	if err := tt.Click("取消新建密钥"); err != nil {
		t.Fatal(err)
	}
	key := offlineIdentities().state.Keys[0]
	a.connection.managed.keys = offlineIdentities().state.Keys
	a.form.AgentFingerprint = key.Fingerprint
	tt.Frame()
	if err := tt.Click("复制公钥"); err != nil {
		t.Fatal(err)
	}
	if tt.Clipboard() != key.PublicKey+" "+key.Name {
		t.Fatal("the selected public key was not copied")
	}
}

func TestConnectionAuthSystemSmartCardLeavesPINToMacOS(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("system smart cards use macOS")
	}
	a := &app{}
	tt := ui.NewTester(a.hardwareForm, 560, 500)
	if _, ok := tt.Find("读取系统智能卡"); !ok {
		t.Fatal("system smart-card entry is missing")
	}
	a.selectHardwareDriver(connectcfg.MacSmartCard)
	a.form.Hardware.TokenSerial = "synthetic-card"
	tt.Frame()
	if tt.HasText("输入设备 PIN") || tt.HasText("设备 PIN") {
		t.Fatal("system card PIN must be entered only in the OS prompt")
	}
}
