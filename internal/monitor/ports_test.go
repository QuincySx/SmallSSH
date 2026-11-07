package monitor

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func portFixture(source, rows, result string) string {
	var b strings.Builder
	b.WriteString("\n@@PORTS_BEGIN\ntime 1793923200\n@@PORTS_SOURCE\n" + source + "\n")
	for _, line := range strings.Split(strings.TrimSuffix(rows, "\n"), "\n") {
		if line != "" {
			b.WriteString("P " + line + "\n")
		}
	}
	b.WriteString("@@PORTS_RESULT\n" + result + "\n@@PORTS_END\n")
	return b.String()
}

func TestPortSnapshotSSIPv6OwnersAndSocketScopes(t *testing.T) {
	rows := `tcp LISTEN 0 128 0.0.0.0:443 0.0.0.0:* users:(("nginx",pid=10,fd=6),("nginx",pid=11,fd=6))
tcp LISTEN 0 128 [::1]:5432 [::]:* users:(("postgres",pid=12,fd=7))
udp UNCONN 0 0 [fe80::1%eth0]:5353 [::]:* users:(("avahi-daemon",pid=13,fd=8))
tcp ESTAB 0 0 [2001:db8::1]:22 [2001:db8::2]:51234 users:(("sshd",pid=14,fd=9))
udp ESTAB 0 0 192.0.2.10:45000 10.0.0.2:53 users:(("dns-client",pid=15,fd=10))
tcp ESTAB 0 0 [::ffff:127.0.0.1]:1234 [::ffff:127.0.0.2]:5678 users:(("worker",pid=16,fd=11))`
	stats, err := Parse(sample("100 0 100 800 0 0 0 0", 1, 2)+portFixture("ss", rows, "0 0"), nil)
	if err != nil {
		t.Fatal(err)
	}
	p := stats.PortSnapshot
	if p == nil || p.Status != "ready" || p.Source != "ss" || p.Time.Unix() != 1793923200 || len(p.Ports) != 6 {
		t.Fatalf("snapshot: %+v", p)
	}
	if len(p.Ports[0].Owners) != 2 || p.Ports[0].Owners[1].PID != 11 || p.Ports[0].PID != 10 {
		t.Fatal("shared socket owners were lost", p.Ports[0])
	}
	if !p.Ports[0].Listening() || PortLoopback(p.Ports[0].LocalAddress) || !PortLoopback(p.Ports[1].LocalAddress) || !p.Ports[2].Listening() || p.Ports[2].LocalAddress != "fe80::1%eth0" {
		t.Fatal("listener classification", p.Ports)
	}
	if !p.Ports[3].ExternalConnection() || p.Ports[3].PeerAddress != "2001:db8::2" || !p.Ports[4].ExternalConnection() || p.Ports[5].ExternalConnection() {
		t.Fatal("peer classification", p.Ports)
	}
	if stats.MemoryTotal == 0 || stats.TCP.Established != 0 {
		t.Fatal("existing sample changed", stats)
	}
}

func TestPortSnapshotNetstatUDPAndUnavailableOwners(t *testing.T) {
	rows := `Active Internet connections (servers and established)
Proto Recv-Q Send-Q Local Address Foreign Address State PID/Program name
tcp 0 0 0.0.0.0:22 0.0.0.0:* LISTEN 812/sshd
tcp6 0 0 :::443 :::* LISTEN -
udp 0 0 127.0.0.1:53 0.0.0.0:* -
udp6 0 0 fe80::1%eth0:5353 :::* 14/avahi
udp 0 0 192.0.2.10:45000 10.0.0.2:53 ESTABLISHED 15/dns-client`
	_, p := extractPortSnapshot(portFixture("netstat", rows, "0 0"), time.Now())
	if p.Status != "partial" || !p.ProcessLimited || len(p.Ports) != 5 || p.Ports[0].Process != "sshd" {
		t.Fatalf("netstat snapshot: %+v", p)
	}
	if p.Ports[1].LocalAddress != "::" || p.Ports[1].LocalPort != "443" || !p.Ports[2].Listening() || !p.Ports[3].Listening() || !p.Ports[4].ExternalConnection() {
		t.Fatal(p.Ports)
	}
}

