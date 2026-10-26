package main

import (
	"flag"
	"log"
	"os"

	"github.com/egoist/mygo"
	"sshclient/internal/desktop"
	"sshclient/internal/desktopcontrol"
	"sshclient/internal/localipc"
)

func main() {
	background := flag.Bool("background", mygo.IsDev() || os.Getenv("SSH_WORKBENCH_DEV_BACKGROUND") == "1", "后台启动，明确显示前不弹出窗口")
	dir := flag.String("data-dir", "", "私有数据目录；后台开发默认使用独立目录")
	flag.Parse()
	if *dir == "" {
		*dir = localipc.DefaultDir()
		if *background {
			*dir = desktopcontrol.DefaultDir()
			if devDir := os.Getenv("SSH_WORKBENCH_DEV_DATA_DIR"); devDir != "" {
				*dir = devDir
			}
		}
	}
	if e := desktop.RunWithOptions(*dir, desktop.Options{Background: *background}); e != nil {
		log.Fatal(e)
	}
}
