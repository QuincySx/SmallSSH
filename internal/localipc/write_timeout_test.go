package localipc

import (
	"net"
	"testing"
	"time"
)

func TestFrameWriteTimesOutWhenThePeerStopsReading(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	wire := &Wire{Conn: a, WriteTimeout: 30 * time.Millisecond}
	done := make(chan error, 1)
	go func() { done <- wire.Send(struct{ Action string }{"response"}) }()
	select {
	case err := <-done:
		if timeout, ok := err.(net.Error); !ok || !timeout.Timeout() {
			t.Fatal("stalled frame did not report a write timeout", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stalled frame exceeded its write budget")
	}
}

func TestFrameWriteBudgetDoesNotExpireAnIdleConnection(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	wire := &Wire{Conn: a, WriteTimeout: 30 * time.Millisecond}
	time.Sleep(60 * time.Millisecond)
	done := make(chan error, 1)
	go func() { var message map[string]string; done <- (&Wire{Conn: b}).Receive(&message) }()
	if err := wire.Send(map[string]string{"action": "after idle"}); err != nil {
		t.Fatal("idle time consumed the next frame's budget", err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
