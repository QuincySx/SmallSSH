package monitor

import (
	"strings"
	"time"
)

// Counter snapshots stay private; only the derived rates cross the broker wire.
type CPUUsage struct {
	Name   string  `json:"name"`
	Used   float64 `json:"used"`
	User   float64 `json:"user"`
	System float64 `json:"system"`
	Wait   float64 `json:"wait"`
	Steal  float64 `json:"steal"`
	Guest  float64 `json:"guest"`
	Valid  bool    `json:"valid"`
	ticks  [10]uint64
}

type Network struct {
	Name                 string `json:"name"`
	RX, TX               uint64
	RXPackets, TXPackets uint64
	RXErrors, TXErrors   uint64
	RXDropped, TXDropped uint64
	RXRate, TXRate       float64
	Valid                bool
}

type BlockIO struct {
	Name                  string `json:"name"`
	ReadBytes, WriteBytes uint64
	ReadRate, WriteRate   float64
	ReadIOPS, WriteIOPS   float64
	Busy                  float64
	Valid                 bool
	reads, writes, busyMS uint64
}

func subtract(a, b uint64) uint64 {
	if b > a {
		return 0
	}
	return a - b
}

func parseCPU(f []string, previous *Stats) CPUUsage {
	current := CPUUsage{Name: f[0]}
	for j := range min(10, len(f)-1) {
		current.ticks[j] = num(f[j+1])
	}
	if previous == nil {
		return current
	}
	var before *CPUUsage
	if current.Name == "cpu" {
		before = &previous.CPUDetail
	} else {
		for j := range previous.Cores {
			if previous.Cores[j].Name == current.Name {
				before = &previous.Cores[j]
				break
			}
		}
	}
	if before == nil || before.Name != current.Name {
		return current
	}
	var delta [10]uint64
	var total uint64
	for j, value := range current.ticks {
		if value < before.ticks[j] {
			return current
		}
		delta[j] = value - before.ticks[j]
		if j < 8 {
			total += delta[j]
		}
	}
	if total == 0 {
		return current
	}
	scale := 100 / float64(total)
	current.Used = float64(total-delta[3]-delta[4]) * scale
	current.Guest = float64(delta[8]+delta[9]) * scale
	current.User = float64(subtract(delta[0]+delta[1], delta[8]+delta[9])) * scale
	current.System = float64(delta[2]+delta[5]+delta[6]) * scale
	current.Wait = float64(delta[4]) * scale
	current.Steal = float64(delta[7]) * scale
	current.Valid = true
	return current
}

func parseNetwork(name string, f []string, previous *Stats, now time.Time) Network {
	n := Network{Name: name, RX: num(f[0]), RXPackets: num(f[1]), RXErrors: num(f[2]), RXDropped: num(f[3]), TX: num(f[8]), TXPackets: num(f[9]), TXErrors: num(f[10]), TXDropped: num(f[11])}
	if previous == nil {
		return n
	}
	elapsed := now.Sub(previous.Time).Seconds()
	if elapsed <= 0 {
		return n
	}
	for _, old := range previous.Networks {
		if old.Name == name && n.RX >= old.RX && n.TX >= old.TX {
			n.RXRate, n.TXRate, n.Valid = float64(n.RX-old.RX)/elapsed, float64(n.TX-old.TX)/elapsed, true
			break
		}
	}
	return n
}

func parseBlock(f []string, previous *Stats, now time.Time) BlockIO {
	b := BlockIO{Name: f[0], reads: num(f[1]), writes: num(f[5]), ReadBytes: num(f[3]) * 512, WriteBytes: num(f[7]) * 512, busyMS: num(f[10])}
	if previous == nil {
		return b
	}
	elapsed := now.Sub(previous.Time).Seconds()
	if elapsed <= 0 {
		return b
	}
	for _, old := range previous.BlockIO {
		if old.Name != b.Name {
			continue
		}
		if b.ReadBytes < old.ReadBytes || b.WriteBytes < old.WriteBytes || b.reads < old.reads || b.writes < old.writes || b.busyMS < old.busyMS {
			return b
		}
		b.ReadRate, b.WriteRate = float64(b.ReadBytes-old.ReadBytes)/elapsed, float64(b.WriteBytes-old.WriteBytes)/elapsed
		b.ReadIOPS, b.WriteIOPS = float64(b.reads-old.reads)/elapsed, float64(b.writes-old.writes)/elapsed
		b.Busy, b.Valid = min(100, float64(b.busyMS-old.busyMS)/elapsed/10), true
		break
	}
	return b
}

// PrimaryNetwork follows the default route, avoiding double-counting virtual
// bridges and containers in the main chart. A missing route falls back to an
// interface with traffic; each interface remains available in the detail view.
func (s *Stats) PrimaryNetwork() string {
	for _, n := range s.Networks {
		if n.Name == s.DefaultNetwork {
			return n.Name
		}
	}
	for _, n := range s.Networks {
		if n.Name != "lo" && (n.RX > 0 || n.TX > 0) && !strings.HasPrefix(n.Name, "veth") && !strings.HasPrefix(n.Name, "docker") && !strings.HasPrefix(n.Name, "br-") {
			return n.Name
		}
	}
	if len(s.Networks) > 0 {
		return s.Networks[0].Name
	}
	return ""
}
