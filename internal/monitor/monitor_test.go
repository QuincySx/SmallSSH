package monitor

import (
	"fmt"
	"math"
	"testing"
	"time"
)

func sample(cpu string, rx, tx int) string {
	return fmt.Sprintf(`@@CPU
cpu %s
@@MEM
MemTotal: 1024000 kB
MemAvailable: 409600 kB
@@NET
lo: 999999 0 0 0 0 0 0 0 999999 0 0 0 0 0 0 0
eth0: %d 0 0 0 0 0 0 0 %d 0 0 0 0 0 0 0
@@LOAD
0.1 0.2 0.3 1/100 42
@@DISK
Filesystem 1024-blocks Used Available Capacity Mounted on
/dev/vda1 1024 512 512 50%% /
@@PROCESS
PID %%CPU %%MEM COMMAND
42 12.5 3.2 sshd
`, cpu, rx, tx)
}
func TestSamplesCounterDeltasAndReset(t *testing.T) {
	a, e := Parse(sample("100 0 100 800 0 0 0 0 999 999", 100, 200), nil)
	if e != nil {
		t.Fatal(e)
	}
	a.Time = time.Now().Add(-2 * time.Second)
	b, e := Parse(sample("150 0 150 900 0 0 0 0 999 999", 500, 1000), &a)
	if e != nil {
		t.Fatal(e)
	}
	if b.CPU != 50 || b.MemoryUsed != 600*1024*1024 || len(b.Disks) != 1 || len(b.Processes) != 1 || math.Abs(b.RXPerSecond-200) > 3 || b.RX != 500 {
		t.Fatal(b)
	}
	c, e := Parse(sample("1 0 1 8 0 0 0 0", 1, 1), &b)
	if e != nil || c.CPU != 0 || c.RXPerSecond != 0 {
		t.Fatal(c, e)
	}
	if _, e = Parse("untrusted missing sample", nil); e == nil {
		t.Fatal("accepted invalid stats")
	}
}
