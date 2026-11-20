//go:build preview_qa

package desktop

import (
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/desktop/components"
	"sshclient/internal/monitor"
)

func TestWindowsMetricsSnapshotsWithoutWindows(t *testing.T) {
	if os.Getenv("SSH_CLIENT_PREVIEW_QA") != "1" {
		t.Skip("opt-in offline preview")
	}
	directory := os.Getenv("SSH_CLIENT_UI_SNAPSHOT_DIR")
	base, _ := filepath.Abs("../../build")
	relative, err := filepath.Rel(base, directory)
	if err != nil || !filepath.IsAbs(directory) || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		t.Fatal("snapshot output must be inside build")
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	for _, dark := range []bool{false, true} {
		name := "light"
		if dark {
			name = "dark"
		}
		t.Run(name, func(t *testing.T) {
			a := statusDemo()
			a.statusMetric = "CPU"
			sess := a.sessions[0]
			sess.stats.Time = time.Now().Add(-12 * time.Second)
			sess.stats.SampleIntervalSeconds = 10
			sess.stats.ProcessCount, sess.stats.RunningProcesses = 137, -1
			sess.stats.CPU, sess.stats.Load = 40, ""
			sess.stats.Inventory = &monitor.Inventory{OS: "Windows Server", Hostname: "windows-fixture", Architecture: "AMD64", Cores: 2}
			sess.stats.MemoryTotal, sess.stats.MemoryUsed, sess.stats.MemoryAvailable = 8<<30, 4<<30, 4<<30
			sess.stats.CPUDetail = monitor.CPUUsage{Name: "cpu", Used: 40, User: 25, System: 15, Valid: true, BasicOnly: true}
			sess.stats.Cores = []monitor.CPUUsage{
				{Name: "cpu0", Used: 20, User: 12, System: 8, Valid: true, BasicOnly: true},
				{Name: "cpu1", Used: 60, User: 38, System: 22, Valid: true, BasicOnly: true},
			}
			sess.history = statusHistory{}
			sess.history.add(sess.stats)
			view := ui.NewTester(func(c *ui.Context) {
				components.ApplyTheme(c, false)
				ui.Column(c).Fill().Padding(12).Gap(10).Children(func() {
					a.statusIndicator(c, sess)
					a.systemSummary(c, sess.stats)
					a.resourceDetail(c, sess)
				})
			}, 900, 900)
			view.SetDark(dark)
			if !view.HasText("137 任务 · 运行数未知") || !view.HasText("25.0%") || !view.HasText("15.0%") || !view.HasText("● 实时") {
				t.Fatal("Windows actual/unknown data or normal cadence is missing", view.Texts())
			}
			file, err := os.Create(filepath.Join(directory, "windows-metrics-"+name+".png"))
			if err != nil {
				t.Fatal(err)
			}
			encodeErr := png.Encode(file, view.Image())
			closeErr := file.Close()
			if encodeErr != nil || closeErr != nil {
				t.Fatal(encodeErr, closeErr)
			}
		})
	}
}
