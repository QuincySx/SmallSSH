package broker

import (
	"testing"

	"sshclient/internal/accesspolicy"
	"sshclient/internal/vault"
)

// A host list served without owner proof must not leak how to reach a machine or
// how it authenticates, only what a caller needs to ask for access to it.
func TestRedactedHostListKeepsOnlyIdentityForDiscovery(t *testing.T) {
	full := []vault.Summary{{
		ID: "abc123", Name: "生产数据库", Group: "生产",
		Host: "10.0.0.9", Port: 2222, User: "deploy", Auth: "key",
		Fingerprint: "SHA256:secret", AgentFingerprint: "SHA256:agent",
		Policy: accesspolicy.Policy{Disabled: true, ProtectedPaths: []string{"/etc"}},
	}}
	out := redactedSummaries(full)
	if len(out) != 1 {
		t.Fatalf("dropped a connection: %v", out)
	}
	got := out[0]
	if got.ID != "abc123" || got.Name != "生产数据库" || got.Group != "生产" {
		t.Fatalf("lost the discovery fields: %+v", got)
	}
	if got.Host != "" || got.Port != 0 || got.User != "" || got.Auth != "" || got.Fingerprint != "" || got.AgentFingerprint != "" || got.Policy.Disabled || got.Policy.Unrestricted || len(got.Policy.ProtectedPaths) != 0 {
		t.Fatalf("leaked reachability or policy: %+v", got)
	}
	// The caller's slice is not aliased into the response.
	if full[0].Host != "10.0.0.9" || len(full[0].Policy.ProtectedPaths) != 1 {
		t.Fatal("redaction modified the stored profile")
	}
}