func TestPortSnapshotResultRequiresCurrentKnownSource(t *testing.T) {
	const row = `tcp LISTEN 0 128 *:22 *:* users:(("sshd",pid=812,fd=4))`
	for _, tc := range []struct{ name, text string }{
		{"missing-source", "\n@@PORTS_BEGIN\ntime 1793923200\n@@PORTS_RESULT\n0 0\n@@PORTS_END\n"},
		{"missing-source-value", "\n@@PORTS_BEGIN\n@@PORTS_SOURCE\n@@PORTS_RESULT\n0 0\n@@PORTS_END\n"},
		{"unknown-source", portFixture("lsof", row, "0 0")},
		{"unknown-after-success", strings.TrimSuffix(portFixture("ss", row, "0 0"), "@@PORTS_END\n") + "@@PORTS_SOURCE\nlsof\n@@PORTS_RESULT\n0 0\n@@PORTS_END\n"},
		{"missing-value-after-success", strings.TrimSuffix(portFixture("ss", row, "0 0"), "@@PORTS_END\n") + "@@PORTS_SOURCE\n@@PORTS_RESULT\n0 0\n@@PORTS_END\n"},
		{"missing-result", strings.Replace(portFixture("ss", row, "0 0"), "@@PORTS_RESULT\n0 0\n", "", 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := Parse(sample("100 0 100 800 0 0 0 0", 1, 2)+tc.text, nil)
			if err != nil || s.total != 1000 || s.MemoryTotal != 1024000*1024 {
				t.Fatal("invalid port provenance damaged CPU/memory parsing", err, s.total, s.MemoryTotal)
			}
			p := s.PortSnapshot
			if p == nil || p.Status != "unavailable" || p.Note != "incomplete-output" {
				t.Fatalf("result without a current known source became available: %+v", p)
			}
			if tc.name != "missing-result" && (p.Source != "" || len(p.Ports) != 0) {
				t.Fatal("invalid source reused a previous candidate", p.Source, len(p.Ports))
			}
		})
	}
}

func TestPortSnapshotEscapedSSOwners(t *testing.T) {
	row := `tcp LISTEN 0 128 *:22 *:* users:(("quo\"te\\name\n",pid=42,fd=3),("tab\tworker",pid=43,fd=4))`
	_, p := extractPortSnapshot(portFixture("ss", row, "0 0"), time.Now())
	if p.Status != "ready" || len(p.Ports) != 1 || len(p.Ports[0].Owners) != 2 {
		t.Fatalf("escaped owner identities not parsed: %+v", p)
	}
	port := p.Ports[0]
	if port.Process != "quo\"te\\name\n" || port.PID != 42 || port.Owners[1].Process != "tab\tworker" || port.Owners[1].PID != 43 {
		t.Fatal("owner escaping was changed or lost", port)
	}
}

func TestPortSnapshotEmptyFailuresTruncationAndLegacyStats(t *testing.T) {
	for _, tc := range []struct {
		name, text, status, note string
		truncated                bool
	}{
		{"empty", portFixture("ss", "", "0 0"), "ready", "", false},
		{"failure", portFixture("ss", "", "2 0"), "unavailable", "probe-failed", false},
		{"missing", "\n@@PORTS_BEGIN\ntime 1793923200\n@@PORTS_UNAVAILABLE\nmissing-tools\n@@PORTS_END\n", "unavailable", "missing-tools", false},
		{"malformed", portFixture("ss", "unexpected output", "0 0"), "partial", "unrecognized-output", false},
		{"truncated", portFixture("ss", "tcp LISTEN 0 128 *:22 *:* users:((\"sshd\",pid=812,fd=4))", "0 1"), "partial", "", true},
		{"incomplete", strings.TrimSuffix(portFixture("ss", "", "0 0"), "@@PORTS_END\n"), "unavailable", "incomplete-output", false},
		{"oversized", "\n@@PORTS_BEGIN\n@@PORTS_SOURCE\nss\nP " + strings.Repeat("x", 200<<10), "unavailable", "incomplete-output", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := Parse(sample("100 0 100 800 0 0 0 0", 1, 2)+tc.text, nil)
			if err != nil || s.MemoryTotal == 0 || s.PortSnapshot == nil {
				t.Fatal("port failure affected base sample", err, s)
			}
			p := s.PortSnapshot
			if p.Status != tc.status || p.Note != tc.note || p.Truncated != tc.truncated {
				t.Fatalf("want %s/%s truncated %v; got %+v", tc.status, tc.note, tc.truncated, p)
			}
		})
	}
	legacy, err := Parse(sample("100 0 100 800 0 0 0 0", 1, 2), nil)
	if err != nil || legacy.PortSnapshot != nil {
		t.Fatal("legacy sampler requires port markers", err, legacy)
	}
	encoded, err := json.Marshal(legacy)
	if err != nil || strings.Contains(string(encoded), "portSnapshot") {
		t.Fatal("legacy wire format gained a required field", err, string(encoded))
	}
	var decoded Stats
	if err := json.Unmarshal([]byte(`{"cpu":12,"memoryTotal":1024}`), &decoded); err != nil || decoded.PortSnapshot != nil || decoded.CPU != 12 {
		t.Fatal("old stats did not decode", err, decoded)
	}
}

