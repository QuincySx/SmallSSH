package client

import (
	"context"

	"sshclient/internal/connectcfg"
)

type routeContextKey struct{}
type routeContext struct {
	host  string
	route connectcfg.RouteSelection
}

// WithRoute pins an owner operation to its source session. An explicit zero
// selection restores the saved default even inside a temporary session context.
// Ordinary and delegated requests do not read this context value.
func WithRoute(ctx context.Context, host string, route connectcfg.RouteSelection) context.Context {
	return context.WithValue(ctx, routeContextKey{}, routeContext{host, route})
}

func RouteFromContext(ctx context.Context, host string) (connectcfg.RouteSelection, bool) {
	value, ok := ctx.Value(routeContextKey{}).(routeContext)
	return value.route, ok && value.host == host
}
