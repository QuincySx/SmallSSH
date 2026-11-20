//go:build with_utls && with_quic

// Package testproxy supplies generated, loopback-only protocol fixtures.
package testproxy

import (
	"context"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter/certificate"
	"github.com/sagernet/sing-box/adapter/endpoint"
	"github.com/sagernet/sing-box/adapter/inbound"
	"github.com/sagernet/sing-box/adapter/outbound"
	boxservice "github.com/sagernet/sing-box/adapter/service"
	"github.com/sagernet/sing-box/dns"
	"github.com/sagernet/sing-box/dns/transport/local"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/protocol/anytls"
	"github.com/sagernet/sing-box/protocol/direct"
	"github.com/sagernet/sing-box/protocol/hysteria2"
	"github.com/sagernet/sing-box/protocol/shadowsocks"
	"github.com/sagernet/sing-box/protocol/snell"
	"github.com/sagernet/sing-box/protocol/vless"
	sjson "github.com/sagernet/sing/common/json"

	"sshclient/internal/connectcfg"
	"sshclient/internal/proxyconfig"
)

const UUID = "5b1c2824-6f20-40c3-99f9-5b4a212d6a16"
const password = "synthetic-proxy-fixture-password"

func New(t testing.TB, kind string) connectcfg.Proxy {
	t.Helper()
	protocol := kind
	if kind == "reality" {
		protocol = "vless"
	} else if kind == "snell6" || kind == "snell6-unshaped" || kind == "snell6-unsafe-raw" {
		protocol = "snell"
	}
	var port int
	if protocol == "hysteria2" {
		l, err := net.ListenPacket("udp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port = l.LocalAddr().(*net.UDPAddr).Port
		_ = l.Close()
	} else {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port = l.Addr().(*net.TCPAddr).Port
		_ = l.Close()
	}
	server := map[string]any{"type": protocol, "tag": "fixture", "listen": "127.0.0.1", "listen_port": port}
	client := map[string]any{"type": protocol, "server": "127.0.0.1", "server_port": port}
	if protocol == "shadowsocks" {
		server["method"], server["password"] = "aes-128-gcm", password
		client["method"], client["password"] = "aes-128-gcm", password
	} else if protocol == "snell" {
		server["version"], server["psk"] = 5, password
		client["version"], client["psk"] = 5, password
		if kind != "snell" {
			mode := "default"
			if kind != "snell6" {
				mode = strings.TrimPrefix(kind, "snell6-")
			}
			server["version"], client["version"] = 6, 6
			server["mode"], client["mode"] = mode, mode
			client["reuse"] = true
		}
	} else {
		if protocol == "vless" {
			server["users"] = []map[string]string{{"name": "fixture", "uuid": UUID}}
			client["uuid"] = UUID
		} else {
			server["users"] = []map[string]string{{"name": "fixture", "password": password}}
			client["password"] = password
		}
		if kind == "reality" {
			camouflage := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))
			camouflage.EnableHTTP2 = true
			camouflage.Config.ErrorLog = log.New(io.Discard, "", 0)
			camouflage.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
			camouflage.StartTLS()
			t.Cleanup(camouflage.Close)
			_, camouflagePort, err := net.SplitHostPort(camouflage.Listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			n, _ := strconv.Atoi(camouflagePort)
			key, err := ecdh.X25519().GenerateKey(rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			server["tls"] = map[string]any{"enabled": true, "server_name": "proxy.fixture", "reality": map[string]any{
				"enabled": true, "private_key": base64.RawURLEncoding.EncodeToString(key.Bytes()), "short_id": []string{"0123456789abcdef"},
				"handshake": map[string]any{"server": "127.0.0.1", "server_port": n},
			}}
			client["flow"] = "xtls-rprx-vision"
			server["users"] = []map[string]string{{"name": "fixture", "uuid": UUID, "flow": "xtls-rprx-vision"}}
			client["tls"] = map[string]any{"enabled": true, "server_name": "proxy.fixture", "utls": map[string]any{"enabled": true, "fingerprint": "chrome"},
				"reality": map[string]any{"enabled": true, "public_key": base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()), "short_id": "0123456789abcdef"}}
		} else {
			public, key, err := ed25519.GenerateKey(rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			certificate := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "proxy.fixture"}, DNSNames: []string{"proxy.fixture"},
				NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
			der, err := x509.CreateCertificate(rand.Reader, certificate, certificate, public, key)
			if err != nil {
				t.Fatal(err)
			}
			pkcs8, err := x509.MarshalPKCS8PrivateKey(key)
			if err != nil {
				t.Fatal(err)
			}
			cert, err := x509.ParseCertificate(der)
			if err != nil {
				t.Fatal(err)
			}
			pin := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
			server["tls"] = map[string]any{"enabled": true, "certificate": string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), "key": string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8}))}
			client["tls"] = map[string]any{"enabled": true, "server_name": "proxy.fixture", "insecure": true, "certificate_public_key_sha256": []string{base64.StdEncoding.EncodeToString(pin[:])}}
		}
	}
	in := inbound.NewRegistry()
	shadowsocks.RegisterInbound(in)
	snell.RegisterInbound(in)
	vless.RegisterInbound(in)
	anytls.RegisterInbound(in)
	hysteria2.RegisterInbound(in)
	out := outbound.NewRegistry()
	direct.RegisterOutbound(out)
	resolvers := dns.NewTransportRegistry()
	local.RegisterTransport(resolvers)
	ctx := box.Context(context.Background(), in, out, endpoint.NewRegistry(), resolvers, boxservice.NewRegistry(), certificate.NewRegistry())
	raw, _ := json.Marshal(map[string]any{"log": map[string]any{"disabled": true}, "inbounds": []any{server}, "outbounds": []any{map[string]any{"type": "direct", "tag": "direct"}}})
	var options option.Options
	if err := sjson.UnmarshalContext(ctx, raw, &options); err != nil {
		t.Fatal("synthetic proxy server config", err)
	}
	instance, err := box.New(box.Options{Context: ctx, Options: options})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = instance.Close() })
	if err := instance.Start(); err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(client)
	nodes, err := proxyconfig.Import(string(raw))
	if err != nil || len(nodes) != 1 {
		t.Fatal("synthetic proxy client config", err)
	}
	node := nodes[0]
	return connectcfg.Proxy{Mode: node.Kind, Address: node.Address, Config: node.Config, Password: node.Secret}
}
