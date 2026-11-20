package desktop

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path"
	"path/filepath"
	"strings"

	"sshclient/internal/connectcfg"
	"sshclient/internal/sshdata"
	"sshclient/internal/transfer"
)

func (a *app) promptDownload(endpoint, dir string, entries []sshdata.File, selectedRoute ...connectcfg.RouteSelection) {
	if endpoint == "" || endpoint == localEndpoint || len(entries) == 0 {
		return
	}
	entries = append([]sshdata.File(nil), entries...)
	route := a.fileRoute(endpoint, selectedRoute...)
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
			a.queueFileEntries(endpoint, dir, entries, localEndpoint, filepath.Clean(value), route)
		})
}

type fileTransferItem struct {
	name, source string
	directory    bool
}

func (a *app) queueFileEntries(fromEndpoint, sourceDir string, entries []sshdata.File, toEndpoint, destinationDir string, selectedRoute ...connectcfg.RouteSelection) {
	upload := fromEndpoint == localEndpoint
	host, direction := fromEndpoint, "download"
	if upload {
		host, direction = toEndpoint, "upload"
	}
	items := make([]fileTransferItem, 0, len(entries))
	for _, entry := range entries {
		source := joinPath(upload, sourceDir, entry.Name)
		items = append(items, fileTransferItem{name: entry.Name, source: source, directory: entry.Directory})
	}
	a.queueFileTransfers(host, direction, destinationDir, a.fileRoute(host, selectedRoute...), items)
}

func (a *app) queueFileTransfers(host, direction, destinationDir string, route connectcfg.RouteSelection, entries []fileTransferItem) {
	upload := direction == "upload"
	endpoint, fingerprint := "", ""
	for _, profile := range a.connections {
		if profile.ID == host {
			endpoint = fmt.Sprintf("%s@%s", profile.User, net.JoinHostPort(profile.Host, fmt.Sprint(profile.Port)))
			fingerprint = profile.Fingerprint
			break
		}
	}
	gen, manager, detached := a.generation, a.transfers, a.toolsWindowFocused()
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
			if entry.name == "" || entry.name == "." || entry.name == ".." || strings.ContainsAny(entry.name, "/\\\x00") {
				failed = errors.New(tr(nil, "文件名称无效"))
				break
			}
			var job transfer.Job
			if upload {
				job, failed = queueUploadFile(manager, host, destinationDir, entry, route)
			} else {
				job, failed = manager.AddDownload(host, filepath.Join(destinationDir, entry.name), entry.source, endpoint, fingerprint, entry.directory, route)
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

func queueUploadFile(manager *transfer.Manager, host, destination string, item fileTransferItem, route connectcfg.RouteSelection) (transfer.Job, error) {
	if !filepath.IsAbs(item.source) {
		return transfer.Job{}, errors.New(tr(nil, "请选择普通文件"))
	}
	info, err := os.Lstat(item.source)
	if err != nil {
		return transfer.Job{}, err
	}
	if item.directory || info.IsDir() {
		return transfer.Job{}, errors.New(tr(nil, "目录上传尚不支持，请选择普通文件"))
	}
	if !info.Mode().IsRegular() {
		return transfer.Job{}, errors.New(tr(nil, "请选择普通文件"))
	}
	return manager.Add(host, "upload", item.source, path.Join(destination, item.name), route)
}
