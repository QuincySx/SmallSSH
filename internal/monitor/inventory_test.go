package monitor

import (
	"fmt"
	"strings"
	"testing"
)

func TestInventorySeparatesStartupEnablementFromRuntimeAndKeepsDisabled(t *testing.T) {
	i := ParseInventory(`@@HOST
tiny-vps
@@ARCH
aarch64
@@MODEL
Virtual Machine
@@OS
"Ubuntu 24.04 LTS"
@@CPU_MODEL
ARM test processor
@@CORES
2
@@SERVICE_FILES
web.service enabled enabled
frp.service enabled disabled
temporary.service enabled-runtime disabled
disabled.service disabled enabled
@@SERVICE_UNITS
web.service loaded active running Web server
frp.service loaded failed failed FRP tunnel
temporary.service loaded inactive dead Temporary worker
@@SERVICE_MANAGER
systemd
`)
	if i.OS != "Ubuntu 24.04 LTS" || i.Cores != 2 || i.Model != "Virtual Machine" || len(i.Services) != 4 {
		t.Fatal(i)
	}
	if i.Services[0].Name != "disabled.service" || i.Services[0].Enabled != "disabled" || i.Services[1].Active != "failed" || i.Services[2].Enabled != "enabled-runtime" || i.Services[3].Sub != "running" {
		t.Fatal(i.Services)
	}
	var text strings.Builder
	text.WriteString("@@SERVICE_FILES\n")
	for n := 0; n < 1300; n++ {
		fmt.Fprintf(&text, "service-%04d.service enabled enabled\n", n)
	}
	limited := ParseInventory(text.String())
	if len(limited.Services) != 1024 || limited.ServiceNote == "" {
		t.Fatal("unbounded or silently truncated service inventory")
	}
}

func TestInventoryFallbackAndUnavailableAreExplicit(t *testing.T) {
	i := ParseInventory("@@SERVICE_RC\nnetwork\nsshd\nsshd\n@@SERVICE_MANAGER\nOpenRC\n")
	if len(i.Services) != 2 || i.Services[0].Active != "unknown" || i.Manager != "OpenRC" {
		t.Fatal(i)
	}
	if missing := ParseInventory("@@SERVICE_MANAGER\nSysV / unknown\n"); missing.ServiceNote == "" {
		t.Fatal("missing interface was reported as a known empty list")
	}
}

func TestProcessSamplesIncludeMemoryHeavyEntriesWithoutDuplicates(t *testing.T) {
	s, e := Parse(sample("100 0 100 800 0 0 0 0", 1, 2)+"@@PROCESS\n42 13.0 3.2 120 sshd\n99 0.1 85.0 640000 worker\n@@UPTIME\n1234.5 34\n", nil)
	if e != nil || len(s.Processes) != 2 || s.Processes[1].RSS != 640000*1024 || s.Processes[1].Name != "worker" || s.Uptime != 1234.5 {
		t.Fatal(s, e)
	}
}
