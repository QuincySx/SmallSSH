package monitor

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"time"
)

type ContainerStats struct {
	ID                                              string
	CPU, MemoryPercent                              float64
	Memory, Limit, RX, TX, Read, Write              uint64
	PIDs                                            uint64
	CPUValid, MemoryValid, NetworkValid, BlockValid bool
}

type DockerSnapshot struct {
	Time                           time.Time
	EngineID, Socket, Status, Note string
	Rows                           []ContainerStats
}

func ParseDockerSnapshot(text string) DockerSnapshot {
	s := DockerSnapshot{Time: time.Now().UTC(), Status: "unavailable"}
	if d := ParseInventory(text).Docker; d != nil {
		s.EngineID, s.Socket, s.Status, s.Note, s.Rows = d.EngineID, d.Socket, d.Status, d.Note, d.Resources
	}
	return s
}

// ByteAmount accepts Docker's human-readable decimal and binary byte units.
func ByteAmount(text string) (float64, bool) {
	text = strings.TrimSpace(text)
	n := 0
	for n < len(text) && (text[n] >= '0' && text[n] <= '9' || text[n] == '.') {
		n++
	}
	v, err := strconv.ParseFloat(text[:n], 64)
	if err != nil || math.IsInf(v, 0) || math.IsNaN(v) {
		return 0, false
	}
	unit := strings.ToUpper(strings.TrimSpace(text[n:]))
	for i, label := range []string{"B", "KB", "MB", "GB", "TB", "PB", "EB"} {
		if unit == label {
			for range i {
				v *= 1000
			}
			return v, v < math.MaxUint64
		}
	}
	for i, label := range []string{"B", "KIB", "MIB", "GIB", "TIB", "PIB", "EIB"} {
		if unit == label {
			for range i {
				v *= 1024
			}
			return v, v < math.MaxUint64
		}
	}
	return 0, false
}

func pairBytes(text string) (uint64, uint64, bool) {
	a, b, ok := strings.Cut(text, "/")
	if !ok {
		return 0, 0, false
	}
	left, l := ByteAmount(a)
	right, r := ByteAmount(b)
	return uint64(left), uint64(right), l && r
}

func parseContainerStats(line string) (ContainerStats, bool) {
	var raw struct{ ID, CPUPerc, MemPerc, MemUsage, NetIO, BlockIO, PIDs string }
	if json.Unmarshal([]byte(line), &raw) != nil || len(raw.ID) != 64 {
		return ContainerStats{}, false
	}
	s := ContainerStats{ID: raw.ID, PIDs: num(raw.PIDs)}
	cpu, err := strconv.ParseFloat(strings.TrimSuffix(raw.CPUPerc, "%"), 64)
	if err == nil && cpu >= 0 && !math.IsInf(cpu, 0) && !math.IsNaN(cpu) {
		s.CPU, s.CPUValid = cpu, true
	}
	s.Memory, s.Limit, s.MemoryValid = pairBytes(raw.MemUsage)
	s.MemoryPercent, _ = strconv.ParseFloat(strings.TrimSuffix(raw.MemPerc, "%"), 64)
	if math.IsNaN(s.MemoryPercent) || math.IsInf(s.MemoryPercent, 0) {
		s.MemoryPercent = 0
	}
	s.RX, s.TX, s.NetworkValid = pairBytes(raw.NetIO)
	s.Read, s.Write, s.BlockValid = pairBytes(raw.BlockIO)
	return s, true
}
