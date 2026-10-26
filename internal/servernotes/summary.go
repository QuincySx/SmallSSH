package servernotes

import (
	"fmt"
	"sort"
	"strings"

	"sshclient/internal/monitor"
)

func infrastructureService(name string) bool {
	name = strings.TrimSuffix(strings.ToLower(name), ".service")
	for _, prefix := range []string{"systemd-", "cloud-", "dbus", "kmod-", "user@", "getty@", "serial-getty@", "polkit", "unattended-upgrades", "apt-daily", "networkmanager"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	switch name {
	case "ssh", "sshd", "apparmor", "cron", "crond", "rsyslog", "syslog", "auditd", "acpid", "atd", "chrony", "chronyd", "ntp", "ntpd", "networking", "network", "irqbalance", "rngd", "udisks2", "accounts-daemon", "packagekit", "qemu-guest-agent", "aliyun", "aliyun-service", "assist-daemon", "aegis", "docker", "containerd", "snapd", "ufw", "firewalld", "multipathd", "gssproxy", "iprdump", "iprinit", "iprupdate":
		return true
	}
	return false
}

func serviceState(s monitor.Service) string {
	if s.Sub == "running" {
		return "运行中"
	}
	if s.Active == "failed" {
		return "运行失败"
	}
	if s.Active == "active" {
		return "状态未核验"
	}
	if s.Active == "inactive" || s.Active == "deactivating" {
		return "未运行"
	}
	return "状态未核验"
}

func containerState(state string) string {
	switch state {
	case "running":
		return "运行中"
	case "paused":
		return "已暂停"
	case "restarting":
		return "重启中"
	case "exited", "created":
		return "未运行"
	case "dead":
		return "异常"
	}
	return "状态未核验"
}

// Text lists only applications and domains. OS plumbing, socket addresses and
// config paths belong in Details. This is a local transform of one existing
// snapshot: no extra SSH, DNS, HTTP or model request is made.
func (r Report) Text() string {
	type item struct {
		app    application
		states map[string]bool
	}
	apps := map[string]*item{}
	add := func(app application, state string) {
		if apps[app.name] == nil {
			apps[app.name] = &item{app: app, states: map[string]bool{}}
		}
		apps[app.name].states[state] = true
	}
	for _, s := range r.Inventory.Services {
		known, ok := identifyService(s.Name)
		if ok && (s.Active == "active" || s.Sub == "running" || s.Active == "failed" || s.Enabled == "enabled") {
			add(known, serviceState(s))
		} else if !ok && (s.Sub == "running" || s.Active == "failed") && r.CustomServices[s.Name] != "" && !infrastructureService(s.Name) {
			add(application{clean(strings.TrimSuffix(s.Name, ".service"), 80), 5}, serviceState(s))
		}
	}
	if d := r.Inventory.Docker; d != nil && d.Status == "ready" {
		for _, c := range d.Containers {
			app, ok := identifyContainer(c)
			if !ok {
				app = application{clean(c.Names, 80), 5}
				if app.name == "" {
					app.name = clean(c.Image, 80)
				}
			}
			add(app, containerState(c.State))
		}
	}
	items := make([]*item, 0, len(apps))
	for _, v := range apps {
		items = append(items, v)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].app.order != items[j].app.order {
			return items[i].app.order < items[j].app.order
		}
		return items[i].app.name < items[j].app.name
	})
	var services []string
	for _, v := range items {
		var states []string
		for state := range v.states {
			if state != "运行中" && state != "状态未核验" {
				states = append(states, state)
			}
		}
		sort.Strings(states)
		label := v.app.name
		if len(states) > 0 {
			if v.states["运行中"] {
				label += "（部分" + strings.Join(states, " / ") + "）"
			} else {
				label += "（" + strings.Join(states, " / ") + "）"
			}
		}
		services = append(services, label)
	}
	serviceText := "暂未识别到业务服务"
	if len(services) > 0 {
		serviceText = shortList(services, 8)
	}
	lines := []string{"服务：" + serviceText}
	var domains []string
	for _, d := range r.Domains {
		label := d.Name
		switch d.Status {
		case "匹配已知地址":
		case "未匹配已知地址（可能经 CDN / 代理）":
			label += "（解析地址不同）"
		case "解析未成功":
			label += "（本次解析未成功）"
		default:
			label += "（未核验）"
		}
		domains = append(domains, label)
	}
	if len(domains) > 0 {
		lines = append(lines, "\n域名：\n"+strings.Join(domains[:min(len(domains), 8)], "\n"))
		if len(domains) > 8 {
			lines = append(lines, fmt.Sprintf("另 %d 个，见检测详情", len(domains)-8))
		}
	} else {
		lines = append(lines, "\n域名：暂未发现")
	}
	return strings.Join(lines, "\n")
}

func shortList(values []string, limit int) string {
	text := strings.Join(values[:min(len(values), limit)], "、")
	if len(values) > limit {
		text += fmt.Sprintf("，另 %d 项", len(values)-limit)
	}
	return text
}

// SavedPresentation also upgrades the display of pre-summary snapshots. The
// original evidence is preserved verbatim; viewing it does not write the vault
// or probe the server again, and it cannot dirty a handwritten draft.
func SavedPresentation(summary, details string) (string, string) {
	if strings.HasPrefix(summary, "服务：") && strings.Contains(summary, "\n域名：") {
		return summary, details
	}
	evidence := summary
	if details != "" {
		evidence = details
	}
	legacy := strings.HasPrefix(evidence, "系统：") || strings.Contains(evidence, "运行中的服务（") || strings.Contains(evidence, "已激活的服务（") || strings.Contains(evidence, "监听地址（不代表公网已放行）")
	if !legacy {
		return summary, details
	}
	r := Report{}
	lines := strings.Split(evidence, "\n")
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if (strings.HasPrefix(line, "运行中的服务（") || strings.HasPrefix(line, "已激活的服务（")) && i+1 < len(lines) {
			for _, name := range strings.Split(strings.TrimSpace(lines[i+1]), "、") {
				if strings.HasSuffix(name, ".service") {
					// Legacy snapshots did not record the substate. Do not turn
					// active one-shot tasks into "running" applications.
					r.Inventory.Services = append(r.Inventory.Services, monitor.Service{Name: name, Active: "active"})
					if app, ok := identifyService(name); ok && details != "" && strings.Contains(summary, app.name+" · 运行中") {
						r.Inventory.Services[len(r.Inventory.Services)-1].Sub = "running"
					}
				}
			}
		}
		if strings.HasPrefix(line, "容器：") {
			parts := strings.Split(strings.TrimPrefix(line, "容器："), " · ")
			if len(parts) >= 3 {
				if r.Inventory.Docker == nil {
					r.Inventory.Docker = &monitor.DockerInfo{Status: "ready"}
				}
				r.Inventory.Docker.Containers = append(r.Inventory.Docker.Containers, monitor.Container{Names: parts[0], State: parts[1], Image: parts[2]})
			}
		}
		parts := strings.Split(line, " · ")
		if len(parts) >= 2 {
			if name, ok := domainName(parts[0]); ok {
				d := Domain{Name: name, Status: parts[1]}
				if len(parts) > 2 {
					d.IPs = strings.Split(parts[2], ", ")
				}
				r.Domains = append(r.Domains, d)
			}
		}
	}
	return r.Text(), evidence
}
