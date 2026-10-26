package desktop

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/monitor"
	"sshclient/internal/serverops"
)

func TestFileSourceHeaderKeepsResolvedServerAndFollowMode(t *testing.T) {
	a := fileFollowDemo()
	p := a.primaryFilePane
	tt := ui.NewTester(func(c *ui.Context) { a.fileLocationHeader(c, p) }, 240, 40)
	if !tt.HasText("连接 A") {
		t.Fatal("following mode hid the actual server", tt.Texts())
	}
	if _, ok := tt.Find("文件来源跟随终端"); !ok {
		t.Fatal("following mode lost its accessible indicator")
	}
	a.active = 1
	a.syncFollowingFilePanes()
	tt.Frame()
	if !tt.HasText("连接 B") || tt.HasText("连接 A") {
		t.Fatal("following header retained the old server", tt.Texts())
	}
	a.sessions[1].target.Kind = "docker-terminal"
	tt.Frame()
	if _, ok := tt.Find("容器宿主机文件来源"); !ok || !tt.HasText("连接 B") {
		t.Fatal("container context hid the host file source")
	}
	a.chooseFileEndpoint(p, "c")
	tt.Frame()
	if !tt.HasText("连接 C") {
		t.Fatal("pinned source was not shown", tt.Texts())
	}
	if _, ok := tt.Find("文件来源跟随终端"); ok {
		t.Fatal("pinned source still advertised following")
	}
	for _, width := range []int{160, 240, 420} {
		tt.SetSize(width, 40)
		bounds, ok := tt.Find("文件位置")
		if !ok || bounds.X < 0 || bounds.X+bounds.W > float32(width)+1 {
			t.Fatal("source control overflowed its header", width, bounds)
		}
	}
}

func TestComposePreviewKeepsChangedPlanImagesAndRiskVisible(t *testing.T) {
	a := managementApp()
	d := a.sessions[0].stats.Inventory.Docker
	row := d.Containers[0]
	row.Project, row.Service = "site", "web"
	row.ComposeDir, row.ComposeFiles = "/srv/site", "/srv/site/compose.yaml"
	a.openComposeUpdate([]monitor.Container{row}, d)
	s := a.prompt.docker
	s.plan = &serverops.ComposePlan{Hash: strings.Repeat("a", 64), Services: []string{"web"}, Images: []string{"example/site:next"}}
	planned := slices.Clone(s.plan.Images)
	tt := ui.NewTester(a.promptView, 760, 650)
	if s.detailsExpanded || !tt.HasText("example/site:next") || !tt.HasText("计划镜像与当前容器不同") {
		t.Fatal("changed plan target was hidden behind collapsed details", tt.Texts())
	}
	if !tt.HasText("将拉取镜像并重建所选服务，可能短暂中断。") {
		t.Fatal("recreate risk was hidden")
	}
	if !slices.Equal(planned, s.plan.Images) {
		t.Fatal("presentation mutated the approved image plan")
	}
	s.plan.Images = []string{row.Image}
	tt.Frame()
	if tt.HasText("计划镜像与当前容器不同") || s.detailsExpanded {
		t.Fatal("identical images were presented as a change")
	}
	if err := tt.Click("配置与计划详情"); err != nil {
		t.Fatal(err)
	}
	if !s.detailsExpanded || !tt.HasText("/srv/site") || !tt.HasText("计划镜像") {
		t.Fatal("the full plan could not be reviewed", tt.Texts())
	}
	s.sourceError = "配置来源不一致"
	tt.Frame()
	if !tt.HasText(s.sourceError) {
		t.Fatal("source mismatch was hidden")
	}
}

func TestDockerSamplingWarningsRemainVisibleWithCompactFooter(t *testing.T) {
	a := managementApp()
	s := a.sessions[0]
	s.stats.Inventory.Docker.Note = "容器清单已截断"
	s.stats.DockerStats = &monitor.DockerSnapshot{Time: time.Now(), Status: "unavailable", Note: "容器资源读取超时或失败"}
	tt := ui.NewTester(func(c *ui.Context) { a.dockerView(c, s.stats.Inventory) }, 700, 520)
	for _, message := range []string{"容器清单已截断", "容器资源读取超时或失败"} {
		if !tt.HasText(message) {
			t.Fatal("important Docker warning was hidden", message)
		}
	}
	if _, ok := tt.Find("Docker 清单摘要"); !ok {
		t.Fatal("compact inventory count was missing")
	}
}
