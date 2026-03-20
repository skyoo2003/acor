package acor

import "sync"

type trieCache struct {
	mu       sync.RWMutex
	prefixes []string
	outputs  map[string][]string
	valid    bool
}

func (c *trieCache) invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.valid = false
}

func (c *trieCache) set(prefixes []string, outputs map[string][]string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.prefixes = prefixes
	c.outputs = outputs
	c.valid = true
}

func (c *trieCache) get() (prefixes []string, outputs map[string][]string, valid bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.prefixes, c.outputs, c.valid
}
