package sshconn

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sshclient/internal/sshdata"
	"sshclient/internal/sshkey"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"sshclient/internal/connectcfg"
	"sshclient/internal/netroute"
	"sshclient/internal/vault"
)

type HostIdentity = sshdata.HostIdentity

// ProbeHost stops during key exchange, before any user authentication. A network
// observation is a candidate identity; the caller must obtain owner confirmation.
func ProbeHost(parent context.Context, host string, port int) (HostIdentity, error) {
	return ProbeRoute(parent, host, port, connectcfg.Network{}, nil)
}
func ProbeRoute(parent context.Context, host string, port int, network connectcfg.Network, relays []vault.Profile) (HostIdentity, error) {
	if err := ValidateTarget(host, port); err != nil {
		return HostIdentity{}, err
	}
	ctx, cancel := context.WithTimeout(parent, 8*time.Second)
	defer cancel()
	addr := net.JoinHostPort(host, fmt.Sprint(port))
	var raw net.Conn
	var err error
	if len(relays) == 0 {
		raw, err = netroute.Dial(ctx, addr, network.Proxy)
	} else {
		var relay *Client
		relay, err = DialRoute(ctx, relays)
		if err == nil {
			defer relay.Close()
			raw, err = relay.SSH.DialContext(ctx, "tcp", addr)
		}
	}
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

type KeyInfo = sshdata.KeyInfo

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

func ValidateTarget(host string, port int) error { return sshkey.ValidateTarget(host, port) }
func privateSigner(raw, passphrase string) (ssh.Signer, error) {
	return sshkey.ParsePrivateKey(raw, passphrase)
}
func InspectPrivateKey(raw, passphrase string) (KeyInfo, error) {
	return sshkey.InspectPrivateKey(raw, passphrase)
}
