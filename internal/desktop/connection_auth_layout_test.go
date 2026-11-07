package desktop

import (
	"context"
	"strconv"
	"testing"

	"github.com/egoist/mygo/ui"
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
						{"硬件密钥（PKCS#11）", "pkcs11", "PKCS#11 原生模块输入"},
						{"服务器密码", "password", "服务器密码输入"},
					}
					if locale == "en" {
						fields = []string{"Name input", "Server address input", "Port input", "Group input", "Username input", "Login method selection"}
						options = []struct{ label, kind, branch string }{
							{"Existing private key", "key", "Private key file input"},
							{"SSH agent", "agent", "Load agent keys"},
							{"Hardware key (PKCS#11)", "pkcs11", "PKCS#11 native module input"},
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
