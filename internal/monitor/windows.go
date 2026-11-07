package monitor

// Windows servers need a different probe and much lighter sampling: every
// PowerShell start costs real CPU on the server, so counters run at a tenth of
// the Linux rate and the inventory at a fifth. Nothing here installs a service
// or writes to the server.

import (
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
)

// PlatformProbe tells the broker which sampler to use. It is valid in both
// POSIX sh and cmd.exe: %OS% expands on Windows, $(uname -s) expands elsewhere,
// and neither shell treats the other's syntax as an error that would end the
// command.
const PlatformProbe = "echo @@PROBE %OS% $(uname -s)"

const windowsStatsScript = `$ErrorActionPreference = 'SilentlyContinue'
[Console]::OutputEncoding = [Text.Encoding]::UTF8
'@@HOST'
[Environment]::MachineName
'@@CPU'
$cpu = Get-CimInstance Win32_PerfFormattedData_PerfOS_Processor | Where-Object { $_.Name -eq '_Total' }
if (-not $cpu) { $cpu = Get-CimInstance Win32_Processor | Measure-Object -Property LoadPercentage -Average }
if ($cpu.PercentProcessorTime -ne $null) { $cpu.PercentProcessorTime } else { [int]$cpu.Average }
'@@MEM'
$os = Get-CimInstance Win32_OperatingSystem
$os.TotalVisibleMemorySize
$os.FreePhysicalMemory
$os.TotalVirtualMemorySize
$os.FreeVirtualMemory
'@@LOAD'
(Get-CimInstance Win32_Processor | Measure-Object -Property LoadPercentage -Average).Average
'@@CORES'
[Environment]::ProcessorCount
'@@UPTIME'
[int]((Get-Date) - $os.LastBootUpTime).TotalSeconds
'@@BOOTID'
(Get-CimInstance Win32_ComputerSystemProduct).UUID
'@@DISK'
Get-CimInstance Win32_LogicalDisk -Filter 'DriveType=3' | ForEach-Object {
  if ($_.Size -gt 0) { "$($_.DeviceID)|$($_.FileSystem)|$($_.Size)|$($_.FreeSpace)|$($_.VolumeName)" }
}
'@@PROCESS'
Get-Process | Sort-Object -Property WorkingSet64 -Descending | Select-Object -First 20 | ForEach-Object {
  "$($_.Id)|$($_.ProcessName)|$($_.WorkingSet64)|$([int64]$_.CPU)"
}
'@@NET'
Get-NetAdapterStatistics | ForEach-Object {
  "$($_.Name)|$($_.ReceivedBytes)|$($_.SentBytes)"
}
'@@END'
`

const windowsInventoryScript = `$ErrorActionPreference = 'SilentlyContinue'
[Console]::OutputEncoding = [Text.Encoding]::UTF8
'@@HOST'
[Environment]::MachineName
'@@OS'
(Get-CimInstance Win32_OperatingSystem).Caption
'@@KERNEL'
[Environment]::OSVersion.Version.ToString()
'@@ARCH'
$env:PROCESSOR_ARCHITECTURE
'@@MODEL'
(Get-CimInstance Win32_ComputerSystem).Model
'@@CPU_MODEL'
(Get-CimInstance Win32_Processor | Select-Object -First 1).Name
'@@CORES'
[Environment]::ProcessorCount
'@@SERVICES'
Get-Service | Sort-Object -Property Name | ForEach-Object {
  $start = $_.StartType
  if (-not $start) { $start = 'Unknown' }
  "$($_.Name)|$($_.Status)|$start"
}
'@@MANAGER'
Windows 服务管理器
'@@END'
`

// encodePowerShell wraps a script so no quoting, percent or ampersand can be
// changed by the login shell between the broker and PowerShell.
func encodePowerShell(script string) string {
	units := utf16.Encode([]rune(script))
	buf := make([]byte, 0, len(units)*2)
	for _, u := range units {
		buf = append(buf, byte(u), byte(u>>8))
	}
	return "powershell -NoProfile -NonInteractive -EncodedCommand " + base64.StdEncoding.EncodeToString(buf)
}

var (
	// WindowsStatsCommand samples counters; the broker runs it every 10 s.
	WindowsStatsCommand = encodePowerShell(windowsStatsScript)
	// WindowsInventoryCommand reads system facts and services; every 5 min.
	WindowsInventoryCommand = encodePowerShell(windowsInventoryScript)
	// WindowsProbeCommand is the second identity probe for servers whose login
	// shell is PowerShell, where %OS% never expands.
	WindowsProbeCommand = encodePowerShell(`$PSVersionTable.PSVersion.Major`)
)

