package monitor

import (
	"bufio"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const maxPortRecords = 1024

// The producer writes its exit status inside the pipe. The limiter drains the
// producer (so hitting the display limit does not cause SIGPIPE), bounds both
// records and bytes, and returns the producer's success to the fixed fallback.
// No command line or socket payload is requested, and no remote files are made.
const portCommand = `
printf '\n@@PORTS_BEGIN\n'
printf 'time %s\n' "$(date -u +%s 2>/dev/null)"
ports_limit() {
 awk '
 /^@@PORTS_EXIT [0-9]+$/ { code=$2; ended=1; next }
 NF {
   if (rows < 1024 && length($0) <= 4096 && bytes + length($0) + 3 <= 524288) {
     print "P " $0; rows++; bytes += length($0) + 3
   } else { limited=1 }
 }
 END {
   if (!ended) code=125
   printf "@@PORTS_RESULT\n%d %d\n", code, limited
   exit (code != 0)
 }'
}
ports_done=0
ports_available=0
if command -v ss >/dev/null 2>&1; then
 ports_available=1
 printf '@@PORTS_SOURCE\nss\n'
 { ss -H -O -n -a -t -u -p 2>/dev/null; printf '\n@@PORTS_EXIT %s\n' "$?"; } | ports_limit
 [ "$?" -eq 0 ] && ports_done=1
fi
if [ "$ports_done" -eq 0 ] && command -v netstat >/dev/null 2>&1; then
 ports_available=1
 printf '@@PORTS_SOURCE\nnetstat\n'
 { netstat -W -n -a -t -u -p 2>/dev/null; printf '\n@@PORTS_EXIT %s\n' "$?"; } | ports_limit
 if [ "$?" -eq 0 ]; then
   ports_done=1
 else
   printf '@@PORTS_SOURCE\nnetstat\n'
   { netstat -n -a -t -u -p 2>/dev/null; printf '\n@@PORTS_EXIT %s\n' "$?"; } | ports_limit
   [ "$?" -eq 0 ] && ports_done=1
 fi
fi
if [ "$ports_done" -eq 0 ]; then
 printf '@@PORTS_UNAVAILABLE\n'
 if [ "$ports_available" -eq 0 ]; then printf 'missing-tools\n'; else printf 'probe-failed\n'; fi
fi
printf '@@PORTS_END\n'
`

// PortSnapshot is a passive Linux socket-table sample. A nil snapshot means an
// older sampler supplied no port data. Partial data must never imply zero ports.
type PortSnapshot struct {
	Time           time.Time `json:"time"`
	Status         string    `json:"status"`
	Source         string    `json:"source,omitempty"`
	Note           string    `json:"note,omitempty"`
	Truncated      bool      `json:"truncated,omitempty"`
	ProcessLimited bool      `json:"processLimited,omitempty"`
	Ports          []Port    `json:"ports"`
}

type PortOwner struct {
	Process string `json:"process,omitempty"`
	PID     int    `json:"pid,omitempty"`
}

type Port struct {
	Protocol     string      `json:"protocol"`
	State        string      `json:"state"`
	LocalAddress string      `json:"localAddress"`
	LocalPort    string      `json:"localPort"`
	PeerAddress  string      `json:"peerAddress"`
	PeerPort     string      `json:"peerPort"`
	Process      string      `json:"process,omitempty"`
	PID          int         `json:"pid,omitempty"`
	Owners       []PortOwner `json:"owners,omitempty"`
}

// Listening includes unconnected UDP bindings; UDP has no TCP listening state.
func (p Port) Listening() bool {
	return p.State == "LISTEN" || p.Protocol == "UDP" && unspecifiedPortPeer(p.PeerAddress, p.PeerPort)
}

func PortLoopback(address string) bool {
	a, err := netip.ParseAddr(address)
	return err == nil && a.Unmap().IsLoopback()
}

func unspecifiedPortPeer(address, port string) bool {
	if port != "*" && port != "0" {
		return false
	}
	if address == "*" {
		return true
	}
	a, err := netip.ParseAddr(address)
	return err == nil && a.Unmap().IsUnspecified()
}

// ExternalConnection means a concrete non-loopback peer, including private
// addresses. It says nothing about reachability or which side initiated traffic.
func (p Port) ExternalConnection() bool {
	a, err := netip.ParseAddr(p.PeerAddress)
	return !p.Listening() && err == nil && !a.Unmap().IsUnspecified() && !a.Unmap().IsLoopback() && p.PeerPort != "0" && p.PeerPort != "*"
}

func extractPortSnapshot(text string, now time.Time) (string, *PortSnapshot) {
	const marker = "@@PORTS_BEGIN\n"
	start := 0
	if !strings.HasPrefix(text, marker) {
		pos := strings.Index(text, "\n"+marker)
		if pos < 0 {
			return text, nil
		}
		start = pos + 1
	}
	base := text[:start]
	snapshot := &PortSnapshot{Time: now, Status: "unavailable", Note: "incomplete-output"}
	scan := bufio.NewScanner(strings.NewReader(text[start+len(marker):]))
	scan.Buffer(make([]byte, 4096), 16<<10)
	section := ""
	ended, result, malformed := false, false, false
	for scan.Scan() {
		line := scan.Text()
		if strings.HasPrefix(line, "@@PORTS_") {
			section = line
			if line == "@@PORTS_SOURCE" {
				snapshot.Source, snapshot.Ports = "", nil
				snapshot.Status, snapshot.Note = "unavailable", "incomplete-output"
				snapshot.Truncated, snapshot.ProcessLimited = false, false
				result, malformed = false, false
			}
			if line == "@@PORTS_END" {
				ended = true
				break
			}
			continue
		}
		if strings.HasPrefix(line, "time ") && section == "" {
			if seconds, err := strconv.ParseInt(strings.TrimPrefix(line, "time "), 10, 64); err == nil && seconds > 0 {
				snapshot.Time = time.Unix(seconds, 0).UTC()
			}
			continue
		}
		switch section {
		case "@@PORTS_SOURCE":
			if line == "ss" || line == "netstat" {
				snapshot.Source = line
				section = "rows"
			} else {
				malformed = true
				section = "invalid-source"
			}
		case "rows":
			if strings.TrimSpace(line) == "" {
				continue
			}
			if !strings.HasPrefix(line, "P ") {
				malformed = true
				continue
			}
			row := strings.TrimPrefix(line, "P ")
			if snapshot.Source == "netstat" && (strings.HasPrefix(row, "Active Internet connections") || strings.HasPrefix(row, "Proto ")) {
				continue
			}
			port, ok := parsePortRow(row, snapshot.Source)
			if !ok {
				malformed = true
				continue
			}
			if len(snapshot.Ports) >= maxPortRecords {
				snapshot.Truncated = true
				continue
			}
			snapshot.Ports = append(snapshot.Ports, port)
			if port.PID == 0 {
				snapshot.ProcessLimited = true
			}
		case "@@PORTS_RESULT":
			if snapshot.Source == "" {
				malformed = true
				continue
			}
			fields := strings.Fields(line)
			if len(fields) != 2 || fields[1] != "0" && fields[1] != "1" {
				malformed = true
				continue
			}
			code, err := strconv.Atoi(fields[0])
			if err != nil {
				malformed = true
				continue
			}
			result = true
			snapshot.Truncated = snapshot.Truncated || fields[1] == "1"
			if code == 0 {
				snapshot.Status, snapshot.Note = "ready", ""
			} else {
				snapshot.Status, snapshot.Note = "unavailable", "probe-failed"
			}
		case "@@PORTS_UNAVAILABLE":
			snapshot.Status, snapshot.Note = "unavailable", line
		}
	}
	if !ended || !result || scan.Err() != nil {
		if snapshot.Note != "missing-tools" && snapshot.Note != "probe-failed" {
			snapshot.Status, snapshot.Note = "unavailable", "incomplete-output"
		}
	}
	if snapshot.Status == "ready" {
		if malformed {
			snapshot.Status, snapshot.Note = "partial", "unrecognized-output"
		}
		if snapshot.Truncated || snapshot.ProcessLimited {
			snapshot.Status = "partial"
		}
	}
	return base, snapshot
}

var ssPortOwner = regexp.MustCompile(`\("((?:[^"\\]|\\.)*)",pid=([0-9]+),fd=[0-9]+\)`)

func parsePortRow(line, source string) (Port, bool) {
	f := strings.Fields(line)
	var p Port
	var local, peer string
	if source == "ss" {
		if len(f) < 6 || f[0] != "tcp" && f[0] != "udp" {
			return p, false
		}
		if !portQueue(f[2]) || !portQueue(f[3]) {
			return p, false
		}
		p.Protocol, p.State, local, peer = strings.ToUpper(f[0]), portState(f[1]), f[4], f[5]
		for _, match := range ssPortOwner.FindAllStringSubmatch(line, -1) {
			pid, err := strconv.Atoi(match[2])
			if err == nil && pid > 0 {
				name := match[1]
				if decoded, err := strconv.Unquote(`"` + name + `"`); err == nil {
					name = decoded
				}
				p.Owners = append(p.Owners, PortOwner{Process: name, PID: pid})
			}
		}
	} else {
		if len(f) < 5 || f[0] != "tcp" && f[0] != "tcp6" && f[0] != "udp" && f[0] != "udp6" {
			return p, false
		}
		if !portQueue(f[1]) || !portQueue(f[2]) {
			return p, false
		}
		p.Protocol, local, peer = strings.ToUpper(strings.TrimSuffix(f[0], "6")), f[3], f[4]
		owner := 5
		if p.Protocol == "TCP" {
			if len(f) < 6 {
				return p, false
			}
			p.State, owner = portState(f[5]), 6
		} else {
			p.State = "UNCONN"
			if len(f) > 5 && !strings.Contains(f[5], "/") && f[5] != "-" {
				p.State, owner = portState(f[5]), 6
			}
		}
		if len(f) > owner {
			pidText, process, ok := strings.Cut(strings.Join(f[owner:], " "), "/")
			pid, err := strconv.Atoi(pidText)
			if ok && err == nil && pid > 0 {
				p.Owners = append(p.Owners, PortOwner{Process: process, PID: pid})
			}
		}
	}
	var ok bool
	if p.LocalAddress, p.LocalPort, ok = parsePortEndpoint(local); !ok {
		return Port{}, false
	}
	if p.PeerAddress, p.PeerPort, ok = parsePortEndpoint(peer); !ok {
		return Port{}, false
	}
	if len(p.Owners) > 0 {
		p.Process, p.PID = p.Owners[0].Process, p.Owners[0].PID
	}
	return p, true
}

func parsePortEndpoint(endpoint string) (string, string, bool) {
	colon := strings.LastIndexByte(endpoint, ':')
	if colon < 1 {
		return "", "", false
	}
	address, port := endpoint[:colon], endpoint[colon+1:]
	if strings.HasPrefix(address, "[") && strings.HasSuffix(address, "]") {
		address = address[1 : len(address)-1]
	}
	if address != "*" {
		if _, err := netip.ParseAddr(address); err != nil {
			return "", "", false
		}
	}
	if port != "*" {
		if _, err := strconv.ParseUint(port, 10, 16); err != nil {
			return "", "", false
		}
	}
	return address, port, true
}

func portQueue(value string) bool {
	_, err := strconv.ParseUint(value, 10, 64)
	return err == nil
}

func portState(state string) string {
	state = strings.ReplaceAll(strings.ToUpper(state), "_", "-")
	if state == "ESTABLISHED" {
		return "ESTAB"
	}
	return state
}
