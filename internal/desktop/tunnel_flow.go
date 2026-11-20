package desktop

import (
	"net"
	"strconv"
	"strings"
)

type tunnelHop struct {
	title, role, address string
	current              bool
}
type tunnelPath struct {
	hops                                              [3]tunnelHop
	purpose, summary, listenLabel, targetLabel, scope string
}

// The drawing follows where listeners and outbound dials actually live in
// sshconn.Forward. The target's loopback address belongs to the exit device,
// which changes with remote forwarding; it never means the viewer's device
// merely because the address is 127.0.0.1.
func describeTunnel(kind, listen, target, server string) tunnelPath {
	localTarget := false
	if host, _, err := net.SplitHostPort(target); err == nil {
		ip := net.ParseIP(host)
		localTarget = strings.EqualFold(host, "localhost") || ip != nil && ip.IsLoopback()
	}
	path := tunnelPath{purpose: "本机入口 → 远端服务（本地转发）", listenLabel: "本机入口地址", targetLabel: "远端服务地址", scope: "在本机开一个端口，通过 SSH 访问远端服务。"}
	path.hops = [3]tunnelHop{{"本机", "访问入口", listen, true}, {"SSH 服务器", "转发出口", server, false}, {"目标服务", "从服务器访问", target, false}}
	path.summary = "本机程序经 SSH 服务器，访问服务器网络中的目标服务。"
	if localTarget {
		path.hops[2].role = "服务器本机服务"
		path.summary = "本机程序通过 SSH 隧道，访问 SSH 服务器上的服务。"
	}
	switch kind {
	case "remote":
		path.purpose = "服务器入口 → 本机服务（远程转发）"
		path.listenLabel, path.targetLabel, path.scope = "服务器入口地址", "本机服务地址", "把本机服务映射到服务器端口；入口仅供服务器本机使用。"
		path.hops = [3]tunnelHop{{"SSH 服务器", "访问入口", server + "\n" + listen, false}, {"本机", "转发出口", "", true}, {"目标服务", "从本机访问", target, false}}
		path.summary = "SSH 服务器上的程序经本机，访问本机网络中的目标服务。"
		if localTarget {
			path.hops[2].role = "本机上的服务"
			path.summary = "SSH 服务器上的程序通过隧道，访问当前设备上的服务。"
		}
	case "socks5":
		path.purpose = "本机代理 → SSH 服务器 → 应用指定目标"
		path.listenLabel, path.scope = "本机 SOCKS5 地址", "SSH：右键目标服务器 → 终端连接 → SOCKS5。其他软件选 SOCKS5 并填写此地址。"
		path.hops[0].role = "SOCKS5 入口"
		path.hops[2] = tunnelHop{"动态目标", "由程序指定", "任意 TCP 目标", false}
		path.summary = "本机程序使用此 SOCKS5 端口，经 SSH 服务器连接程序指定的目标。"
	}
	return path
}

func tunnelKindLabel(kind string) string {
	switch kind {
	case "remote":
		return "远程转发"
	case "socks5":
		return "SOCKS5"
	default:
		return "本地转发"
	}
}

func (a *app) tunnelServerEndpoint(id string) string {
	for _, p := range a.connections {
		if p.ID == id {
			return net.JoinHostPort(p.Host, strconv.Itoa(p.Port))
		}
	}
	return a.hostName(id)
}

func (a *app) tunnelServerName(id string) string {
	for _, p := range a.connections {
		if p.ID == id {
			return p.Name
		}
	}
	return id
}
