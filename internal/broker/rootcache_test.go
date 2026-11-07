package broker

import (
	"testing"
	"time"
)

func TestProtectedRootCacheExpiresAndClears(t *testing.T) {
	now := time.Now()
	cache := protectedRootCache{now: func() time.Time { return now }}
	key := rootCacheKey(poolLease{key: poolKey{profile: "profile"}}, []string{"/etc", "/home"})

	if _, ok := cache.get(key); ok {
		t.Fatal("empty cache returned an entry")
	}
	cache.put(key, []string{"/etc", "/home/user"})
	got, ok := cache.get(key)
	if !ok || len(got) != 2 || got[1] != "/home/user" {
		t.Fatalf("cache lost the canonical roots: %v %v", got, ok)
	}
	now = now.Add(protectedRootsTTL - time.Second)
	if _, ok = cache.get(key); !ok {
		t.Fatal("entry expired early")
	}
	now = now.Add(2 * time.Second)
	if _, ok = cache.get(key); ok {
		t.Fatal("entry outlived its ttl")
	}
	cache.put(key, []string{"/etc"})
	cache.clear()
	if _, ok = cache.get(key); ok {
		t.Fatal("clear kept an entry")
	}
}

// A different root list is a different key: changing the policy cannot be served
// from a resolution cached for the old one.
func TestProtectedRootCacheKeysByRootsAndProfile(t *testing.T) {
	cache := protectedRootCache{}
	first := rootCacheKey(poolLease{key: poolKey{profile: "profile"}}, []string{"/etc"})
	second := rootCacheKey(poolLease{key: poolKey{profile: "profile"}}, []string{"/etc", "/srv"})
	other := rootCacheKey(poolLease{key: poolKey{profile: "other"}}, []string{"/etc"})
	cache.put(first, []string{"/etc"})
	for _, key := range []protectedRootKey{second, other} {
		if _, ok := cache.get(key); ok {
			t.Fatalf("key %v shared a resolution with another configuration", key)
		}
	}
}

// A completed old resolver can publish after clear. Its result must remain
// inaccessible to the new configuration, even when profile ID/roots match.
func TestProtectedRootCacheRejectsLateResultsFromAnOldConfiguration(t *testing.T) {
	cache := protectedRootCache{}
	old := poolLease{key: poolKey{profile: "profile", host: "old-host", user: "old-user"}, generation: 4}
	roots := []string{"/protected"}
	cache.clear()
	cache.put(rootCacheKey(old, roots), []string{"/old-canonical"})
	current := old
	current.generation++
	if value, ok := cache.get(rootCacheKey(current, roots)); ok {
		t.Fatal("old credential generation supplied protected paths", value)
	}
	current = old
	current.key.host = "new-host"
	if _, ok := cache.get(rootCacheKey(current, roots)); ok {
		t.Fatal("another endpoint reused protected paths")
	}
	current = old
	current.key.user = "new-user"
	if _, ok := cache.get(rootCacheKey(current, roots)); ok {
		t.Fatal("another SSH account reused protected paths")
	}
}
