package desktop

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"sshclient/docs/design"
	"sshclient/internal/connectcfg"
	"sshclient/internal/desktop/components"
	"sshclient/internal/hardware"
	"sshclient/internal/protocol"
)

var connectionSelectIconSize = design.Length("text-icon-compact")

type connectionOption struct{ value, label string }
type optionMenuStyle struct {
	width, inset, rowHeight, rowInset, fontSize float32
}

func optionSelect(c *ui.Context, label string, value *string, options []connectionOption) bool {
	return optionSelectHint(c, label, value, options, "")
}
func optionSelectHint(c *ui.Context, label string, value *string, options []connectionOption, hint string, menuStyle ...optionMenuStyle) bool {
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
		parts.Trigger.FillWidth().Height(components.FormHeight).Padding(6, 10).Radius(theme.Radius).Background(theme.Surface).Border(1, theme.Border).Label(tr(c, label+"选择")).Tooltip(tr(c, hint)).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
				ui.Text(c, tr(c, text)).SingleLine().Grow(1)
				ui.Icon(c, components.ChevronDownIcon).Size(connectionSelectIconSize, connectionSelectIconSize).TextColor(theme.TextMuted)
			})
		})
		parts.Popup(func(panel ui.Element) {
			panel.Padding(4).Background(theme.Background).Border(1, theme.Border).Radius(theme.Radius).MaxHeight(260).Children(func() {
				if len(menuStyle) > 0 {
					panel.Width(menuStyle[0].width).MinWidth(0).Padding(menuStyle[0].inset).
						Background(theme.Surface).Label(tr(c, label+"选项"))
				}
				ui.Scroll(c).FillWidth().MaxHeight(240).Children(func() {
					ui.Column(c).FillWidth().Children(func() {
						for _, option := range options {
							item := parts.Item(option.value).Padding(6, 8).Radius(theme.Radius)
							if len(menuStyle) == 0 {
								if item.Highlighted() {
									item.Background(theme.Surface)
								}
								item.Children(func() { ui.Text(c, tr(c, option.label)) })
								continue
							}
							style := menuStyle[0]
							item.Height(style.rowHeight).Padding(0, style.rowInset).AlignItems(ui.Center)
							item.Label(tr(c, option.label))
							if item.Highlighted() {
								item.Background(theme.SurfaceHover)
							}
							item.Children(func() {
								ui.Text(c, tr(c, option.label)).FontSize(style.fontSize).SingleLine()
							})
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
		optionSelectHint(c, "字符编码", &a.form.Terminal.Encoding, []connectionOption{{"", "自动（UTF-8 优先）"}, {"utf-8", "UTF-8"}, {"gb18030", "GB18030"}, {"gbk", "GBK"}}, "自动编码按首个非 ASCII 输出识别；乱码时请显式选择 GB18030 / GBK。只转换终端，文件保持原始字节。")
		optionSelect(c, "退格序列", &a.form.Terminal.Backspace, []connectionOption{{"", "自动（协商 ^?）"}, {"del", "^?（DEL）"}, {"bs", "^H（BS）"}})
		interval := strconv.Itoa(a.form.Network.KeepaliveSeconds)
		if optionSelectHint(c, "SSH 保活间隔", &interval, []connectionOption{{"0", "关闭"}, {"15", "15 秒"}, {"30", "30 秒"}, {"60", "60 秒"}, {"120", "120 秒"}, {"300", "300 秒"}}, "定时探测连接，帮助维持空闲连接并发现断连。关闭后不再发送探测。") {
			a.form.Network.KeepaliveSeconds, _ = strconv.Atoi(interval)
		}
		ui.Text(c, tr(c, "开：定时探测连接；关：不发送探测。")).FontSize(11).FillWidth().TextColor(c.Theme().TextMuted)
		ui.Column(c).FillWidth().Disabled(a.form.Network.KeepaliveSeconds == 0).Children(func() {
			failures := strconv.Itoa(a.form.Network.KeepaliveFailures)
			if optionSelect(c, "无响应等待上限", &failures, []connectionOption{{"0", "3 个保活间隔（默认）"}, {"1", "1 个保活间隔"}, {"3", "3 个保活间隔"}, {"5", "5 个保活间隔"}}) {
				a.form.Network.KeepaliveFailures, _ = strconv.Atoi(failures)
			}
		})
		if a.form.Network.KeepaliveSeconds != 0 {
			intervals := a.form.Network.KeepaliveFailures
			if intervals == 0 {
				intervals = 3
			}
			ui.Text(c, fmt.Sprintf(tr(c, "最长等待 %d 秒，超时断开。"), a.form.Network.KeepaliveSeconds*intervals)).FontSize(11).FillWidth().TextColor(c.Theme().TextMuted)
		}
	})
}

func (a *app) connectionNetworkForm(c *ui.Context) {
	ui.Column(c).FillWidth().Gap(components.FormGap).Children(func() {
		a.connectionRoutePicker(c)
		if a.form.Network.RouteID == "" {
			a.connectionCustomNetwork(c)
		}
		a.routeMeasurementsView(c)
	})
}

func (a *app) connectionCustomNetwork(c *ui.Context) {
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
			a.connectionField(c, "proxy-password", tr(c, "代理密码"), &a.form.Network.Proxy.Password, true, "")
		}
	}
	options := []connectionOption{{"", "直达目标（不经 SSH 跳板机）"}}
	for _, profile := range a.connections {
		if profile.ID != a.form.ID {
			options = append(options, connectionOption{profile.ID, profile.Name + " · " + profile.ID})
		}
	}
	optionSelectHint(c, "自有 SSH 跳板机", &a.form.Network.Relay, options, "跳板机使用已保存并核对指纹的 SSH 连接；最多 4 跳，每跳独立验证。代理与跳板机不能在同一跳并选。")
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
		if a.connection.phase == "route-measure" {
			if components.Button(c, tr(c, "停止测量")).Clicked() {
				a.stopEditorOperation()
			}
		} else if components.Button(c, tr(c, "测量耗时")).Tooltip(tr(c, "先核对指纹；不登录目标服务器。中转会登录已保存的跳板机。")).Disabled(a.form.Fingerprint == "" || a.connection.phase != "").Clicked() {
			a.measureConnectionRoutes()
		}
	}).Description(tr(c, "测量连接耗时，不代表下载速度。"))
	if a.connection.measuredNetwork != a.routeMeasurementKey() {
		return
	}
	best := -1
	for i, result := range a.connection.routeMeasurements {
		label := tr(c, "直连 / 当前代理")
		if result.Relay {
			label = tr(c, "自有 SSH 跳板机")
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
				if a.form.Network.RouteID != "" {
					a.form.Network.RouteID = ""
					a.form.Network.Proxy = connectcfg.Proxy{}
				}
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
	keep := network.RouteID == "" && a.details != nil && !a.connection.replaceProxy && network.Proxy.Password == "" && network.Proxy.Public() == a.details.Network.Proxy.Public()
	query := protocol.Message{Action: "measureRoutes", ID: a.form.ID, Target: a.form.Host, Port: port, Network: &network, KeepProxyCredential: keep, ExpectedFingerprint: a.form.Fingerprint}
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
func installedOpenSC() string {
	var paths []string
	switch runtime.GOOS {
	case "darwin":
		paths = []string{"/Library/OpenSC/lib/opensc-pkcs11.so", "/opt/homebrew/opt/opensc/lib/pkcs11/opensc-pkcs11.so", "/usr/local/opt/opensc/lib/pkcs11/opensc-pkcs11.so"}
	case "windows":
		if root := os.Getenv("ProgramFiles"); filepath.IsAbs(root) {
			paths = []string{filepath.Join(root, "OpenSC Project", "OpenSC", "pkcs11", "opensc-pkcs11.dll")}
		}
	case "linux":
		paths = []string{"/usr/lib/opensc-pkcs11.so", "/usr/lib64/opensc-pkcs11.so", "/usr/lib/pkcs11/opensc-pkcs11.so", "/usr/lib64/pkcs11/opensc-pkcs11.so"}
		switch runtime.GOARCH {
		case "amd64":
			paths = append(paths, "/usr/lib/x86_64-linux-gnu/opensc-pkcs11.so")
		case "arm64":
			paths = append(paths, "/usr/lib/aarch64-linux-gnu/opensc-pkcs11.so")
		}
	}
	return firstInstalledDriver(paths...)
}

func installedYKCS11() string {
	switch runtime.GOOS {
	case "darwin":
		return firstInstalledDriver("/usr/local/lib/libykcs11.dylib", "/usr/local/lib/libykcs11.so", "/opt/homebrew/opt/yubico-piv-tool/lib/libykcs11.dylib", "/usr/local/opt/yubico-piv-tool/lib/libykcs11.dylib")
	case "windows":
		if root := os.Getenv("ProgramFiles"); filepath.IsAbs(root) {
			return firstInstalledDriver(filepath.Join(root, "Yubico", "Yubico PIV Tool", "bin", "libykcs11.dll"))
		}
	case "linux":
		paths := []string{"/usr/local/lib/libykcs11.so", "/usr/lib/libykcs11.so.2", "/usr/lib64/libykcs11.so.2"}
		switch runtime.GOARCH {
		case "amd64":
			paths = append(paths, "/usr/lib/x86_64-linux-gnu/libykcs11.so.2")
		case "arm64":
			paths = append(paths, "/usr/lib/aarch64-linux-gnu/libykcs11.so.2")
		}
		return firstInstalledDriver(paths...)
	}
	return ""
}

func firstInstalledDriver(paths ...string) string {
	for _, path := range paths {
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			return path
		}
	}
	return ""
}

func hardwareDriverName(c *ui.Context, path string) string {
	if path == connectcfg.MacSmartCard {
		return tr(c, "macOS 系统智能卡")
	}
	name := filepath.Base(path)
	switch {
	case strings.HasPrefix(name, "opensc-pkcs11."):
		return "OpenSC"
	case strings.HasPrefix(name, "libykcs11."):
		return "YubiKey · YKCS11"
	default:
		return name
	}
}

func (a *app) selectHardwareDriver(path string) {
	a.form.Hardware = connectcfg.Hardware{Module: path}
	a.connection.hardwareKeys = nil
	a.connection.hardwarePIN = false
	delete(a.connection.errors, "form")
}

func (a *app) hardwareForm(c *ui.Context) {
	config := &a.form.Hardware
	e := &a.connection
	ui.Column(c).FillWidth().Gap(components.FormGap).Children(func() {
		ui.Row(c).FillWidth().Wrap().Gap(8).AlignItems(ui.Center).Children(func() {
			if runtime.GOOS == "darwin" && config.Module == "" && components.Button(c, tr(c, "读取系统智能卡")).Disabled(e.phase != "").Clicked() {
				a.selectHardwareDriver(connectcfg.MacSmartCard)
				a.inspectHardware()
			}
			components.Button(c, tr(c, "选择驱动")).Disabled(e.phase != "").Menu(func(m *ui.Menu) {
				if runtime.GOOS == "darwin" && m.Item(tr(c, "macOS 系统智能卡")).Chosen() {
					a.selectHardwareDriver(connectcfg.MacSmartCard)
				}
				for _, path := range []string{installedYKCS11(), installedOpenSC()} {
					if path != "" && m.Item(hardwareDriverName(c, path)).Chosen() {
						a.selectHardwareDriver(path)
					}
				}
				if m.Item(tr(c, "选择驱动文件…")).Chosen() {
					w := a.win
					a.selectConnectionFile(func() ([]string, error) {
						return mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: w, Title: tr(nil, "选择已安装的 PKCS#11 驱动"), ShowHiddenFiles: true})
					}, "选择驱动文件…", "form", a.selectHardwareDriver)
				}
			})
			if config.Module != "" {
				ui.Text(c, hardwareDriverName(c, config.Module)).SingleLine().MinWidth(0)
			}
			if e.phase == "hardware" {
				if components.Button(c, tr(c, "停止读取")).Clicked() {
					a.stopEditorOperation()
				}
			} else if config.Module != "" {
				label := "读取设备"
				if config.TokenSerial != "" {
					label = "读取密钥"
				}
				if components.Button(c, tr(c, label)).Disabled(e.phase != "").Clicked() {
					a.inspectHardware()
				}
			}
			if config.TokenSerial != "" && components.Button(c, tr(c, "更换设备")).Disabled(e.phase != "").Clicked() {
				config.TokenSerial, config.KeyID, config.Fingerprint, config.PIN = "", "", "", ""
				e.hardwareKeys = nil
				e.hardwarePIN = false
			}
		})
		if config.TokenSerial != "" && config.Module != connectcfg.MacSmartCard {
			if e.hardwarePIN || config.PIN != "" {
				a.connectionField(c, "pkcs11-pin", tr(c, "设备 PIN"), &config.PIN, true, "")
			} else {
				ui.Row(c).FillWidth().Children(func() {
					if components.Button(c, tr(c, "输入设备 PIN")).Clicked() {
						e.hardwarePIN = true
					}
				})
			}
		}
		selectedVisible := false
		for _, key := range e.hardwareKeys {
			if key.Serial == "" {
				continue
			}
			selected := config.TokenSerial == key.Serial && config.KeyID == key.ID && config.Fingerprint == key.Fingerprint
			selectedVisible = selectedVisible || selected
			ui.Row(c).FillWidth().Padding(8).Gap(8).AlignItems(ui.Center).Radius(c.Theme().Radius).Background(c.Theme().Surface).Border(1, c.Theme().Border).Children(func() {
				name := key.Token
				if name == "" {
					name = key.Model
				}
				if name == "" {
					name = key.Serial
				}
				if config.Module == connectcfg.MacSmartCard {
					name = tr(c, "系统 PIV 智能卡")
				}
				label := "选择设备"
				if key.ID != "" {
					label = "选择密钥"
				}
				ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
					ui.Text(c, reviewText(name)).FillWidth().Wrap()
					if key.ID != "" {
						ui.Text(c, reviewText(key.Label+" "+key.Fingerprint)).FontSize(11).Selectable().FillWidth().Wrap()
					}
				})
				if selected {
					ui.Text(c, tr(c, "已选中")).Shrink(0).TextColor(c.Theme().TextMuted)
				} else if components.Button(c, tr(c, label)).Shrink(0).Key("hardware-select-" + key.Serial + key.ID + key.Fingerprint).Clicked() {
					if config.TokenSerial != key.Serial {
						config.PIN = ""
					}
					config.TokenSerial, config.KeyID, config.Fingerprint = key.Serial, key.ID, key.Fingerprint
					e.hardwarePIN = key.ID == ""
				}
			})
		}
		if config.Fingerprint != "" && !selectedVisible {
			ui.Text(c, config.Fingerprint).FontSize(11).Selectable().FillWidth().Wrap()
		}
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
			if len(keys) == 0 {
				label := "未找到硬件设备或密钥"
				if config.Module == connectcfg.MacSmartCard {
					label = "未找到含可用密钥的 PIV 智能卡"
				}
				a.connection.errors["form"] = tr(nil, label)
			}
		})
	}()
}

// stopEditorOperation abandons the running form operation (route measuring,
// hardware reading) without closing the form. Its late result fails the
// sequence check and is dropped.
func (a *app) stopEditorOperation() {
	if a.connection.cancel != nil {
		a.connection.cancel()
		a.connection.cancel = nil
	}
	a.connection.sequence++
	a.connection.phase, a.connection.message = "", ""
}
