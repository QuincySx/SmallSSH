package broker

import (
	"strings"
	"sync"
	"time"
)

// A mutation used to spend two SFTP round trips per protected root — eleven of
// them by default — before touching the file it was approved for. The roots are
// the owner's configuration, so their canonical form is cached briefly; a policy
// change drops it, and a link swapped after the fact is still caught because the
// path under the root is resolved every time.
const protectedRootsTTL = 30 * time.Second

type protectedRootEntry struct {
	canonical []string
	expires   time.Time
}

type protectedRootCache struct {
	mu      sync.Mutex
	entries map[string]protectedRootEntry
	now     func() time.Time
}

func (c *protectedRootCache) get(key string) ([]string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok || !c.clock().Before(entry.expires) {
		delete(c.entries, key)
		return nil, false
	}
	return entry.canonical, true
}

func (c *protectedRootCache) put(key string, canonical []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = map[string]protectedRootEntry{}
	}
	c.entries[key] = protectedRootEntry{canonical: canonical, expires: c.clock().Add(protectedRootsTTL)}
}

// clear drops every entry, for a policy change, a profile update or a lock.
// clock is the cache's time source, overridable in tests.
func (c *protectedRootCache) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

func (c *protectedRootCache) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = nil
}

func rootCacheKey(profileID string, roots []string) string {
	return profileID + "\x00" + strings.Join(roots, "\x00")
}
