package monitor

import (
	"strings"
	"testing"
	"time"
)

const windowsSample = `@@HOST
win-host
@@CPU
12
@@MEM
8188
4094
10240
8192
@@LOAD
15
@@CORES
4
@@UPTIME
90310
@@BOOTID
4C4C4544-0000-0000-0000-000000000000
@@DISK
C:|NTFS|511305|204522|系统
D:|NTFS|1048576|524288|
@@PROCESS
4|System|143360|120
812|explorer|204800|42
@@NET
Ethernet|1048576|524288
@@END
`

const windowsOlderSample = `@@HOST
win-host
@@CPU
30
@@MEM
8188
2048
10240
4096
@@LOAD
20
@@CORES
4
@@UPTIME
90300
@@BOOTID
4C4C4544-0000-0000-0000-000000000000
@@DISK
C:|NTFS|511305|204522|系统
@@PROCESS
4|System|143360|100
812|explorer|204800|32
@@NET
Ethernet|524288|262144
@@END
`

func TestParseWindowsMapsCountersAndRates(t *testing.T) {
	previous, err := ParseWindows(windowsOlderSample, nil)
	if err != nil {
		t.Fatal(err)
	}
	previous.Time = previous.Time.Add(-10 * time.Second)
	stats, err := ParseWindows(windowsSample, &previous)
	if err != nil {
		t.Fatal(err)
	}
	if stats.CPU != 12 || stats.Load != "15%" || len(stats.Cores) != 4 {
		t.Fatal("cpu fields", stats.CPU, stats.Load, len(stats.Cores))
	}
	if stats.MemoryTotal != 8188*1024 || stats.MemoryUsed != (8188-4094)*1024 || stats.MemoryFree != 4094*1024 {
		t.Fatal("memory fields", stats.MemoryTotal, stats.MemoryUsed, stats.MemoryFree)
	}
	if len(stats.Disks) != 2 || stats.Disks[0].Mount != "C: · 系统" || stats.Disks[0].Used != (511305-204522) {
		t.Fatal("disk fields", stats.Disks)
	}
	if len(stats.Processes) != 2 || stats.Processes[0].Name != "System" || stats.Processes[0].RSS != 143360 {
		t.Fatal("process fields", stats.Processes)
	}
	// 20 CPU-seconds over 10 s on 4 cores is 50%.
	if got := stats.Processes[0].CPU; got < 49.9 || got > 50.1 {
		t.Fatal("process cpu rate", got)
	}
	if stats.Uptime != 90310 || stats.BootID == "" || stats.ProcessCount != 2 {
		t.Fatal("uptime/boot/process count", stats.Uptime, stats.BootID, stats.ProcessCount)
	}
	if stats.RX != 1048576 || stats.RXPerSecond < 52000 || stats.RXPerSecond > 53000 {
		t.Fatal("network totals or rate", stats.RX, stats.RXPerSecond)
	}
	if len(stats.Networks) != 1 || stats.Networks[0].RXRate <= 0 {
		t.Fatal("network rows", stats.Networks)
	}
}

func TestParseWindowsRejectsAnEmptySample(t *testing.T) {
	if _, err := ParseWindows("@@END\n", nil); err == nil {
		t.Fatal("empty sample accepted")
	}
}

func TestParseWindowsInventoryReadsServicesAndIdentity(t *testing.T) {
	text := strings.Join([]string{
		"@@HOST", "win-host",
		"@@OS", "Microsoft Windows Server 2022 Standard",
		"@@KERNEL", "10.0.20348.0",
		"@@ARCH", "AMD64",
		"@@MODEL", "PowerEdge R650",
		"@@CPU_MODEL", "Intel(R) Xeon(R) Gold 6338 CPU @ 2.00GHz",
		"@@CORES", "16",
		"@@SERVICES", "Spooler|Stopped|Automatic", "W32Time|Running|Manual", "wuauserv|Running|Disabled",
		"@@MANAGER", "Windows 服务管理器",
		"@@END",
	}, "\n")
	info := ParseWindowsInventory(text, time.Now())
	if info.Hostname != "win-host" || info.Cores != 16 || info.Manager != "Windows 服务管理器" {
		t.Fatal("identity", info)
	}
	if !strings.Contains(info.OS, "Windows Server 2022") || info.Architecture != "AMD64" || info.Model != "PowerEdge R650" {
		t.Fatal("system facts", info)
	}
	if len(info.Services) != 3 {
		t.Fatal("services", info.Services)
	}
	if info.Services[0].Active != "stopped" || info.Services[0].Enabled != "enabled" {
		t.Fatal("service state or start type", info.Services[0])
	}
	if info.Services[2].Enabled != "disabled" || info.Services[1].Enabled != "manual" {
		t.Fatal("start type mapping", info.Services[1], info.Services[2])
	}
}

func TestPlatformProbeRecognizesWindowsAndLinux(t *testing.T) {
	if !WindowsPlatform("@@PROBE Windows_NT $(uname -s)\r\n") {
		t.Fatal("cmd.exe probe not recognized")
	}
	if WindowsPlatform("@@PROBE %OS% Linux\n") {
		t.Fatal("linux probe treated as Windows")
	}
	if WindowsPlatform("") {
		t.Fatal("empty probe treated as Windows")
	}
}

func TestEncodedPowerShellCarriesNoShellMetacharacters(t *testing.T) {
	command := encodePowerShell(`Get-CimInstance Win32_LogicalDisk -Filter 'DriveType=3' | ForEach-Object { "$($_.Size)" }`)
	if !strings.HasPrefix(command, "powershell -NoProfile -NonInteractive -EncodedCommand ") {
		t.Fatal("unexpected command shape", command)
	}
	payload := strings.TrimPrefix(command, "powershell -NoProfile -NonInteractive -EncodedCommand ")
	for _, bad := range []string{"|", "&", ";", "%", "\"", "$", " "} {
		if strings.Contains(payload, bad) {
			t.Fatalf("payload carries %q", bad)
		}
	}
}
