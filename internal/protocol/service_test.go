package protocol

import (
	"encoding/json"
	"testing"
	"time"
)

func TestServiceContractUsesLowerCamelCase(t *testing.T) {
	started := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	noticeUntil := started.Add(10 * time.Second)
	raw, err := json.Marshal(ServiceMessage{
		Action: "status", Major: 1, Minor: 3, Epoch: "synthetic-epoch",
		Status:    &ServiceStatus{State: "locked", StartedAt: started, ProtocolMajor: 1, ProtocolMinor: 3, UnlockNoticeUntil: &noticeUntil},
		ErrorCode: "synthetic_error", Error: "synthetic error",
	})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"action", "major", "minor", "epoch", "status", "errorCode", "error"} {
		if _, ok := fields[name]; !ok {
			t.Fatalf("missing lowerCamelCase field %q in %s", name, raw)
		}
	}
	var statusFields map[string]json.RawMessage
	if err := json.Unmarshal(fields["status"], &statusFields); err != nil {
		t.Fatal(err)
	}
	if _, ok := statusFields["unlockNoticeUntil"]; !ok {
		t.Fatalf("missing lowerCamelCase unlock notice in %s", raw)
	}
	var message ServiceMessage
	if err := json.Unmarshal(raw, &message); err != nil {
		t.Fatal(err)
	}
	if message.Status == nil || !message.Status.StartedAt.Equal(started) || message.Status.State != ServiceStateLocked ||
		message.Status.UnlockNoticeUntil == nil || !message.Status.UnlockNoticeUntil.Equal(noticeUntil) {
		t.Fatalf("service message round trip changed status: %#v", message)
	}
	if ServiceProtocolMajor != 1 || ServiceProtocolMinor != 3 {
		t.Fatalf("unexpected service protocol version %d.%d", ServiceProtocolMajor, ServiceProtocolMinor)
	}
	raw, err = json.Marshal(ServiceMessage{QuickUnlock: []byte("e"), QuickUnlockState: &QuickUnlockState{Available: true, Expires: started}})
	if err != nil {
		t.Fatal(err)
	}
	fields = nil
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"quickUnlock", "quickUnlockState"} {
		if _, ok := fields[name]; !ok {
			t.Fatalf("missing lowerCamelCase field %q in %s", name, raw)
		}
	}
}
