package server

import (
	"sync"
	"time"
)

// cacheTTL is how long a fetched profile/company stays hot. 24 hours means
// reviewers hammering the same URL across the review window almost never
// trigger a second upstream fetch — cheap for us, gentle on the account.
const cacheTTL = 24 * time.Hour

// staleMaxAge is the hard eviction horizon. Entries past their TTL are kept
// this long so an upstream failure can still serve the last good response
// (stale-if-error) instead of erroring out.
const staleMaxAge = 7 * 24 * time.Hour

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

// getStale returns the last good response regardless of freshness — the
// fallback when an upstream fetch fails (stale-if-error).
func (c *cache) getStale(key string) ([]byte, bool) {
	c.mu.RLock()
	e, ok := c.entries[key]
	c.mu.RUnlock()
	if !ok || time.Since(e.fetchedAt) > staleMaxAge {
		return nil, false
	}
	return e.data, true
}

func (c *cache) set(key string, data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// lazy sweep of ancient entries while we're here (n stays tiny); entries
	// past TTL are kept for stale-if-error until staleMaxAge.
	for k, e := range c.entries {
		if time.Since(e.fetchedAt) > staleMaxAge {
			delete(c.entries, k)
		}
	}
	c.entries[key] = cacheEntry{data: data, fetchedAt: time.Now()}
}
