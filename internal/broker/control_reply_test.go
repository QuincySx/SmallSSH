package broker

import (
	"net"
	"sync"
	"testing"
	"time"

	"sshclient/internal/approval"
	"sshclient/internal/localipc"
	"sshclient/internal/vault"
)

type observedWriteConn struct {
	net.Conn
	started chan struct{}
	once    sync.Once
}

func (c *observedWriteConn) Write(p []byte) (int, error) {
	c.once.Do(func() { close(c.started) })
	return c.Conn.Write(p)
}

func TestStoppedReaderCannotHoldTheOwnerLock(t *testing.T) {
	owner, err := vault.New([]byte("synthetic blocked writer regression password"))
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	log, err := openAudit(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer log.close()
	server := &Server{Vault: owner, audit: log, channels: make(map[string]*callerChannel)}
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	connection := &observedWriteConn{Conn: a, started: make(chan struct{})}
	wire := &localipc.Wire{Conn: connection, WriteTimeout: time.Second}
	done := make(chan error, 1)
	go func() {
		done <- server.handleMessage(wire, Message{Action: "delegationChallenge", ID: "not-approved", Operation: approval.Operation{Kind: "exec", Client: "stopped reader"}}, &callerChannel{})
	}()
	select {
	case <-connection.started:
	case <-time.After(2 * time.Second):
		t.Fatal("response did not reach the stopped reader")
	}
	// The pipe has no reader, so Send is blocked at this exact point.
	if !server.ownerMu.TryLock() {
		t.Fatal("a stopped reader retained the global owner lock")
	}
	server.ownerMu.Unlock()
	a.Close()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("failed response write was ignored")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("response did not stop after disconnect")
	}
}
