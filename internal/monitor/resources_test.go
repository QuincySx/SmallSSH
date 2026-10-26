package monitor

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
)

func resourceSample(tick int) string {
	return fmt.Sprintf(`@@CPU
cpu %d 0 100 %d %d 0 0 0 999 0
cpu0 %d 0 100 %d %d 0 0 0
@@MEM
MemTotal: 524288 kB
MemAvailable: 300000 kB
MemFree: 150000 kB
Buffers: 10000 kB
Cached: 110000 kB
SReclaimable: 30000 kB
Shmem: 5000 kB
SwapTotal: 262144 kB
SwapFree: 200000 kB
@@NET
eth0: %d 12 1 2 0 0 0 0 %d 24 3 4 0 0 0 0
docker0: 900000 0 0 0 0 0 0 0 600000 0 0 0 0 0 0 0
@@DEFAULTNET
eth0
@@LOAD
0.10 0.20 0.30 2/88 99
@@DISK
Filesystem Type 1024-blocks Used Available Capacity Mounted on
/dev/vda1 ext4 1048576 524288 500000 51%% /
@@BLOCK
vda %d 0 %d 100 %d 0 %d 100 0 %d 200
@@PROCESS_DETAIL
PID %%CPU %%MEM RSS USER STAT ELAPSED COMMAND
42 15.2 2.0 8192 root Ss 01:22 sshd
`, 100+tick*20, 800+tick*70, 10+tick*10, 100+tick*20, 800+tick*70, 10+tick*10, 1000+tick*2048, 2000+tick*1024, 100+tick*2, 200+tick*16, 100+tick*3, 200+tick*8, 100+tick*200)
}

func TestResourceDetailsUseMatchedCountersAndKernelUnits(t *testing.T) {
	a, e := Parse(resourceSample(0), nil)
	if e != nil {
		t.Fatal(e)
	}
	if a.CPUDetail.Valid || a.Networks[0].Valid || a.BlockIO[0].Valid {
		t.Fatal("first sample fabricated a rate")
	}
	a.Time = time.Now().Add(-2 * time.Second)
	b, e := Parse(resourceSample(1), &a)
	if e != nil {
		t.Fatal(e)
	}
	if !b.CPUDetail.Valid || b.CPU != 20 || len(b.Cores) != 1 || b.Cores[0].Used != 20 || b.CPUDetail.Wait != 10 {
		t.Fatal(b.CPUDetail, b.Cores, b.CPU)
	}
	if b.MemoryAvailable != 300000*1024 || b.MemoryCached != 135000*1024 || b.SwapUsed != 62144*1024 {
		t.Fatal("memory accounting", b)
	}
	n := b.Networks[0]
	io := b.BlockIO[0]
	if !n.Valid || math.Abs(n.RXRate-1024) > 10 || n.RXDropped != 2 || n.TXErrors != 3 || b.PrimaryNetwork() != "eth0" {
		t.Fatal(n, b.PrimaryNetwork())
	}
	if !io.Valid || math.Abs(io.ReadRate-4096) > 40 || math.Abs(io.WriteIOPS-1.5) > .02 || math.Abs(io.Busy-10) > .1 {
		t.Fatal(io)
	}
	if b.Disks[0].Filesystem != "ext4" || b.Disks[0].Total != 1<<30 || b.Processes[0].Name != "sshd" || b.Processes[0].User != "root" || b.ProcessCount != 88 {
		t.Fatal(b)
	}
	// Reboot, hotplug and a future previous timestamp must never cause spikes.
	b.Time = time.Now().Add(-2 * time.Second)
	reset, e := Parse(resourceSample(0), &b)
	if e != nil {
		t.Fatal(e)
	}
	if reset.CPUDetail.Valid || reset.Networks[0].Valid || reset.BlockIO[0].Valid {
		t.Fatal("counter reset was graphed", reset)
	}
	a.Time = time.Now().Add(time.Minute)
	future, _ := Parse(resourceSample(2), &a)
	if future.Networks[0].Valid || future.BlockIO[0].Valid {
		t.Fatal("negative sampling interval")
	}
}

func TestResourceMissingAndFallbacks(t *testing.T) {
	text := strings.ReplaceAll(resourceSample(0), "@@DEFAULTNET\neth0\n", "")
	s, e := Parse(text, nil)
	if e != nil || s.PrimaryNetwork() != "eth0" {
		t.Fatal(s, e)
	}
	text = strings.ReplaceAll(text, "Cached: 110000 kB", "Cached: 1 kB")
	text = strings.ReplaceAll(text, "SReclaimable: 30000 kB", "SReclaimable: 1 kB")
	text = strings.ReplaceAll(text, "SwapFree: 200000 kB", "SwapFree: 300000 kB")
	s, e = Parse(text, nil)
	if e != nil || s.MemoryCached != 0 || s.SwapUsed != 0 {
		t.Fatal("counter underflow", s, e)
	}
}
