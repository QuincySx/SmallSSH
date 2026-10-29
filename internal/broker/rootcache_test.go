package broker

import (
	"testing"
	"time"
)

func TestProtectedRootCacheExpiresAndClears(t *testing.T) {
	now := time.Now()
	cache := protectedRootCache{now: func() time.Time { return now }}
	key := rootCacheKey("profile", []string{"/etc", "/home"})

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
	first := rootCacheKey("profile", []string{"/etc"})
	second := rootCacheKey("profile", []string{"/etc", "/srv"})
	other := rootCacheKey("other", []string{"/etc"})
	cache.put(first, []string{"/etc"})
	for _, key := range []string{second, other} {
		if _, ok := cache.get(key); ok {
			t.Fatalf("key %q shared a resolution with another configuration", key)
		}
	}
}
