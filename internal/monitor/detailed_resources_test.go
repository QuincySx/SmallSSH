package monitor

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
)

func TestInterfacesAddressesAndVirtualLinks(t *testing.T) {
	i := ParseInventory(`@@INTERFACES
eth0 up 0 00:11:22:33:44:55 1500
veth42 up 1 00:11:22:33:44:56 1500
lo unknown 1 00:00:00:00:00:00 65536
@@ADDRESSES
2: eth0 inet 192.0.2.10/24 brd 192.0.2.255 scope global eth0
2: eth0 inet6 2001:db8::10/64 scope global
3: veth42@if4 inet 172.19.0.1/16 scope global veth42
1: lo inet 127.0.0.1/8 scope host lo
`)
	if len(i.Interfaces) != 3 || len(i.Interfaces[0].Addresses) != 2 || i.Interfaces[0].Virtual || !i.Interfaces[2].Virtual || i.Interfaces[2].Addresses[0] != "172.19.0.1/16" {
		t.Fatal(i.Interfaces)
	}
	if i.Interfaces[0].MTU != 1500 || i.Interfaces[0].State != "up" {
		t.Fatal(i.Interfaces[0])
	}
}

func TestTCPRetransmissionUsesDeltasAndKeepsLoopbackOutOfTotal(t *testing.T) {
	before, err := Parse(sample("100 0 100 800 0 0 0 0", 100, 200)+"@@TCP\nTcp: RtoAlgorithm ActiveOpens PassiveOpens CurrEstab RetransSegs OutSegs\nTcp: 1 100 50 8 10 1000\n", nil)
	if err != nil {
		t.Fatal(err)
	}
	before.Time = time.Now().Add(-2 * time.Second)
	after, err := Parse(sample("110 0 110 880 0 0 0 0", 200, 300)+"@@TCP\nTcp: RtoAlgorithm ActiveOpens PassiveOpens CurrEstab RetransSegs OutSegs\nTcp: 1 110 60 9 11 1100\n", &before)
	if err != nil {
		t.Fatal(err)
	}
	if !after.TCP.RateValid || after.TCP.RetransmitPercent != 1 || after.TCP.Established != 9 || len(after.Networks) != 2 || after.RX != 200 || after.PrimaryNetwork() != "eth0" {
		t.Fatal(after.TCP, after.Networks, after.RX)
	}
	reset, _ := Parse(sample("1 0 1 8 0 0 0 0", 1, 1)+"@@TCP\nTcp: RtoAlgorithm ActiveOpens PassiveOpens CurrEstab RetransSegs OutSegs\nTcp: 1 1 1 1 1 1\n", &after)
	if reset.TCP.RateValid {
		t.Fatal("reset fabricated retransmission rate")
	}
}

func TestGuestAndStealDoNotDoubleCountCPUTime(t *testing.T) {
	before, err := Parse(sample("100 0 100 800 0 0 0 0 50 0", 1, 2), nil)
	if err != nil {
		t.Fatal(err)
	}
	after, err := Parse(sample("150 0 120 820 0 0 0 10 60 0", 2, 3), &before)
	if err != nil {
		t.Fatal(err)
	}
	u := after.CPUDetail
	if !u.Valid || u.User != 40 || u.Guest != 10 || u.System != 20 || u.Steal != 10 || after.CPU != 80 {
		t.Fatal(u)
	}
}

func TestContainerStatsUnitsUnknownsAndMemoryLimit(t *testing.T) {
	id := strings.Repeat("a", 64)
	text := fmt.Sprintf("@@DOCKER_STATUS\nready\n@@DOCKER_SOCKET\n/var/run/docker.sock\n@@DOCKER_INFO\n{\"ID\":\"engine\"}\n@@DOCKER_STATS\n{\"ID\":%q,\"CPUPerc\":\"203.50%%\",\"MemPerc\":\"10.0%%\",\"MemUsage\":\"205MiB / 2GiB\",\"NetIO\":\"21GB / 100MB\",\"BlockIO\":\"606MB / 38GB\",\"PIDs\":\"12\"}\n", id)
	s := ParseDockerSnapshot(text)
	if s.Status != "ready" || len(s.Rows) != 1 {
		t.Fatal(s)
	}
	r := s.Rows[0]
	if r.CPU != 203.5 || !r.MemoryValid || r.Memory != 205<<20 || r.Limit != 2<<30 || r.RX != 21000000000 || r.Write != 38000000000 || !r.NetworkValid || r.PIDs != 12 {
		t.Fatal(r)
	}
	for _, invalid := range []string{"--", "NaN%", "Inf%"} {
		line := fmt.Sprintf(`{"ID":%q,"CPUPerc":%q,"MemUsage":"--","NetIO":"--","BlockIO":"--"}`, id, invalid)
		r, ok := parseContainerStats(line)
		if !ok || r.CPUValid || r.MemoryValid || r.NetworkValid || r.BlockValid {
			t.Fatal(r, ok)
		}
	}
	if _, ok := ByteAmount("NaNMiB"); ok {
		t.Fatal("invalid byte value accepted")
	}
	v, ok := ByteAmount("2.5 kB")
	if !ok || math.Abs(v-2500) > .1 {
		t.Fatal(v, ok)
	}
}

func TestMountPathsTypesCapacityAndReadOnlyRemainSeparate(t *testing.T) {
	text := sample("100 0 100 800 0 0 0 0", 1, 2) + `@@DISK
Filesystem Type 1024-blocks Used Available Capacity Mounted on
overlayfs:/overlay overlay 75776 71680 4096 95% /
/dev/ubi0_2 ubifs 75776 71680 4096 95% /overlay
/dev/root squashfs 16384 16384 0 100% /rom
/dev/vdb1 ext4 100000 20000 80000 20% /mnt/my disk
@@MOUNT_FLAGS
overlayfs:/overlay / overlay rw,relatime 0 0
/dev/ubi0_2 /overlay ubifs rw,relatime 0 0
/dev/root /rom squashfs ro,relatime 0 0
/dev/vdb1 /mnt/my\040disk ext4 ro,relatime 0 0
`
	s, err := Parse(text, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Disks) != 5 {
		t.Fatal(s.Disks)
	}
	for _, d := range s.Disks {
		if d.Mount == "/rom" && (!d.ReadOnly || !d.ModeKnown || d.Filesystem != "squashfs" || d.Total != 16<<20) {
			t.Fatal(d)
		}
		if d.Mount == "/" && (d.ReadOnly || !d.ModeKnown) {
			t.Fatal(d)
		}
		if d.Mount == "/mnt/my disk" && !d.ReadOnly {
			t.Fatal("escaped mount point not matched", d)
		}
	}
}
