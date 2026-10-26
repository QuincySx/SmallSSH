//go:build !windows

package sshconn_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"net"
	"os"
	"path/filepath"
	"sshclient/internal/sshconn"
	"sshclient/internal/testserver"
	"testing"
	"time"
)

func agentSocket(t *testing.T, serve func(net.Conn)) string {
	t.Helper()
	dir, e := os.MkdirTemp("", "ssh-agent-test-")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	name := filepath.Join(dir, "agent.sock")
	l, e := net.Listen("unix", name)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, e := l.Accept()
			if e != nil {
				return
			}
			go func() { defer c.Close(); serve(c) }()
		}
	}()
	t.Setenv("SSH_AUTH_SOCK", name)
	return name
}
func TestAgentSelectsOnePinnedIdentity(t *testing.T) {
	fixture := testserver.New(t)
	raw, e := ssh.ParseRawPrivateKey([]byte(fixture.PrivateKey))
	if e != nil {
		t.Fatal(e)
	}
	ring := agent.NewKeyring()
	ring.Add(agent.AddedKey{PrivateKey: raw})
	_, other, _ := ed25519.GenerateKey(rand.Reader)
	ring.Add(agent.AddedKey{PrivateKey: other})
	agentSocket(t, func(c net.Conn) { agent.ServeAgent(ring, c) })
	identities, err := sshconn.AgentIdentities(context.Background())
	if err != nil || len(identities) != 2 {
		t.Fatal(identities, err)
	}
	p := fixture.Profile()
	p.Auth = "agent"
	p.Password = ""
	p.AgentFingerprint = ssh.FingerprintSHA256(fixture.Key.PublicKey())
	c, e := sshconn.Dial(context.Background(), p)
	if e != nil {
		t.Fatal(e)
	}
	c.Close()
	p.AgentFingerprint = "SHA256:not-present"
	if c, e = sshconn.Dial(context.Background(), p); e == nil {
		c.Close()
		t.Fatal("agent selected another identity")
	}
}
func TestUnresponsiveAgentHonorsCancellation(t *testing.T) {
	agentSocket(t, func(c net.Conn) {
		b := make([]byte, 1024)
		for {
			if _, e := c.Read(b); e != nil {
				return
			}
		}
	})
	fixture := testserver.New(t)
	p := fixture.Profile()
	p.Auth = "agent"
	p.AgentFingerprint = "SHA256:unused"
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	if c, e := sshconn.Dial(ctx, p); e == nil {
		c.Close()
		t.Fatal("connected through silent agent")
	}
	if time.Since(start) > time.Second {
		t.Fatal("agent ignored cancellation")
	}
	listCtx, stopList := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer stopList()
	start = time.Now()
	if _, e := sshconn.AgentIdentities(listCtx); e == nil || time.Since(start) > time.Second {
		t.Fatal("public identity read ignored cancellation", e)
	}
}
