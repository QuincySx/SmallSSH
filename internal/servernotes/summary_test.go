package servernotes

import (
	"strings"
	"testing"

	"sshclient/internal/monitor"
)

func TestSummaryListsOnlyServicesAndDomains(t *testing.T) {
	r := Report{Complete: true, Inventory: monitor.Inventory{OS: "Debian GNU/Linux 12", Manager: "systemd", Services: []monitor.Service{
		{Name: "apparmor.service", Active: "active", Sub: "exited"},
		{Name: "cloud-final.service", Active: "active", Sub: "exited"},
		{Name: "dbus.service", Active: "active", Sub: "running"},
		{Name: "systemd-journald.service", Active: "active", Sub: "running"},
		{Name: "ssh.service", Active: "active", Sub: "running"},
		{Name: "substore.service", Active: "active", Sub: "running"},
		{Name: "openresty.service", Active: "active", Sub: "running"},
		{Name: "frps.service", Active: "active", Sub: "running"},
	}, Docker: &monitor.DockerInfo{Status: "missing"}}, Ports: []string{"TCP 127.0.0.53:53", "TCP *:9080"}, Domains: []Domain{
		{Name: "sub.example.test", Status: "匹配已知地址", Source: "/etc/nginx/conf.d/sub.conf"},
		{Name: "www.example.test", Status: "匹配已知地址"},
		{Name: "example.test", Status: "未匹配已知地址（可能经 CDN / 代理）", IPs: []string{"198.51.100.7"}},
	}}
	text := r.Text()
	for _, want := range []string{"服务：Sub-Store、OpenResty、FRP 服务端", "域名：\nsub.example.test\nwww.example.test\nexample.test（解析地址不同）"} {
		if !strings.Contains(text, want) {
			t.Fatal("summary did not answer purpose or highlight a DNS difference", want, text)
		}
	}
	for _, noise := range []string{"apparmor", "cloud-final", "dbus", "journald", "ssh.service", "Debian", "Docker", "127.0.0.53", "/etc/nginx", "证书自动续期", "网站可访问", "订阅管理", "运行中", "198.51.100.7", "可能经 CDN"} {
		if strings.Contains(text, noise) {
			t.Fatal("plumbing, raw evidence or unverified claim leaked into the main summary", noise, text)
		}
	}
	if !strings.Contains(r.Details(), "apparmor.service") || !strings.Contains(r.Details(), "/etc/nginx/conf.d/sub.conf") || !strings.Contains(r.Details(), "127.0.0.53") {
		t.Fatal("concise summary lost underlying evidence")
	}
	if len(text) > 1000 {
		t.Fatal("three applications still produced a wall of text", len(text))
	}
}

func TestSummaryMarksOnlyServiceExceptionsAndKeepsUnknownNames(t *testing.T) {
	r := Report{Inventory: monitor.Inventory{Services: []monitor.Service{
		{Name: "postgresql.service", Enabled: "enabled", Active: "active", Sub: "exited"},
		{Name: "frpc.service", Enabled: "enabled", Active: "inactive", Sub: "dead"},
		{Name: "acme-importer.service", Active: "active", Sub: "running"},
		{Name: "custom-api.service", Active: "failed", Sub: "failed"},
		{Name: "redis.service", Enabled: "disabled", Active: "inactive"},
	}, Docker: &monitor.DockerInfo{Status: "ready", Containers: []monitor.Container{
		{Names: "subscription-1", Image: "docker.io/example/sub-store:latest", State: "running"},
		{Names: "custom-worker", Image: "example/private-worker:v1", State: "exited"},
	}}}}
	r.CustomServices = map[string]string{"acme-importer.service": "/etc/systemd/system/acme-importer.service", "custom-api.service": "/etc/systemd/system/custom-api.service"}
	text := r.Text()
	for _, want := range []string{"服务：Sub-Store", "FRP 客户端（未运行）", "PostgreSQL", "acme-importer", "custom-api（运行失败）", "custom-worker（未运行）"} {
		if !strings.Contains(text, want) {
			t.Fatal(want, text)
		}
	}
	for _, invalid := range []string{"Redis", "证书", "运行中", "用途", "一次性任务", "状态未核验", "容器应用"} {
		if strings.Contains(text, invalid) {
			t.Fatal("unknown/one-shot/inactive software was misclassified", invalid, text)
		}
	}
	if text := (Report{}).Text(); !strings.Contains(text, "服务：暂未识别") || !strings.Contains(text, "域名：暂未发现") || strings.Contains(text, "没有服务") {
		t.Fatal("missing evidence was treated as absence", text)
	}
}

