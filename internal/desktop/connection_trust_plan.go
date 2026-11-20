package desktop

import (
	"context"
	"errors"
	"fmt"
	"time"

	"sshclient/internal/connectcfg"
	"sshclient/internal/vault"
)

type connectionTrustStep struct {
	target vault.Summary
	route  connectcfg.RouteSelection
}

// Plan from the outermost SSH hop to the destination. Only public metadata is
// needed; credentials and actual route resolution remain in the broker.
func (a *app) connectionTrustPlan(host string, route connectcfg.RouteSelection) ([]connectionTrustStep, error) {
	if err := route.Validate(); err != nil {
		return nil, err
	}
	var steps []connectionTrustStep
	seen := map[string]bool{}
	var visit func(string, connectcfg.RouteSelection, int) error
	visit = func(id string, selection connectcfg.RouteSelection, depth int) error {
		if depth >= 4 || seen[id] {
			return errors.New(tr(nil, "SSH 跳板线路循环或超过 4 跳"))
		}
		seen[id] = true
		var target vault.Summary
		for _, p := range a.connections {
			if p.ID == id {
				target = p
				break
			}
		}
		if target.ID == "" {
			return errors.New(tr(nil, "连接不存在，请重新选择服务器"))
		}
		if selection.Mode == "session" {
			source := a.liveRelay(selection.ID)
			if source == nil {
				return errors.New(tr(nil, "中继会话已结束，请重新选择"))
			}
			if err := visit(source.host, source.target.Route, depth+1); err != nil {
				return err
			}
		} else if selection.Mode == "tunnel" {
			if a.liveSOCKS(selection.ID) == nil {
				return errors.New(tr(nil, "SOCKS5 入口已停止"))
			}
		} else if err := visitSavedRelay(target, selection, depth, visit, a.routes); err != nil {
			return err
		}
		if target.Fingerprint == "" {
			steps = append(steps, connectionTrustStep{target, selection})
		}
		return nil
	}
	err := visit(host, route, 0)
	return steps, err
}

func visitSavedRelay(target vault.Summary, selection connectcfg.RouteSelection, depth int, visit func(string, connectcfg.RouteSelection, int) error, routes []vault.RouteSummary) error {
	network := selection.Apply(target.Network)
	if err := network.Validate(); err != nil {
		return err
	}
	relay := network.Relay
	if network.RouteID != "" {
		found := false
		for _, saved := range routes {
			if saved.ID == network.RouteID {
				relay = saved.Relay
				found = true
				break
			}
		}
		if !found {
			return errors.New(tr(nil, "线路不可用，请重新选择"))
		}
	}
	if relay != "" {
		if err := visit(relay, connectcfg.RouteSelection{}, depth+1); err != nil {
			return err
		}
	}
	return nil
}

// Explicit connection actions use this gate before creating an approval or
// operation stream. Accepting a fingerprint must not revoke an already-started
// request, nor require opening a disposable terminal to initialize trust.
func (a *app) ensureConnectionTrusted(ctx context.Context, host string, route connectcfg.RouteSelection, ready func(), failed func(error)) {
	owner, err := a.unlockedOwner()
	if err != nil {
		failed(err)
		return
	}
	steps, err := a.connectionTrustPlan(host, route)
	if err != nil {
		failed(err)
		return
	}
	if len(steps) == 0 {
		ready()
		return
	}
	gen, cli := a.generation, a.client
	finished := false
	var stop func() bool
	finish := func(err error) {
		if finished {
			return
		}
		finished = true
		if stop != nil {
			stop()
		}
		if gen != a.generation || !a.online {
			return
		}
		if err != nil {
			failed(err)
		} else {
			ready()
		}
	}
	stop = context.AfterFunc(ctx, func() { a.postUI(func() { finish(ctx.Err()) }) })
	var next func(int)
	next = func(index int) {
		if finished {
			return
		}
		if ctx.Err() != nil {
			finish(ctx.Err())
			return
		}
		if index == len(steps) {
			finish(nil)
			return
		}
		step := steps[index]
		go func() {
			result, err := cli.CallWithOwner(ctx, hostIdentityQuery(step.target, step.route), owner)
			a.postUI(func() {
				if finished || gen != a.generation || !a.online {
					return
				}
				if ctx.Err() != nil {
					finish(ctx.Err())
					return
				}
				if err != nil {
					finish(fmt.Errorf("%s: %w", step.target.Name, err))
					return
				}
				if result.HostIdentity == nil {
					finish(errors.New(tr(nil, "未能读取服务器指纹")))
					return
				}
				identity := *result.HostIdentity
				a.showHostIdentityPrompt(step.target, identity, func() {
					if finished || gen != a.generation || !a.online {
						return
					}
					if ctx.Err() != nil {
						finish(ctx.Err())
						return
					}
					go func() {
						saveCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
						defer cancel()
						err := cli.SaveHostTrust(saveCtx, vault.HostTrustChange{ID: step.target.ID, Host: step.target.Host, Port: step.target.Port, Previous: step.target.Fingerprint, Fingerprint: identity.Fingerprint}, owner)
						a.postUI(func() {
							if finished || gen != a.generation || !a.online {
								return
							}
							if err != nil {
								finish(err)
								return
							}
							for i, p := range a.connections {
								if p.ID == step.target.ID && p.Host == step.target.Host && p.Port == step.target.Port {
									a.connections[i].Fingerprint = identity.Fingerprint
								}
							}
							next(index + 1)
						})
					}()
				}, func() { finish(context.Canceled) })
			})
		}()
	}
	next(0)
}
