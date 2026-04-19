// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"sync"
)

// Compile-time check.
var _ operations = (*inMemoryOps)(nil)

// inMemoryOps adapts a matchEngine to satisfy the operations interface for
// pure in-memory mode. Suggest/SuggestIndex return ErrSuggestRequiresRedis.
type inMemoryOps struct {
	mu            sync.RWMutex
	engine        matchEngine
	preset        Preset
	caseSensitive bool
	keywordSet    map[string]struct{}
}

func newInMemoryOps(preset Preset, caseSensitive bool) *inMemoryOps {
	return &inMemoryOps{
		engine:        newMatchEngine(preset),
		preset:        preset,
		caseSensitive: caseSensitive,
		keywordSet:    make(map[string]struct{}),
	}
}

func (o *inMemoryOps) add(_ context.Context, keyword string) (int, error) {
	keyword = normalizeKeyword(keyword, o.caseSensitive)
	if keyword == "" {
		return 0, nil
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if _, exists := o.keywordSet[keyword]; exists {
		return 0, nil
	}
	o.keywordSet[keyword] = struct{}{}
	o.engine.buildFromKeywords(o.keywordSet)
	return 1, nil
}

func (o *inMemoryOps) remove(_ context.Context, keyword string) (int, error) {
	keyword = normalizeKeyword(keyword, o.caseSensitive)
	if keyword == "" {
		return 0, nil
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if _, exists := o.keywordSet[keyword]; !exists {
		return 0, nil
	}
	delete(o.keywordSet, keyword)
	o.engine.buildFromKeywords(o.keywordSet)
	return 1, nil
}

func (o *inMemoryOps) find(_ context.Context, text string) ([]string, error) {
	if text == "" {
		return []string{}, nil
	}
	text = normalizeText(text, o.caseSensitive)
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.engine.find(text), nil
}

func (o *inMemoryOps) findIndex(_ context.Context, text string) (map[string][]int, error) {
	if text == "" {
		return map[string][]int{}, nil
	}
	text = normalizeText(text, o.caseSensitive)
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.engine.findIndex(text), nil
}

func (o *inMemoryOps) suggest(_ context.Context, _ string) ([]string, error) {
	return nil, ErrSuggestRequiresRedis
}

func (o *inMemoryOps) suggestIndex(_ context.Context, _ string) (map[string][]int, error) {
	return nil, ErrSuggestRequiresRedis
}

func (o *inMemoryOps) flush(_ context.Context) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.keywordSet = make(map[string]struct{})
	o.engine.buildFromKeywords(o.keywordSet)
	return nil
}

func (o *inMemoryOps) info(_ context.Context) (*AhoCorasickInfo, error) {
	o.mu.RLock()
	defer o.mu.RUnlock()
	mi := o.engine.info()
	return &AhoCorasickInfo{
		Keywords:    mi.Keywords,
		Nodes:       mi.Nodes,
		Preset:      mi.Preset,
		MemoryBytes: mi.MemoryBytes,
		TrieDepth:   mi.TrieDepth,
	}, nil
}
