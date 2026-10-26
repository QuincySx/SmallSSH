package desktop

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/monitor"
)

func composeProjectsDemo() *app {
	a := responsiveWorkspaceDemo()
	a.diagnosticTab = 4
	s := a.sessions[0]
	s.target.Endpoint, s.target.HostFingerprint = "root@192.0.2.10:22", "SHA256:fixture"
	d := s.stats.Inventory.Docker
	d.Containers = nil
	for i, spec := range []struct{ project, service, state, status string }{
		{"blog", "web", "running", "Up 1 day (healthy)"},
		{"blog", "db", "running", "Up 1 day (unhealthy)"},
		{"blog", "cache", "running", "Up 1 day"},
		{"blog", "worker", "paused", "Up 1 day (Paused)"},
		{"docs", "web", "exited", "Exited (0) 1 hour ago"},
		{"jobs", "worker", "running", "Up 1 day"},
	} {
		d.Containers = append(d.Containers, monitor.Container{ID: fmt.Sprintf("%064x", i+1), Names: spec.project + "-" + spec.service + "-1", Service: spec.service, Project: spec.project, State: spec.state, Status: spec.status, Image: "example/" + spec.service + ":stable", ComposeDir: "/srv/" + spec.project, ComposeFiles: "/srv/" + spec.project + "/compose.yaml"})
	}
	s.stats.DockerStats = &monitor.DockerSnapshot{Time: time.Now(), Status: "ready", EngineID: d.EngineID, Socket: d.Socket}
	for i, row := range d.Containers[:4] {
		s.stats.DockerStats.Rows = append(s.stats.DockerStats.Rows, monitor.ContainerStats{ID: row.ID, CPU: []float64{30, 10, 5, 0}[i], Memory: []uint64{64 << 20, 128 << 20, 32 << 20, 1 << 20}[i], CPUValid: true, MemoryValid: true})
	}
	a.managementState().dockerTab = 2
	return a
}