func TestSummaryDoesNotTreatEveryUnknownSystemServiceAsAnApplication(t *testing.T) {
	r := Parse("@@SERVICE_MANAGER\nsystemd\n@@SERVICE_FILES\n1panel.service enabled\nportal-api.service enabled\nvendor-helper.service enabled\ngssproxy.service enabled\niprdump.service enabled\niprinit.service enabled\niprupdate.service enabled\n@@SERVICE_UNITS\n1panel.service loaded active running panel\nportal-api.service loaded active running portal\nvendor-helper.service loaded active running helper\ngssproxy.service loaded active running authentication\niprdump.service loaded active running disks\niprinit.service loaded active running disks\niprupdate.service loaded active running disks\n@@NOTE_UNIT\tportal-api.service\t/etc/systemd/system/portal-api.service\n@@NOTE_UNIT\tvendor-helper.service\t/usr/lib/systemd/system/vendor-helper.service\n@@NOTE_UNIT\tgssproxy.service\t/etc/systemd/system/gssproxy.service\n@@NOTE_DONE\n", "192.0.2.10")
	text := r.Text()
	if !strings.Contains(text, "服务：1Panel、portal-api") {
		t.Fatal("packaged applications or custom units were lost", text)
	}
	for _, noise := range []string{"gssproxy", "iprdump", "iprinit", "iprupdate", "vendor-helper", "另 "} {
		if strings.Contains(text, noise) {
			t.Fatal("system components were counted as applications", noise, text)
		}
		if noise != "另 " && !strings.Contains(r.Details(), noise) {
			t.Fatal("system evidence was discarded rather than folded", noise)
		}
	}
	if !strings.Contains(r.Details(), "/etc/systemd/system/portal-api.service") {
		t.Fatal("custom-service identification lost its source")
	}
	for _, file := range []string{"/usr/lib/systemd/system/a.service", "/etc/systemd/system-extra/a.service", "/etc/systemd/system/../../vendor/a.service"} {
		bad := Parse("@@NOTE_UNIT\ta.service\t"+file+"\n@@NOTE_DONE\n", "192.0.2.10")
		if len(bad.CustomServices) != 0 {
			t.Fatal("non-custom path accepted as proof", file)
		}
	}
}

func TestLegacyNotesSummarizedLocallyAndEvidencePreserved(t *testing.T) {
	legacy := "系统：Debian GNU/Linux 12\n\n运行中的服务（systemd）：\napparmor.service、dbus.service、frps.service、substore.service、openresty.service\n\nDocker：未检测到\n\n监听地址（不代表公网已放行）：\nTCP *:9080\n\n域名（配置 / 连接地址发现，本机解析核验）：\n\nwww.example.test · 匹配已知地址 · 192.0.2.10\n  来源：/etc/nginx/site.conf\n\nexample.test · 未匹配已知地址（可能经 CDN / 代理） · 198.51.100.7\n  来源：/etc/nginx/site.conf"
	summary, details := SavedPresentation(legacy, "")
	if details != legacy || !strings.Contains(summary, "服务：Sub-Store、OpenResty、FRP 服务端") || !strings.Contains(summary, "\n域名：\nwww.example.test\nexample.test（解析地址不同）") {
		t.Fatal("legacy snapshot did not become useful without losing evidence", summary, details)
	}
	if strings.Contains(summary, "Sub-Store · 运行中") || strings.Contains(summary, "dbus.service") {
		t.Fatal("legacy active-only service invented running state or exposed system plumbing", summary)
	}
	if got, evidence := SavedPresentation(summary, details); got != summary || evidence != details {
		t.Fatal("current summary was interpreted as legacy evidence")
	}
	containerOnly := "系统：Debian\n\n服务：未读取到运行清单；不代表没有服务\n\n容器：sub-store · running · example/sub-store:latest"
	if got, _ := SavedPresentation(containerOnly, ""); !strings.Contains(got, "服务：Sub-Store") {
		t.Fatal("container-only legacy snapshot was left as raw inventory", got)
	}
	oldPurpose := "订阅管理：Sub-Store · 运行中\n网站 / 反向代理：OpenResty · 运行中\n内网穿透：FRP 服务端 · 运行中\n\n关联域名：www.example.test"
	if got, original := SavedPresentation(oldPurpose, legacy); !strings.HasPrefix(got, "服务：Sub-Store、OpenResty、FRP 服务端\n") || strings.Contains(got, "用途") || original != legacy {
		t.Fatal("previous purpose summaries were not simplified locally", got)
	}
}
