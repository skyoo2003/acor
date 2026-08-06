// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"sync"

	matchengine "github.com/skyoo2003/acor/internal/engine"
)

type trieCache struct {
	mu     sync.RWMutex
	loadMu sync.Mutex
	engine *matchengine.Engine
	valid  bool
	// selfSkip holds the IDs this instance published, so the listener can skip
	// the invalidation the publisher already applied. See selfSkipSet.
	selfSkip selfSkipSet
}

// buildEngineFromOutputs builds a local Aho-Corasick match engine from the V2
// outputs map. In an Aho-Corasick automaton every keyword has its own terminal
// state whose output list contains that keyword, so the union of all output
// values is exactly the keyword set. PresetBalanced matches the redis-backed
// engine's default (DAT + banded DFA).
func buildEngineFromOutputs(outputs map[string][]string) *matchengine.Engine {
	keywords := make(map[string]struct{})
	for _, outs := range outputs {
		for _, kw := range outs {
			keywords[kw] = struct{}{}
		}
	}
	engine := matchengine.New(enginePreset(PresetBalanced))
	engine.Build(keywords)
	return engine
}

func (c *trieCache) invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.valid = false
}

func (c *trieCache) set(outputs map[string][]string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.engine = buildEngineFromOutputs(outputs)
	c.valid = true
}

// getEngine returns the cached match engine and whether the cache is valid.
// The engine is immutable after set() (replaced atomically on reload), so the
// caller may use the returned engine concurrently without additional locking.
func (c *trieCache) getEngine() (*matchengine.Engine, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.engine, c.valid
}