// ParseWindows turns the marker output into the same Stats the desktop already
// renders. Rates come from the previous sample, as on Linux.
func ParseWindows(text string, previous *Stats) (Stats, error) {
	s := Stats{Time: time.Now().UTC()}
	var memLines []string
	section := ""
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, "@@") {
			section = line
			continue
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		f := strings.Split(line, "|")
		switch section {
		case "@@CPU":
			if v, err := strconv.ParseFloat(strings.TrimSpace(line), 64); err == nil {
				s.CPU = v
				s.CPUDetail = CPUUsage{Used: v, Valid: true}
			}
		case "@@MEM":
			memLines = append(memLines, strings.TrimSpace(line))
		case "@@LOAD":
			if v, err := strconv.ParseFloat(strings.TrimSpace(line), 64); err == nil {
				s.Load = strconv.FormatFloat(v, 'f', 0, 64) + "%"
			}
		case "@@CORES":
			n, _ := strconv.Atoi(strings.TrimSpace(line))
			s.Cores = make([]CPUUsage, n)
		case "@@UPTIME":
			s.Uptime, _ = strconv.ParseFloat(strings.TrimSpace(line), 64)
		case "@@BOOTID":
			s.BootID = strings.TrimSpace(line)
		case "@@DISK":
			if len(f) >= 4 {
				total, _ := strconv.ParseUint(f[2], 10, 64)
				free, _ := strconv.ParseUint(f[3], 10, 64)
				if total > 0 {
					d := Disk{Device: f[0], Mount: f[0], Filesystem: f[1], Total: total, Free: free}
					d.Used = total - min(free, total)
					if len(f) >= 5 && f[4] != "" {
						d.Mount = f[0] + " · " + f[4]
					}
					s.Disks = append(s.Disks, d)
				}
			}
		case "@@PROCESS":
			if len(f) >= 4 {
				pid, err := strconv.Atoi(f[0])
				if err != nil {
					continue
				}
				rss, _ := strconv.ParseUint(f[2], 10, 64)
				seconds, _ := strconv.ParseFloat(f[3], 64)
				s.Processes = append(s.Processes, Process{PID: pid, Name: f[1], RSS: rss, cpuSeconds: seconds})
			}
		case "@@NET":
			if len(f) >= 3 {
				rx, _ := strconv.ParseUint(f[1], 10, 64)
				tx, _ := strconv.ParseUint(f[2], 10, 64)
				s.RX += rx
				s.TX += tx
				n := Network{Name: f[0], RX: rx, TX: tx, Valid: true}
				if previous != nil {
					for _, old := range previous.Networks {
						if old.Name != n.Name {
							continue
						}
						if elapsed := s.Time.Sub(previous.Time).Seconds(); elapsed > 0 {
							if rx >= old.RX {
								n.RXRate = float64(rx-old.RX) / elapsed
							}
							if tx >= old.TX {
								n.TXRate = float64(tx-old.TX) / elapsed
							}
						}
						break
					}
				}
				s.Networks = append(s.Networks, n)
			}
		}
	}
	if len(memLines) >= 2 {
		totalKB, _ := strconv.ParseUint(memLines[0], 10, 64)
		freeKB, _ := strconv.ParseUint(memLines[1], 10, 64)
		s.MemoryTotal = totalKB * 1024
		s.MemoryFree = freeKB * 1024
		s.MemoryAvailable = min(s.MemoryFree, s.MemoryTotal)
		s.MemoryUsed = s.MemoryTotal - s.MemoryAvailable
		if len(memLines) >= 4 {
			swapTotal, _ := strconv.ParseUint(memLines[2], 10, 64)
			swapFree, _ := strconv.ParseUint(memLines[3], 10, 64)
			s.SwapTotal = swapTotal * 1024
			if swapTotal >= swapFree {
				s.SwapUsed = (swapTotal - swapFree) * 1024
			}
		}
	}
	if len(s.Cores) == 0 && s.CPUDetail.Used > 0 {
		s.Cores = []CPUUsage{s.CPUDetail}
	}
	for i := range s.Cores {
		s.Cores[i] = CPUUsage{Used: s.CPU, Valid: true}
	}
	s.ProcessCount = len(s.Processes)
	s.RunningProcesses = len(s.Processes)
	if s.MemoryTotal == 0 && s.CPU == 0 && len(s.Processes) == 0 {
		return s, errors.New("未收到有效 Windows 监控数据")
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
			cores := float64(max(1, len(s.Cores)))
			for i := range s.Processes {
				for _, old := range previous.Processes {
					if old.PID != s.Processes[i].PID {
						continue
					}
					if delta := s.Processes[i].cpuSeconds - old.cpuSeconds; delta >= 0 {
						s.Processes[i].CPU = delta / elapsed * 100 / cores
					}
					break
				}
			}
		}
	}
	return s, nil
}

// ParseWindowsInventory fills the system page and the service list.
func ParseWindowsInventory(text string, started time.Time) Inventory {
	i := Inventory{Time: started, Manager: "Windows 服务管理器"}
	section := ""
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, "@@") {
			section = line
			continue
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		switch section {
		case "@@HOST":
			i.Hostname = strings.TrimSpace(line)
		case "@@OS":
			i.OS = strings.TrimSpace(line)
		case "@@KERNEL":
			i.Kernel = strings.TrimSpace(line)
		case "@@ARCH":
			i.Architecture = strings.TrimSpace(line)
		case "@@MODEL":
			i.Model = strings.TrimSpace(line)
		case "@@CPU_MODEL":
			i.CPU = strings.TrimSpace(line)
		case "@@CORES":
			i.Cores, _ = strconv.Atoi(strings.TrimSpace(line))
		case "@@MANAGER":
			i.Manager = strings.TrimSpace(line)
		case "@@SERVICES":
			f := strings.Split(line, "|")
			if len(f) >= 3 {
				i.Services = append(i.Services, Service{Name: f[0], Active: strings.ToLower(f[1]), Enabled: startTypeLabel(f[2])})
			}
		}
	}
	if i.Manager == "" {
		i.Manager = "Windows 服务管理器"
	}
	if len(i.Services) > 1024 {
		i.Services = i.Services[:1024]
		i.ServiceNote = "服务列表已截断为前 1024 项"
	}
	return i
}

func startTypeLabel(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "automatic":
		return "enabled"
	case "manual":
		return "manual"
	case "disabled":
		return "disabled"
	}
	return strings.ToLower(strings.TrimSpace(value))
}

// WindowsPlatform reports whether the probe output came from Windows.
func WindowsPlatform(probeOutput string) bool {
	return strings.Contains(probeOutput, "Windows_NT")
}