func TestComposeProjectCardsDrillDownUpdateAndReturnWithoutMovingTerminal(t *testing.T) {
	a := composeProjectsDemo()
	t.Cleanup(a.cancelDockerWork)
	tt := ui.NewTester(a.view, 1280, 1000)
	terminal, _ := tt.Find("终端区域")
	d := a.sessions[0].stats.Inventory.Docker
	for _, label := range []string{"Compose 项目 blog", "Compose 项目 docs", "Compose 项目 jobs", "45.0%", "225.0 MiB", "● 需关注", "不健康", "未采集", "另有 1 个容器 ›"} {
		if _, found := tt.Find(label); !found {
			t.Fatal("project summary missing", label, tt.Texts())
		}
	}
	if _, found := tt.Find("容器 " + d.Containers[0].ID); found {
		t.Fatal("project list still repeats full container cards")
	}
	if err := tt.Click("Compose 项目 blog"); err != nil {
		t.Fatal(err)
	}
	if a.managementState().composeProject != "blog" || !tt.HasText("/srv/blog") {
		t.Fatal("project did not open", tt.Texts())
	}
	if _, found := tt.Find("容器 " + d.Containers[4].ID); found {
		t.Fatal("another project's container leaked into details")
	}
	if err := chooseComposeProjectMenu(tt, "blog", "更新项目…"); err != nil {
		t.Fatal(err)
	}
	if !a.prompt.open || a.prompt.docker.compose.Project != "blog" || len(a.prompt.docker.services) != 4 || a.prompt.docker.sourceError != "" {
		t.Fatal("update did not capture this project's services", a.prompt.docker)
	}
	if err := tt.Click("取消"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("查看容器 blog-db-1"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("返回 Compose 项目"); err != nil {
		t.Fatal(err)
	}
	if a.managementState().composeProject != "blog" || a.managementState().containerDetails != nil {
		t.Fatal("container return lost the project")
	}
	if err := tt.Click("返回 Compose 项目列表"); err != nil {
		t.Fatal(err)
	}
	current, _ := tt.Find("终端区域")
	if a.managementState().composeProject != "" || current != terminal {
		t.Fatal("inspection changed the terminal layout", terminal, current)
	}
	if err := tt.Click("搜索 Docker"); err != nil {
		t.Fatal(err)
	}
	tt.Type("example/db")
	if _, found := tt.Find("Compose 项目 blog"); !found {
		t.Fatal("container/image search did not find its project")
	}
	if _, found := tt.Find("Compose 项目 docs"); found {
		t.Fatal("search did not filter projects")
	}
	if err := tt.Click("Compose 项目 blog"); err != nil {
		t.Fatal(err)
	}
	if _, found := tt.Find("容器 " + d.Containers[0].ID); found {
		t.Fatal("project detail search did not filter containers")
	}
	if err := chooseComposeProjectMenu(tt, "blog", "更新项目…"); err != nil {
		t.Fatal(err)
	}
	if len(a.prompt.docker.services) != 4 {
		t.Fatal("a display filter changed the project update scope")
	}
}

func TestComposeProjectCardsAndDetailsReflowAndReserveScrollbarSpace(t *testing.T) {
	a := composeProjectsDemo()
	a.fileExpanded = true
	tt := ui.NewTester(a.view, 1600, 1000)
	for _, width := range []int{1600, 560, 1000, 1280, 2560, 560} {
		tt.SetSize(width, 1000)
		grid, _ := tt.Find("Compose 项目卡片")
		first, ok := tt.Find("Compose 项目 blog")
		second, ok2 := tt.Find("Compose 项目 docs")
		if !ok || !ok2 || first.W < 300 || first.X < grid.X || second.X+second.W > grid.X+grid.W-statusScrollGutter+1 || (first.Y == second.Y) != (width >= 1280) {
			t.Fatal("project cards did not fill/reflow safely", width, grid, first, second)
		}
	}
	d := a.sessions[0].stats.Inventory.Docker
	d.Containers = d.Containers[:4]
	tt.SetSize(2560, 1000)
	card, _ := tt.Find("Compose 项目 blog")
	grid, _ := tt.Find("Compose 项目卡片")
	if card.W < grid.W-statusScrollGutter-4 {
		t.Fatal("a single project reserved an empty column", card, grid)
	}
	if err := tt.Click("Compose 项目 blog"); err != nil {
		t.Fatal(err)
	}
	for _, width := range []int{1600, 560, 1000, 1280, 2560, 560} {
		tt.SetSize(width, 1000)
		grid, _ := tt.Find("Compose 项目容器")
		first, ok := tt.Find("容器 " + d.Containers[1].ID)
		second, ok2 := tt.Find("容器 " + d.Containers[3].ID)
		update, action := tt.Find("项目菜单 blog")
		if !ok || !ok2 || first.W < 300 || second.X+second.W > grid.X+grid.W-statusScrollGutter+1 || (first.Y == second.Y) != (width >= 1280) || !action || update.X+update.W > float32(width) {
			t.Fatal("project details did not reflow", width, first, second, update, grid)
		}
	}
}

func TestComposeProjectRefreshMissingSamplesAndSourceConflictsStayHonest(t *testing.T) {
	a := composeProjectsDemo()
	a.fileExpanded = true
	tt := ui.NewTester(a.view, 1280, 900)
	s := a.sessions[0]
	d := s.stats.Inventory.Docker
	s.stats.DockerStats.Rows = s.stats.DockerStats.Rows[:1]
	tt.Frame()
	if !tt.HasText("30.0% · 部分") || !tt.HasText("64.0 MiB · 部分") {
		t.Fatal("partial aggregate pretends to be a total", tt.Texts())
	}
	s.stats.DockerStats.Time = time.Now().Add(-time.Minute)
	tt.Frame()
	if tt.HasText("30.0%") || !tt.HasText("未采集") {
		t.Fatal("stale resource data retained")
	}
	if err := tt.Click("Compose 项目 blog"); err != nil {
		t.Fatal(err)
	}
	d.Containers[1].ComposeDir = "/different"
	tt.Frame()
	if !tt.HasText("配置来源不一致") {
		t.Fatal(tt.Texts())
	}
	if err := chooseComposeProjectMenu(tt, "blog", "更新项目…"); err == nil {
		t.Fatal("conflicting source menu item is enabled")
	}
	if a.prompt.open {
		t.Fatal("conflicting sources allowed an update")
	}
	d.EngineID = "replacement-engine"
	tt.Frame()
	if !tt.HasText("项目已不在当前 Docker 清单中") {
		t.Fatal("selection silently followed a replacement engine")
	}
	if _, ok := tt.Find("项目菜单 blog"); ok {
		t.Fatal("stale project exposed update")
	}
	if err := tt.Click("返回 Compose 项目列表"); err != nil {
		t.Fatal(err)
	}
	d.Containers = []monitor.Container{{ID: strings.Repeat("a", 64), Names: "blog-db-1", State: "running"}}
	tt.Frame()
	if !tt.HasText("未发现 Compose 项目") {
		t.Fatal("project was guessed from the container name")
	}
}
