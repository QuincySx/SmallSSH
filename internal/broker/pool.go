package broker

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"sshclient/internal/sshconn"
	"sshclient/internal/vault"
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

// Only public connection identity is a cache key. Credential changes are
// fenced by the generation advanced whenever configuration invalidates a pool.
type poolKey struct {
	profile, host, user, fingerprint, auth, agent string
	options                                       string
	port                                          int
}

type poolLease struct {
	key        poolKey
	generation uint64
}

type connPool struct {
	mu         sync.Mutex
	entries    map[poolKey][]pooledConn
	generation uint64
	now        func() time.Time
}

func (p *connPool) clock() time.Time {
	if p.now != nil {
		return p.now()
	}
	return time.Now()
}

func (p *connPool) lease(key poolKey) poolLease {
	p.mu.Lock()
	defer p.mu.Unlock()
	return poolLease{key: key, generation: p.generation}
}

// take returns a current-generation connection already bound to the new
// operation's cancellation, including while opening SSH or SFTP channels.
func (p *connPool) take(ctx context.Context, lease poolLease) *sshconn.Client {
	p.mu.Lock()
	defer p.mu.Unlock()
	if lease.generation != p.generation {
		return nil
	}
	key := lease.key
	list := p.entries[key]
	for len(list) > 0 {
		entry := list[len(list)-1]
		list = list[:len(list)-1]
		if p.clock().Sub(entry.idleAt) <= pooledIdleTTL {
			p.entries[key] = list
			entry.client.Bind(ctx)
			return entry.client
		}
		entry.client.Close()
	}
	delete(p.entries, key)
	return nil
}

// put keeps a connection for the next operation, or closes it when its share is
// already full.
func (p *connPool) put(lease poolLease, c *sshconn.Client) {
	if c == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if lease.generation != p.generation {
		c.Close()
		return
	}
	// Check, detach and publish under the same lock as clear. A connection
	// checked out before a configuration change can never re-enter this pool.
	c.Detach()
	if p.entries == nil {
		p.entries = map[poolKey][]pooledConn{}
	}
	key := lease.key
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
	p.generation++
	for key, list := range p.entries {
		for _, entry := range list {
			entry.client.Close()
		}
		delete(p.entries, key)
	}
}

func pooledKey(profile vault.Profile) poolKey {
	b, _ := json.Marshal(struct {
		Network  any
		Hardware any
		Terminal any
	}{profile.Network.Public(), profile.Hardware.Public(), profile.Terminal})
	return poolKey{profile: profile.ID, host: profile.Host, port: profile.Port, user: profile.User, fingerprint: profile.Fingerprint, auth: profile.Auth, agent: profile.AgentFingerprint, options: string(b)}
}
