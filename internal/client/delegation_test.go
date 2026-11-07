package client

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"sshclient/internal/approval"
	"sshclient/internal/localipc"
)

func TestSessionCannotBeSavedSerializedOrRestored(t *testing.T) {
	dir := t.TempDir()
	d := &Delegation{ParentID: "synthetic-session", Directory: dir, Ticket: "copied-ticket", PublicKey: "public-key"}
	path := filepath.Join(dir, "session.json")
	if err := d.Save(path); !errors.Is(err, ErrSessionNotExportable) {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("created a reusable session file", err)
	}
	if _, err := json.Marshal(d); !errors.Is(err, ErrSessionNotExportable) {
		t.Fatal("serialized session authority", err)
	}
	// JSON receives both addressable pointers and unaddressable values in callers.
	if _, err := json.Marshal(*d); !errors.Is(err, ErrSessionNotExportable) {
		t.Fatal("session value bypassed the non-exportable contract", err)
	}
	if err := json.Unmarshal([]byte(`{"key":"copied key","ticket":"copied ticket"}`), d); !errors.Is(err, ErrSessionNotExportable) {
		t.Fatal("decoded old session credentials", err)
	}
	if err := os.WriteFile(path, []byte(`{"key":"copied key"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadDelegation(path, dir); !errors.Is(err, ErrSessionNotExportable) {
		t.Fatal("restored persisted authority", err)
	}
	if err := d.Validate(dir); err == nil {
		t.Fatal("metadata became authority without a live channel")
	}
	d.Close()
}

func TestSessionCopiesShareCloseAndSigningState(t *testing.T) {
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	a, b := net.Pipe()
	defer b.Close()
	ctx, cancel := context.WithCancel(context.Background())
	h := &requestChannel{wire: &localipc.Wire{Conn: a}, ctx: ctx, cancel: cancel, id: "copy-session", ticket: "synthetic"}
	d := &Delegation{Directory: t.TempDir(), ParentID: h.id, Ticket: h.ticket, state: &delegationState{key: key, control: h, pid: os.Getpid()}}
	defer d.Close()
	copy := *d
	message := []byte("synthetic signing/close interleaving")
	for _, session := range []*Delegation{d, &copy} {
		if err := session.Validate(d.Directory); err != nil {
			t.Fatal("value copy lost its live authority", err)
		}
		if sig, err := session.sign(message); err != nil || !ed25519.Verify(pub, message, sig) {
			t.Fatal("value copy could not sign before close", err)
		}
	}
	var wg sync.WaitGroup
	start := make(chan struct{})
	for _, session := range []*Delegation{d, &copy} {
		wg.Go(func() {
			<-start
			for range 100 {
				sig, err := session.sign(message)
				if err == nil && !ed25519.Verify(pub, message, sig) {
					t.Error("close raced with signing and produced a corrupt signature")
				}
			}
		})
	}
	close(start)
	copy.Close()
	wg.Wait()
	for _, session := range []*Delegation{d, &copy} {
		if !errors.Is(session.Validate(d.Directory), approval.ErrDenied) {
			t.Fatal("closing a copy left another copy valid")
		}
		if _, err := session.sign(message); !errors.Is(err, approval.ErrDenied) {
			t.Fatal("closing a copy left another copy able to sign", err)
		}
	}
	for _, value := range key {
		if value != 0 {
			t.Fatal("shared key bytes survived close")
		}
	}
}
