// Package cache provides an in-memory LRU response cache for proxy calls.
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

type entry struct {
	value     []byte
	expiresAt time.Time
	lastUsed  time.Time
}

// Cache is a thread-safe in-memory LRU response cache.
type Cache struct {
	mu         sync.RWMutex
	items      map[string]*entry
	maxEntries int
	defaultTTL time.Duration
	hits       atomic.Int64
	misses     atomic.Int64
	excludeSrc map[string]struct{}
}

// New creates a new Cache with the given max entries and default TTL.
func New(maxEntries int, defaultTTL time.Duration, excludeSources []string) *Cache {
	if maxEntries <= 0 {
		maxEntries = 500
	}
	if defaultTTL <= 0 {
		defaultTTL = 5 * time.Minute
	}
	exc := make(map[string]struct{}, len(excludeSources))
	for _, s := range excludeSources {
		exc[s] = struct{}{}
	}
	return &Cache{
		items:      make(map[string]*entry, maxEntries),
		maxEntries: maxEntries,
		defaultTTL: defaultTTL,
		excludeSrc: exc,
	}
}

// Key generates a cache key from a canonical tool name and sorted input arguments.
func Key(canonicalName string, input map[string]any) string {
	h := sha256.New()
	h.Write([]byte(canonicalName))
	h.Write([]byte{0}) // separator
	if len(input) > 0 {
		// Sort keys for deterministic hashing
		keys := make([]string, 0, len(input))
		for k := range input {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		sorted := make(map[string]any, len(input))
		for _, k := range keys {
			sorted[k] = input[k]
		}
		b, _ := json.Marshal(sorted)
		h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// IsSourceExcluded returns true if the given source should bypass caching.
func (c *Cache) IsSourceExcluded(sourceID string) bool {
	_, ok := c.excludeSrc[sourceID]
	return ok
}

// Get returns the cached response for a key, or nil and false if not found/expired.
func (c *Cache) Get(key string) ([]byte, bool) {
	c.mu.RLock()
	e, ok := c.items[key]
	c.mu.RUnlock()
	if !ok {
		c.misses.Add(1)
		return nil, false
	}
	if time.Now().After(e.expiresAt) {
		c.mu.Lock()
		delete(c.items, key)
		c.mu.Unlock()
		c.misses.Add(1)
		return nil, false
	}
	c.mu.Lock()
	e.lastUsed = time.Now()
	c.mu.Unlock()
	c.hits.Add(1)
	return e.value, true
}

// Put stores a response in the cache with the default TTL.
func (c *Cache) Put(key string, value []byte) {
	c.PutWithTTL(key, value, c.defaultTTL)
}

// PutWithTTL stores a response in the cache with a specific TTL.
func (c *Cache) PutWithTTL(key string, value []byte, ttl time.Duration) {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	// Evict if at capacity
	if len(c.items) >= c.maxEntries {
		c.evictLRU()
	}
	c.items[key] = &entry{
		value:     value,
		expiresAt: now.Add(ttl),
		lastUsed:  now,
	}
}

// Clear removes all entries from the cache.
func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[string]*entry, c.maxEntries)
	c.hits.Store(0)
	c.misses.Store(0)
}

// Stats returns cache statistics.
func (c *Cache) Stats() (hits, misses int64, size int) {
	c.mu.RLock()
	size = len(c.items)
	c.mu.RUnlock()
	return c.hits.Load(), c.misses.Load(), size
}

// MaxEntries returns the configured cache capacity.
func (c *Cache) MaxEntries() int {
	if c == nil {
		return 0
	}
	return c.maxEntries
}

// evictLRU removes the least recently used entry. Must be called with mu held.
func (c *Cache) evictLRU() {
	var oldestKey string
	var oldestTime time.Time
	first := true
	for k, e := range c.items {
		if first || e.lastUsed.Before(oldestTime) {
			oldestKey = k
			oldestTime = e.lastUsed
			first = false
		}
	}
	if oldestKey != "" {
		delete(c.items, oldestKey)
	}
}
