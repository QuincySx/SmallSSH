package monitor

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

const windowsSample = `@@HOST
win-host
@@CPU
12|8|4
@@MEM
8188
4094
10240
8192
@@LOAD
15
@@CORES
4
@@CORE_USAGE
0|2|1|1
1|10|6|4
2|20|15|5
3|16|10|6
@@PROCESS_COUNT
137
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
30|20|10
@@MEM
8188
2048
10240
4096
@@LOAD
20
@@CORES
4
@@CORE_USAGE
0|2|1|1
1|10|6|4
2|20|15|5
3|16|10|6
@@PROCESS_COUNT
137
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
	if stats.CPU != 12 || stats.Load != "" || len(stats.Cores) != 4 {
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
	if stats.Uptime != 90310 || stats.BootID == "" || stats.ProcessCount != 137 || stats.RunningProcesses != -1 {
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

func TestParseWindowsRejectsInvalidCoreCounts(t *testing.T) {
	for _, count := range []string{"-1", "0", "4097", "9223372036854775807", "999999999999999999999999999", "not-a-number"} {
		t.Run(count, func(t *testing.T) {
			sample := strings.Replace(windowsSample, "@@CORES\n4\n", "@@CORES\n"+count+"\n", 1)
			if _, err := ParseWindows(sample, nil); err == nil {
				t.Fatal("accepted invalid core count", count)
			}
		})
	}
}

func TestWindowsMetricsKeepActualCoreSamplesAndTotalCount(t *testing.T) {
	stats, err := ParseWindows(windowsSample, nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats.CPUDetail.User != 8 || stats.CPUDetail.System != 4 {
		t.Fatal("aggregate CPU breakdown lost", stats.CPUDetail)
	}
	for i, want := range []float64{2, 10, 20, 16} {
		if got := stats.Cores[i]; !got.Valid || got.Name != "cpu"+strconv.Itoa(i) || got.Used != want {
			t.Fatal("core sample was copied from aggregate or dropped", i, got)
		}
	}
	if stats.ProcessCount != 137 || len(stats.Processes) != 2 || stats.RunningProcesses != -1 {
		t.Fatal("process totals were inferred from displayed rows", stats.ProcessCount, stats.RunningProcesses)
	}
}

func TestWindowsUnavailableCountersAreNotInvented(t *testing.T) {
	sample := "@@CPU\n42\n@@CORES\n4\n@@MEM\n8188\n4094\n@@PROCESS\n812|explorer|204800|42\n@@END\n"
	stats, err := ParseWindows(sample, nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats.CPUDetail.Valid || stats.ProcessCount != -1 || stats.RunningProcesses != -1 {
		t.Fatal("unavailable measurements were given valid values", stats.CPUDetail, stats.ProcessCount, stats.RunningProcesses)
	}
	if len(stats.Cores) != 4 {
		t.Fatal("lost known logical core count")
	}
	for _, core := range stats.Cores {
		if core.Valid {
			t.Fatal("missing core measurement copied aggregate", core)
		}
	}
	if _, err := ParseWindows("@@CPU\n0|0|0\n@@END\n", nil); err != nil {
		t.Fatal("a measured zero CPU sample was rejected", err)
	}
}

func TestWindowsCoreCountersBoundInvalidAndNonFiniteSamples(t *testing.T) {
	sample := "@@CPU\nNaN|0|0\n@@CORES\n2\n@@CORE_USAGE\n0|Inf|0|0\n1|150|110|-1\n4096|10|5|5\n-1|20|10|10\n@@MEM\n8188\n4094\n@@PROCESS_COUNT\ninvalid\n@@END\n"
	stats, err := ParseWindows(sample, nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats.CPUDetail.Valid || len(stats.Cores) != 2 || stats.Cores[0].Valid {
		t.Fatal("accepted invalid or unbounded counter", stats.CPUDetail, stats.Cores)
	}
	if got := stats.Cores[1]; !got.Valid || got.Used != 100 || got.User != 100 || got.System != 0 {
		t.Fatal("finite counter bounds were not enforced", got)
	}
	if stats.ProcessCount != -1 {
		t.Fatal("invalid process count accepted")
	}
}

func TestWindowsProbeCountsProcessesBeforeSelectingVisibleRows(t *testing.T) {
	count := strings.Index(windowsStatsScript, "$processes.Count")
	rows := strings.Index(windowsStatsScript, "$processes | Sort-Object")
	if count < 0 || rows < count || !strings.Contains(windowsStatsScript, "PercentUserTime,PercentPrivilegedTime") {
		t.Fatal("probe omitted real total or core breakdown counters")
	}
}

func TestWindowsSampleAvailabilityAndNetworkBaselines(t *testing.T) {
	first, err := ParseWindows(windowsOlderSample, nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.SampleIntervalSeconds != 10 || !first.CPUDetail.BasicOnly || !first.Cores[0].BasicOnly {
		t.Fatal("Windows sampling metadata missing", first.SampleIntervalSeconds, first.CPUDetail)
	}
	if len(first.Networks) != 1 || first.Networks[0].Valid {
		t.Fatal("first network counters were presented as a measured rate", first.Networks)
	}
	first.Time = time.Now().Add(-10 * time.Second)
	second, err := ParseWindows(windowsSample, &first)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Networks[0].Valid || second.Networks[0].RXRate <= 0 || second.Networks[0].TXRate <= 0 {
		t.Fatal("two valid baselines did not produce a rate", second.Networks)
	}
	second.Time = time.Now().Add(-10 * time.Second)
	reset := strings.Replace(windowsSample, "Ethernet|1048576|524288", "Ethernet|1|1048576", 1)
	third, err := ParseWindows(reset, &second)
	if err != nil {
		t.Fatal(err)
	}
	if third.Networks[0].Valid || third.Networks[0].RXRate != 0 || third.Networks[0].TXRate != 0 {
		t.Fatal("counter rollback was shown as a real network rate", third.Networks)
	}
}
