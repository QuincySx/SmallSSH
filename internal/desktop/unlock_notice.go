package desktop

import (
	"context"
	"time"

	"sshclient/internal/client"
	"sshclient/internal/protocol"
)

func (a *app) pollUnlockNotice(epoch uint64, reader *client.ServiceStatusReader) *client.ServiceStatusReader {
	if a.unlockService.Executable == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(a.ctx, time.Second)
	defer cancel()
	if reader == nil {
		var err error
		reader, err = a.unlockService.OpenStatusReader(ctx)
		if err != nil {
			return nil
		}
	}
	status, err := reader.Status(ctx)
	if err != nil {
		reader.Close()
		return nil
	}
	if status.State == protocol.ServiceStateLocked && status.UnlockNoticeUntil != nil {
		a.postUI(func() { a.showUnlockNotice(epoch, *status.UnlockNoticeUntil, time.Now()) })
	}
	return reader
}

func (a *app) showUnlockNotice(epoch uint64, until, now time.Time) {
	if a.online || a.create || a.epoch.Load() != epoch || a.presence.quitting || !a.startup.mayPopup() ||
		a.presence.effects == nil || !now.Before(until) || a.lastUnlockNotice.Equal(until) {
		return
	}
	a.lastUnlockNotice = until
	if a.presence.effects.mainNeedsRestore() || !a.isActive() {
		a.showFromPresence()
	}
}
