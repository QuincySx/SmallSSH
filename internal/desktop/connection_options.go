package desktop

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/broker"
	"sshclient/internal/desktop/components"
	"sshclient/internal/hardware"
)

type connectionOption struct{ value, label string }

func optionSelect(c *ui.Context, label string, value *string, options []connectionOption) bool {
	changed := false
	ui.Field(c, tr(c, label), func() {
		parts := ui.SelectBase(c, value)
		text := *value
		if text == "" {
			text = options[0].label
		}
		for _, option := range options {
			if option.value == *value {
				text = option.label
			}
		}
		theme := c.Theme()
		parts.Trigger.FillWidth().Height(components.FormHeight).Padding(6, 10).Radius(theme.Radius).Background(theme.Surface).Border(1, theme.Border).Label(tr(c, label+"选择")).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() { ui.Text(c, tr(c, text)).SingleLine().Grow(1); ui.Text(c, "⌄").TextColor(theme.TextMuted) })
		})
		parts.Popup(func(panel ui.Element) {
			panel.Padding(4).Background(theme.Background).Border(1, theme.Border).Radius(theme.Radius).MaxHeight(260).Children(func() {
				ui.Scroll(c).FillWidth().MaxHeight(240).Children(func() {
					ui.Column(c).FillWidth().Children(func() {
						for _, option := range options {
							item := parts.Item(option.value).Padding(6, 8).Radius(theme.Radius)
							if item.Highlighted() {
								item.Background(theme.Surface)
							}
							item.Children(func() { ui.Text(c, tr(c, option.label)) })
						}
					})
				})
			})
		})
		changed = parts.Trigger.Changed()
	})
	return changed
}
func (a *app) connectionTerminalForm(c *ui.Context) {
	ui.Column(c).FillWidth().Gap(components.FormGap).Children(func() {
		optionSelect(c, "字符编码", &a.form.Terminal.Encoding, []connectionOption{{"", "自动（UTF-8 优先）"}, {"utf-8", "UTF-8"}, {"gb18030", "GB18030"}, {"gbk", "GBK"}})
		ui.Text(c, tr(c, "自动编码按首个非 ASCII 输出识别；乱码时请显式选择 GB18030 / GBK。只转换终端，文件保持原始字节。")).FontSize(11).FillWidth().TextColor(c.Theme().TextMuted)
		optionSelect(c, "退格序列", &a.form.Terminal.Backspace, []connectionOption{{"", "自动（协商 ^?）"}, {"del", "^?（DEL）"}, {"bs", "^H（BS）"}})
		interval := strconv.Itoa(a.form.Network.KeepaliveSeconds)
		if optionSelect(c, "SSH 保活间隔", &interval, []connectionOption{{"0", "关闭"}, {"15", "15 秒"}, {"30", "30 秒"}, {"60", "60 秒"}, {"120", "120 秒"}, {"300", "300 秒"}}) {
			a.form.Network.KeepaliveSeconds, _ = strconv.Atoi(interval)
		}
		failures := strconv.Itoa(a.form.Network.KeepaliveFailures)
		if optionSelect(c, "无响应断开阈值", &failures, []connectionOption{{"0", "3 个保活间隔（默认）"}, {"1", "1 个保活间隔"}, {"3", "3 个保活间隔"}, {"5", "5 个保活间隔"}}) {
			a.form.Network.KeepaliveFailures, _ = strconv.Atoi(failures)
		}
	})
}

