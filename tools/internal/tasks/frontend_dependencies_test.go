package tasks

import "testing"

func TestFrontendExcludesConnectionRuntimesWhileAllowingWebSocketCodec(t *testing.T) {
	if !forbiddenFrontendDependency("sshclient/internal/identityagent") {
		t.Fatal("native signing runtime may be linked into a frontend")
	}
	for _, path := range []string{"sshclient/internal/broker", "sshclient/internal/service", "sshclient/internal/sshconn", "sshclient/internal/netroute", "github.com/sagernet/sing", "github.com/sagernet/sing-box/outbound", "github.com/anytls/sing-anytls", "github.com/sagernet/ws/unknown"} {
		if !forbiddenFrontendDependency(path) {
			t.Errorf("frontend may link backend runtime %s", path)
		}
	}
	for _, path := range []string{"github.com/sagernet/ws", "github.com/sagernet/ws/wsutil", "sshclient/internal/vncview"} {
		if forbiddenFrontendDependency(path) {
			t.Errorf("standalone viewer dependency rejected: %s", path)
		}
	}
}
