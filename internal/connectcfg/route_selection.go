package connectcfg

import (
	"errors"
	"strings"
)

// RouteSelection is an owner-selected route for one session. The zero value
// uses the connection's saved configuration. It contains references, never
// proxy credentials, and is part of the immutable signed operation.
type RouteSelection struct {
	Mode string `json:"mode,omitempty"`
	ID   string `json:"id,omitempty"`
}

func (r RouteSelection) Validate() error {
	switch r.Mode {
	case "", "direct":
		if r.ID == "" {
			return nil
		}
	case "saved", "relay", "session", "tunnel":
		if r.ID != "" && len(r.ID) <= 128 && !strings.ContainsAny(r.ID, "\x00\r\n") {
			return nil
		}
	}
	return errors.New("本次连接线路无效")
}

func (r RouteSelection) Temporary() bool { return r != (RouteSelection{}) }

// Apply only changes routing. Keepalive and other connection settings remain
// those of the target profile. Call Validate before applying untrusted input.
func (r RouteSelection) Apply(n Network) Network {
	if !r.Temporary() {
		return n
	}
	n.RouteID, n.Proxy, n.Relay = "", Proxy{}, ""
	if r.Mode == "saved" {
		n.RouteID = r.ID
	}
	if r.Mode == "relay" {
		n.Relay = r.ID
	}
	if r.Mode == "session" || r.Mode == "tunnel" {
		// A missed server-side runtime resolution must fail closed, never dial direct.
		n.RouteID = "runtime:" + r.Mode + ":" + r.ID
	}
	return n
}

func (r RouteSelection) Key() string { return r.Mode + ":" + r.ID }
