package desktop

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path"
	"path/filepath"
	"strings"

	"sshclient/internal/sshconn"
	"sshclient/internal/transfer"
)

func (a *app) promptDownload(endpoint, dir string, entries []sshconn.File) {
	if endpoint == "" || endpoint == localEndpoint || len(entries) == 0 {
		return
	}
	entries = append([]sshconn.File(nil), entries...)
	home, err := os.UserHomeDir()
	if err != nil {
		a.status = err.Error()
		return
	}
	destination := filepath.Join(home, "Downloads")
	for _, pane := range a.fileLayout.panes() {
		if pane.endpoint == localEndpoint {
			destination = pane.browser().dir
			break
		}
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, reviewText(entry.Name))
	}
	a.ask(tr(nil, "下载到本机"), a.endpointName(endpoint)+" · "+dir,
		strings.Join(names, "\n")+tr(nil, "\n\n文件夹将递归下载，保留层级和空目录。已有目标不会覆盖；符号链接和特殊文件会明确报错。"),
		tr(nil, "本地目标文件夹（完整路径）"), destination, tr(nil, "开始下载"), func(value string) {
			if !filepath.IsAbs(value) {
				a.status = tr(nil, "请输入本地目标文件夹的完整路径")
				return
			}
			a.queueFileEntries(endpoint, dir, entries, localEndpoint, filepath.Clean(value))
		})
}

func (a *app) queueFileEntries(fromEndpoint, sourceDir string, entries []sshconn.File, toEndpoint, destinationDir string) {
	upload := fromEndpoint == localEndpoint
	host, direction := fromEndpoint, "download"
	if upload {
		host, direction = toEndpoint, "upload"
	}
	endpoint, fingerprint := "", ""
	for _, profile := range a.connections {
		if profile.ID == host {
			endpoint = fmt.Sprintf("%s@%s", profile.User, net.JoinHostPort(profile.Host, fmt.Sprint(profile.Port)))
			fingerprint = profile.Fingerprint
			break
		}
	}
	gen, manager, detached := a.generation, a.transfers, a.toolsWindowFocused()
	entries = append([]sshconn.File(nil), entries...)
	a.work(func() (func(), error) {
		if !upload {
			if endpoint == "" || fingerprint == "" {
				return nil, errors.New(tr(nil, "连接身份不可用，请刷新连接后重试"))
			}
			st, err := os.Stat(destinationDir)
			if err != nil {
				return nil, err
			}
			if !st.IsDir() {
				return nil, errors.New(tr(nil, "本地目标必须是文件夹"))
			}
		}
		ids := []string{}
		var failed error
		for _, entry := range entries {
			if entry.Name == "" || entry.Name == "." || entry.Name == ".." || strings.ContainsAny(entry.Name, "/\\\x00") {
				failed = errors.New(tr(nil, "文件名称无效"))
				break
			}
			local, remote := filepath.Join(sourceDir, entry.Name), path.Join(destinationDir, entry.Name)
			var job transfer.Job
			if upload {
				if entry.Directory {
					failed = errors.New(tr(nil, "目录上传尚不支持，请选择普通文件"))
					break
				}
				job, failed = manager.Add(host, direction, local, remote)
			} else {
				local, remote = filepath.Join(destinationDir, entry.Name), path.Join(sourceDir, entry.Name)
				job, failed = manager.AddDownload(host, local, remote, endpoint, fingerprint, entry.Directory)
			}
			if failed != nil {
				break
			}
			ids = append(ids, job.ID)
		}
		return func() {
			if a.generation != gen {
				return
			}
			a.jobs = manager.List()
			a.setTransferWindow(detached)
			a.transfersOpen = true
			for _, id := range ids {
				a.runTransfer(id)
			}
			if failed != nil {
				a.status = fmt.Sprintf(tr(nil, "已建立 %d 项，后续失败: %s"), len(ids), failed)
			}
		}, nil
	})
}
