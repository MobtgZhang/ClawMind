package codectx

import (
	"sync"
	"time"
)

// Cache holds workspace profiles so a large repository is not walked on every turn. Without it a
// 3000-file repo costs a full filesystem walk per message, which is the most expensive part of a
// turn that does not need the profile at all.
type Cache struct {
	mu      sync.Mutex
	ttl     time.Duration
	entries map[string]cacheEntry
}

type cacheEntry struct {
	profile *Profile
	at      time.Time
}

// NewCache returns a cache with the given freshness window. ttl <= 0 means entries are reused
// until the process restarts.
func NewCache(ttl time.Duration) *Cache {
	return &Cache{ttl: ttl, entries: map[string]cacheEntry{}}
}

// Detect returns a cached profile, rebuilding it when the entry is stale.
func (c *Cache) Detect(root string) *Profile {
	if c == nil {
		return Detect(root)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	key := root
	if e, ok := c.entries[key]; ok && (c.ttl <= 0 || time.Since(e.at) < c.ttl) {
		return e.profile
	}
	p := Detect(root)
	c.entries[key] = cacheEntry{profile: p, at: time.Now()}
	return p
}

// Invalidate drops one entry, used after a change that alters the shape of the workspace.
func (c *Cache) Invalidate(root string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, root)
}
