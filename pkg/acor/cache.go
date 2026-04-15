package acor

import (
	"sync"
	"time"
)

// pendingSelfInvalidationTTL limits how long a self-skip entry lives.
// Lost pub/sub messages leave inert entries; this TTL prevents unbounded
// growth in long-running processes. 30s is orders of magnitude beyond
// typical Redis pub/sub delivery latency.
const pendingSelfInvalidationTTL = 30 * time.Second

type trieCache struct {
	mu        sync.RWMutex
	loadMu    sync.Mutex
	prefixes  []string
	prefixSet map[string]struct{}
	outputs   map[string][]string
	valid     bool
	// pendingSelfInvalidations holds self-published message IDs so the listener
	// can skip cache invalidation already performed by the local publisher.
	// sync.Map is used for lock-free access by concurrent publisher/listener goroutines.
	// Entries store time.Time values; expired entries are cleaned up lazily or via
	// cleanupExpiredSelfInvalidations to prevent unbounded growth from lost messages.
	pendingSelfInvalidations sync.Map
}

func skipSelfSet(c *trieCache, id string) {
	c.pendingSelfInvalidations.Store(id, time.Now())
}

func skipSelfClear(c *trieCache, id string) {
	c.pendingSelfInvalidations.Delete(id)
}

// skipSelfCheck atomically checks and removes a self-published message ID.
// Returns true if the ID was found and not expired (self-message → skip invalidation).
func skipSelfCheck(c *trieCache, id string) bool {
	val, loaded := c.pendingSelfInvalidations.LoadAndDelete(id)
	if !loaded {
		return false
	}
	t, ok := val.(time.Time)
	if !ok {
		return false
	}
	age := time.Since(t)
	if age < 0 {
		return false
	}
	return age < pendingSelfInvalidationTTL
}

// cleanupExpiredSelfInvalidations removes stale entries to prevent unbounded map growth
// when Redis pub/sub messages are lost. Safe for concurrent use.
func cleanupExpiredSelfInvalidations(c *trieCache) {
	cutoff := time.Now().Add(-pendingSelfInvalidationTTL)
	c.pendingSelfInvalidations.Range(func(key, value interface{}) bool {
		t, ok := value.(time.Time)
		if !ok {
			c.pendingSelfInvalidations.Delete(key)
			return true
		}
		if t.Before(cutoff) {
			c.pendingSelfInvalidations.Delete(key)
		}
		return true
	})
}

func cloneOutputs(in map[string][]string) map[string][]string {
	if in == nil {
		return nil
	}
	out := make(map[string][]string, len(in))
	for k, v := range in {
		out[k] = append([]string(nil), v...)
	}
	return out
}

func (c *trieCache) invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.valid = false
}

func (c *trieCache) set(prefixes []string, outputs map[string][]string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.prefixes = append([]string(nil), prefixes...)
	c.outputs = cloneOutputs(outputs)
	c.prefixSet = make(map[string]struct{}, len(prefixes))
	for _, p := range prefixes {
		c.prefixSet[p] = struct{}{}
	}
	c.valid = true
}

func (c *trieCache) get() (prefixes []string, outputs map[string][]string, valid bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return append([]string(nil), c.prefixes...), cloneOutputs(c.outputs), c.valid
}

// getPrefixSet returns the cached prefix set for O(1) lookups.
// The returned map is safe to read concurrently (it is replaced, not mutated, by set).
func (c *trieCache) getPrefixSet() map[string]struct{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.prefixSet
}
