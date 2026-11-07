package protocol

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

const syntheticKeyHash = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func syntheticOwnerPrepare() OwnerPrepare {
	return OwnerPrepare{
		Epoch:           "synthetic-epoch",
		Challenge:       "synthetic-challenge",
		Binding:         "synthetic-binding",
		Generation:      7,
		PublicKey:       []byte("synthetic-public-key"),
		LifetimeSeconds: 300,
		KeyHash:         syntheticKeyHash,
	}
}

func TestServiceOwnerPreparePayloadGolden(t *testing.T) {
	payload, err := ServiceOwnerPreparePayload(syntheticOwnerPrepare())
	if err != nil {
		t.Fatal(err)
	}
	want := "smallssh/service-owner/prepare/v1\x00" +
		`{"epoch":"synthetic-epoch","challenge":"synthetic-challenge","binding":"synthetic-binding","generation":7,"publicKey":"c3ludGhldGljLXB1YmxpYy1rZXk=","lifetimeSeconds":300,"keyHash":"` + syntheticKeyHash + `"}`
	if string(payload) != want {
		t.Fatalf("payload = %q, want %q", payload, want)
	}
}

func TestServiceOwnerPreparePayloadBindsEveryField(t *testing.T) {
	baseline, err := ServiceOwnerPreparePayload(syntheticOwnerPrepare())
	if err != nil {
		t.Fatal(err)
	}
	changes := []struct {
		name   string
		change func(*OwnerPrepare)
	}{
		{"epoch", func(p *OwnerPrepare) { p.Epoch = "synthetic-epoch-other" }},
		{"challenge", func(p *OwnerPrepare) { p.Challenge = "synthetic-challenge-other" }},
		{"binding", func(p *OwnerPrepare) { p.Binding = "synthetic-binding-other" }},
		{"generation", func(p *OwnerPrepare) { p.Generation++ }},
		{"public key", func(p *OwnerPrepare) { p.PublicKey = []byte("synthetic-public-key-other") }},
		{"lifetime", func(p *OwnerPrepare) { p.LifetimeSeconds++ }},
		{"key hash", func(p *OwnerPrepare) { p.KeyHash = "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210" }},
	}
	for _, test := range changes {
		t.Run(test.name, func(t *testing.T) {
			prepare := syntheticOwnerPrepare()
			test.change(&prepare)
			changed, err := ServiceOwnerPreparePayload(prepare)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Equal(changed, baseline) {
				t.Fatalf("changing %s did not change payload", test.name)
			}
		})
	}
}

func TestServiceProtocolOwnerFieldsRoundTrip(t *testing.T) {
	if ServiceProtocolMajor != 1 || ServiceProtocolMinor != 1 {
		t.Fatalf("service protocol = %d.%d, want 1.1", ServiceProtocolMajor, ServiceProtocolMinor)
	}
	if ServiceStateUnlocked != "unlocked" || ServiceStateStopping != "stopping" {
		t.Fatalf("service states = %q, %q", ServiceStateUnlocked, ServiceStateStopping)
	}

	started := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	expires := started.Add(10 * time.Minute)
	prepare := syntheticOwnerPrepare()
	want := ServiceMessage{
		Action:       "ownerPrepare",
		Major:        1,
		Minor:        1,
		Epoch:        "synthetic-epoch",
		Status:       &ServiceStatus{State: ServiceStateUnlocked, StartedAt: started, ProtocolMajor: 1, ProtocolMinor: 1, Generation: 7},
		ErrorCode:    "synthetic_error",
		Error:        "synthetic error",
		Challenge:    "synthetic-challenge",
		Generation:   7,
		ID:           "synthetic-id",
		OwnerPrepare: &prepare,
		UnlockKey:    []byte("synthetic-unlock-key"),
		Proof:        []byte("synthetic-proof"),
		OwnerInfo:    &ServiceOwnerInfo{ID: "synthetic-owner", Epoch: "synthetic-epoch", Expires: expires, BrokerKey: "synthetic-broker-key"},
	}
	raw, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got ServiceMessage
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Action != want.Action || got.Major != want.Major || got.Minor != want.Minor || got.Epoch != want.Epoch ||
		got.ErrorCode != want.ErrorCode || got.Error != want.Error || got.Challenge != want.Challenge ||
		got.Generation != want.Generation || got.ID != want.ID || !bytes.Equal(got.UnlockKey, want.UnlockKey) ||
		!bytes.Equal(got.Proof, want.Proof) {
		t.Fatalf("service message round trip changed fields: %#v", got)
	}
	if got.Status == nil || got.Status.State != want.Status.State || !got.Status.StartedAt.Equal(started) ||
		got.Status.ProtocolMajor != want.Status.ProtocolMajor || got.Status.ProtocolMinor != want.Status.ProtocolMinor ||
		got.Status.Generation != want.Status.Generation {
		t.Fatalf("status round trip changed fields: %#v", got.Status)
	}
	if got.OwnerPrepare == nil || !reflect.DeepEqual(*got.OwnerPrepare, prepare) {
		t.Fatalf("owner prepare round trip = %#v, want %#v", got.OwnerPrepare, prepare)
	}
	if got.OwnerInfo == nil || got.OwnerInfo.ID != want.OwnerInfo.ID || got.OwnerInfo.Epoch != want.OwnerInfo.Epoch ||
		!got.OwnerInfo.Expires.Equal(want.OwnerInfo.Expires) || got.OwnerInfo.BrokerKey != want.OwnerInfo.BrokerKey {
		t.Fatalf("owner info round trip = %#v, want %#v", got.OwnerInfo, want.OwnerInfo)
	}
}

func TestServiceOwnerInfoFieldContract(t *testing.T) {
	typ := reflect.TypeOf(ServiceOwnerInfo{})
	want := []struct {
		name string
		kind reflect.Kind
		tag  string
	}{
		{"ID", reflect.String, `json:"id"`},
		{"Epoch", reflect.String, `json:"epoch"`},
		{"Expires", reflect.Struct, `json:"expires"`},
		{"BrokerKey", reflect.String, `json:"brokerKey"`},
	}
	if typ.NumField() != len(want) {
		t.Fatalf("ServiceOwnerInfo has %d fields, want %d", typ.NumField(), len(want))
	}
	for index, expected := range want {
		field := typ.Field(index)
		if field.Name != expected.name || field.Type.Kind() != expected.kind || string(field.Tag) != expected.tag {
			t.Errorf("field %d = %s %s %s, want %s %s %s", index, field.Name, field.Type, field.Tag, expected.name, expected.kind, expected.tag)
		}
	}

	raw, err := json.Marshal(ServiceOwnerInfo{ID: "synthetic-owner", Epoch: "synthetic-epoch", BrokerKey: "synthetic-broker-key"})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"id", "epoch", "expires", "brokerKey"} {
		if _, ok := fields[name]; !ok {
			t.Fatalf("missing ServiceOwnerInfo field %q in %s", name, raw)
		}
	}
	if len(fields) != len(want) {
		t.Fatalf("ServiceOwnerInfo JSON has fields %v, want only %d", fields, len(want))
	}
}
