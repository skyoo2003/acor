package acor

import (
	"sync"
	"sync/atomic"
)

type trieCache struct {
	mu       sync.RWMutex
	loadMu   sync.Mutex
	prefixes []string
	outputs  map[string][]string
	valid    bool
	// skipSelf is an atomic flag used to skip cache invalidation caused by
	// the instance's own publishInvalidate call. Without this, the pub/sub
	// listener would receive the message it published and invalidate the
	// cache a second time, creating a race with concurrent Find calls.
	skipSelf int32
}

func skipSelfSet(c *trieCache)   { atomic.StoreInt32(&c.skipSelf, 1) }
func skipSelfClear(c *trieCache) { atomic.StoreInt32(&c.skipSelf, 0) }
func skipSelfCheck(c *trieCache) bool {
	return atomic.CompareAndSwapInt32(&c.skipSelf, 1, 0)
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
