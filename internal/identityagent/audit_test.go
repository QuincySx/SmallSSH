package identityagent

import (
	"errors"
	"sync"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestAgentAuditFailureNeverReleasesSignature(t *testing.T) {
	for _, stage := range []string{"ssh_requested", "ssh_signed"} {
		t.Run(stage, func(t *testing.T) {
			b := newFakeBackend(t)
			var mu sync.Mutex
			var events []SignEvent
			s, path := startFake(t, b, func(e *SignEvent) error {
				mu.Lock()
				defer mu.Unlock()
				events = append(events, *e)
				if e.Event == stage {
					return errors.New("synthetic audit disk failure")
				}
				return nil
			})
			_ = s
			client, conn := agentClient(t, path)
			defer conn.Close()
			keys, err := client.List()
			if err != nil {
				t.Fatal(err)
			}
			key, err := ssh.ParsePublicKey(keys[0].Blob)
			if err != nil {
				t.Fatal(err)
			}
			if signature, err := client.Sign(key, []byte("synthetic secret payload excluded from audit")); err == nil || signature != nil {
				t.Fatal("audit failure released a valid signature")
			}
			mu.Lock()
			defer mu.Unlock()
			if len(events) == 0 || events[0].PeerPID == 0 || events[0].ProcessInstance == "" || events[0].DataHash == "" || events[0].Fingerprint == "" {
				t.Fatal("audit lost verified caller or payload commitment", events)
			}
		})
	}
}
