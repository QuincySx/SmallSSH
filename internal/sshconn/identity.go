package sshconn

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

type HostIdentity struct {
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Algorithm   string `json:"algorithm"`
	Fingerprint string `json:"fingerprint"`
}

func ValidateTarget(host string, port int) error {
	if host == "" || len(host) > 255 || strings.ContainsAny(host, "\x00\r\n\t /\\") || port < 1 || port > 65535 {
		return errors.New("请填写有效的服务器地址和 1–65535 之间的端口")
	}
	return nil
}

// ProbeHost stops during key exchange, before any user authentication. A network
// observation is a candidate identity; the caller must obtain owner confirmation.
func ProbeHost(parent context.Context, host string, port int) (HostIdentity, error) {
	if err := ValidateTarget(host, port); err != nil {
		return HostIdentity{}, err
	}
	ctx, cancel := context.WithTimeout(parent, 8*time.Second)
	defer cancel()
	addr := net.JoinHostPort(host, fmt.Sprint(port))
	raw, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return HostIdentity{}, fmt.Errorf("无法连接服务器 %s（TCP 连接）: %w", addr, err)
	}
	defer raw.Close()
	stop := context.AfterFunc(ctx, func() { raw.Close() })
	defer stop()
	deadline, _ := ctx.Deadline()
	raw.SetDeadline(deadline)
	identity := HostIdentity{Host: host, Port: port}
	observed := errors.New("host identity observed")
	_, _, _, err = ssh.NewClientConn(raw, addr, &ssh.ClientConfig{User: "host-identity-check", HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
		identity.Algorithm = key.Type()
		identity.Fingerprint = ssh.FingerprintSHA256(key)
		return observed
	}})
	if ctx.Err() != nil {
		return HostIdentity{}, fmt.Errorf("未能读取 SSH 主机身份 %s（SSH 握手）: %w", addr, ctx.Err())
	}
	if !errors.Is(err, observed) || identity.Fingerprint == "" {
		return HostIdentity{}, fmt.Errorf("未能读取 SSH 主机身份 %s（SSH 握手）: %w", addr, err)
	}
	return identity, nil
}

type KeyInfo struct {
	Algorithm   string `json:"algorithm"`
	Fingerprint string `json:"fingerprint"`
	Comment     string `json:"comment,omitempty"`
}

func privateSigner(raw, passphrase string) (ssh.Signer, error) {
	var signer ssh.Signer
	var err error
	if passphrase != "" {
		signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(raw), []byte(passphrase))
	} else {
		signer, err = ssh.ParsePrivateKey([]byte(raw))
	}
	if err != nil {
		var missing *ssh.PassphraseMissingError
		if errors.As(err, &missing) {
			return nil, errors.New("这份私钥已加密，请填写私钥口令")
		}
		if _, _, _, _, pubErr := ssh.ParseAuthorizedKey([]byte(raw)); pubErr == nil {
			return nil, errors.New("选择的是公钥，请选择对应的私钥文件")
		}
		return nil, errors.New("私钥格式不受支持或私钥口令不正确")
	}
	return signer, nil
}

func InspectPrivateKey(raw, passphrase string) (KeyInfo, error) {
	signer, err := privateSigner(raw, passphrase)
	if err != nil {
		return KeyInfo{}, err
	}
	return KeyInfo{Algorithm: signer.PublicKey().Type(), Fingerprint: ssh.FingerprintSHA256(signer.PublicKey())}, nil
}

// AgentIdentities returns public metadata only. No signing or private key export.
func AgentIdentities(parent context.Context) ([]KeyInfo, error) {
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	c, err := dialAgent(ctx)
	if err != nil {
		return nil, errors.New("未找到可用的 SSH 密钥代理，请先启动并解锁密钥管理软件")
	}
	defer c.Close()
	stop := context.AfterFunc(ctx, func() { c.Close() })
	defer stop()
	keys, err := agent.NewClient(c).List()
	if err != nil {
		return nil, errors.New("无法读取代理中的公开密钥")
	}
	if len(keys) > 128 {
		return nil, errors.New("代理中的身份过多，请先精简身份列表")
	}
	out := make([]KeyInfo, 0, len(keys))
	for _, key := range keys {
		pub, err := ssh.ParsePublicKey(key.Blob)
		if err != nil {
			continue
		}
		comment := strings.Map(func(r rune) rune {
			if r < 32 || r == 127 {
				return -1
			}
			return r
		}, key.Comment)
		if len(comment) > 128 {
			comment = comment[:128]
		}
		out = append(out, KeyInfo{Algorithm: pub.Type(), Fingerprint: ssh.FingerprintSHA256(pub), Comment: comment})
	}
	if len(out) == 0 {
		return nil, errors.New("代理中还没有可使用的密钥")
	}
	return out, nil
}