func TestPortSnapshotEndpointValidationAndWildcardClassification(t *testing.T) {
	for _, endpoint := range []string{"[::]:443", ":::443", "[fe80::1%eth0]:5353", "*:22", "0.0.0.0:*", "[::ffff:127.0.0.1]:22"} {
		if _, _, ok := parsePortEndpoint(endpoint); !ok {
			t.Fatal("valid numeric endpoint rejected", endpoint)
		}
	}
	for _, endpoint := range []string{"host:80", "127.0.0.1:http", "127.0.0.1:65536", "127.0.0.1:-1", "[::]:"} {
		if _, _, ok := parsePortEndpoint(endpoint); ok {
			t.Fatal("invalid endpoint accepted", endpoint)
		}
	}
	for _, peer := range []string{"*", "0.0.0.0", "::", "::ffff:0.0.0.0", "::ffff:127.0.0.1"} {
		p := Port{Protocol: "TCP", State: "ESTAB", PeerAddress: peer, PeerPort: "443"}
		if p.ExternalConnection() {
			t.Fatal("wildcard/loopback peer classified external", p)
		}
	}
}

func TestPortProbeFixedFallbackExitMarkersAndBounds(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixed Linux probe needs a POSIX shell; parsers run on all platforms")
	}
	for _, tc := range []struct {
		name, ss, netstat, status, source, note string
		rows                                    int
		truncated                               bool
	}{
		{name: "success", ss: "printf 'tcp LISTEN 0 128 0.0.0.0:22 *:* users:((\"sshd\",pid=812,fd=4))\\n'", status: "ready", source: "ss", rows: 1},
		{name: "empty", ss: "exit 0", status: "ready", source: "ss"},
		{name: "fallback", ss: "exit 7", netstat: "printf 'tcp 0 0 0.0.0.0:22 0.0.0.0:* LISTEN 812/sshd\\n'", status: "ready", source: "netstat", rows: 1},
		{name: "busybox", ss: "exit 7", netstat: "if [ \"$1\" = -W ]; then exit 2; fi\nprintf 'udp 0 0 127.0.0.1:53 0.0.0.0:* 42/dns\\n'", status: "ready", source: "netstat", rows: 1},
		{name: "failed", ss: "exit 7", netstat: "exit 9", status: "unavailable", source: "netstat", note: "probe-failed"},
		{name: "missing", status: "unavailable", note: "missing-tools"},
		{name: "limited", ss: "n=0\nwhile [ \"$n\" -lt 1030 ]; do printf 'tcp LISTEN 0 128 *:22 *:* users:((\"sshd\",pid=812,fd=4))\\n'; n=$((n+1)); done", status: "partial", source: "ss", rows: maxPortRecords, truncated: true},
		{name: "long-line", ss: "printf '%s\\n' '" + strings.Repeat("x", 5000) + "'", status: "partial", source: "ss", truncated: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output, p := runPortProbeFixture(t, tc.ss, tc.netstat)
			if p == nil || p.Status != tc.status || p.Source != tc.source || p.Note != tc.note || len(p.Ports) != tc.rows || p.Truncated != tc.truncated {
				t.Fatalf("probe snapshot: %+v; output: %s", p, output)
			}
			if tc.name == "fallback" && !strings.Contains(string(output), "@@PORTS_RESULT\n7 0\n") {
				t.Fatal("pipe swallowed the producer failure", string(output))
			}
			if len(output) > 2<<20 {
				t.Fatal("bounded probe output exceeded total budget", len(output))
			}
		})
	}
}

