package desktop

import (
	"context"
	"errors"

	"sshclient/internal/connectcfg"
	"sshclient/internal/desktop/components"
	"sshclient/internal/protocol"
	"sshclient/internal/sshdata"
	"sshclient/internal/vault"
)

// Trust is completed before creating the terminal's approved SSH request.
func (a *app) readFirstHostIdentity(ctx context.Context, target vault.Summary, pending *session, opened func(*session), gen int) {
	cleanup := func() {
		delete(a.connecting, target.ID)
		if cancel := pending.connectionCancel; cancel != nil {
			pending.connectionCancel = nil
			cancel()
		}
	}
	a.ensureConnectionTrusted(ctx, target.ID, pending.target.Route, func() {
		cleanup()
		if gen != a.generation || !a.online || !a.hasSession(pending) {
			return
		}
		a.openShellInGroup(target.ID, true, pending.group, opened, pending)
	}, func(err error) {
		cleanup()
		if gen != a.generation || !a.online || !a.hasSession(pending) {
			return
		}
		pending.closed = true
		pending.connectionState = "error"
		pending.connectionError = connectionError(err)
		if errors.Is(err, context.Canceled) {
			pending.connectionError = tr(nil, "连接已取消")
		}
		a.failPendingTerminals(target.ID, pending.connectionError)
		a.status = pending.connectionError
	})
}

func (a *app) showHostIdentityPrompt(target vault.Summary, identity sshdata.HostIdentity, accept func(), canceled ...func()) {
	a.ask(tr(nil, "首次连接"), target.User+"@"+targetID(target.Host, target.Port), tr(nil, "服务器指纹")+"\n"+identity.Fingerprint, "", "", tr(nil, "信任指纹并连接"), func(string) { accept() })
	a.prompt.size = components.DialogSmall
	if len(canceled) > 0 {
		a.prompt.cancel = canceled[0]
	}
}

func hostIdentityQuery(target vault.Summary, route connectcfg.RouteSelection) protocol.Message {
	query := protocol.Message{Action: "probeHost", ID: target.ID, Target: target.Host, Port: target.Port}
	if route.Mode == "session" || route.Mode == "tunnel" {
		query.ConnectionRoute = &route
	} else if route.Temporary() {
		network := route.Apply(target.Network)
		query.Network = &network
	}
	return query
}
