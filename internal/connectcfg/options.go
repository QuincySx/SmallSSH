// Package connectcfg defines saved connection options without credentials access.
package connectcfg

import (
	"encoding/hex"
	"errors"
	"net"
	"path/filepath"
	"strconv"
	"strings"

	"sshclient/internal/proxyconfig"
)

type Terminal struct {
	Encoding  string `json:"encoding,omitempty"`
	Backspace string `json:"backspace,omitempty"`
}

func (t Terminal) Validate() error {
	switch t.Encoding {
	case "", "auto", "utf-8", "gb18030", "gbk":
	default:
		return errors.New("字符编码需为自动、UTF-8、GB18030 或 GBK")
	}
	switch t.Backspace {
	case "", "auto", "del", "bs":
	default:
		return errors.New("退格序列需为自动、^? 或 ^H")
	}
	return nil
}
func (t Terminal) Erase() byte {
	if t.Backspace == "bs" {
		return 8
	}
	return 127
}

type Proxy struct {
	Mode     string `json:"mode,omitempty"`
	Address  string `json:"address,omitempty"`
	User     string `json:"user,omitempty"`
	Password string `json:"password,omitempty"`
	// Config is a restricted outbound JSON with credentials removed. Password
	// holds the encrypted credential JSON for embedded protocols only.
	Config string `json:"config,omitempty"`
}

func (p Proxy) Public() Proxy { p.Password = ""; return p }
func (p Proxy) Validate() error {
	if proxyconfig.Supported(p.Mode) {
		if p.User != "" {
			return errors.New("导入线路不使用独立的代理用户名")
		}
		_, err := proxyconfig.Outbound(p.Mode, p.Address, p.Config, p.Password, false)
		return err
	}
	if p.Config != "" {
		return errors.New("此代理类型不接受节点 JSON")
	}
	if len(p.Address) > 512 || len(p.User) > 255 || len(p.Password) > 255 || strings.ContainsAny(p.Address+p.User+p.Password, "\x00\r\n") {
		return errors.New("代理字段无效或超过上限")
	}
	if (p.Mode == "http" || p.Mode == "https") && strings.Contains(p.User, ":") {
		return errors.New("HTTP 代理用户名不能包含冒号")
	}
	switch p.Mode {
	case "", "none":
		return nil
	case "http", "https", "socks5":
	default:
		return errors.New("代理类型需为不使用代理、HTTP、HTTPS 或 SOCKS5")
	}
	if err := Address(p.Address, false); err != nil {
		return errors.New("代理地址需为主机:端口")
	}
	if len(p.User) > 255 || len(p.Password) > 255 || strings.ContainsAny(p.User+p.Password, "\x00\r\n") {
		return errors.New("代理认证字段无效或超过 255 字节")
	}
	if p.User == "" && p.Password != "" {
		return errors.New("代理密码需要用户名")
	}
	return nil
}

type Network struct {
	RouteID           string `json:"routeID,omitempty"`
	Proxy             Proxy  `json:"proxy,omitzero"`
	Relay             string `json:"relay,omitempty"`
	KeepaliveSeconds  int    `json:"keepaliveSeconds,omitempty"`
	KeepaliveFailures int    `json:"keepaliveFailures,omitempty"`
}

func (n Network) Public() Network { n.Proxy = n.Proxy.Public(); return n }
func (n Network) Validate() error {
	if len(n.RouteID) > 128 || strings.ContainsAny(n.RouteID, "\x00\r\n") {
		return errors.New("线路 ID 无效")
	}
	if n.RouteID != "" && (n.Relay != "" || n.Proxy != (Proxy{})) {
		return errors.New("已保存线路不能与连接专用代理或 SSH 中转并选")
	}
	if err := n.Proxy.Validate(); err != nil {
		return err
	}
	if n.Relay != "" && n.Proxy.Mode != "" && n.Proxy.Mode != "none" {
		return errors.New("同一跳不能同时使用外部代理和 SSH 中转")
	}
	if len(n.Relay) > 128 || strings.ContainsAny(n.Relay, "\x00\r\n") {
		return errors.New("中转连接 ID 无效")
	}
	if n.KeepaliveSeconds < 0 || n.KeepaliveSeconds > 3600 || n.KeepaliveSeconds > 0 && n.KeepaliveSeconds < 5 || n.KeepaliveFailures < 0 || n.KeepaliveFailures > 10 {
		return errors.New("保活间隔需为 0 或 5–3600 秒，失败上限为 1–10 次（0 使用 3 次）")
	}
	return nil
}

// MacSmartCard selects the OS-backed PIV interface instead of a PKCS#11 file.
const MacSmartCard = "macos-smartcard"

type Hardware struct {
	Module       string `json:"module,omitempty"`
	ModuleSHA256 string `json:"moduleSHA256,omitempty"`
	TokenSerial  string `json:"tokenSerial,omitempty"`
	KeyID        string `json:"keyId,omitempty"`
	Fingerprint  string `json:"fingerprint,omitempty"`
	PIN          string `json:"pin,omitempty"`
}

func (h Hardware) Public() Hardware { h.PIN = ""; return h }
func (h Hardware) Validate() error {
	hash, e := hex.DecodeString(h.ModuleSHA256)
	id, ie := hex.DecodeString(h.KeyID)
	driverOK := filepath.IsAbs(h.Module) && len(h.Module) <= 4096 && !strings.ContainsAny(h.Module, "\x00\r\n") && e == nil && len(hash) == 32
	if h.Module == MacSmartCard {
		driverOK = h.ModuleSHA256 == "" && h.PIN == "" && len(h.TokenSerial) == 32 && len(id) == 32
	}
	if !driverOK || len(h.TokenSerial) == 0 || len(h.TokenSerial) > 32 || ie != nil || len(id) == 0 || len(id) > 128 || !strings.HasPrefix(h.Fingerprint, "SHA256:") || len(h.PIN) > 1024 {
		return errors.New("硬件密钥需指定原生模块完整路径及 SHA256、令牌序列号、密钥 ID 和公钥指纹")
	}
	return nil
}

// Tunnel is immutable approval metadata. Dynamic forwards intentionally grant
// access to arbitrary destinations and therefore require a full broad grant.
type Tunnel struct {
	Kind   string `json:"kind,omitempty"`
	Listen string `json:"listen,omitempty"`
	Target string `json:"target,omitempty"`
}

func Address(value string, zero bool) error {
	host, port, e := net.SplitHostPort(value)
	n, pe := strconv.Atoi(port)
	if e != nil || pe != nil || n < 0 || n > 65535 || n == 0 && !zero || host == "" || len(host) > 255 || strings.ContainsAny(host, "\x00\r\n /\\") {
		return errors.New("无效 TCP 地址")
	}
	return nil
}
func (t Tunnel) Validate() error {
	if t.Kind != "local" && t.Kind != "remote" && t.Kind != "socks5" {
		return errors.New("隧道类型需为本地、远程或 SOCKS5")
	}
	if e := Address(t.Listen, true); e != nil {
		return e
	}
	host, _, _ := net.SplitHostPort(t.Listen)
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return errors.New("隧道仅可监听明确的回环 IP（127.0.0.1 或 ::1）")
	}
	if t.Kind == "socks5" {
		if t.Target != "" {
			return errors.New("动态隧道不能预设固定目标")
		}
		return nil
	}
	return Address(t.Target, false)
}
