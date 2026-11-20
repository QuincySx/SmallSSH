//go:build probe_baseline

package main

import (
	"context"
	"encoding/json"
	"net"
)

func dial(ctx context.Context, raw []byte, target string) (net.Conn, func(), error) {
	var config struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		return nil, nil, err
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", target)
	return conn, func() {}, err
}
