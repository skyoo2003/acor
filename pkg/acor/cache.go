package acor

import (
	"sync"
)

type trieCache struct {
	mu       sync.RWMutex
	loadMu   sync.Mutex
	prefixes []string
	outputs  map[string][]string
	valid    bool
	// pendingSelfInvalidations holds self-published message IDs so the listener
	// can skip cache invalidation already performed by the local publisher.
	// sync.Map is used for lock-free access by concurrent publisher/listener goroutines.
	pendingSelfInvalidations sync.Map
}

func skipSelfSet(c *trieCache, id string) {
	c.pendingSelfInvalidations.Store(id, struct{}{})
}

func skipSelfClear(c *trieCache, id string) {
	c.pendingSelfInvalidations.Delete(id)
}

// skipSelfCheck atomically checks and removes a self-published message ID.
// Returns true if the ID was found (self-message → skip invalidation).
func skipSelfCheck(c *trieCache, id string) bool {
	_, loaded := c.pendingSelfInvalidations.LoadAndDelete(id)
	return loaded
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
	c.valid = true
}

func (c *trieCache) get() (prefixes []string, outputs map[string][]string, valid bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return append([]string(nil), c.prefixes...), cloneOutputs(c.outputs), c.valid
}
