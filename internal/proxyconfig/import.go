package proxyconfig

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
)

// Import never follows a URL. A complete sing-box JSON document contributes
// only supported outbounds; none of its global options are returned or run.
func Import(input string) ([]Node, error) {
	input = strings.TrimSpace(input)
	if input == "" || len(input) > MaxInput {
		return nil, errors.New("请粘贴分享链接或节点 JSON，最多 64 KiB")
	}
	if input[0] != '{' && input[0] != '[' {
		c, err := parseURL(input)
		if err != nil {
			return nil, err
		}
		n, err := normalize(c)
		if err != nil {
			return nil, err
		}
		return []Node{n}, nil
	}
	var records []json.RawMessage
	collection := input[0] == '['
	if collection {
		if json.Unmarshal([]byte(input), &records) != nil {
			return nil, errors.New("节点 JSON 列表格式无效")
		}
	} else {
		var root map[string]json.RawMessage
		if json.Unmarshal([]byte(input), &root) != nil || root == nil {
			return nil, errors.New("节点 JSON 格式无效")
		}
		if outbounds, ok := root["outbounds"]; ok {
			collection = true
			if json.Unmarshal(outbounds, &records) != nil {
				return nil, errors.New("outbounds 需为节点列表")
			}
		} else {
			records = []json.RawMessage{json.RawMessage(input)}
		}
	}
	if len(records) == 0 || len(records) > 128 {
		return nil, errors.New("一次最多导入 128 个节点")
	}
	var nodes []Node
	for _, raw := range records {
		var kind struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(raw, &kind) != nil {
			return nil, errors.New("节点 JSON 格式无效")
		}
		if collection && !Supported(kind.Type) {
			continue
		}
		c, err := decode(raw)
		if err != nil {
			return nil, err
		}
		n, err := normalize(c)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, n)
	}
	if len(nodes) == 0 {
		return nil, errors.New("未找到支持的出站节点；SSR、订阅地址及规则节点不在导入范围内")
	}
	return nodes, nil
}

