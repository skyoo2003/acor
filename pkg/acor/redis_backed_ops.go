// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
)

// redisBackedAC implements the operations interface directly, so AhoCorasick
// dispatches into it with no adapter in between. Suggest/SuggestIndex are the
// only operations preset mode cannot serve (there is no prefix index locally).
var (
	_ operations  = (*redisBackedAC)(nil)
	_ batchWriter = (*redisBackedAC)(nil)
)

// add inserts a keyword into the automaton. The keyword is written atomically
// to Redis via a V2 Lua script (optimistic locking), then the local automaton
// is rebuilt and an invalidation is published.
func (ac *redisBackedAC) add(ctx context.Context, keyword string) (int, error) {
	return ac.addWith(ctx, keyword, true)
}

// addDeferred writes a keyword but skips the local rebuild and pub/sub publish;
// commitBatch performs both once for the whole batch. This turns AddMany's N
// per-keyword automaton rebuilds into a single rebuild.
func (ac *redisBackedAC) addDeferred(ctx context.Context, keyword string) (int, error) {
	return ac.addWith(ctx, keyword, false)
}

// addWith is the shared Add path. rebuild=true rebuilds the local engine and
// publishes on success (single Add); rebuild=false defers both (batch writes).
func (ac *redisBackedAC) addWith(ctx context.Context, keyword string, rebuild bool) (int, error) {
	keyword = normalizeKeyword(keyword, ac.caseSensitive)
	if keyword == "" {
		return 0, nil
	}

	added, err := retryOnConflict(ctx, func() (int, error) { return ac.tryAdd(ctx, keyword, rebuild) })
	if err == nil && added == 1 && rebuild {
		ac.publishInvalidate(ctx)
	}
	return added, err
}

func (ac *redisBackedAC) tryAdd(ctx context.Context, keyword string, rebuild bool) (int, error) {
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

	ac.applyLocalWrite(keyword, true, newVersion, rebuild)
	return 1, nil
}

// applyLocalWrite applies a committed Redis write to the local state under lock.
// add selects insertion vs deletion of keyword in the keyword set. rebuild=true
// rebuilds the engine now and clears the stale flag; rebuild=false marks the
// engine stale so a concurrent Find reloads from Redis until commitBatch rebuilds.
func (ac *redisBackedAC) applyLocalWrite(keyword string, add bool, newVersion int64, rebuild bool) {
	ac.mu.Lock()
	if add {
		ac.keywordSet[keyword] = struct{}{}
	} else {
		delete(ac.keywordSet, keyword)
	}
	ac.localVersion = newVersion
	if rebuild {
		ac.engine = buildEngine(ac.preset, ac.keywordSet)
		ac.stale = false
	} else {
		ac.stale = true
	}
	ac.mu.Unlock()
}

// commitBatch refreshes the local engine once and publishes a single
// invalidation, collapsing the rebuild/publish that addDeferred/removeDeferred
// skipped during a batch.
//
// It reloads from Redis (the source of truth) rather than rebuilding from the
// local keyword set: that set is only incrementally maintained, so it can miss a
// keyword another node wrote concurrently, and a remote invalidation received
// during the batch must not be silently cleared by setting stale=false against a
// stale local view. On reload failure the deferred writes left the engine stale,
// so the next Find retries the reload.
func (ac *redisBackedAC) commitBatch(ctx context.Context) {
	if err := ac.reloadFromRedis(ctx); err != nil {
		ac.markStale()
	}
	ac.publishInvalidate(ctx)
}

// remove deletes a keyword from the automaton.
func (ac *redisBackedAC) remove(ctx context.Context, keyword string) (int, error) {
	return ac.removeWith(ctx, keyword, true)
}

// removeDeferred is Remove with the local rebuild and publish deferred to
// commitBatch; see addDeferred.
func (ac *redisBackedAC) removeDeferred(ctx context.Context, keyword string) (int, error) {
	return ac.removeWith(ctx, keyword, false)
}

func (ac *redisBackedAC) removeWith(ctx context.Context, keyword string, rebuild bool) (int, error) {
	keyword = normalizeKeyword(keyword, ac.caseSensitive)
	if keyword == "" {
		return 0, nil
	}

	removed, err := retryOnConflict(ctx, func() (int, error) { return ac.tryRemove(ctx, keyword, rebuild) })
	if err == nil && removed == 1 && rebuild {
		ac.publishInvalidate(ctx)
	}
	return removed, err
}

func (ac *redisBackedAC) tryRemove(ctx context.Context, keyword string, rebuild bool) (int, error) {
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

	ac.applyLocalWrite(keyword, false, newVersion, rebuild)
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
	return e.FindIndex(text), nil
}

// flush removes all keywords from the automaton.
func (ac *redisBackedAC) flush(ctx context.Context) error {
	if err := flushV2Keys(ctx, ac.storage, ac.name); err != nil {
		return err
	}

	ac.mu.Lock()
	ac.keywordSet = make(map[string]struct{})
	ac.engine = buildEngine(ac.preset, ac.keywordSet)
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
		Preset:      mi.Preset,
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