func TestPortProbeSingleLineByteBoundaryPreservesExitCode(t *testing.T) {
	accepted, oversized := portProbeSizedRow(4096), portProbeSizedRow(4097)
	const small = `tcp LISTEN 0 128 *:80 *:* users:(("web",pid=80,fd=4))`
	script := "printf '%s\\n' '" + accepted + "' '" + oversized + "' '" + small + "'\nexit 7"
	output, p := runPortProbeFixture(t, script, "")
	if p.Status != "unavailable" || p.Note != "probe-failed" || !p.Truncated || len(p.Ports) != 2 {
		t.Fatal("single-line limit lost failure or valid rows", p.Status, p.Note, p.Truncated, len(p.Ports))
	}
	if !strings.Contains(string(output), "P "+accepted+"\n") || strings.Contains(string(output), "P "+oversized+"\n") || !strings.Contains(string(output), "P "+small+"\n") {
		t.Fatal("4096-byte row boundary or drain-after-limit behavior changed", len(output))
	}
	if !strings.Contains(string(output), "@@PORTS_RESULT\n7 1\n") {
		t.Fatal("single-line limit swallowed producer exit status", len(output))
	}
}

func TestPortProbeCumulativeByteBoundaryPreservesExitCode(t *testing.T) {
	// Each accepted P-prefixed row including its newline is exactly 4096 bytes.
	// The 129th row hits the byte cap before the independent 1024-record cap.
	row := portProbeSizedRow(4093)
	for _, tc := range []struct{ name, exit, status string }{
		{"success", "0", "partial"},
		{"failure", "9", "unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			script := "n=0\nwhile [ \"$n\" -lt 129 ]; do printf '%s\\n' '" + row + "'; n=$((n+1)); done\nexit " + tc.exit
			output, p := runPortProbeFixture(t, script, "")
			if p.Status != tc.status || !p.Truncated || len(p.Ports) != 128 {
				t.Fatal("byte cap failed before the record cap", p.Status, p.Truncated, len(p.Ports))
			}
			rowBytes := 0
			for _, line := range strings.Split(string(output), "\n") {
				if strings.HasPrefix(line, "P ") {
					rowBytes += len(line) + 1
				}
			}
			if rowBytes != 512<<10 {
				t.Fatal("row output did not reach the exact 512KiB cap", rowBytes)
			}
			if !strings.Contains(string(output), "@@PORTS_RESULT\n"+tc.exit+" 1\n") {
				t.Fatal("byte cap swallowed producer exit status", len(output))
			}
		})
	}
}

func portProbeSizedRow(length int) string {
	const prefix = `tcp LISTEN 0 128 *:22 *:* users:(("`
	const suffix = `",pid=812,fd=4))`
	return prefix + strings.Repeat("x", length-len(prefix)-len(suffix)) + suffix
}

func runPortProbeFixture(t *testing.T, ss, netstat string) ([]byte, *PortSnapshot) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fixed Linux probe needs a POSIX shell; parsers run on all platforms")
	}
	bin := t.TempDir()
	for _, tool := range []string{"awk", "date"} {
		path, err := exec.LookPath(tool)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(path, filepath.Join(bin, tool)); err != nil {
			t.Fatal(err)
		}
	}
	for name, script := range map[string]string{"ss": ss, "netstat": netstat} {
		if script != "" {
			if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+script+"\n"), 0700); err != nil {
				t.Fatal(err)
			}
		}
	}
	cmd := exec.Command("/bin/sh", "-c", portCommand)
	cmd.Env = []string{"PATH=" + bin, "LC_ALL=C"}
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatal(err, string(output))
	}
	_, p := extractPortSnapshot(string(output), time.Now())
	if p == nil {
		t.Fatal("fixture probe emitted no snapshot", string(output))
	}
	return output, p
}
