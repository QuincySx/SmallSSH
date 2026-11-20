package client

import (
	"context"
	"sshclient/internal/connectcfg"
	"testing"
)

func TestRouteContextIsHostScopedAndExplicitDefaultOverrides(t *testing.T) {
	route := connectcfg.RouteSelection{Mode: "saved", ID: "route-a"}
	ctx := WithRoute(context.Background(), "a", route)
	route.ID = "mutated"
	if got, ok := RouteFromContext(ctx, "a"); !ok || got.ID != "route-a" {
		t.Fatal(got, ok)
	}
	if _, ok := RouteFromContext(ctx, "b"); ok {
		t.Fatal("route leaked to another host")
	}
	ctx = WithRoute(ctx, "a", connectcfg.RouteSelection{})
	if got, ok := RouteFromContext(ctx, "a"); !ok || got.Temporary() {
		t.Fatal("default inherited temporary route", got, ok)
	}
}
