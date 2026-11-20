package proxyconfig

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

const fixtureUUID = "5b1c2824-6f20-40c3-99f9-5b4a212d6a16"

func TestImportSnellV6CommonJSONForms(t *testing.T) {
	const node = `{"type":"snell","tag":"self-hk-tencent","server":"00.00.00.00","server_port":76,"version":6,"reuse":true,"psk":"xxxx","mode":"default"}`
	for name, input := range map[string]string{
		"single object": node,
		"node array":    "[" + node + "]",
		"full config":   `{"outbounds":[` + node + `]}`,
	} {
		t.Run(name, func(t *testing.T) {
			nodes, err := Import(input)
			if err != nil || len(nodes) != 1 {
				t.Fatal("valid Snell v6 JSON rejected", err)
			}
			n := nodes[0]
			if n.Name != "self-hk-tencent" || n.Kind != "snell" || n.Address != "00.00.00.00:76" || strings.Contains(n.Config, "xxxx") {
				t.Fatal("node metadata or credential separation changed")
			}
			raw, err := Outbound(n.Kind, n.Address, n.Config, n.Secret, true)
			if err != nil {
				t.Fatal("cannot reconstruct imported outbound", err)
			}
			var fields map[string]any
			if err := json.Unmarshal(raw, &fields); err != nil {
				t.Fatal(err)
			}
			if fields["version"] != float64(6) || fields["mode"] != "default" || fields["reuse"] != true || fields["psk"] != "xxxx" {
				t.Fatal("v6 parameters lost during normalization")
			}
		})
	}
}

func TestSnellV6ModesAndValidationErrors(t *testing.T) {
	const node = `{"type":"snell","server":"proxy.invalid","server_port":443,"version":6,"psk":"synthetic-snell-v6-key","mode":"default"}`
	for _, mode := range []string{"", "default", "unshaped", "unsafe-raw"} {
		nodes, err := Import(strings.Replace(node, `"mode":"default"`, `"mode":"`+mode+`"`, 1))
		if err != nil {
			t.Fatal("supported Snell v6 mode rejected", mode, err)
		}
		raw, err := Outbound(nodes[0].Kind, nodes[0].Address, nodes[0].Config, nodes[0].Secret, true)
		if err != nil {
			t.Fatal("v6 reconstruction", err)
		}
		var config Config
		if err := json.Unmarshal(raw, &config); err != nil || config.Version != 6 || config.Mode != mode {
			t.Fatal("v6 mode or version changed", err)
		}
	}
	for _, tt := range []struct{ input, message string }{
		{strings.Replace(node, `"version":6`, `"version":7`, 1), "Snell 版本"},
		{strings.Replace(node, `"version":6`, `"version":4`, 1), "mode 仅适用于 Snell v6"},
		{strings.Replace(node, `"mode":"default"`, `"mode":"unknown"`, 1), "Snell v6 的 mode"},
		{strings.Replace(node, `"psk":"synthetic-snell-v6-key"`, `"psk":""`, 1), "Snell 需要 PSK"},
		{strings.Replace(node, `"server_port":443`, `"server_port":"443"`, 1), "节点 JSON"},
		{strings.Replace(node, `"type":"snell"`, `"type":"unknown"`, 1), "不支持此节点协议"},
		{strings.TrimSuffix(node, "}") + `,"detour":"not-allowed"}`, "不支持"},
	} {
		if _, err := Import(tt.input); err == nil || !strings.Contains(err.Error(), tt.message) || strings.Contains(err.Error(), "synthetic-snell-v6-key") {
			t.Fatal("missing specific validation or exposed credential", err)
		}
	}
}

func TestImportLinksSeparatesCredentialsAndReconstructsNodes(t *testing.T) {
	for _, tt := range []struct {
		input, kind, secret string
	}{
		{"ss://" + base64.RawURLEncoding.EncodeToString([]byte("aes-128-gcm:synthetic-ss-password")) + "@proxy.invalid:443#Example", "shadowsocks", "synthetic-ss-password"},
		{"ss://" + base64.StdEncoding.EncodeToString([]byte("aes-256-gcm:synthetic-legacy-password@proxy.invalid:8388")), "shadowsocks", "synthetic-legacy-password"},
		{"ss://2022-blake3-aes-128-gcm:MTIzNDU2Nzg5MGFiY2RlZg%3D%3D@[::1]:443", "shadowsocks", "MTIzNDU2Nzg5MGFiY2RlZg=="},
		{"vless://" + fixtureUUID + "@proxy.invalid:443?security=reality&sni=proxy.invalid&pbk=" + base64.RawURLEncoding.EncodeToString(make([]byte, 32)) + "&sid=ab12&flow=xtls-rprx-vision", "vless", fixtureUUID},
		{"vless://" + fixtureUUID + "@proxy.invalid:443?security=tls&type=ws&path=%2Fssh&host=proxy.invalid", "vless", fixtureUUID},
		{"anytls://synthetic%3Aanytls%40password@proxy.invalid:443?sni=proxy.invalid", "anytls", "synthetic:anytls@password"},
		{"hy2://synthetic-hy2-password@proxy.invalid:443?obfs=salamander&obfs-password=synthetic-obfs-password", "hysteria2", "synthetic-hy2-password"},
		{"hysteria2://synthetic-user:synthetic-auth@proxy.invalid:443/", "hysteria2", "synthetic-user:synthetic-auth"},
	} {
		t.Run(tt.kind+tt.secret, func(t *testing.T) {
			nodes, err := Import(tt.input)
			if err != nil || len(nodes) != 1 {
				t.Fatal("valid synthetic link rejected", err)
			}
			n := nodes[0]
			public, _ := json.Marshal(n)
			if n.Kind != tt.kind || strings.Contains(string(public), tt.secret) || strings.Contains(string(public), "synthetic-obfs-password") {
				t.Fatal("secret escaped public node metadata")
			}
			raw, err := Outbound(n.Kind, n.Address, n.Config, n.Secret, true)
			if err != nil || !strings.Contains(string(raw), tt.secret) {
				t.Fatal("credential reconstruction failed", err)
			}
			if _, err := Outbound(n.Kind, n.Address, n.Config, "", false); err != nil {
				t.Fatal("redacted edit draft rejected", err)
			}
			if _, err := Outbound(n.Kind, n.Address, n.Config, "", true); err == nil {
				t.Fatal("redacted draft accepted for dialing")
			}
		})
	}
}

