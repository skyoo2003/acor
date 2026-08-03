// SPDX-License-Identifier: Apache-2.0

package acor

import "context"

// batchPlanner is implemented by modes that can apply a whole batch of keyword
// writes in a single Redis transaction instead of one per keyword.
//
// Both V2 write paths read the entire trie, replan it, and write it back on every
// single-keyword call, so a batch of N cost O(N^2): AddMany of 1000 keywords took
// ~1.05s against a real server. Planning the batch once makes it two round trips
// regardless of N.
//
// V1 does not implement it and keeps the per-keyword loop: its write path walks
// per trie node, with different economics, and it is the legacy schema.
type batchPlanner interface {
	// addManyAtomic inserts every keyword in one transaction and returns those
	// that were not already present. An error means nothing was written.
	addManyAtomic(ctx context.Context, keywords []string) ([]string, error)
	// removeManyAtomic deletes every keyword in one transaction and returns those
	// that were actually present. An error means nothing was written.
	removeManyAtomic(ctx context.Context, keywords []string) ([]string, error)
}

var (
	_ batchPlanner = (*redisBackedAC)(nil)
	_ batchPlanner = (*v2Operations)(nil)
)

// normalizeKeywords applies the same normalization the single-keyword paths do
// and drops entries that normalize away, so planning never sees an empty string.
func normalizeKeywords(keywords []string, caseSensitive bool) []string {
	out := make([]string, 0, len(keywords))
	for _, kw := range keywords {
		if n := normalizeKeyword(kw, caseSensitive); n != "" {
			out = append(out, n)
		}
	}
	return out
}

// --- preset mode ---

func (ac *redisBackedAC) addManyAtomic(ctx context.Context, keywords []string) ([]string, error) {
	normalized := normalizeKeywords(keywords, ac.caseSensitive)
	if len(normalized) == 0 {
		return nil, nil
	}

	var added []string
	_, err := retryOnConflict(ctx, func() (int, error) {
		snap, err := readTrieSnapshot(ctx, ac.storage, ac.name)
		if err != nil {
			return 0, err
		}
		outputs, newlyAdded := planAddMany(snap, normalized)
		if len(newlyAdded) == 0 {
			added = nil
			return 0, nil
		}
		newVersion, err := commitV2Write(ctx, ac.redisClient, ac.name, snap, outputs, false)
		if err != nil {
			// A lost CAS race retries from a fresh snapshot, so anything this
			// attempt staged must not leak into the next one.
			added = nil
			return 0, err
		}
		added = newlyAdded
		ac.applyCommittedBatch(snap, newVersion)
		return len(newlyAdded), nil
	})
	if err != nil {
		return nil, err
	}
	if len(added) > 0 {
		ac.publishInvalidate(ctx)
	}
	return added, nil
}

func (ac *redisBackedAC) removeManyAtomic(ctx context.Context, keywords []string) ([]string, error) {
	normalized := normalizeKeywords(keywords, ac.caseSensitive)
	if len(normalized) == 0 {
		return nil, nil
	}

	var removed []string
	_, err := retryOnConflict(ctx, func() (int, error) {
		snap, err := readTrieSnapshot(ctx, ac.storage, ac.name)
		if err != nil {
			return 0, err
		}
		outputs, gone := planRemoveMany(snap, normalized)
		if len(gone) == 0 {
			removed = nil
			return 0, nil
		}
		newVersion, err := commitV2Write(ctx, ac.redisClient, ac.name, snap, outputs, true)
		if err != nil {
			removed = nil
			return 0, err
		}
		removed = gone
		ac.applyCommittedBatch(snap, newVersion)
		return len(gone), nil
	})
	if err != nil {
		return nil, err
	}
	if len(removed) > 0 {
		ac.publishInvalidate(ctx)
	}
	return removed, nil
}

// applyCommittedBatch installs the batch's own committed snapshot as the local
// view, rebuilding the engine once for the whole batch.
//
// It rebuilds from snap rather than the incrementally maintained keywordSet for the
// reason commitBatch documents: that set can be missing a keyword another node
// wrote, so clearing stale entries against it would drop that write from local
// reads until the next invalidation. snap is authoritative instead — the CAS that
// just succeeded proves Redis was still at snap's version, and planAddMany /
// planRemoveMany already folded this batch into snap.Keywords.
func (ac *redisBackedAC) applyCommittedBatch(snap *trieSnapshot, newVersion int64) {
	ac.mu.Lock()
	ac.applyReload(snap)
	ac.localVersion = newVersion
	ac.mu.Unlock()
}

// --- V2 mode ---

func (o *v2Operations) addManyAtomic(ctx context.Context, keywords []string) ([]string, error) {
	normalized := normalizeKeywords(keywords, o.caseSensitive)
	if len(normalized) == 0 {
		return nil, nil
	}

	var added []string
	_, err := retryOnConflict(ctx, func() (int, error) {
		snap, err := readTrieSnapshot(ctx, o.storage, o.name)
		if err != nil {
			return 0, err
		}
		outputs, newlyAdded := planAddMany(snap, normalized)
		if len(newlyAdded) == 0 {
			added = nil
			return 0, nil
		}
		if _, err := commitV2Write(ctx, o.client, o.name, snap, outputs, false); err != nil {
			added = nil
			return 0, err
		}
		added = newlyAdded
		return len(newlyAdded), nil
	})
	if err != nil {
		return nil, err
	}
	if len(added) > 0 {
		o.publishInvalidate(ctx)
	}
	return added, nil
}

func (o *v2Operations) removeManyAtomic(ctx context.Context, keywords []string) ([]string, error) {
	normalized := normalizeKeywords(keywords, o.caseSensitive)
	if len(normalized) == 0 {
		return nil, nil
	}

	var removed []string
	_, err := retryOnConflict(ctx, func() (int, error) {
		snap, err := readTrieSnapshot(ctx, o.storage, o.name)
		if err != nil {
			return 0, err
		}
		outputs, gone := planRemoveMany(snap, normalized)
		if len(gone) == 0 {
			removed = nil
			return 0, nil
		}
		if _, err := commitV2Write(ctx, o.client, o.name, snap, outputs, true); err != nil {
			removed = nil
			return 0, err
		}
		removed = gone
		return len(gone), nil
	})
	if err != nil {
		return nil, err
	}
	if len(removed) > 0 {
		o.publishInvalidate(ctx)
	}
	return removed, nil
}
