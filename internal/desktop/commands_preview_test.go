//go:build preview_qa

package desktop

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"sshclient/internal/snippet"
)

// Opt-in, offline snapshots for the saved-command panel and dialogs. They
// never start a window, a terminal or a broker.
func TestSavedCommandSnapshotsWithoutWindows(t *testing.T) {
	if os.Getenv("SSH_CLIENT_PREVIEW_QA") != "1" {
		t.Skip("opt-in offline snapshots")
	}
	dir := os.Getenv("SSH_CLIENT_UI_SNAPSHOT_DIR")
	if !filepath.IsAbs(dir) {
		t.Fatal("absolute snapshot directory required")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	commands := []snippet.Command{
		{ID: "update", Name: "更新系统", Group: "运维", Body: "sudo apt update && sudo apt upgrade -y $1",
			Variants: []snippet.Variant{{OS: snippet.OSRedHat, Body: "sudo dnf upgrade -y $1"}},
			Args:     []snippet.Arg{{Type: snippet.TypeText, Name: "软件包", Preset: "nginx"}}},
		{ID: "deploy", Name: "发布前端", Group: "发布", Body: "cd /srv/app && ./deploy.sh"},
		{ID: "disk", Name: "磁盘占用", Body: "df -h"},
	}
	for _, variant := range []string{"panel", "panel-narrow", "panel-filter", "empty", "editor", "editor-legacy", "run", "run-fixed", "run-dark", "run-history", "run-narrow", "run-no-history", "run-many"} {
		t.Run(variant, func(t *testing.T) {
			a := &app{online: true, snippets: commands, snippetSelected: 0}
			width, height, view := 760, 380, a.commandsView
			dark := variant == "run-dark"
			switch variant {
			case "panel-narrow":
				width = 380
			case "empty":
				a.snippets = nil
				width = 620
			case "panel-filter":
				a.snippetGroup = "运维"
			case "editor":
				a.newSnippet()
				a.snippetForm = snippet.Command{ID: "update", Name: "更新系统", Group: "运维",
					Body:     "sudo apt update && sudo apt upgrade -y $1",
					Variants: []snippet.Variant{{OS: snippet.OSRedHat, Body: "sudo dnf upgrade -y $1"}},
					Args:     []snippet.Arg{{Type: snippet.TypeText, Name: "软件包", Preset: "nginx"}}}
				a.syncSnippetArgs()
				view, width, height = a.snippetDialog, 640, 760
			case "editor-legacy":
				a.newSnippet()
				a.snippetForm.Name, a.snippetForm.Body = "字段统计", "awk '{print $1}' access.log | sort | uniq -c"
				view, width, height = a.snippetDialog, 640, 620
			case "run", "run-dark", "run-fixed", "run-history", "run-narrow", "run-no-history", "run-many":
				command := snippet.Command{ID: "inspect", Name: "查看容器", Body: "pct config $1", Args: []snippet.Arg{{Name: "ContainerID", Type: snippet.TypeInt, Preset: "100"}}}
				if variant == "run-fixed" {
					command.Args[0].SkipPrompt = true
				}
				if variant == "run-no-history" {
					command.Args[0].NoHistory = true
				}
				if variant == "run-many" {
					command.Body = "pct set $1 -hostname $2 -memory $3 -cores $4"
					command.Args = append(command.Args, snippet.Arg{Name: "名称", Preset: "web"}, snippet.Arg{Name: "内存 MiB", Type: snippet.TypeInt, Preset: "1024"}, snippet.Arg{Name: "CPU 核心", Type: snippet.TypeInt, Preset: "2"})
				}
				state := &snippetRunState{command: command, label: "PVE 测试节点 · example@192.0.2.10:22", system: snippet.OSDefault}
				a.fillSnippetValues(state)
				state.histories = [][]string{{"101", "102", "200", "201"}}
				a.prompt = promptState{open: true, snippet: state, target: state.label}
				view, width, height = a.promptView, 620, 640
				if variant == "run-narrow" {
					width = 420
				}
				if variant == "run-many" {
					height = 740
				}
			}
			tt := ui.NewTester(view, width, height)
			tt.SetDark(dark)
			if variant == "run-history" {
				if err := tt.Click("参数 1 历史"); err != nil {
					t.Fatal(err)
				}
			}
			file, err := os.OpenFile(filepath.Join(dir, "commands-"+variant+".png"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
			if err != nil {
				t.Fatal(err)
			}
			err = png.Encode(file, tt.Image())
			closeErr := file.Close()
			if err != nil || closeErr != nil {
				t.Fatal(err, closeErr)
			}
		})
	}
}
