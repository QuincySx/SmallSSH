package desktop

import (
	"github.com/egoist/mygo/ui"
	"sshclient/internal/monitor"
	"testing"
	"time"
)

func TestWindowsUnknownRunningCountIsExplicitInSummary(t *testing.T) {
	a := &app{}
	stats := &monitor.Stats{ProcessCount: 137, RunningProcesses: -1}
	view := ui.NewTester(func(c *ui.Context) { a.systemSummary(c, stats) }, 700, 90)
	if !view.HasText("137 任务 · 运行数未知") || view.HasText("137 任务 · -1 运行") {
		t.Fatal("unknown running count shown as measured", view.Texts())
	}
	stats.ProcessCount = -1
	view = ui.NewTester(func(c *ui.Context) { a.systemSummary(c, stats) }, 700, 90)
	if !view.HasText("任务数量未知") || view.HasText("-1 任务 · 运行数未知") || view.HasText("0 任务 · 0 运行") {
		t.Fatal("missing process count shown as measured", view.Texts())
	}
}

func TestWindowsStatusUsesActualCadenceWithoutFalseDelay(t *testing.T) {
	a := &app{}
	sess := &session{stats: &monitor.Stats{Time: time.Now().Add(-12 * time.Second), SampleIntervalSeconds: 10}}
	view := ui.NewTester(func(c *ui.Context) { a.statusIndicator(c, sess) }, 700, 90)
	if !view.HasText("● 实时") {
		t.Fatal("normal Windows sample interval was called delayed", view.Texts())
	}
	sess.stats.Time = time.Now().Add(-31 * time.Second)
	view = ui.NewTester(func(c *ui.Context) { a.statusIndicator(c, sess) }, 700, 90)
	if view.HasText("● 实时") {
		t.Fatal("missed Windows sample periods were not called delayed", view.Texts())
	}
	if statusSampleInterval(&monitor.Stats{}) != 2*time.Second || statusSampleInterval(sess.stats) != 10*time.Second {
		t.Fatal("legacy and Windows sample periods lost their defaults")
	}
}

func TestWindowsCPUOmitsUnavailableDetailAndTrendValues(t *testing.T) {
	a := statusDemo()
	a.statusMetric = "CPU"
	sess := a.sessions[0]
	sess.stats.CPU, sess.stats.Load = 40, ""
	sess.stats.CPUDetail = monitor.CPUUsage{Name: "cpu", Used: 40, User: 25, System: 15, Valid: true, BasicOnly: true}
	sess.stats.Cores = []monitor.CPUUsage{{Name: "cpu0", Used: 40, User: 25, System: 15, Valid: true, BasicOnly: true}}
	sess.stats.SampleIntervalSeconds = 10
	sess.history = statusHistory{}
	sess.history.add(sess.stats)
	point := sess.history.points[0]
	if !point.cpuValid || point.cpuExtraValid || statusPointMaxGap(point) <= 10*time.Second || statusPointMaxGap(statusPoint{}) != 8*time.Second {
		t.Fatal("Windows history invented Linux detail or split ordinary samples", point)
	}
	if parts := coreParts(nil, sess.stats.Cores[0]); len(parts) != 2 {
		t.Fatal("core meter included unmeasured Windows components", parts)
	}
	view := ui.NewTester(func(c *ui.Context) { a.resourceDetail(c, sess) }, 900, 900)
	unknown := 0
	for _, text := range view.Texts() {
		if text == "—" {
			unknown++
		}
	}
	if unknown < 6 || coreText(sess.stats) != "1 核 · 负载 —" {
		t.Fatal("unmeasured CPU details or load have blank or fabricated values", view.Texts())
	}
	zero := false
	for _, text := range view.Texts() {
		zero = zero || text == "0.0%"
	}
	if !view.HasText("25.0%") || !view.HasText("15.0%") || zero {
		t.Fatal("CPU detail omitted actual data or displayed invented zeroes", view.Texts())
	}
}
