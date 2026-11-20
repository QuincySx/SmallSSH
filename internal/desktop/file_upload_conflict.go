package desktop

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"slices"

	"sshclient/internal/approval"
	"sshclient/internal/client"
	"sshclient/internal/desktop/components"
	"sshclient/internal/transfer"
)

type uploadConflict struct {
	job          transfer.Job
	originalHash string
	host         dialogHost
}

func inspectUploadConflict(ctx context.Context, cli client.Client, owner client.OwnerProof, job transfer.Job, host dialogHost) (uploadConflict, error) {
	ctx = client.WithRoute(ctx, job.Host, job.Route)
	stream, err := cli.StartWithOwner(ctx, approval.Operation{
		Host: job.Host, Endpoint: job.Endpoint, HostFingerprint: job.HostFingerprint, Kind: "get", Path: job.Remote,
	}, owner)
	if err != nil {
		return uploadConflict{}, fmt.Errorf("%s: %w", tr(nil, "无法核对同名文件，未覆盖"), err)
	}
	defer stream.Close()
	sum := sha256.New()
	if _, _, err = stream.Copy(sum, io.Discard); err != nil {
		return uploadConflict{}, fmt.Errorf("%s: %w", tr(nil, "无法核对同名文件，未覆盖"), err)
	}
	return uploadConflict{job: job, originalHash: hex.EncodeToString(sum.Sum(nil)), host: host}, nil
}

func (a *app) showNextUploadConflict() {
	if len(a.uploadConflicts) == 0 || a.prompt.open || a.busy || !a.online {
		return
	}
	for len(a.uploadConflicts) > 0 {
		conflict := a.uploadConflicts[0]
		a.uploadConflicts = a.uploadConflicts[1:]
		job := conflict.job
		if a.transferCancelling[job.ID] || !slices.Contains(a.jobs, job) {
			continue
		}
		a.ask(tr(nil, "覆盖远程文件？"), a.endpointName(job.Host)+" · "+job.Remote,
			tr(nil, "远程已有同名文件。覆盖会保留旧副本。"), "", "", tr(nil, "覆盖"), func(string) {
				a.runFileTransfer(job.ID, &conflict)
			})
		a.prompt.size = components.DialogSmall
		a.prompt.danger = true
		a.prompt.cancelText = tr(nil, "跳过")
		a.prompt.host = conflict.host
		a.prompt.detached = conflict.host == dialogHostTools && a.toolsDetached && !a.toolsHidden
		if conflict.host == dialogHostTools && !a.prompt.detached {
			a.prompt.host = dialogHostMain
		}
		return
	}
}