func (a *app) connectionNetworkForm(c *ui.Context) {
	ui.Column(c).FillWidth().Gap(components.FormGap).Children(func() {
		optionSelect(c, "网络代理", &a.form.Network.Proxy.Mode, []connectionOption{{"", "不使用代理"}, {"http", "HTTP CONNECT"}, {"https", "HTTPS CONNECT"}, {"socks5", "SOCKS5"}})
		if mode := a.form.Network.Proxy.Mode; mode != "" && mode != "none" {
			a.connectionField(c, "proxy-address", tr(c, "代理主机:端口"), &a.form.Network.Proxy.Address, false, "")
			a.connectionField(c, "proxy-user", tr(c, "代理用户名"), &a.form.Network.Proxy.User, false, tr(c, "留空表示不认证；代理负责解析目标域名"))
			if a.details != nil {
				ui.Field(c, tr(c, "代理认证"), func() {
					if ui.Checkbox(c, &a.connection.replaceProxy, tr(c, "更换代理密码")).Changed() && !a.connection.replaceProxy {
						a.form.Network.Proxy.Password = ""
					}
				})
			}
			if a.details == nil || a.connection.replaceProxy {
				a.connectionField(c, "proxy-password", tr(c, "代理密码"), &a.form.Network.Proxy.Password, true, tr(c, "与连接一起加密保存；不会返回给调用方"))
			}
		}
		options := []connectionOption{{"", "直达目标（不经 SSH 中转）"}}
		for _, profile := range a.connections {
			if profile.ID != a.form.ID {
				options = append(options, connectionOption{profile.ID, profile.Name + " · " + profile.ID})
			}
		}
		optionSelect(c, "自有 SSH 中转", &a.form.Network.Relay, options)
		ui.Text(c, tr(c, "中转只使用你保存并核对指纹的服务器；最多 4 跳，每跳独立验证，不承诺固定提速。代理配置用于第一跳，不能与同一跳的 SSH 中转并选。")).FontSize(11).FillWidth().TextColor(c.Theme().TextMuted)
		a.routeMeasurementsView(c)
	})
}

