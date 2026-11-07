package main

import (
	"testing"

	"sshclient/internal/connectcfg"
	"sshclient/internal/vault"
)

func TestHostOptionsProxyCredentialRetentionAndExplicitClear(t *testing.T) {
	saved := vault.Profile{Auth: "password", Password: "saved-login", Network: connectcfg.Network{Proxy: connectcfg.Proxy{Mode: "socks5", Address: "127.0.0.1:1080", User: "example", Password: "saved-proxy"}}}
	same := saved.Network.Public()
	changed := same
	changed.Proxy.Address = "127.0.0.1:1081"
	replacement := same
	replacement.Proxy.Password = "new-proxy"
	for _, tt := range []struct {
		name     string
		options  hostOptions
		keep     bool
		password string
	}{
		{"omitted group preserves", hostOptions{}, true, ""},
		{"clear without group", hostOptions{ClearProxyPassword: true}, false, ""},
		{"same proxy preserves", hostOptions{Network: &same}, true, ""},
		{"new address never inherits", hostOptions{Network: &changed}, false, ""},
		{"explicit replacement", hostOptions{Network: &replacement}, false, "new-proxy"},
		{"clear wins over supplied password", hostOptions{Network: &replacement, ClearProxyPassword: true}, false, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			change := tt.options.apply(saved)
			if change.KeepProxyCredential != tt.keep || change.Profile.Network.Proxy.Password != tt.password {
				t.Fatal("incorrect proxy credential retention / clearing")
			}
			if !change.KeepCredential || change.Profile.Password != "" {
				t.Fatal("options edit retransmitted or replaced the saved login secret")
			}
		})
	}
}
