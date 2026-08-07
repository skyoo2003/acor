// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"

	"github.com/redis/go-redis/v9"
)

// batchPlanner is implemented by modes that can apply a whole batch of keyword
// writes in a single Redis transaction instead of one per keyword.
//
// Both V2 write paths read the entire trie, replan it, and write it back on every
// single-keyword call, so a batch of N cost O(N^2): AddMany of 1000 keywords took
// ~1.05s against a real server. Planning the batch once makes it two round trips
// regardless of N.
//
// V1 does not implement it and keeps the per-keyword loop: its write path walks
// per trie node, with different economics, and it is the legacy schema. Callers
// pass keywords already screened and normalized.
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

// applyManyAtomic runs the shared V2 snapshot-plan-CAS loop. afterCommit is used
// by preset mode to install the committed snapshot in its local engine.
func applyManyAtomic(ctx context.Context, storage KVStorage, client redis.UniversalClient, name string,
	keywords []string, clearOutputs bool,
	plan func(*trieSnapshot, []string) (map[string][]string, []string),
	afterCommit func(*trieSnapshot, int64)) ([]string, error) {
	var applied []string
	_, err := retryOnConflict(ctx, func() (int, error) {
		snap, err := readTrieSnapshot(ctx, storage, name)
		if err != nil {
			return 0, err
		}
		outputs, changed := plan(snap, keywords)
		if len(changed) == 0 {
			applied = nil
			return 0, nil
		}
		newVersion, err := commitV2Write(ctx, client, name, snap, outputs, clearOutputs)
		if err != nil {
			// A lost CAS race retries from a fresh snapshot, so anything this
			// attempt staged must not leak into the next one.
			applied = nil
			return 0, err
		}
		applied = changed
		if afterCommit != nil {
			afterCommit(snap, newVersion)
		}
		return len(changed), nil
	})
	if err != nil {
		return nil, err
	}
	return applied, nil
}

// --- preset mode ---

func (ac *redisBackedAC) addManyAtomic(ctx context.Context, keywords []string) ([]string, error) {
	added, err := applyManyAtomic(ctx, ac.storage, ac.redisClient, ac.name, keywords,
		false, planAddMany, ac.applyCommittedWrite)
	if len(added) > 0 {
		ac.publishInvalidate(ctx)
	}
	return added, err
}

func (ac *redisBackedAC) removeManyAtomic(ctx context.Context, keywords []string) ([]string, error) {
	removed, err := applyManyAtomic(ctx, ac.storage, ac.redisClient, ac.name, keywords,
		true, planRemoveMany, ac.applyCommittedWrite)
	if len(removed) > 0 {
		ac.publishInvalidate(ctx)
	}
	return removed, err
}

// applyCommittedWrite installs a write's own committed snapshot as the local view,
// rebuilding the engine once. Every preset-mode write routes through it — single
// keyword and whole batch alike — so both leave the same local state.
//
// It rebuilds from snap rather than the incrementally maintained keywordSet: that
// set can be missing a keyword another node wrote, so rebuilding against it while
// clearing stale drops that write from local reads with nothing left to restore it
// (stale is false and the version now matches, so neither the listener nor the
// poller will re-fetch). snap is authoritative instead — the CAS that just
// succeeded proves Redis was still at snap's version, and the plan functions
// already folded this write into snap.Keywords.
func (ac *redisBackedAC) applyCommittedWrite(snap *trieSnapshot, newVersion int64) {
	ac.mu.Lock()
	ac.applyReload(snap)
	ac.localVersion = newVersion
	ac.mu.Unlock()
}

// --- V2 mode ---

func (o *v2Operations) addManyAtomic(ctx context.Context, keywords []string) ([]string, error) {
	added, err := applyManyAtomic(ctx, o.storage, o.client, o.name, keywords,
		false, planAddMany, nil)
	if len(added) > 0 {
		o.publishInvalidate(ctx)
	}
	return added, err
}

func (o *v2Operations) removeManyAtomic(ctx context.Context, keywords []string) ([]string, error) {
	removed, err := applyManyAtomic(ctx, o.storage, o.client, o.name, keywords,
		true, planRemoveMany, nil)
	if len(removed) > 0 {
		o.publishInvalidate(ctx)
	}
	return removed, err
}
