// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
)

// redisBackedAC implements the operations interface directly, so AhoCorasick
// dispatches into it with no adapter in between. Suggest/SuggestIndex are the
// only operations preset mode cannot serve (there is no prefix index locally).
var _ operations = (*redisBackedAC)(nil)

// add inserts a keyword into the automaton. The keyword is written atomically
// to Redis via a V2 Lua script (optimistic locking), then the local automaton
// is rebuilt and an invalidation is published.
func (ac *redisBackedAC) add(ctx context.Context, keyword string) (int, error) {
	keyword = normalizeKeyword(keyword, ac.caseSensitive)
	if keyword == "" {
		return 0, nil
	}

	added, err := retryOnConflict(ctx, func() (int, error) { return ac.tryAdd(ctx, keyword) })
	if err == nil && added == 1 {
		ac.publishInvalidate(ctx)
	}
	return added, err
}

func (ac *redisBackedAC) tryAdd(ctx context.Context, keyword string) (int, error) {
	snap, err := readTrieSnapshot(ctx, ac.storage, ac.name)
	if err != nil {
		return 0, err
	}

	outputs, changed := planAdd(snap, keyword)
	if !changed {
		return 0, nil
	}

	newVersion, err := commitV2Write(ctx, ac.redisClient, ac.name, snap, outputs, false)
	if err != nil {
		return 0, err
	}

	ac.applyLocalWrite(keyword, true, newVersion)
	return 1, nil
}

// applyLocalWrite applies a committed Redis write to the local state under lock,
// rebuilding the engine and clearing the stale flag. add selects insertion vs
// deletion of keyword in the keyword set.
func (ac *redisBackedAC) applyLocalWrite(keyword string, add bool, newVersion int64) {
	ac.mu.Lock()
	if add {
		ac.keywordSet[keyword] = struct{}{}
	} else {
		delete(ac.keywordSet, keyword)
	}
	ac.localVersion = newVersion
	ac.rebuildEngine()
	ac.stale = false
	ac.mu.Unlock()
}

// remove deletes a keyword from the automaton.
func (ac *redisBackedAC) remove(ctx context.Context, keyword string) (int, error) {
	keyword = normalizeKeyword(keyword, ac.caseSensitive)
	if keyword == "" {
		return 0, nil
	}

	removed, err := retryOnConflict(ctx, func() (int, error) { return ac.tryRemove(ctx, keyword) })
	if err == nil && removed == 1 {
		ac.publishInvalidate(ctx)
	}
	return removed, err
}

func (ac *redisBackedAC) tryRemove(ctx context.Context, keyword string) (int, error) {
	snap, err := readTrieSnapshot(ctx, ac.storage, ac.name)
	if err != nil {
		return 0, err
	}

	outputs, changed := planRemove(snap, keyword)
	if !changed {
		return 0, nil
	}

	newVersion, err := commitV2Write(ctx, ac.redisClient, ac.name, snap, outputs, true)
	if err != nil {
		return 0, err
	}

	ac.applyLocalWrite(keyword, false, newVersion)
	return 1, nil
}

// find searches the text for all keywords using the local automaton.
func (ac *redisBackedAC) find(ctx context.Context, text string) ([]string, error) {
	if text == "" {
		return []string{}, nil
	}
	text = normalizeText(text, ac.caseSensitive)

	e, err := ac.loadEngine(ctx)
	if err != nil {
		return nil, err
	}

	// Honor a canceled ctx at the match boundary; the in-memory scan itself isn't
	// ctx-threaded, and loadEngine returns without touching Redis on a warm engine.
	// Mirrors v2Operations.find.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return e.Find(text), nil
}

// findIndex searches the text for all keywords and returns their start indices.
func (ac *redisBackedAC) findIndex(ctx context.Context, text string) (map[string][]int, error) {
	if text == "" {
		return map[string][]int{}, nil
	}
	text = normalizeText(text, ac.caseSensitive)

	e, err := ac.loadEngine(ctx)
	if err != nil {
		return nil, err
	}

	// See find: honor an already-canceled/expired ctx before the in-memory match.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return e.FindIndex(text), nil
}

// flush removes all keywords from the automaton.
func (ac *redisBackedAC) flush(ctx context.Context) error {
	if err := flushV2Keys(ctx, ac.storage, ac.name); err != nil {
		return err
	}

	ac.mu.Lock()
	ac.keywordSet = make(map[string]struct{})
	ac.rebuildEngine()
	ac.stale = false
	ac.mu.Unlock()

	ac.publishInvalidate(ctx)
	return nil
}

// info returns statistics about the local automaton state.
func (ac *redisBackedAC) info(_ context.Context) (*AhoCorasickInfo, error) {
	ac.mu.RLock()
	mi := ac.engine.Info()
	ac.mu.RUnlock()
	return &AhoCorasickInfo{
		Keywords:    mi.Keywords,
		Nodes:       mi.Nodes,
		Preset:      presetFromEngine(mi.Preset),
		MemoryBytes: mi.MemoryBytes,
		TrieDepth:   mi.TrieDepth,
	}, nil
}

// suggest is unsupported in preset mode: the local automaton holds no prefix
// index, and preset mode deliberately keeps reads off Redis.
func (ac *redisBackedAC) suggest(_ context.Context, _ string) ([]string, error) {
	return nil, ErrSuggestRequiresRedis
}

func (ac *redisBackedAC) suggestIndex(_ context.Context, _ string) (map[string][]int, error) {
	return nil, ErrSuggestRequiresRedis
}
