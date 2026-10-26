package servernotes

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"path"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"sshclient/internal/monitor"
)

type Domain struct {
	Name, Source, Status string
	IPs                  []string
}
type Report struct {
	Inventory      monitor.Inventory
	Ports          []string
	Domains        []Domain
	Warnings       []string
	Complete       bool
	CustomServices map[string]string
}

func domainName(raw string) (string, bool) {
	s := strings.ToLower(strings.Trim(strings.TrimSpace(raw), "\"'"))
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		u, err := url.Parse(s)
		if err != nil || u.User != nil || u.RawQuery != "" || u.Path != "" {
			return "", false
		}
		s = u.Host
	}
	if host, _, err := net.SplitHostPort(s); err == nil {
		s = host
	}
	s = strings.TrimSuffix(s, ".")
	base := strings.TrimPrefix(strings.TrimPrefix(s, "*."), ".")
	if len(s) > 253 || !strings.Contains(base, ".") {
		return "", false
	}
	if _, err := netip.ParseAddr(base); err == nil {
		return "", false
	}
	for _, label := range strings.Split(base, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return "", false
			}
		}
	}
	return s, true
}

func Parse(text, host string) Report {
	r := Report{Inventory: monitor.ParseInventory(text)}
	ports, domains := map[string]bool{}, map[string]bool{}
	domainLimit := false
	addDomain := func(raw, source string) {
		if name, ok := domainName(raw); ok && !domains[name] {
			if len(r.Domains) >= 32 {
				if !domainLimit {
					r.Warnings = append(r.Warnings, "域名超过 32 个，仅核验前 32 个")
					domainLimit = true
				}
				return
			}
			domains[name] = true
			r.Domains = append(r.Domains, Domain{Name: name, Source: clean(source, 256), Status: "未核验"})
		}
	}
	addDomain(host, "连接地址")
	scan := bufio.NewScanner(strings.NewReader(text))
	scan.Buffer(make([]byte, 4096), 128<<10)
	for scan.Scan() {
		f := strings.Split(scan.Text(), "\t")
		switch f[0] {
		case "@@NOTE_UNIT":
			if len(f) == 3 && len(f[1]) <= 256 && strings.HasSuffix(f[1], ".service") && clean(f[1], 256) == f[1] && len(f[2]) <= 512 {
				file := path.Clean(f[2])
				if (strings.HasPrefix(file, "/etc/systemd/system/") || strings.HasPrefix(file, "/usr/local/lib/systemd/system/")) && clean(file, 512) == file {
					if r.CustomServices == nil {
						r.CustomServices = make(map[string]string)
					}
					if len(r.CustomServices) < 128 {
						r.CustomServices[f[1]] = file
					}
				}
			}
		case "@@NOTE_DOMAIN":
			if len(f) == 3 {
				addDomain(f[2], f[1])
			}
		case "@@NOTE_PORT":
			if len(f) == 3 && (f[1] == "tcp" || f[1] == "udp") && len(ports) < 256 {
				if len(f[2]) <= 256 && strings.LastIndex(f[2], ":") > 0 {
					ports[strings.ToUpper(f[1])+" "+clean(f[2], 256)] = true
				}
			}
		case "@@NOTE_WARNING":
			if len(f) == 2 && len(r.Warnings) < 8 {
				r.Warnings = append(r.Warnings, clean(f[1], 256))
			}
		case "@@NOTE_DONE":
			r.Complete = true
		}
	}
	if scan.Err() != nil {
		r.Complete = false
	}
	for p := range ports {
		r.Ports = append(r.Ports, p)
	}
	sort.Strings(r.Ports)
	sort.Slice(r.Domains, func(i, j int) bool { return r.Domains[i].Name < r.Domains[j].Name })
	return r
}

type Lookup func(context.Context, string) ([]net.IPAddr, error)

// DNS can verify discovered names, not enumerate every name pointing to an IP.
// No HTTP request, reverse-IP service, external API, or credential is involved.
func (r *Report) VerifyDNS(ctx context.Context, host string, lookup Lookup) {
	if lookup == nil {
		lookup = net.DefaultResolver.LookupIPAddr
	}
	known := map[netip.Addr]bool{}
	add := func(s string) {
		if p, e := netip.ParsePrefix(s); e == nil {
			s = p.Addr().String()
		}
		if p, e := netip.ParseAddr(s); e == nil && p.IsGlobalUnicast() {
			known[p.Unmap()] = true
		}
	}
	if p, e := netip.ParseAddr(host); e == nil {
		add(p.String())
	} else {
		probe, stop := context.WithTimeout(ctx, 2*time.Second)
		ips, err := lookup(probe, host)
		stop()
		if err == nil {
			for _, ip := range ips {
				add(ip.IP.String())
			}
		}
	}
	for _, iface := range r.Inventory.Interfaces {
		if !iface.Virtual {
			for _, ip := range iface.Addresses {
				add(ip)
			}
		}
	}
	jobs := make(chan int, len(r.Domains))
	for i := range r.Domains {
		jobs <- i
	}
	close(jobs)
	var wg sync.WaitGroup
	for worker := 0; worker < 4; worker++ {
		wg.Go(func() {
			for i := range jobs {
				d := &r.Domains[i]
				if strings.HasPrefix(d.Name, "*.") || strings.HasPrefix(d.Name, ".") {
					d.Status = "通配符配置，未核验"
					continue
				}
				if ctx.Err() != nil {
					d.Status = "核验超时或取消"
					continue
				}
				probe, stop := context.WithTimeout(ctx, 2*time.Second)
				ips, err := lookup(probe, d.Name)
				stop()
				if err != nil || len(ips) == 0 {
					d.Status = "解析未成功"
					continue
				}
				match := false
				seen := map[string]bool{}
				for _, ip := range ips {
					if a, ok := netip.AddrFromSlice(ip.IP); ok {
						a = a.Unmap()
						if known[a] {
							match = true
						}
						if !seen[a.String()] && len(d.IPs) < 8 {
							seen[a.String()] = true
							d.IPs = append(d.IPs, a.String())
						}
					}
				}
				sort.Strings(d.IPs)
				d.Status = "未匹配已知地址（可能经 CDN / 代理）"
				if match {
					d.Status = "匹配已知地址"
				}
				if len(known) == 0 {
					d.Status = "已解析，缺少可比较的服务器地址"
				}
			}
		})
	}
	wg.Wait()
}

