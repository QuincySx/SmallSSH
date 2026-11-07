package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"sshclient/internal/approval"
	"sshclient/internal/client"
	"sshclient/internal/connectcfg"
	"sshclient/internal/hardware"
	"sshclient/internal/vault"
)

func tunnelCommand(ctx context.Context, c client.Client, label string, args []string) (int, error) {
	if len(args) < 3 || len(args) > 4 {
		return 2, errors.New("用法: smallssh tunnel local|remote|socks5 连接ID 回环监听地址 [目标主机:端口]")
	}
	spec := connectcfg.Tunnel{Kind: args[0], Listen: args[2]}
	if len(args) == 4 {
		spec.Target = args[3]
	}
	if e := spec.Validate(); e != nil {
		return 2, e
	}
	r, ticket, e := c.Request(ctx, approval.Operation{Host: args[1], Client: label, Kind: "tunnel", Tunnel: spec}, 15*time.Minute)
	if e != nil {
		return 1, e
	}
	writeApprovalNotice(os.Stderr, r)
	if e = c.Wait(ctx, r.ID, ticket); e != nil {
		return 1, e
	}
	stream, e := c.Start(ctx, r.ID, ticket, 0, 0)
	if e != nil {
		return 1, e
	}
	defer stream.Close()
	fmt.Fprintln(os.Stderr, "隧道已启动：", stream.ListenAddress, "；Ctrl+C 关闭监听与现有连接")
	for {
		m, e := stream.Receive()
		if e != nil {
			if ctx.Err() != nil {
				return 0, nil
			}
			return 1, e
		}
		if m.Error != "" {
			return 1, errors.New(m.Error)
		}
		if m.Action == "done" {
			return 0, nil
		}
	}
}

type hostOptions struct {
	Terminal           *connectcfg.Terminal `json:"terminal,omitempty"`
	Network            *connectcfg.Network  `json:"network,omitempty"`
	Hardware           *connectcfg.Hardware `json:"hardware,omitempty"`
	ClearProxyPassword bool                 `json:"clearProxyPassword,omitempty"`
}

func configureHost(ctx context.Context, c client.Client, dir, id string, biometric bool) (int, error) {
	owner, e := openOwner(ctx, dir, biometric, "修改连接的终端、代理、中转或硬件密钥配置")
	if e != nil {
		return 1, e
	}
	defer owner.Close()
	profile, e := owner.Profile(id)
	if e != nil {
		return 1, e
	}
	var options hostOptions
	data, e := io.ReadAll(io.LimitReader(os.Stdin, (64<<10)+1))
	if e != nil || len(data) > 64<<10 {
		return 2, errors.New("连接选项 JSON 最多 64 KiB")
	}
	defer wipe(data)
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if e = decoder.Decode(&options); e != nil {
		return 2, errors.New("请从 stdin 提供连接选项 JSON")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return 2, errors.New("仅接受一个连接选项 JSON 对象")
	}
	return 0, c.SaveConnection(ctx, options.apply(profile), owner)
}

func (options hostOptions) apply(profile vault.Profile) vault.ProfileChange {
	previous := profile.Network.Proxy.Public()
	if options.Terminal != nil {
		profile.Terminal = *options.Terminal
	}
	if options.Network != nil {
		profile.Network = *options.Network
	}
	keep := options.Hardware == nil
	if options.Hardware != nil {
		profile.Auth = "pkcs11"
		profile.Hardware = *options.Hardware
	}
	profile.Password, profile.PrivateKey, profile.KeyPassphrase = "", "", ""
	if keep {
		profile.Hardware.PIN = ""
	}
	keepProxy := !options.ClearProxyPassword && profile.Network.Proxy.Password == "" && profile.Network.Proxy.Public() == previous
	if options.Network == nil {
		profile.Network.Proxy.Password = ""
		keepProxy = !options.ClearProxyPassword
	}
	if options.ClearProxyPassword {
		profile.Network.Proxy.Password = ""
	}
	return vault.ProfileChange{Profile: profile, KeepCredential: keep, KeepProxyCredential: keepProxy}
}
func inspectHardwareCommand(ctx context.Context, c client.Client, dir string, biometric bool, args []string) (int, error) {
	if len(args) < 1 || len(args) > 2 {
		return 2, errors.New("用法: smallssh host hardware 模块完整路径 [令牌序列号]；选定序列号后从本地终端读取 PIN")
	}
	owner, e := openOwner(ctx, dir, biometric, "读取 PKCS#11 驱动和硬件公钥")
	if e != nil {
		return 1, e
	}
	defer owner.Close()
	config := connectcfg.Hardware{Module: args[0]}
	if len(args) == 2 {
		config.TokenSerial = args[1]
		pin, e := password("硬件 PIN（不会自动重试；取消可直接退出）: ")
		if e != nil {
			return 1, e
		}
		defer wipe(pin)
		config.PIN = string(pin)
	}
	hash, keys, e := hardware.Inspect(ctx, config)
	if e != nil {
		return 1, e
	}
	for i := range keys {
		keys[i].Public = nil
	}
	return 0, json.NewEncoder(os.Stdout).Encode(struct {
		ModuleSHA256 string         `json:"moduleSHA256"`
		Keys         []hardware.Key `json:"keys"`
	}{hash, keys})
}
