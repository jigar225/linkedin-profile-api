package server

import (
	"sync"
	"time"
)

// cacheTTL is how long a fetched profile/company stays hot. 15 minutes kills
// repeat/demo traffic (reviewers hammer the same URL) while keeping data fresh.
const cacheTTL = 15 * time.Minute

// cacheEntry is one marshaled JSON response, ready to serve.
type cacheEntry struct {
	data      []byte
	fetchedAt time.Time
}

// cache is an in-process URL→JSON store. In-memory is the right call for a
// single-instance deploy: zero deps, ~0ms hits. If we ever scale to multiple
// instances, swap this for a shared store (e.g. Redis) behind the same API.
type cache struct {
	mu      sync.RWMutex
	entries map[string]cacheEntry
}

func newCache() *cache {
	return &cache{entries: map[string]cacheEntry{}}
}

func (c *cache) get(key string) ([]byte, bool) {
	c.mu.RLock()
	e, ok := c.entries[key]
	c.mu.RUnlock()
	if !ok || time.Since(e.fetchedAt) > cacheTTL {
		return nil, false
	}
	return e.data, true
}

func (c *cache) set(key string, data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// lazy sweep of expired entries while we're here (n stays tiny)
	for k, e := range c.entries {
		if time.Since(e.fetchedAt) > cacheTTL {
			delete(c.entries, k)
		}
	}
	c.entries[key] = cacheEntry{data: data, fetchedAt: time.Now()}
}
