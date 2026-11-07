package localipc

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func TestPartialFrameWriteClosesTransport(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	wire := &Wire{Conn: a, WriteTimeout: 50 * time.Millisecond}
	done := make(chan error, 1)
	go func() { done <- wire.Send(map[string]string{"action": "partial"}) }()
	var header [4]byte
	if _, err := io.ReadFull(b, header[:]); err != nil {
		t.Fatal(err)
	}
	// The peer has consumed the header but refuses the body. A retry cannot
	// repair the stream: its new header would be read as the abandoned body.
	if err := <-done; err == nil {
		t.Fatal("partial frame did not fail")
	}
	assertPeerClosed(t, b)
}

func TestInvalidFrameClosesTransport(t *testing.T) {
	for _, payload := range []string{"", "{"} {
		t.Run(payload, func(t *testing.T) {
			a, b := net.Pipe()
			defer a.Close()
			defer b.Close()
			done := make(chan error, 1)
			go func() {
				var message map[string]string
				done <- (&Wire{Conn: a}).Receive(&message)
			}()
			frame := make([]byte, 4+len(payload))
			binary.BigEndian.PutUint32(frame, uint32(len(payload)))
			copy(frame[4:], payload)
			if _, err := b.Write(frame); err != nil {
				t.Fatal(err)
			}
			if err := <-done; err == nil {
				t.Fatal("invalid frame was accepted")
			}
			assertPeerClosed(t, b)
		})
	}
}

func TestRejectedEncodingLeavesTransportUsable(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	wire := &Wire{Conn: a, WriteTimeout: time.Second}
	for _, invalid := range []any{make(chan int), strings.Repeat("x", MaxFrame)} {
		if err := wire.Send(invalid); err == nil {
			t.Fatal("invalid local encoding was accepted")
		}
	}
	done := make(chan error, 1)
	go func() {
		var message map[string]string
		err := (&Wire{Conn: b}).Receive(&message)
		if err == nil && message["action"] != "still valid" {
			err = errors.New("rejected encoding wrote a partial frame")
		}
		done <- err
	}()
	if err := wire.Send(map[string]string{"action": "still valid"}); err != nil {
		t.Fatal("pre-write encoding failure closed the connection", err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestFrameIdentityFailureClosesTransport(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	denied := errors.New("synthetic identity change")
	done := make(chan error, 1)
	go func() {
		wire := &Wire{Conn: a, Verify: func() error { return denied }}
		var message map[string]string
		done <- wire.Receive(&message)
	}()
	if err := (&Wire{Conn: b}).Send(map[string]string{"action": "changed identity"}); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, denied) {
		t.Fatal("identity failure was lost", err)
	}
	assertPeerClosed(t, b)
}

func assertPeerClosed(t *testing.T, peer net.Conn) {
	t.Helper()
	peer.SetReadDeadline(time.Now().Add(time.Second))
	var next [1]byte
	if _, err := peer.Read(next[:]); !errors.Is(err, io.EOF) {
		t.Fatal("failed frame left the transport open", err)
	}
}
