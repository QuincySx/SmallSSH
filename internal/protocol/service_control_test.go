package protocol

import (
	"bytes"
	"testing"
)

func syntheticOwnerControl() ServiceOwnerControl {
	return ServiceOwnerControl{
		Action:     "lock",
		Epoch:      "synthetic-epoch",
		Generation: 7,
		Binding:    "synthetic-binding",
		OwnerID:    "synthetic-owner",
	}
}

func TestServiceOwnerControlPayloadGolden(t *testing.T) {
	payload, err := ServiceOwnerControlPayload(syntheticOwnerControl())
	if err != nil {
		t.Fatal(err)
	}
	want := "smallssh/service-owner/control/v1\x00" +
		`{"action":"lock","epoch":"synthetic-epoch","generation":7,"binding":"synthetic-binding","ownerID":"synthetic-owner"}`
	if string(payload) != want {
		t.Fatal("control payload does not match the fixed golden bytes")
	}
}

func TestServiceOwnerControlPayloadBindsEveryField(t *testing.T) {
	baseline, err := ServiceOwnerControlPayload(syntheticOwnerControl())
	if err != nil {
		t.Fatal(err)
	}
	changes := []struct {
		name   string
		change func(*ServiceOwnerControl)
	}{
		{"action", func(p *ServiceOwnerControl) { p.Action = "stop" }},
		{"epoch", func(p *ServiceOwnerControl) { p.Epoch = "synthetic-epoch-other" }},
		{"generation", func(p *ServiceOwnerControl) { p.Generation++ }},
		{"binding", func(p *ServiceOwnerControl) { p.Binding = "synthetic-binding-other" }},
		{"owner ID", func(p *ServiceOwnerControl) { p.OwnerID = "synthetic-owner-other" }},
	}
	for _, test := range changes {
		t.Run(test.name, func(t *testing.T) {
			control := syntheticOwnerControl()
			test.change(&control)
			changed, err := ServiceOwnerControlPayload(control)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Equal(changed, baseline) {
				t.Fatalf("changing %s did not change payload", test.name)
			}
		})
	}
}

func TestServiceOwnerControlPayloadDomainSeparatedFromPrepare(t *testing.T) {
	controlPayload, err := ServiceOwnerControlPayload(syntheticOwnerControl())
	if err != nil {
		t.Fatal(err)
	}
	preparePayload, err := ServiceOwnerPreparePayload(syntheticOwnerPrepare())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(controlPayload, []byte(serviceOwnerControlDomain)) {
		t.Fatal("control payload does not use the control domain")
	}
	if !bytes.HasPrefix(preparePayload, []byte(serviceOwnerPrepareDomain)) {
		t.Fatal("prepare payload no longer uses the prepare domain")
	}
	if serviceOwnerControlDomain == serviceOwnerPrepareDomain {
		t.Fatal("control and prepare domains are not separated")
	}
	if bytes.HasPrefix(controlPayload, []byte(serviceOwnerPrepareDomain)) || bytes.HasPrefix(preparePayload, []byte(serviceOwnerControlDomain)) {
		t.Fatal("control and prepare payloads share a domain")
	}
}
