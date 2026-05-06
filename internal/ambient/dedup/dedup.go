// Package dedup implements the client-side fingerprint dedup that the ambient
// Runner uses at the enqueue boundary (per ADR-066 §Decision item 3).
//
// The substrate's design splits dedup into two layers:
//
//	Client-side (this package): cheap SHA-256 fingerprint check against an
//	  LRU cache with a TTL window. Drops no-op events (lock-screen spam,
//	  paused video, identical clipboard re-copies) BEFORE pipeline cost is
//	  incurred. Default window: 60 seconds. Default capacity: 4096 entries.
//
//	Server-side (existing internal/jobs/worker.go): post-pipeline ContentHash
//	  dedup against the durable objects table. Catches whatever the client
//	  missed (cross-machine duplicates via federation, replay after long
//	  network loss, etc.). Stays as the network-tolerant safety net.
//
// The Cache type is the production implementation of the ambient.Dedup
// interface. It is goroutine-safe; the Runner calls IsDuplicate from its
// dispatch goroutine and the cache may also be inspected by status / MCP
// read paths concurrently.
package dedup

import (
	"container/list"
	"sync"
	"time"
)

// Default values for Cache configuration. Used by NewCache when WithWindow /
// WithCapacity are not supplied. The 60-second window matches the ambient
// substrate's design intent (per ADR-066 §Decision item 3); the 4096-entry
// cap keeps memory bounded under typical capture rates (~70 events/sec for
// 60s of full-blast burst is unusual but bounded).
const (
	DefaultWindow   = 60 * time.Second
	DefaultCapacity = 4096
)

// Cache is a TTL-bounded LRU keyed on fingerprint string. Inserting a
// fingerprint that's already present (and not yet expired) returns true
// from IsDuplicate; otherwise the cache records the fingerprint and returns
// false.
//
// Eviction triggers:
//
//	- TTL: entries older than Window are treated as not-present
//	- Capacity: when len(cache) reaches Capacity, the least-recently-used
//	  entry is evicted to make room for the new one
//
// Cache is goroutine-safe. The Runner's dispatch goroutine calls
// IsDuplicate concurrently with status / health read paths.
type Cache struct {
	mu       sync.Mutex
	window   time.Duration
	capacity int
	now      func() time.Time

	// LRU implementation: a doubly-linked list ordered by recency, plus a
	// map for O(1) lookup. Each list element points at an entry struct.
	order *list.List
	index map[string]*list.Element
}

// entry is the value stored in the LRU list.
type entry struct {
	fingerprint string
	insertedAt  time.Time
}

// Option configures Cache construction.
type Option func(*Cache)

// WithWindow sets the TTL window. Entries older than window are treated as
// not-present (allowing re-emission of the same fingerprint after the
// window has elapsed).
func WithWindow(window time.Duration) Option {
	return func(c *Cache) { c.window = window }
}

// WithCapacity sets the maximum number of entries the cache will hold. When
// the cache is at capacity, inserting a new fingerprint evicts the
// least-recently-used existing entry.
func WithCapacity(capacity int) Option {
	return func(c *Cache) { c.capacity = capacity }
}

// withClock sets the time source. Tests use this with a fake clock; production
// callers leave it at the default time.Now. Unexported because there's no
// real-world reason for a production caller to override the wall clock.
func withClock(now func() time.Time) Option {
	return func(c *Cache) { c.now = now }
}

// NewCache constructs a Cache with the supplied options. Defaults to
// DefaultWindow + DefaultCapacity + time.Now.
func NewCache(opts ...Option) *Cache {
	c := &Cache{
		window:   DefaultWindow,
		capacity: DefaultCapacity,
		now:      time.Now,
		order:    list.New(),
		index:    make(map[string]*list.Element),
	}
	for _, opt := range opts {
		opt(c)
	}
	if c.capacity < 1 {
		c.capacity = DefaultCapacity
	}
	if c.window <= 0 {
		c.window = DefaultWindow
	}
	return c
}

// IsDuplicate reports whether fingerprint has been recorded within the TTL
// window. Side effects:
//
//	- If absent (or expired): records the fingerprint and returns false
//	  (i.e. "not a duplicate; let it through").
//	- If present and within window: refreshes the LRU position and returns
//	  true (i.e. "duplicate; the Runner will drop this event").
//	- If present but expired: deletes the stale entry, records fresh, and
//	  returns false.
//
// Empty fingerprints are never treated as duplicates (they bypass dedup at
// the source level too); IsDuplicate("") returns false without recording.
func (c *Cache) IsDuplicate(fingerprint string) bool {
	if fingerprint == "" {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	if elem, ok := c.index[fingerprint]; ok {
		ent := elem.Value.(*entry)
		if now.Sub(ent.insertedAt) <= c.window {
			// Within window — refresh LRU position, report duplicate.
			c.order.MoveToFront(elem)
			ent.insertedAt = now
			return true
		}
		// Expired — remove and fall through to insert fresh.
		c.order.Remove(elem)
		delete(c.index, fingerprint)
	}
	// Insert. Evict LRU if at capacity.
	if c.order.Len() >= c.capacity {
		c.evictLRULocked()
	}
	elem := c.order.PushFront(&entry{fingerprint: fingerprint, insertedAt: now})
	c.index[fingerprint] = elem
	return false
}

// Len returns the number of entries currently in the cache. Used by tests
// and by the future MCP health tool.
func (c *Cache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.order.Len()
}

// Sweep removes all expired entries. Called opportunistically; not required
// for correctness because IsDuplicate handles expiration on lookup, but
// useful to bound memory when the dedup rate is high relative to the cache
// capacity.
func (c *Cache) Sweep() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	removed := 0
	for {
		elem := c.order.Back()
		if elem == nil {
			break
		}
		ent := elem.Value.(*entry)
		if now.Sub(ent.insertedAt) <= c.window {
			break // ordered by recency; nothing older
		}
		c.order.Remove(elem)
		delete(c.index, ent.fingerprint)
		removed++
	}
	return removed
}

// evictLRULocked removes the oldest entry. Caller MUST hold c.mu.
func (c *Cache) evictLRULocked() {
	elem := c.order.Back()
	if elem == nil {
		return
	}
	ent := elem.Value.(*entry)
	c.order.Remove(elem)
	delete(c.index, ent.fingerprint)
}
