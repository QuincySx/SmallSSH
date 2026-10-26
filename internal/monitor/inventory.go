package monitor

import (
	"bufio"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Fixed read-only probes, independent of names or values supplied by a caller.
// Slow inventory is separate from the two-second counters and has its own timeout.
const InventoryCommand = `LC_ALL=C; export LC_ALL
printf '\n@@HOST\n'; uname -n
printf '\n@@KERNEL\n'; uname -r
printf '\n@@ARCH\n'; uname -m
printf '\n@@OS\n'; sed -n 's/^PRETTY_NAME=//p' /etc/os-release 2>/dev/null
printf '\n@@MODEL\n'; cat /sys/devices/virtual/dmi/id/product_name 2>/dev/null
printf '\n@@CPU_MODEL\n'; awk -F: '/^(model name|Hardware)[[:space:]]*:/ {sub(/^[[:space:]]+/, "", $2); print $2; exit}' /proc/cpuinfo
printf '\n@@CORES\n'; getconf _NPROCESSORS_ONLN
if command -v systemctl >/dev/null 2>&1; then
  printf '\n@@SERVICE_FILES\n'
  systemctl list-unit-files --type=service --no-pager --no-legend --full 2>/dev/null | head -n 1025
  printf '\n@@SERVICE_UNITS\n'
  systemctl list-units --type=service --all --plain --no-pager --no-legend --full 2>/dev/null | head -n 2048
  printf '\n@@SERVICE_MANAGER\nsystemd\n'
elif command -v rc-status >/dev/null 2>&1; then
  printf '\n@@SERVICE_RC\n'
  for entry in /etc/runlevels/default/* /etc/runlevels/boot/*; do
    [ -e "$entry" ] || continue
    printf '%s\n' "${entry##*/}"
  done
  printf '\n@@SERVICE_MANAGER\nOpenRC\n'
else
  printf '\n@@SERVICE_RC\n'
  for entry in /etc/rc[2-5].d/S*; do
    [ -e "$entry" ] || continue
    name=${entry##*/}; printf '%s\n' "${name#???}"
  done
  printf '\n@@SERVICE_MANAGER\nSysV / unknown\n'
fi
` + InterfaceCommand + DockerCommand

type Service struct {
	Name        string `json:"name"`
	Enabled     string `json:"enabled"`
	Active      string `json:"active"`
	Sub         string `json:"sub"`
	Description string `json:"description"`
}

type Inventory struct {
	Time                                           time.Time `json:"time"`
	Hostname, OS, Kernel, Architecture, Model, CPU string
	Cores                                          int         `json:"cores"`
	Manager                                        string      `json:"manager"`
	Services                                       []Service   `json:"services"`
	ServiceNote                                    string      `json:"serviceNote,omitempty"`
	Docker                                         *DockerInfo `json:"docker,omitempty"`
	Interfaces                                     []Interface `json:"interfaces,omitempty"`
}

func ParseInventory(text string) Inventory {
	i := Inventory{Time: time.Now().UTC()}
	section := ""
	services := map[string]Service{}
	states := map[string]Service{}
	interfaces := map[string]*Interface{}
	scan := bufio.NewScanner(strings.NewReader(text))
	scan.Buffer(make([]byte, 4096), 128<<10)
	for scan.Scan() {
		line := strings.TrimSpace(scan.Text())
		if strings.HasPrefix(line, "@@") {
			section = line
			continue
		}
		if line == "" {
			continue
		}
		if strings.HasPrefix(section, "@@DOCKER_") {
			if i.Docker == nil {
				i.Docker = &DockerInfo{Status: "checking"}
			}
			i.Docker.parse(section, line)
			continue
		}
		if section == "@@INTERFACES" || section == "@@ADDRESSES" {
			parseInterfaceLine(section, line, interfaces)
			continue
		}
		switch section {
		case "@@HOST":
			i.Hostname = line
		case "@@KERNEL":
			i.Kernel = line
		case "@@ARCH":
			i.Architecture = line
		case "@@OS":
			i.OS = strings.Trim(line, "\"'")
		case "@@MODEL":
			i.Model = line
		case "@@CPU_MODEL":
			i.CPU = line
		case "@@CORES":
			i.Cores, _ = strconv.Atoi(line)
		case "@@SERVICE_MANAGER":
			i.Manager = line
		case "@@SERVICE_FILES":
			f := strings.Fields(line)
			if len(f) < 2 || !strings.HasSuffix(f[0], ".service") {
				continue
			}
			if len(services) >= 1024 {
				i.ServiceNote = "只显示前 1024 个服务"
				continue
			}
			services[f[0]] = Service{Name: f[0], Enabled: f[1], Active: "unknown"}
		case "@@SERVICE_UNITS":
			f := strings.Fields(line)
			if len(f) >= 4 && strings.HasSuffix(f[0], ".service") {
				states[f[0]] = Service{Active: f[2], Sub: f[3], Description: strings.Join(f[4:], " ")}
			}
		case "@@SERVICE_RC":
			if len(services) < 1024 {
				services[line] = Service{Name: line, Enabled: "enabled", Active: "unknown"}
			}
		}
	}
	if scan.Err() != nil {
		i.ServiceNote = "服务清单读取不完整"
		if i.Docker != nil {
			i.Docker.Note = "Docker 清单读取不完整"
		}
	}
	for name, s := range services {
		if state, ok := states[name]; ok {
			s.Active = state.Active
			s.Sub = state.Sub
			s.Description = state.Description
		}
		i.Services = append(i.Services, s)
	}
	sort.Slice(i.Services, func(a, b int) bool { return i.Services[a].Name < i.Services[b].Name })
	for _, network := range interfaces {
		i.Interfaces = append(i.Interfaces, *network)
	}
	sort.Slice(i.Interfaces, func(a, b int) bool { return i.Interfaces[a].Name < i.Interfaces[b].Name })
	if len(i.Services) == 0 && i.ServiceNote == "" {
		i.ServiceNote = "未发现可读取的服务；可能没有配置、缺少系统接口或当前账户权限不足"
	}
	return i
}
