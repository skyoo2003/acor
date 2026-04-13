package acor

import (
	"sync"
	"sync/atomic"
)

type trieCache struct {
	mu                       sync.RWMutex
	loadMu                   sync.Mutex
	prefixes                 []string
	outputs                  map[string][]string
	valid                    bool
	pendingSelfInvalidations int32
}

func skipSelfSet(c *trieCache)   { atomic.AddInt32(&c.pendingSelfInvalidations, 1) }
func skipSelfClear(c *trieCache) { atomic.AddInt32(&c.pendingSelfInvalidations, -1) }
func skipSelfCheck(c *trieCache) bool {
	for {
		n := atomic.LoadInt32(&c.pendingSelfInvalidations)
		if n == 0 {
			return false
		}
		if atomic.CompareAndSwapInt32(&c.pendingSelfInvalidations, n, n-1) {
			return true
		}
	}
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