func (a *app) routeMeasurementKey() string {
	value := struct {
		Host, Port, Fingerprint string
		Network                 any
	}{a.form.Host, a.port, a.form.Fingerprint, a.form.Network.Public()}
	b, _ := json.Marshal(value)
	return string(b)
}
func (a *app) routeMeasurementsView(c *ui.Context) {
	ui.Field(c, tr(c, "路径实测"), func() {
		if components.Button(c, tr(c, "测量直连 / 中转握手耗时")).Disabled(a.form.Fingerprint == "").Clicked() {
			a.measureConnectionRoutes()
		}
	}).Description(tr(c, "先核对指纹；不登录目标服务器，只测量连接建立耗时。中转需要登录已保存的跳板机，不代表持续传输带宽。"))
	if a.connection.measuredNetwork != a.routeMeasurementKey() {
		return
	}
	best := -1
	for i, result := range a.connection.routeMeasurements {
		label := tr(c, "直连 / 当前代理")
		if result.Relay {
			label = tr(c, "自有 SSH 中转")
		}
		text := fmt.Sprintf(tr(c, "%s · 握手 %d ms"), label, result.HandshakeMillis)
		if !result.Available {
			text = label + " · " + reviewText(result.Error)
		} else if best < 0 || result.HandshakeMillis < a.connection.routeMeasurements[best].HandshakeMillis {
			best = i
		}
		ui.Text(c, text).FontSize(11).FillWidth()
	}
	if len(a.connection.routeMeasurements) > 1 && best >= 0 {
		if components.Button(c, tr(c, "按实测选择更快路径")).Clicked() {
			if !a.connection.routeMeasurements[best].Relay {
				a.form.Network.Relay = ""
			}
			a.connection.message = tr(nil, "已按本次握手测量选择路径；保存连接后生效")
		}
	}
}
func (a *app) measureConnectionRoutes() {
	if a.connection.phase != "" {
		return
	}
	owner, e := a.unlockedOwner()
	if e != nil {
		a.connection.errors["form"] = e.Error()
		return
	}
	port, e := strconv.Atoi(a.port)
	if e != nil {
		a.connection.errors["form"] = e.Error()
		return
	}
	network := a.form.Network
	keep := a.details != nil && !a.connection.replaceProxy && network.Proxy.Password == "" && network.Proxy.Public() == a.details.Network.Proxy.Public()
	query := broker.Message{Action: "measureRoutes", ID: a.form.ID, Target: a.form.Host, Port: port, Network: &network, KeepProxyCredential: keep, ExpectedFingerprint: a.form.Fingerprint}
	key := a.routeMeasurementKey()
	ctx, seq, gen := a.editorOperation("route-measure")
	a.connection.message = tr(nil, "正在比较连接路径，尚未登录目标服务器…")
	go func() {
		result, e := a.client.CallWithOwner(ctx, query, owner)
		a.postUI(func() {
			if !a.editorCurrent(seq, gen) {
				return
			}
			a.connection.cancel()
			a.connection.cancel = nil
			a.connection.phase = ""
			a.connection.message = ""
			if e != nil {
				a.connection.errors["form"] = e.Error()
				return
			}
			a.connection.routeMeasurements = result.RouteMeasurements
			a.connection.measuredNetwork = key
			delete(a.connection.errors, "form")
		})
	}()
}
func (a *app) hardwareForm(c *ui.Context) {
	ui.Column(c).FillWidth().Gap(components.FormGap).Children(func() {
		a.connectionField(c, "pkcs11-module", tr(c, "PKCS#11 原生模块"), &a.form.Hardware.Module, false, tr(c, "驱动会以当前用户执行原生代码，只选择可信模块；支持 PKCS#11 RSA / ECDSA，FIDO2 通过 agent"))
		a.connectionField(c, "pkcs11-serial", tr(c, "令牌序列号"), &a.form.Hardware.TokenSerial, false, tr(c, "可先留空读取设备，再选择令牌后输入 PIN；不会遍历尝试 PIN"))
		a.connectionField(c, "pkcs11-pin", tr(c, "硬件 PIN"), &a.form.Hardware.PIN, true, tr(c, "按设备要求触摸确认；PIN 加密保存，不尝试默认 PIN，不自动重试"))
		ui.Field(c, tr(c, "硬件识别"), func() {
			label := tr(c, "加载驱动并读取令牌 / 公钥")
			if components.Button(c, label).MinWidth(0).MaxWidthPercent(100).Tooltip(label).Disabled(a.form.Hardware.Module == "").Clicked() {
				a.inspectHardware()
			}
		})
		if a.form.Hardware.ModuleSHA256 != "" {
			ui.Text(c, "SHA256 · "+a.form.Hardware.ModuleSHA256).FontSize(11).Selectable().FillWidth()
		}
		for _, key := range a.connection.hardwareKeys {
			ui.Column(c).FillWidth().Padding(12).Gap(6).Radius(c.Theme().Radius).Background(c.Theme().Surface).Border(1, c.Theme().Border).Children(func() {
				ui.Text(c, reviewText(key.Token+" · "+key.Model+" · "+key.Serial)).FillWidth().FontSize(12)
				label := tr(c, "选择此令牌")
				if key.ID != "" {
					label = tr(c, "选择此公钥")
					ui.Text(c, reviewText(key.Algorithm+" · ID "+key.ID)).FontSize(11)
					ui.Text(c, reviewText(key.Fingerprint)).Selectable().FillWidth().FontSize(11)
				}
				if components.Button(c, label).Key("hardware-select-" + key.Serial + key.ID + key.Fingerprint).Clicked() {
					a.form.Hardware.TokenSerial = key.Serial
					a.form.Hardware.KeyID = key.ID
					a.form.Hardware.Fingerprint = key.Fingerprint
				}
			})
		}
		if a.form.Hardware.KeyID != "" {
			ui.Text(c, fmt.Sprintf("%s · ID %s", a.form.Hardware.Fingerprint, a.form.Hardware.KeyID)).FontSize(11).Selectable().FillWidth()
		}
		ui.Text(c, tr(c, "FIDO2 / sk- 密钥请选 SSH 密钥代理；此处不导出私钥、不初始化或修改硬件令牌。")).FontSize(11).FillWidth().TextColor(c.Theme().TextMuted)
	})
}
func (a *app) inspectHardware() {
	if a.connection.phase != "" {
		return
	}
	if _, e := a.unlockedOwner(); e != nil {
		a.status = e.Error()
		return
	}
	config := a.form.Hardware
	ctx, seq, gen := a.editorOperation("hardware")
	a.connection.message = tr(nil, "正在读取硬件；如设备要求，请触摸确认")
	go func() {
		bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		hash, keys, e := hardware.Inspect(bounded, config)
		a.postUI(func() {
			if !a.editorCurrent(seq, gen) {
				return
			}
			a.connection.cancel()
			a.connection.cancel = nil
			a.connection.phase = ""
			a.connection.message = ""
			if e != nil {
				a.connection.errors["form"] = e.Error()
				return
			}
			a.form.Hardware.ModuleSHA256 = hash
			a.connection.hardwareKeys = keys
			delete(a.connection.errors, "form")
		})
	}()
}