func TestImportFullConfigExtractsOnlyNodesAndPreservesSnellCompatibility(t *testing.T) {
	nodes, err := Import(`{"dns":{"servers":[{"type":"https","server":"untrusted.invalid"}]},"inbounds":[{"type":"tun"}],"experimental":{"debug":{"listen":"0.0.0.0:6060"}},"outbounds":[{"type":"direct"},{"type":"snell","tag":"Synthetic Snell","server":"proxy.invalid","server_port":443,"version":5,"psk":"synthetic-snell-psk","userkey":"synthetic-userkey"},{"type":"anytls","server":"proxy.invalid","server_port":443,"password":"synthetic-anytls","tls":{"enabled":true}}]}`)
	if err != nil || len(nodes) != 2 {
		t.Fatal("supported nodes not extracted", err)
	}
	if !strings.Contains(nodes[0].Config, `"version":4`) {
		t.Fatal("Snell v5 did not map to v4 TCP-compatible implementation")
	}
	for _, n := range nodes {
		if strings.Contains(n.Config, "inbounds") || strings.Contains(n.Config, "untrusted.invalid") || strings.Contains(n.Config, "experimental") || strings.Contains(n.Config, "synthetic-") {
			t.Fatal("global configuration or credentials imported into public data")
		}
	}
}

func TestImportRejectsExecutableAndUnsafeConfigurationWithoutEchoingSecrets(t *testing.T) {
	for _, input := range []string{
		`{"type":"shadowsocks","server":"proxy.invalid","server_port":443,"method":"aes-128-gcm","password":"DO-NOT-ECHO","plugin":"external-program"}`,
		`{"type":"anytls","server":"proxy.invalid","server_port":443,"password":"DO-NOT-ECHO","tls":{"enabled":true,"certificate_path":"/private/file"}}`,
		`{"type":"anytls","server":"proxy.invalid","server_port":443,"password":"DO-NOT-ECHO","detour":"somewhere","tls":{"enabled":true}}`,
		`{"type":"anytls","server":"proxy.invalid","server_port":443,"password":"DO-NOT-ECHO","tls":{"enabled":true,"insecure":true}}`,
		`{"type":"anytls","server":"proxy.invalid","server_port":443,"password":"DO-NOT-ECHO","tls":{"enabled":false}}`,
		`{"type":"vless","server":"proxy.invalid","server_port":443,"uuid":"DO-NOT-ECHO","tls":{"enabled":true}}`,
		`{"type":"snell","server":"proxy.invalid","server_port":443,"psk":"DO-NOT-ECHO","version":3}`,
		`{"type":"shadowsocks","server":"proxy.invalid:22","server_port":443,"password":"DO-NOT-ECHO","method":"aes-128-gcm"}`,
		`anytls://DO-NOT-ECHO@proxy.invalid:443?insecure=1`,
		`hy2://DO-NOT-ECHO@proxy.invalid:443?unknown=value`,
		`hy2://DO-NOT-ECHO@proxy.invalid:443?sni=a&sni=b`,
		`ssr://DO-NOT-ECHO`, `https://subscription.invalid/DO-NOT-ECHO`,
		strings.Repeat("DO-NOT-ECHO", MaxInput),
	} {
		if _, err := Import(input); err == nil || strings.Contains(err.Error(), "DO-NOT-ECHO") {
			t.Fatal("unsafe input accepted or error exposed credential")
		}
	}
}

func TestStoredPublicConfigCannotSmuggleSecretsOrSwitchIdentity(t *testing.T) {
	nodes, err := Import(`anytls://synthetic-password@proxy.invalid:443`)
	if err != nil {
		t.Fatal(err)
	}
	n := nodes[0]
	for _, tt := range []struct{ kind, address, config, secret string }{
		{"vless", n.Address, n.Config, n.Secret},
		{n.Kind, "different.invalid:443", n.Config, n.Secret},
		{n.Kind, n.Address, strings.Replace(n.Config, `"type":`, `"password":"smuggled","type":`, 1), n.Secret},
		{n.Kind, n.Address, n.Config, `{"uuid":"` + fixtureUUID + `"}`},
	} {
		if _, err := Outbound(tt.kind, tt.address, tt.config, tt.secret, true); err == nil {
			t.Fatal("stored node identity or credential separation bypassed")
		}
	}
}
