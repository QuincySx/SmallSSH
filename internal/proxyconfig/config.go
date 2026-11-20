// Package proxyconfig imports a restricted set of outbound nodes. It has no
// network, filesystem, listener or sing-box runtime dependencies.
package proxyconfig

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
)

const MaxInput = 64 << 10

type Node struct {
	Name, Kind, Address, Config string
	Secret                      string `json:"-"`
}

// Config contains only the fields this SSH client supports. In particular it
// cannot describe listeners, detours, plugins, local files or control services.
type Config struct {
	Type       string     `json:"type"`
	Tag        string     `json:"tag,omitempty"`
	Server     string     `json:"server"`
	ServerPort uint16     `json:"server_port"`
	Method     string     `json:"method,omitempty"`
	Version    int        `json:"version,omitempty"`
	Mode       string     `json:"mode,omitempty"`
	Password   string     `json:"password,omitempty"`
	UUID       string     `json:"uuid,omitempty"`
	PSK        string     `json:"psk,omitempty"`
	UserKey    string     `json:"userkey,omitempty"`
	Reuse      bool       `json:"reuse,omitempty"`
	Flow       string     `json:"flow,omitempty"`
	Network    string     `json:"network,omitempty"`
	TLS        *TLS       `json:"tls,omitempty"`
	Transport  *Transport `json:"transport,omitempty"`
	Obfs       *Obfs      `json:"obfs,omitempty"`
	UpMbps     int        `json:"up_mbps,omitempty"`
	DownMbps   int        `json:"down_mbps,omitempty"`
}

type TLS struct {
	Enabled    bool     `json:"enabled"`
	ServerName string   `json:"server_name,omitempty"`
	ALPN       []string `json:"alpn,omitempty"`
	Insecure   bool     `json:"insecure,omitempty"`
	Pins       []string `json:"certificate_public_key_sha256,omitempty"`
	UTLS       *UTLS    `json:"utls,omitempty"`
	Reality    *Reality `json:"reality,omitempty"`
}

type UTLS struct {
	Enabled     bool   `json:"enabled"`
	Fingerprint string `json:"fingerprint,omitempty"`
}

type Reality struct {
	Enabled   bool   `json:"enabled"`
	PublicKey string `json:"public_key"`
	ShortID   string `json:"short_id,omitempty"`
}

