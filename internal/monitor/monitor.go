// Package monitor parses fixed Linux sampling commands with no interpolated input.
package monitor

import (
	"bufio"
	"errors"
	"strconv"
	"strings"
	"time"
)

const Command = `LC_ALL=C; export LC_ALL
if [ ! -r /proc/stat ]; then printf 'Linux /proc is required\n' >&2; exit 69; fi
printf '\n@@CPU\n'; sed -n '/^cpu/p' /proc/stat
printf '\n@@MEM\n'; cat /proc/meminfo
printf '\n@@NET\n'; cat /proc/net/dev
printf '\n@@DEFAULTNET\n'; awk '$2 == "00000000" {print $1; exit}' /proc/net/route
printf '\n@@TCP\n'; sed -n '/^Tcp:/p' /proc/net/snmp
printf '\n@@LOAD\n'; cat /proc/loadavg
printf '\n@@DISK\n'; if ! df -PTk 2>/dev/null; then df -Pk 2>/dev/null; fi
printf '\n@@MOUNT_FLAGS\n'; cat /proc/mounts
printf '\n@@BLOCK\n'
for block in /sys/block/*; do
  name=${block##*/}
  case "$name" in loop*|ram*|fd*|sr*) continue;; esac
  set -- "$block"/slaves/*
  [ -e "$1" ] && continue
  [ -r "$block/stat" ] || continue
  printf '%s ' "$name"; cat "$block/stat"
done
printf '\n@@UPTIME\n'; cat /proc/uptime
process_rows=$(ps -eo pid,pcpu,pmem,rss,user,stat,etime,comm --sort=-pcpu | head -n 33
ps -eo pid,pcpu,pmem,rss,user,stat,etime,comm --sort=-rss | head -n 33)
printf '\n@@PROCESS_DETAIL\n%s\n' "$process_rows"
printf '\n@@BOOT_ID\n'; cat /proc/sys/kernel/random/boot_id
printf '\n@@PROCESS_IDENTITY\n'
printf '%s\n' "$process_rows" | awk '$1 ~ /^[0-9]+$/ && !seen[$1]++ {print $1}' | while IFS= read -r pid; do
 [ -r "/proc/$pid/stat" ] || continue
 IFS= read -r stat_line < "/proc/$pid/stat" || continue
 rest=${stat_line##*) }; set -- $rest
 [ "$#" -ge 20 ] || continue
 shift 19
 printf '%s %s\n' "$pid" "$1"
done
true
`

type Disk struct {
	Device     string `json:"device"`
	Mount      string `json:"mount"`
	Total      uint64 `json:"total"`
	Used       uint64 `json:"used"`
	Free       uint64 `json:"free"`
	Filesystem string `json:"filesystem,omitempty"`
	ReadOnly   bool   `json:"readOnly,omitempty"`
	ModeKnown  bool   `json:"modeKnown,omitempty"`
}
type Process struct {
	PID        int     `json:"pid"`
	CPU        float64 `json:"cpu"`
	Memory     float64 `json:"memory"`
	Name       string  `json:"name"`
	RSS        uint64  `json:"rss"`
	User       string  `json:"user,omitempty"`
	State      string  `json:"state,omitempty"`
	Elapsed    string  `json:"elapsed,omitempty"`
	StartTicks uint64  `json:"startTicks,omitempty"`
}
type Stats struct {
	SourceID         string          `json:"sourceId,omitempty"`
	Time             time.Time       `json:"time"`
	CPU              float64         `json:"cpu"`
	MemoryTotal      uint64          `json:"memoryTotal"`
	MemoryUsed       uint64          `json:"memoryUsed"`
	RX               uint64          `json:"rx"`
	TX               uint64          `json:"tx"`
	RXPerSecond      float64         `json:"rxPerSecond"`
	TXPerSecond      float64         `json:"txPerSecond"`
	Load             string          `json:"load"`
	Disks            []Disk          `json:"disks"`
	Processes        []Process       `json:"processes"`
	Uptime           float64         `json:"uptime"`
	Inventory        *Inventory      `json:"inventory,omitempty"`
	CPUDetail        CPUUsage        `json:"cpuDetail"`
	Cores            []CPUUsage      `json:"cores,omitempty"`
	MemoryAvailable  uint64          `json:"memoryAvailable"`
	MemoryFree       uint64          `json:"memoryFree"`
	MemoryCached     uint64          `json:"memoryCached"`
	MemoryBuffers    uint64          `json:"memoryBuffers"`
	SwapTotal        uint64          `json:"swapTotal"`
	SwapUsed         uint64          `json:"swapUsed"`
	Networks         []Network       `json:"networks,omitempty"`
	DefaultNetwork   string          `json:"defaultNetwork,omitempty"`
	BlockIO          []BlockIO       `json:"blockIO,omitempty"`
	ProcessCount     int             `json:"processCount"`
	ProcessNote      string          `json:"processNote,omitempty"`
	BootID           string          `json:"bootId,omitempty"`
	TCP              TCPInfo         `json:"tcp"`
	DockerStats      *DockerSnapshot `json:"dockerStats,omitempty"`
	RunningProcesses int             `json:"runningProcesses"`
	total            uint64
}

