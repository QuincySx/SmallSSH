package sshconn

import (
	"context"
	"errors"
	"sshclient/internal/sshdata"
	"sshclient/internal/vault"
	"sync"
	"time"
)

type RouteMeasurement = sshdata.RouteMeasurement

// MeasureRoutes stops before destination authentication and checks the saved
// fingerprint. This measures connection setup, not sustained transfer speed.
func MeasureRoutes(parent context.Context, route []vault.Profile) ([]RouteMeasurement, error) {
	if len(route) == 0 || route[0].Fingerprint == "" {
		return nil, errors.New("请先保存并核对目标主机指纹")
	}
	target := route[0]
	candidates := []bool{false}
	if len(route) > 1 {
		candidates = append(candidates, true)
	}
	ctx, cancel := context.WithTimeout(parent, 6*time.Second)
	defer cancel()
	measured := make([]RouteMeasurement, len(candidates))
	var tasks sync.WaitGroup
	for i, relay := range candidates {
		tasks.Go(func() {
			started := time.Now()
			network := target.Network
			var hops []vault.Profile
			if relay {
				hops = route[1:]
			} else {
				network.Relay = ""
			}
			identity, e := ProbeRoute(ctx, target.Host, target.Port, network, hops)
			if e == nil && identity.Fingerprint != target.Fingerprint {
				e = errors.New("候选路径的目标主机指纹不匹配，未认证也未推荐该路径")
			}
			result := RouteMeasurement{Relay: relay, Available: e == nil, HandshakeMillis: max(1, time.Since(started).Milliseconds())}
			if e != nil {
				result.Error = e.Error()
				if len(result.Error) > 1024 {
					result.Error = result.Error[:1024]
				}
			}
			measured[i] = result
		})
	}
	tasks.Wait()
	return measured, nil
}
