package broker

import (
	"testing"
	"time"

	"sshclient/internal/sshconn"
)

// A connection that has been idle too long is not handed on, and one key keeps at
// most its own share.
func TestConnPoolDropsIdleConnections(t *testing.T) {
	now := time.Now()
	pool := connPool{now: func() time.Time { return now }}
	if got := pool.take("key"); got != nil {
		t.Fatal("empty pool handed out a connection")
	}
	pool.put("key", &sshconn.Client{})
	if _, ok := pool.entries["key"]; !ok {
		t.Fatal("connection was not kept")
	}
	now = now.Add(pooledIdleTTL + time.Second)
	if got := pool.take("key"); got != nil {
		t.Fatal("an expired connection was handed out")
	}
	if _, ok := pool.entries["key"]; ok {
		t.Fatal("expired entry stayed in the pool")
	}
}

func TestConnPoolCapsEachKeyAndClears(t *testing.T) {
	pool := connPool{}
	for i := 0; i < pooledPerKey+2; i++ {
		pool.put("key", &sshconn.Client{})
	}
	if got := len(pool.entries["key"]); got != pooledPerKey {
		t.Fatalf("pool kept %d connections for one key, want %d", got, pooledPerKey)
	}
	pool.clear()
	if len(pool.entries) != 0 {
		t.Fatal("clear kept entries")
	}
}

// A connection whose profile is never used again has no take to expire it, so
// the broker sweeps the pool on its own clock.
func TestConnPoolSweepsConnectionsNoOperationTouches(t *testing.T) {
	now := time.Now()
	pool := connPool{now: func() time.Time { return now }}
	pool.put("unused", &sshconn.Client{})
	now = now.Add(pooledIdleTTL / 2)
	pool.put("used", &sshconn.Client{})
	now = now.Add(pooledIdleTTL/2 + time.Second)
	pool.sweep()
	if _, ok := pool.entries["unused"]; ok {
		t.Fatal("sweep kept a connection idle past the TTL")
	}
	if _, ok := pool.entries["used"]; !ok {
		t.Fatal("sweep dropped a connection that is still fresh")
	}
}
