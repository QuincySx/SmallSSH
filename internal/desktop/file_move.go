package desktop

import (
	"context"
	"fmt"
	"io"
	"strings"

	"sshclient/internal/approval"
	"sshclient/internal/client"
	"sshclient/internal/sshdata"
)

func (a *app) canMoveDroppedFiles(d fileDrag, p *filePane, dir string) bool {
	if !a.online || a.busy || d.endpoint == "" || d.endpoint != p.endpoint ||
		d.browser == nil || d.browser.dir != d.dir || dir == "" || dir == d.dir {
		return false
	}
	local := d.endpoint == localEndpoint
	if !local && d.browser.route != p.route {
		return false
	}
	for _, file := range d.entries() {
		if file.Name == "" || file.Name == "." || file.Name == ".." ||
			strings.ContainsAny(file.Name, "/\x00") || baseName(local, file.Name) != file.Name {
			return false
		}
		if file.Directory && directoryContains(local, joinPath(local, d.dir, file.Name), dir) {
			return false
		}
	}
	return true
}

func (a *app) moveDroppedFiles(d fileDrag, dir string) {
	entries := append([]sshdata.File(nil), d.entries()...)
	ctx := client.WithRoute(a.operationContext(), d.endpoint, d.browser.route)
	cli := a.client
	local := d.endpoint == localEndpoint
	var owner client.OwnerProof
	if !local {
		var err error
		owner, err = a.unlockedOwner()
		if err != nil {
			d.browser.moveFeedback = reviewText(err.Error())
			return
		}
	}
	d.browser.moveFeedback = tr(nil, "正在移动…")
	a.work(func() (func(), error) {
		results := make([]error, len(entries))
		for i, file := range entries {
			source := joinPath(local, d.dir, file.Name)
			destination := joinPath(local, dir, file.Name)
			results[i] = renameDroppedFile(ctx, cli, owner, d.endpoint, source, destination)
		}
		return func() {
			var failures []string
			for i, file := range entries {
				if results[i] != nil {
					failures = append(failures, reviewText(file.Name)+": "+reviewText(fileMutationError(results[i])))
					continue
				}
				source := joinPath(local, d.dir, file.Name)
				destination := joinPath(local, dir, file.Name)
				a.fileMutationCompleted(d.endpoint, d.browser, d.dir, "rename", source, destination, 0, file.Directory)
				if d.browser.dir == d.dir {
					d.browser.chosen.Remove(file.Name)
				}
			}
			message := fmt.Sprintf(tr(nil, "已移动 %d/%d 项"), len(entries)-len(failures), len(entries))
			if len(failures) > 0 {
				message += " · " + tr(nil, "移动失败") + ": " + strings.Join(failures, " · ")
			}
			a.status = message
			if d.browser.dir == d.dir {
				d.browser.selected, d.browser.anchor = -1, -1
				if remaining := chosenFiles(d.browser); len(remaining) > 0 {
					d.browser.selected = d.browser.filesByName[remaining[0].Name]
				}
				d.browser.moveFeedback = message
			}
		}, nil
	})
}

func renameDroppedFile(ctx context.Context, cli client.Client, owner client.OwnerProof, endpoint, source, destination string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if endpoint == localEndpoint {
		return mutateLocal("rename", source, destination, 0)
	}
	stream, err := cli.StartWithOwner(ctx, approval.Operation{Host: endpoint, Kind: "rename", Path: source, Destination: destination}, owner)
	if err != nil {
		return err
	}
	defer stream.Close()
	_, _, err = stream.Copy(io.Discard, io.Discard)
	return err
}
