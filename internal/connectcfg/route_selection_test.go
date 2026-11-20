package connectcfg

import "testing"

func TestRouteSelectionAppliesWithoutChangingSavedNetwork(t *testing.T) {
	saved := Network{RouteID: "default", KeepaliveSeconds: 30, KeepaliveFailures: 3}
	for _, selection := range []RouteSelection{{}, {Mode: "direct"}, {Mode: "saved", ID: "one-off"}} {
		if err := selection.Validate(); err != nil {
			t.Fatal(err)
		}
		actual := selection.Apply(saved)
		if actual.KeepaliveSeconds != 30 || actual.KeepaliveFailures != 3 || saved.RouteID != "default" {
			t.Fatal("lost connection settings or mutated saved network", actual, saved)
		}
		want := "default"
		if selection.Mode == "direct" {
			want = ""
		}
		if selection.Mode == "saved" {
			want = selection.ID
		}
		if actual.RouteID != want {
			t.Fatal(actual, want)
		}
	}
	inline := Network{Relay: "jump", Proxy: Proxy{Mode: "http", Address: "127.0.0.1:1", Password: "private"}}
	if result := (RouteSelection{Mode: "direct"}).Apply(inline); result.Relay != "" || result.Proxy != (Proxy{}) {
		t.Fatal("direct retained a legacy route", result.Public())
	}
	relay := RouteSelection{Mode: "relay", ID: "jump"}
	if err := relay.Validate(); err != nil {
		t.Fatal(err)
	}
	if result := relay.Apply(saved); result.RouteID != "" || result.Relay != "jump" || result.KeepaliveSeconds != 30 || saved.RouteID != "default" {
		t.Fatal("one-time relay did not replace the route while keeping connection options", result)
	}
	for _, selection := range []RouteSelection{{ID: "hidden"}, {Mode: "direct", ID: "hidden"}, {Mode: "saved"}, {Mode: "relay"}, {Mode: "saved", ID: "bad\n"}, {Mode: "other"}} {
		if selection.Validate() == nil {
			t.Fatal("accepted invalid selection", selection)
		}
	}
}
