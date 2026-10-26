package servernotes

import (
	"context"
	"errors"
	"net"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const fixture = "@@OS\nDebian GNU/Linux 13\n@@SERVICE_MANAGER\nsystemd\n@@SERVICE_FILES\nnginx.service enabled\nfrps.service enabled\n@@SERVICE_UNITS\nnginx.service loaded active running Web server\nfrps.service loaded active running Tunnel\n@@DOCKER_STATUS\nready\n@@DOCKER_CONTAINERS\n{\"ID\":\"fixture\",\"Names\":\"sub-store\",\"Image\":\"example/sub-store:latest\",\"State\":\"running\",\"Ports\":\"127.0.0.1:3001->3001/tcp\",\"Project\":\"subscriptions\"}\n@@NOTE_PROBE\n@@NOTE_PORT\ttcp\t0.0.0.0:443\n@@NOTE_PORT\ttcp\t0.0.0.0:443\n@@NOTE_DOMAIN\t/etc/nginx/conf.d/site.conf\tweb.example.test\n@@NOTE_DOMAIN\t/etc/nginx/conf.d/site.conf\t*.example.test\n@@NOTE_DOMAIN\t/etc/nginx/conf.d/site.conf\tcdn.example.test\n@@NOTE_DOMAIN\t/etc/nginx/conf.d/site.conf\tmissing.example.test\n@@NOTE_DOMAIN\t/etc/nginx/conf.d/site.conf\t$invalid\n@@NOTE_DONE\n"

func TestDiscoveryKeepsEvidenceAndDoesNotInventDNSOwnership(t *testing.T) {
	r := Parse(fixture, "192.0.2.10")
	if !r.Complete || len(r.Domains) != 4 || len(r.Ports) != 1 {
		t.Fatal("bad bounded discovery", r)
	}
	var calls atomic.Int32
	r.VerifyDNS(context.Background(), "192.0.2.10", func(_ context.Context, name string) ([]net.IPAddr, error) {
		calls.Add(1)
		switch name {
		case "web.example.test":
			return []net.IPAddr{{IP: net.ParseIP("192.0.2.10")}}, nil
		case "cdn.example.test":
			return []net.IPAddr{{IP: net.ParseIP("198.51.100.7")}}, nil
		default:
			return nil, errors.New("NXDOMAIN")
		}
	})
	if calls.Load() != 3 {
		t.Fatal("wildcard was queried as a concrete hostname")
	}
	text := r.Details()
	for _, v := range []string{"nginx.service", "frps.service", "sub-store", "subscriptions", "匹配已知地址", "可能经 CDN / 代理", "通配符配置，未核验", "解析未成功", "不代表公网已放行", "来源：/etc/nginx/conf.d/site.conf", "DNS 无法按 IP 列出所有域名"} {
		if !strings.Contains(text, v) {
			t.Fatal("missing evidence or limitation", v, text)
		}
	}
	if strings.Contains(text, "$invalid") {
		t.Fatal("non-domain config value escaped validation")
	}
}

func TestDiscoveryLimitsAndCancellation(t *testing.T) {
	text := fixture
	for i := 0; i < 100; i++ {
		text += "@@NOTE_DOMAIN\t/source\t" + strings.Repeat("x", i+1) + ".example.test\n"
	}
	r := Parse(text, "192.0.2.10")
	if len(r.Domains) != 32 {
		t.Fatal("DNS limit not enforced", len(r.Domains))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r.VerifyDNS(ctx, "192.0.2.10", func(context.Context, string) ([]net.IPAddr, error) {
		t.Fatal("cancelled scan queried DNS")
		return nil, nil
	})
	if len(r.Text()) > 16384 {
		t.Fatal("report exceeded encrypted snapshot limit")
	}
	if Parse(strings.TrimSuffix(fixture, "@@NOTE_DONE\n"), "").Complete {
		t.Fatal("truncated scan looked complete")
	}
}

func TestDiscoveryProbeIsValidFixedShell(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no shell parser on this platform")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-n")
	cmd.Stdin = strings.NewReader(Command)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	for _, unsafe := range []string{"nginx -T", "docker exec", ".env", "ssl_certificate_key", "/root/.ssh", "curl ", "wget "} {
		if strings.Contains(Command, unsafe) {
			t.Fatal("probe expanded beyond metadata", unsafe)
		}
	}
}

func TestConfigExtractorsEmitOnlyStaticSiteNames(t *testing.T) {
	if _, err := exec.LookPath("awk"); err != nil {
		t.Skip("no awk on this platform")
	}
	for _, sample := range []struct {
		script, body string
		names        []string
	}{
		{nginxDomains, "server {\n server_name example.test\n www.example.test *.example.test; # unrelated.test\n proxy_set_header Authorization private-secret;\n ssl_certificate_key /private/key.pem;\n}\n", []string{"example.test", "www.example.test", "*.example.test"}},
		{caddyDomains, "example.test, www.example.test {\n reverse_proxy 127.0.0.1:3000\n header {\n Authorization private-secret\n }\n}\n", []string{"example.test", "www.example.test"}},
	} {
		cmd := exec.Command("awk", "-v", "source=/fixture", sample.script)
		cmd.Stdin = strings.NewReader(sample.body)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatal(err, string(out))
		}
		r := Parse(string(out), "")
		if len(r.Domains) != len(sample.names) {
			t.Fatal("wrong site names", string(out), r.Domains)
		}
		for _, name := range sample.names {
			found := false
			for _, d := range r.Domains {
				if d.Name == name {
					found = true
				}
			}
			if !found {
				t.Fatal("missing name", name)
			}
		}
		for _, secret := range []string{"private-secret", "key.pem", "unrelated.test", "127.0.0.1:3000"} {
			if strings.Contains(string(out), secret) {
				t.Fatal("config extractor emitted unrelated or private data", secret)
			}
		}
	}
}

func TestUnitOriginExtractorOnlyEmitsSelectedMetadata(t *testing.T) {
	if _, err := exec.LookPath("awk"); err != nil {
		t.Skip("no awk on this platform")
	}
	cmd := exec.Command("awk", unitOrigins)
	cmd.Stdin = strings.NewReader("FragmentPath=/etc/systemd/system/custom-api.service\nId=custom-api.service\nEnvironment=secret-value\nExecStart=private-command\n\nId=vendor-helper.service\nFragmentPath=/usr/lib/systemd/system/vendor-helper.service\n")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatal(err, string(out))
	}
	if strings.Contains(string(out), "secret-value") || strings.Contains(string(out), "private-command") {
		t.Fatal("metadata extractor returned sensitive unit contents")
	}
	r := Parse(string(out)+"@@NOTE_DONE\n", "192.0.2.10")
	if len(r.CustomServices) != 1 || r.CustomServices["custom-api.service"] != "/etc/systemd/system/custom-api.service" {
		t.Fatal("metadata extractor lost the custom origin", string(out), r.CustomServices)
	}
}