func parseURL(input string) (Config, error) {
	var c Config
	if strings.ContainsAny(input, "\x00\r\n\t ") {
		return c, errors.New("请一次粘贴一条完整分享链接")
	}
	u, err := url.Parse(input)
	if err != nil {
		return c, errors.New("分享链接格式无效")
	}
	if u.Scheme == "ss" && u.User == nil {
		// Legacy SS encodes the whole authority rather than only userinfo.
		encoded := u.Host + u.Path
		if u.Opaque != "" {
			encoded = u.Opaque
		}
		decoded, ok := decodeBase64(encoded)
		if !ok {
			return c, errors.New("Shadowsocks 链接编码无效")
		}
		parsed, err := url.Parse("ss://" + decoded)
		if err != nil || parsed.User == nil {
			return c, errors.New("Shadowsocks 链接格式无效")
		}
		parsed.Fragment, parsed.RawQuery = u.Fragment, u.RawQuery
		u = parsed
	}
	c.Server, c.Tag = u.Hostname(), u.Fragment
	port := u.Port()
	if port == "" {
		port = "443"
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 || u.User == nil {
		return c, errors.New("分享链接需要有效服务器、端口和认证参数")
	}
	c.ServerPort = uint16(n)
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return c, errors.New("分享链接参数编码无效")
	}
	allowed := ""
	switch strings.ToLower(u.Scheme) {
	case "ss":
		c.Type, allowed = "shadowsocks", " plugin "
		if q.Get("plugin") != "" {
			return c, errors.New("不支持运行 Shadowsocks 外部插件")
		}
		c.Method, c.Password = u.User.Username(), ""
		if password, ok := u.User.Password(); ok {
			c.Password = password
		} else if decoded, ok := decodeBase64(c.Method); ok {
			c.Method, c.Password, _ = strings.Cut(decoded, ":")
		}
	case "vless":
		if _, hasPassword := u.User.Password(); hasPassword {
			return c, errors.New("VLESS 分享链接只接受 UUID 认证")
		}
		c.Type, c.UUID = "vless", u.User.Username()
		allowed = " encryption security sni peer fp alpn pbk sid flow type path host serviceName insecure allowInsecure spx "
		if q.Get("encryption") != "" && q.Get("encryption") != "none" {
			return c, errors.New("不支持此 VLESS encryption 参数")
		}
		security := q.Get("security")
		if security != "tls" && security != "reality" {
			return c, errors.New("VLESS 链接需要 TLS 或 REALITY")
		}
		c.TLS = tlsFromURL(q)
		c.Flow = q.Get("flow")
		if security == "reality" {
			fingerprint := q.Get("fp")
			if fingerprint == "" {
				fingerprint = "chrome"
			}
			c.TLS.UTLS = &UTLS{Enabled: true, Fingerprint: fingerprint}
			c.TLS.Reality = &Reality{Enabled: true, PublicKey: q.Get("pbk"), ShortID: q.Get("sid")}
		}
		transport := q.Get("type")
		if transport != "" && transport != "tcp" && transport != "raw" {
			c.Transport = &Transport{Type: transport, Path: q.Get("path"), ServiceName: q.Get("serviceName")}
			if host := q.Get("host"); host != "" {
				c.Transport.Headers = map[string]string{"Host": host}
			}
		}
	case "anytls", "hy2", "hysteria2":
		c.Type = strings.ToLower(u.Scheme)
		if c.Type == "hy2" {
			c.Type = "hysteria2"
		}
		c.Password = u.User.Username()
		if password, ok := u.User.Password(); ok {
			c.Password += ":" + password
		}
		allowed = " sni peer alpn insecure allowInsecure pinSHA256 "
		c.TLS = tlsFromURL(q)
		if c.Type == "hysteria2" {
			allowed += "obfs obfs-password upmbps downmbps "
			if name := q.Get("obfs"); name != "" {
				c.Obfs = &Obfs{Type: name, Password: q.Get("obfs-password")}
			}
			for key, dest := range map[string]*int{"upmbps": &c.UpMbps, "downmbps": &c.DownMbps} {
				if value := q.Get(key); value != "" {
					n, err := strconv.Atoi(value)
					if err != nil {
						return c, errors.New("Hysteria 2 带宽参数无效")
					}
					*dest = n
				}
			}
		}
	default:
		return c, errors.New("支持 ss://、vless://、anytls://、hy2:// 和 hysteria2://；Snell 请导入节点 JSON，ssr:// 不属于 Shadowsocks")
	}
	if u.Path != "" && u.Path != "/" {
		return c, errors.New("分享链接路径无效；传输路径应位于参数中")
	}
	for key, values := range q {
		if len(values) != 1 || !strings.Contains(allowed, " "+key+" ") {
			return c, errors.New("分享链接包含重复或不支持的参数，请改用受支持的节点 JSON")
		}
	}
	return c, nil
}

func tlsFromURL(q url.Values) *TLS {
	t := &TLS{Enabled: true, ServerName: q.Get("sni")}
	if t.ServerName == "" {
		t.ServerName = q.Get("peer")
	}
	if q.Get("alpn") != "" {
		t.ALPN = strings.Split(q.Get("alpn"), ",")
	}
	t.Insecure = q.Get("insecure") == "1" || q.Get("insecure") == "true" || q.Get("allowInsecure") == "1" || q.Get("allowInsecure") == "true"
	if q.Get("pinSHA256") != "" {
		t.Pins = strings.Split(q.Get("pinSHA256"), ",")
	}
	if q.Get("fp") != "" {
		t.UTLS = &UTLS{Enabled: true, Fingerprint: q.Get("fp")}
	}
	return t
}

func decodeBase64(value string) (string, bool) {
	for _, encoding := range []*base64.Encoding{base64.RawURLEncoding, base64.URLEncoding, base64.RawStdEncoding, base64.StdEncoding} {
		if decoded, err := encoding.DecodeString(value); err == nil {
			return string(decoded), true
		}
	}
	return "", false
}
