package monitor

import "strings"

const InterfaceCommand = `printf '\n@@INTERFACES\n'
for entry in /sys/class/net/*; do
 [ -d "$entry" ] || continue
 name=${entry##*/}; state=unknown; mac=-; mtu=0
 IFS= read -r state < "$entry/operstate" 2>/dev/null || true
 IFS= read -r mac < "$entry/address" 2>/dev/null || true
 IFS= read -r mtu < "$entry/mtu" 2>/dev/null || true
 virtual=0; [ -d "/sys/devices/virtual/net/$name" ] && virtual=1
 printf '%s %s %s %s %s\n' "$name" "$state" "$virtual" "$mac" "$mtu"
done
printf '\n@@ADDRESSES\n'
if command -v ip >/dev/null 2>&1; then ip -o address show 2>/dev/null; fi
`

type Interface struct {
	Name, State, MAC string
	MTU              uint64
	Virtual          bool
	Addresses        []string
}

type TCPInfo struct {
	ActiveOpens, PassiveOpens, Established, Retransmits, SegmentsOut uint64
	RetransmitPercent                                                float64
	Available, RateValid                                             bool
}

func parseTCP(headers, values []string, previous *Stats) TCPInfo {
	i := TCPInfo{Available: true}
	for n, key := range headers {
		v := num(values[n])
		switch key {
		case "ActiveOpens":
			i.ActiveOpens = v
		case "PassiveOpens":
			i.PassiveOpens = v
		case "CurrEstab":
			i.Established = v
		case "RetransSegs":
			i.Retransmits = v
		case "OutSegs":
			i.SegmentsOut = v
		}
	}
	if previous != nil && previous.TCP.Available && i.Retransmits >= previous.TCP.Retransmits && i.SegmentsOut > previous.TCP.SegmentsOut {
		i.RateValid = true
		i.RetransmitPercent = float64(i.Retransmits-previous.TCP.Retransmits) / float64(i.SegmentsOut-previous.TCP.SegmentsOut) * 100
	}
	return i
}

func parseInterfaceLine(section, line string, interfaces map[string]*Interface) {
	f := strings.Fields(line)
	if section == "@@INTERFACES" && len(f) >= 5 {
		i := interfaces[f[0]]
		if i == nil {
			i = &Interface{Name: f[0]}
			interfaces[f[0]] = i
		}
		i.State, i.Virtual, i.MAC, i.MTU = f[1], f[2] == "1", f[3], num(f[4])
	}
	if section == "@@ADDRESSES" && len(f) >= 4 && (f[2] == "inet" || f[2] == "inet6") {
		name := strings.SplitN(f[1], "@", 2)[0]
		i := interfaces[name]
		if i == nil {
			i = &Interface{Name: name}
			interfaces[name] = i
		}
		i.Addresses = append(i.Addresses, f[3])
	}
}
