package broker

import (
	"sync"
	"time"

	"sshclient/internal/sshconn"
)

// A verified SSH connection is worth keeping for a moment. Browsing a directory
// lists it, opens files for details and downloads them in quick succession, and a
// fresh handshake costs 100-300 ms on a real link. Only short operations reuse a
// connection: a terminal keeps its own, and a failed operation closes its own so a
// broken connection is never handed on. Reuse changes nothing about authority —
// every operation still carries its own approved request — and a profile change,
// a policy change or a lock drops the pool.
const (
	pooledIdleTTL = 15 * time.Second
	pooledPerKey  = 2
)

// pooledKinds are the short operations that end cleanly enough to hand their
// connection on: one request and one response, no stream left half-read.
var pooledKinds = map[string]bool{
	"list": true, "file-info": true, "exec": true, "mkdir": true, "remove": true,
	"rename": true, "chmod": true, "file-attributes": true, "compose-plan": true,
	"grant": true, "monitor": true,
}

type pooledConn struct {
	client *sshconn.Client
	idleAt time.Time
}

type connPool struct {
	mu      sync.Mutex
	entries map[string][]pooledConn
	now     func() time.Time
}

func (p *connPool) clock() time.Time {
	if p.now != nil {
		return p.now()
	}
	return time.Now()
}

// take returns an idle connection for key, dropping the ones that idled too long.
func (p *connPool) take(key string) *sshconn.Client {
	p.mu.Lock()
	defer p.mu.Unlock()
	list := p.entries[key]
	for len(list) > 0 {
		entry := list[len(list)-1]
		list = list[:len(list)-1]
		if p.clock().Sub(entry.idleAt) <= pooledIdleTTL {
			p.entries[key] = list
			return entry.client
		}
		entry.client.Close()
	}
	delete(p.entries, key)
	return nil
}

// put keeps a connection for the next operation, or closes it when its share is
// already full.
func (p *connPool) put(key string, c *sshconn.Client) {
	if c == nil {
		return
	}
	// The operation that opened this connection is over. Only the pool decides
	// when it ends now, so the next operation's context cannot close it.
	c.Detach()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.entries == nil {
		p.entries = map[string][]pooledConn{}
	}
	if len(p.entries[key]) >= pooledPerKey {
		c.Close()
		return
	}
	p.entries[key] = append(p.entries[key], pooledConn{client: c, idleAt: p.clock()})
}

// sweep closes every connection that has been idle for longer than the TTL.
// take does the same for its own key, but a connection whose profile is never
// used again would otherwise stay authenticated until the app closes.
func (p *connPool) sweep() {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.clock()
	for key, list := range p.entries {
		kept := list[:0]
		for _, entry := range list {
			if now.Sub(entry.idleAt) > pooledIdleTTL {
				entry.client.Close()
				continue
			}
			kept = append(kept, entry)
		}
		if len(kept) == 0 {
			delete(p.entries, key)
			continue
		}
		p.entries[key] = kept
	}
}

// clear closes every idle connection, for a profile change, a policy change or a
// lock.
func (p *connPool) clear() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for key, list := range p.entries {
		for _, entry := range list {
			entry.client.Close()
		}
		delete(p.entries, key)
	}
}

// pooledKey identifies a connection by the profile and the host key it was
// verified against, so a changed address or a changed fingerprint never reuses a
// connection confirmed under the old one.
func pooledKey(profileID, fingerprint string) string {
	return profileID + "\x00" + fingerprint
}