func num(s string) uint64 { n, _ := strconv.ParseUint(s, 10, 64); return n }
func Parse(text string, previous *Stats) (Stats, error) {
	s := Stats{Time: time.Now().UTC()}
	section := ""
	mem := map[string]uint64{}
	processes := map[int]bool{}
	identities := map[int]uint64{}
	var tcpHeaders []string
	mounts := map[string]bool{}
	mountTypes := map[string]string{}
	scan := bufio.NewScanner(strings.NewReader(text))
	scan.Buffer(make([]byte, 4096), 128<<10)
	for scan.Scan() {
		line := scan.Text()
		if strings.HasPrefix(line, "@@") {
			section = line
			continue
		}
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		switch section {
		case "@@CPU":
			if !strings.HasPrefix(f[0], "cpu") || len(f) < 5 {
				continue
			}
			usage := parseCPU(f, previous)
			if f[0] != "cpu" {
				s.Cores = append(s.Cores, usage)
				continue
			}
			s.CPUDetail = usage
			s.CPU = usage.Used
			for _, v := range f[1:min(len(f), 9)] {
				n := num(v)
				s.total += n
			}
		case "@@MEM":
			if len(f) >= 2 {
				mem[strings.TrimSuffix(f[0], ":")] = num(f[1]) * 1024
			}
		case "@@NET":
			name, rest, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			v := strings.Fields(rest)
			if len(v) >= 16 {
				if strings.TrimSpace(name) != "lo" {
					s.RX += num(v[0])
					s.TX += num(v[8])
				}
				s.Networks = append(s.Networks, parseNetwork(strings.TrimSpace(name), v, previous, s.Time))
			}
		case "@@DEFAULTNET":
			s.DefaultNetwork = f[0]
		case "@@LOAD":
			if len(f) >= 3 {
				s.Load = strings.Join(f[:3], " / ")
			}
			if len(f) >= 4 {
				running, total, _ := strings.Cut(f[3], "/")
				s.RunningProcesses, _ = strconv.Atoi(running)
				s.ProcessCount, _ = strconv.Atoi(total)
			}
		case "@@DISK":
			offset, fs := 0, ""
			if len(f) >= 7 && num(f[1]) == 0 {
				offset, fs = 1, f[1]
			}
			if len(f) >= 6+offset && num(f[1+offset]) > 0 {
				s.Disks = append(s.Disks, Disk{Device: f[0], Mount: strings.Join(f[5+offset:], " "), Total: num(f[1+offset]) * 1024, Used: num(f[2+offset]) * 1024, Free: num(f[3+offset]) * 1024, Filesystem: fs})
			}
		case "@@BLOCK":
			if len(f) >= 12 {
				s.BlockIO = append(s.BlockIO, parseBlock(f, previous, s.Time))
			}
		case "@@MOUNT_FLAGS":
			if len(f) >= 4 {
				mount := decodeMount(f[1])
				readonly := false
				for _, flag := range strings.Split(f[3], ",") {
					if flag == "ro" {
						readonly = true
					}
				}
				mounts[mount] = readonly
				mountTypes[mount] = f[2]
			}
		case "@@PROCESS", "@@PROCESS_DETAIL":
			if len(f) >= 4 {
				pid, e := strconv.Atoi(f[0])
				if e == nil && !processes[pid] {
					processes[pid] = true
					cpu, _ := strconv.ParseFloat(f[1], 64)
					memory, _ := strconv.ParseFloat(f[2], 64)
					p := Process{PID: pid, CPU: cpu, Memory: memory, Name: strings.Join(f[3:], " ")}
					if len(f) >= 5 {
						if rss, e := strconv.ParseUint(f[3], 10, 64); e == nil {
							p.RSS = rss * 1024
							p.Name = strings.Join(f[4:], " ")
						}
					}
					if section == "@@PROCESS_DETAIL" && len(f) >= 8 {
						p.User, p.State, p.Elapsed, p.Name = f[4], f[5], f[6], strings.Join(f[7:], " ")
					}
					s.Processes = append(s.Processes, p)
				}
			}
		case "@@UPTIME":
			s.Uptime, _ = strconv.ParseFloat(f[0], 64)
		case "@@BOOT_ID":
			s.BootID = f[0]
		case "@@PROCESS_IDENTITY":
			if len(f) == 2 {
				pid, err := strconv.Atoi(f[0])
				if err == nil {
					identities[pid] = num(f[1])
				}
			}
		case "@@TCP":
			if len(f) > 1 && f[0] == "Tcp:" {
				if f[1] == "RtoAlgorithm" {
					tcpHeaders = append([]string(nil), f[1:]...)
				} else if len(f)-1 == len(tcpHeaders) {
					s.TCP = parseTCP(tcpHeaders, f[1:], previous)
				}
			}
		}
	}
	if e := scan.Err(); e != nil {
		return s, e
	}
	for n := range s.Processes {
		s.Processes[n].StartTicks = identities[s.Processes[n].PID]
	}
	if len(s.Processes) == 0 && s.ProcessCount > 0 {
		s.ProcessNote = "当前系统未提供兼容的进程清单，可能使用精简版 ps"
	}
	for n := range s.Disks {
		d := &s.Disks[n]
		if d.Filesystem == "" {
			d.Filesystem = mountTypes[d.Mount]
		}
		if ro, ok := mounts[d.Mount]; ok {
			d.ReadOnly, d.ModeKnown = ro, true
		} else if d.Filesystem == "squashfs" || d.Filesystem == "iso9660" {
			d.ReadOnly, d.ModeKnown = true, true
		}
	}
	s.MemoryTotal = mem["MemTotal"]
	s.MemoryFree, s.MemoryBuffers = mem["MemFree"], mem["Buffers"]
	s.MemoryCached = subtract(mem["Cached"]+mem["SReclaimable"], mem["Shmem"])
	s.SwapTotal, s.SwapUsed = mem["SwapTotal"], subtract(mem["SwapTotal"], mem["SwapFree"])
	available, ok := mem["MemAvailable"]
	if !ok {
		available = mem["MemFree"] + mem["Buffers"] + mem["Cached"]
	}
	if available <= s.MemoryTotal {
		s.MemoryUsed = s.MemoryTotal - available
	}
	s.MemoryAvailable = min(available, s.MemoryTotal)
	if s.total == 0 || s.MemoryTotal == 0 {
		return s, errors.New("未收到有效 Linux 监控数据")
	}
	if previous != nil {
		elapsed := s.Time.Sub(previous.Time).Seconds()
		if elapsed > 0 {
			if s.RX >= previous.RX {
				s.RXPerSecond = float64(s.RX-previous.RX) / elapsed
			}
			if s.TX >= previous.TX {
				s.TXPerSecond = float64(s.TX-previous.TX) / elapsed
			}
		}
	}
	return s, nil
}