func clean(value string, limit int) string {
	r := []rune(strings.Map(func(c rune) rune {
		if unicode.IsControl(c) || unicode.In(c, unicode.Cf) {
			return ' '
		}
		return c
	}, value))
	if len(r) > limit {
		r = append(r[:limit], '…')
	}
	return string(r)
}

// Details retains the evidence behind the short, purpose-first summary.
func (r Report) Details() string {
	var lines []string
	if r.Inventory.OS != "" {
		lines = append(lines, "系统："+clean(r.Inventory.OS, 160))
	}
	var services []string
	for _, s := range r.Inventory.Services {
		if s.Active == "active" || s.Sub == "running" {
			services = append(services, clean(s.Name, 128))
		}
	}
	if len(services) > 24 {
		services = append(services[:24], fmt.Sprintf("另 %d 项", len(services)-24))
	}
	if len(services) > 0 {
		lines = append(lines, "已激活的服务（含一次性任务，"+clean(r.Inventory.Manager, 40)+"）：\n"+strings.Join(services, "、"))
	} else {
		lines = append(lines, "服务：未读取到运行清单；不代表没有服务")
	}
	if r.Inventory.ServiceNote != "" {
		lines = append(lines, clean(r.Inventory.ServiceNote, 256))
	}
	if len(r.CustomServices) > 0 {
		names := make([]string, 0, len(r.CustomServices))
		for name := range r.CustomServices {
			names = append(names, name)
		}
		sort.Strings(names)
		var origins []string
		for _, name := range names[:min(len(names), 16)] {
			origins = append(origins, clean(name, 128)+" · "+clean(r.CustomServices[name], 256))
		}
		if len(names) > 16 {
			origins = append(origins, fmt.Sprintf("另 %d 项", len(names)-16))
		}
		lines = append(lines, "自定义服务配置：\n"+strings.Join(origins, "\n"))
	}
	if d := r.Inventory.Docker; d != nil && d.Status == "ready" {
		if len(d.Containers) == 0 {
			lines = append(lines, "Docker：已安装，未发现容器")
		}
		for i, c := range d.Containers {
			if i >= 24 {
				lines = append(lines, fmt.Sprintf("另 %d 个容器", len(d.Containers)-24))
				break
			}
			line := "容器：" + clean(c.Names, 100) + " · " + clean(c.State, 40) + " · " + clean(c.Image, 120)
			if c.Project != "" {
				line += " · Compose " + clean(c.Project, 80)
			}
			if c.Ports != "" {
				line += " · " + clean(c.Ports, 180)
			}
			lines = append(lines, line)
		}
	} else if d != nil && d.Status == "missing" {
		lines = append(lines, "Docker：未检测到")
	} else {
		lines = append(lines, "Docker：未读取到容器清单，可能无权限")
	}
	if len(r.Ports) > 0 {
		ports := r.Ports
		if len(ports) > 32 {
			ports = append(append([]string(nil), ports[:32]...), fmt.Sprintf("另 %d 个监听地址", len(ports)-32))
		}
		lines = append(lines, "监听地址（不代表公网已放行）：\n"+strings.Join(ports, "、"))
	} else {
		lines = append(lines, "监听地址：未读取到记录，可能未提供接口或无权限")
	}
	if len(r.Domains) > 0 {
		lines = append(lines, "域名（配置 / 连接地址发现，本机解析核验）：")
		for _, d := range r.Domains {
			line := d.Name + " · " + d.Status
			if len(d.IPs) > 0 {
				line += " · " + strings.Join(d.IPs, ", ")
			}
			line += "\n  来源：" + d.Source
			lines = append(lines, line)
		}
	} else {
		lines = append(lines, "域名：未发现；不能据此判断没有域名解析到这台服务器")
	}
	for _, w := range r.Warnings {
		lines = append(lines, "检测限制："+w)
	}
	lines = append(lines, "范围：当前账户可读取的服务、容器和常见静态 Nginx / OpenResty / Caddy 配置；未展开自定义 include 或容器内配置。摘要保留已识别应用、容器及自定义配置的服务；其他条目归入详情，不代表没有运行。每份配置最多读取 256 KiB，域名最多 32 个。DNS 无法按 IP 列出所有域名。")
	text := strings.Join(lines, "\n\n")
	if len(text) > 15500 {
		cut := text[:15000]
		for !utf8.ValidString(cut) {
			cut = cut[:len(cut)-1]
		}
		text = cut + "\n\n内容过多，概览已截短。"
	}
	return text
}
