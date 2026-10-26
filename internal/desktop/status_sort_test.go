package desktop

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/monitor"
)

func assertStatusRowOrder(t *testing.T, tt *ui.Tester, want ...string) {
	t.Helper()
	texts := tt.Texts()
	last := -1
	for _, label := range want {
		at := slices.Index(texts, label)
		if at < 0 || at <= last {
			t.Fatal("unexpected table order", want, texts)
		}
		last = at
	}
}

func TestProcessHeadersSortNumbersTextAndDurationBothWays(t *testing.T) {
	a := &app{}
	s := monitor.Stats{MemoryTotal: 100 << 20, Processes: []monitor.Process{
		{PID: 2, Name: "zeta", CPU: 9, RSS: 512, User: "zulu", State: "R", Elapsed: "59:59"},
		{PID: 10, Name: "beta", CPU: 80, RSS: 2 << 20, User: "bob", State: "S", Elapsed: "01:00:00"},
		{PID: 100, Name: "Alpha", CPU: 10, RSS: 1 << 20, User: "alice", State: "T", Elapsed: "1-00:00:00"},
	}}
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Fill().Children(func() { a.processView(c, &s) }) }, 1000, 420)
	assertStatusRowOrder(t, tt, "beta", "Alpha", "zeta")
	for _, test := range []struct {
		header, column string
		asc            []string
	}{
		{"进程", "name", []string{"Alpha", "beta", "zeta"}},
		{"PID", "pid", []string{"zeta", "beta", "Alpha"}},
		{"CPU %", "cpu", []string{"zeta", "Alpha", "beta"}},
		{"内存", "memory", []string{"zeta", "Alpha", "beta"}},
		{"账户", "user", []string{"Alpha", "beta", "zeta"}},
		{"状态", "state", []string{"zeta", "beta", "Alpha"}},
		{"运行时间", "elapsed", []string{"zeta", "beta", "Alpha"}},
	} {
		if err := tt.Click(test.header); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if a.processSort.Column != test.column || a.processSort.Descending {
			t.Fatal("header did not select ascending order", test, a.processSort)
		}
		assertStatusRowOrder(t, tt, test.asc...)
		if err := tt.Click(test.header); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if !a.processSort.Descending {
			t.Fatal("second click did not reverse", test.header)
		}
		want := slices.Clone(test.asc)
		slices.Reverse(want)
		assertStatusRowOrder(t, tt, want...)
	}
	// The incoming sample remains untouched, including its original order.
	if s.Processes[0].PID != 2 {
		t.Fatal("sorting modified the monitor sample")
	}
}

func TestServiceHeadersSortAndRetainChoiceAfterRefreshAndSearch(t *testing.T) {
	a := &app{}
	i := monitor.Inventory{Manager: "systemd", Services: []monitor.Service{
		{Name: "zeta.service", Active: "active", Sub: "running", Enabled: "enabled-runtime"},
		{Name: "alpha.service", Active: "inactive", Enabled: "enabled"},
		{Name: "beta.service", Active: "failed", Enabled: "enabled"},
	}}
	tt := ui.NewTester(func(c *ui.Context) { ui.Column(c).Fill().Children(func() { a.startupView(c, &i) }) }, 1000, 420)
	assertStatusRowOrder(t, tt, "alpha.service", "beta.service", "zeta.service")
	for _, test := range []struct {
		header, column string
		asc, desc      []string
	}{
		{"服务", "name", []string{"alpha.service", "beta.service", "zeta.service"}, []string{"zeta.service", "beta.service", "alpha.service"}},
		{"状态", "state", []string{"beta.service", "alpha.service", "zeta.service"}, []string{"zeta.service", "alpha.service", "beta.service"}},
		{"启用", "enabled", []string{"alpha.service", "beta.service", "zeta.service"}, []string{"zeta.service", "alpha.service", "beta.service"}},
	} {
		// The default name sort is already ascending; clicking it reverses.
		if test.column == "name" {
			if err := tt.Click(test.header); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			assertStatusRowOrder(t, tt, test.desc...)
			if err := tt.Click(test.header); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			assertStatusRowOrder(t, tt, test.asc...)
			continue
		}
		if err := tt.Click(test.header); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		assertStatusRowOrder(t, tt, test.asc...)
		if err := tt.Click(test.header); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		assertStatusRowOrder(t, tt, test.desc...)
	}
	before := a.serviceSort
	slices.Reverse(i.Services)
	tt.Frame()
	assertStatusRowOrder(t, tt, "zeta.service", "alpha.service", "beta.service")
	if err := tt.Click("搜索自启服务"); err != nil {
		t.Fatal(err)
	}
	tt.Type("beta")
	tt.Frame()
	if !tt.HasText("beta.service") || tt.HasText("zeta.service") || a.serviceSort != before {
		t.Fatal("filter lost the sort", tt.Texts(), a.serviceSort)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Key(0, ui.KeyBackspace)
	tt.Frame()
	assertStatusRowOrder(t, tt, "zeta.service", "alpha.service", "beta.service")
}

func TestProcessSortHandlesFallbackMemoryUnknownDurationAndTies(t *testing.T) {
	items := []monitor.Process{{PID: 10, Name: "same", Memory: 3, CPU: 8, Elapsed: ""}, {PID: 2, Name: "same", RSS: 1 << 20, CPU: 8, Elapsed: "00:00"}, {PID: 3, Name: "worker", RSS: 2 << 20, CPU: 8, Elapsed: "01:00:00"}}
	sortProcesses(items, ui.SortOrder{Column: "memory", Descending: true}, 100<<20)
	if items[0].PID != 10 || items[1].PID != 3 || items[2].PID != 2 {
		t.Fatal("mixed memory units sorted as labels", items)
	}
	for _, desc := range []bool{false, true} {
		sortProcesses(items, ui.SortOrder{Column: "elapsed", Descending: desc}, 100<<20)
		if items[len(items)-1].PID != 10 {
			t.Fatal("unknown duration was treated as zero", items)
		}
	}
	sortProcesses(items, ui.SortOrder{Column: "cpu", Descending: true}, 100<<20)
	if items[0].PID != 2 || items[1].PID != 10 {
		t.Fatal("tied samples are unstable", items)
	}
	for _, text := range []string{"oops", "-1:00", "99:99", "18446744073709551615-00:00:00", "18446744073709551615:00:00"} {
		if _, ok := elapsedSeconds(text); ok {
			t.Fatal("accepted invalid duration", text)
		}
	}
}