type Transport struct {
	Type        string            `json:"type"`
	Path        string            `json:"path,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	ServiceName string            `json:"service_name,omitempty"`
}

type Obfs struct {
	Type     string `json:"type"`
	Password string `json:"password,omitempty"`
}

type credentials struct {
	Password     string `json:"password,omitempty"`
	UUID         string `json:"uuid,omitempty"`
	PSK          string `json:"psk,omitempty"`
	UserKey      string `json:"userkey,omitempty"`
	ObfsPassword string `json:"obfs_password,omitempty"`
}

func Supported(kind string) bool {
	switch kind {
	case "shadowsocks", "snell", "vless", "anytls", "hysteria2":
		return true
	}
	return false
}

func Label(kind string) string {
	switch kind {
	case "shadowsocks":
		return "Shadowsocks"
	case "snell":
		return "Snell"
	case "vless":
		return "VLESS"
	case "anytls":
		return "AnyTLS"
	case "hysteria2":
		return "Hysteria 2"
	}
	return strings.ToUpper(kind)
}

func strict(raw []byte, value any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(value) != nil {
		return errors.New("节点 JSON 格式不正确或含不支持的字段")
	}
	if d.Decode(new(any)) != io.EOF {
		return errors.New("请提供一份完整的节点 JSON")
	}
	return nil
}

func decode(raw []byte) (Config, error) {
	var c Config
	if len(raw) > MaxInput {
		return c, errors.New("节点 JSON 最多 64 KiB")
	}
	if err := strict(raw, &c); err != nil {
		return c, err
	}
	if !Supported(c.Type) {
		return c, errors.New("不支持此节点协议；仅支持 Shadowsocks、Snell、VLESS、AnyTLS 和 Hysteria 2")
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return c, errors.New("节点需为 JSON 对象")
	}
	allowed := " type tag server server_port "
	switch c.Type {
	case "shadowsocks":
		allowed += "method password network "
	case "snell":
		allowed += "version psk userkey reuse network mode "
	case "vless":
		allowed += "uuid flow network tls transport "
	case "anytls":
		allowed += "password tls "
	case "hysteria2":
		allowed += "password tls network obfs up_mbps down_mbps "
	}
	for key := range fields {
		if !strings.Contains(allowed, " "+key+" ") {
			return c, errors.New("节点包含该协议不支持的字段；本应用只导入 SSH 出站参数")
		}
	}
	return c, nil
}

func validate(c *Config, complete bool) error {
	if !Supported(c.Type) || c.Server == "" || len(c.Server) > 255 || strings.ContainsAny(c.Server, "\x00\r\n /\\") || c.ServerPort == 0 {
		return errors.New("代理服务器地址和端口无效")
	}
	if strings.Contains(c.Server, ":") && net.ParseIP(c.Server) == nil {
		return errors.New("代理服务器应单独填写主机，端口使用 server_port")
	}
	if c.Network != "" && c.Network != "tcp" {
		return errors.New("线路仅承载 SSH 的 TCP 连接")
	}
	if c.Type != "anytls" {
		c.Network = "tcp"
	}
	for _, value := range []string{c.Password, c.UUID, c.PSK, c.UserKey} {
		if len(value) > 2048 || strings.ContainsAny(value, "\x00\r\n") {
			return errors.New("代理凭据无效或超过上限")
		}
	}
	switch c.Type {
	case "shadowsocks":
		switch c.Method {
		case "aes-128-gcm", "aes-192-gcm", "aes-256-gcm", "chacha20-ietf-poly1305", "xchacha20-ietf-poly1305", "2022-blake3-aes-128-gcm", "2022-blake3-aes-256-gcm", "2022-blake3-chacha20-poly1305":
		default:
			return errors.New("Shadowsocks 仅支持 AEAD 和 AEAD-2022 加密方式")
		}
		if complete && c.Password == "" {
			return errors.New("Shadowsocks 需要密码或密钥")
		}
	case "snell":
		if c.Version == 5 {
			c.Version = 4 // The v5 TCP wire format is v4-compatible; no v5 QUIC.
		}
		if c.Version != 4 && c.Version != 6 {
			return errors.New("Snell 版本仅支持 v4 / v5 的 TCP 兼容模式及 v6")
		}
		if c.Version != 6 && c.Mode != "" {
			return errors.New("mode 仅适用于 Snell v6")
		}
		if c.Version == 6 {
			switch c.Mode {
			case "", "default", "unshaped", "unsafe-raw":
			default:
				return errors.New("Snell v6 的 mode 仅支持 default、unshaped、unsafe-raw")
			}
		}
		if complete && c.PSK == "" {
			return errors.New("Snell 需要 PSK")
		}
	case "vless":
		if complete && !validUUID(c.UUID) {
			return errors.New("VLESS UUID 格式无效")
		}
		if c.Flow != "" && c.Flow != "xtls-rprx-vision" {
			return errors.New("不支持此 VLESS flow")
		}
	case "anytls", "hysteria2":
		if complete && c.Password == "" {
			return errors.New("代理节点需要认证凭据")
		}
	}
	if c.Type == "vless" || c.Type == "anytls" || c.Type == "hysteria2" {
		if c.TLS == nil || !c.TLS.Enabled {
			return errors.New("此代理节点需要启用 TLS")
		}
	}
	if c.TLS != nil {
		t := c.TLS
		if len(t.ServerName) > 255 || strings.ContainsAny(t.ServerName, "\x00\r\n /\\") || len(t.ALPN) > 8 || len(t.Pins) > 4 {
			return errors.New("TLS 参数无效或超过上限")
		}
		for _, a := range t.ALPN {
			if a == "" || len(a) > 255 || strings.ContainsAny(a, "\x00\r\n") {
				return errors.New("TLS ALPN 无效")
			}
		}
		for _, p := range t.Pins {
			b, err := base64.StdEncoding.DecodeString(p)
			if err != nil || len(b) != 32 {
				return errors.New("TLS 公钥指纹需为 Base64 编码的 SHA256")
			}
		}
		if t.Insecure && len(t.Pins) == 0 {
			return errors.New("不能关闭 TLS 身份验证；自签名节点请提供公钥 SHA256 指纹")
		}
		if t.UTLS != nil {
			if c.Type == "hysteria2" {
				return errors.New("Hysteria 2 不支持 uTLS 参数")
			}
			switch t.UTLS.Fingerprint {
			case "", "chrome", "firefox", "safari", "ios", "edge", "random", "randomized":
			default:
				return errors.New("不支持此 TLS 指纹类型")
			}
		}
		if r := t.Reality; r != nil {
			b, err := base64.RawURLEncoding.DecodeString(r.PublicKey)
			sid, sidErr := hex.DecodeString(r.ShortID)
			if c.Type != "vless" || !r.Enabled || err != nil || len(b) != 32 || sidErr != nil || len(sid) > 8 || t.UTLS == nil || !t.UTLS.Enabled {
				return errors.New("REALITY 需要有效公钥、short ID 和启用的 uTLS，仅支持 VLESS")
			}
		}
	}
	if tr := c.Transport; tr != nil {
		if c.Type != "vless" || c.Flow != "" || c.TLS.Reality != nil || len(tr.Path) > 2048 || len(tr.ServiceName) > 255 || strings.ContainsAny(tr.Path+tr.ServiceName, "\x00\r\n") {
			return errors.New("传输参数无效；REALITY / Vision 使用原生 TCP")
		}
		switch tr.Type {
		case "ws", "httpupgrade":
			if tr.ServiceName != "" {
				return errors.New("此传输不使用 gRPC 服务名")
			}
		case "grpc":
			if tr.Path != "" || len(tr.Headers) != 0 {
				return errors.New("gRPC 传输不使用路径或 HTTP 头")
			}
		default:
			return errors.New("仅支持 WebSocket、HTTPUpgrade 和 gRPC 传输")
		}
		for k, v := range tr.Headers {
			if !strings.EqualFold(k, "Host") || len(v) > 255 || strings.ContainsAny(v, "\x00\r\n") {
				return errors.New("传输仅允许 Host 头")
			}
		}
	}
	if c.UpMbps < 0 || c.DownMbps < 0 || c.UpMbps > 100000 || c.DownMbps > 100000 {
		return errors.New("Hysteria 2 带宽参数超出范围")
	}
	if c.Obfs != nil && (c.Obfs.Type != "salamander" || len(c.Obfs.Password) > 2048 || strings.ContainsAny(c.Obfs.Password, "\x00\r\n") || complete && c.Obfs.Password == "") {
		return errors.New("Hysteria 2 仅支持带密码的 Salamander 参数")
	}
	return nil
}

func validUUID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	_, err := hex.DecodeString(strings.ReplaceAll(value, "-", ""))
	return err == nil
}

func normalize(c Config) (Node, error) {
	if err := validate(&c, true); err != nil {
		return Node{}, err
	}
	n := Node{Name: strings.TrimSpace(c.Tag), Kind: c.Type, Address: net.JoinHostPort(c.Server, strconv.Itoa(int(c.ServerPort)))}
	if n.Name == "" {
		n.Name = Label(c.Type) + " · " + c.Server
	}
	if len(n.Name) > 128 || strings.ContainsAny(n.Name, "\x00\r\n") {
		return Node{}, errors.New("节点名称无效或超过上限")
	}
	secret := credentials{Password: c.Password, UUID: c.UUID, PSK: c.PSK, UserKey: c.UserKey}
	if c.Obfs != nil {
		secret.ObfsPassword, c.Obfs.Password = c.Obfs.Password, ""
	}
	c.Tag, c.Password, c.UUID, c.PSK, c.UserKey = "", "", "", "", ""
	public, _ := json.Marshal(c)
	private, _ := json.Marshal(secret)
	n.Config, n.Secret = string(public), string(private)
	return n, nil
}

// Outbound reconstructs one node only after checking both stored halves. A
// blank secret is permitted for public edit drafts, never for actual dialing.
func Outbound(kind, address, config, secret string, complete bool) ([]byte, error) {
	c, err := decode([]byte(config))
	if err != nil || c.Type != kind || net.JoinHostPort(c.Server, strconv.Itoa(int(c.ServerPort))) != address || c.Password != "" || c.UUID != "" || c.PSK != "" || c.UserKey != "" || c.Obfs != nil && c.Obfs.Password != "" || len(secret) > 12<<10 {
		return nil, errors.New("线路公开参数与类型或地址不一致，或包含了凭据")
	}
	var credentials credentials
	if secret != "" {
		if err := strict([]byte(secret), &credentials); err != nil {
			return nil, errors.New("线路凭据格式无效")
		}
	}
	if credentials.UUID != "" && kind != "vless" || (credentials.PSK != "" || credentials.UserKey != "") && kind != "snell" || credentials.Password != "" && kind != "shadowsocks" && kind != "anytls" && kind != "hysteria2" || credentials.ObfsPassword != "" && (kind != "hysteria2" || c.Obfs == nil) {
		return nil, errors.New("线路凭据与协议不匹配")
	}
	c.Password, c.UUID, c.PSK, c.UserKey = credentials.Password, credentials.UUID, credentials.PSK, credentials.UserKey
	if c.Obfs != nil {
		c.Obfs.Password = credentials.ObfsPassword
	}
	if err := validate(&c, complete || secret != ""); err != nil {
		return nil, err
	}
	return json.Marshal(c)
}
