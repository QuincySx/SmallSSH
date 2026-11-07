package desktop

import (
	"fmt"
	"sshclient/internal/sshconn"
)

func filePaneCounts(files []sshconn.File, selected int) string {
	count, dirs := 0, 0
	for _, f := range files {
		if f.Directory {
			dirs++
		} else {
			count++
		}
	}
	return fmt.Sprintf(tr(nil, "%d 个文件 · %d 个目录"), count, dirs) + fmt.Sprintf(tr(nil, " · 已选 %d 项"), selected)
}
