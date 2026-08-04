// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"fmt"
	"strings"

	"golang.org/x/sync/errgroup"
)

// batchWriter is implemented by modes (preset) that can coalesce the local
// automaton rebuild and pub/sub invalidation across a batch of writes. When
// ac.ops satisfies it, AddMany/RemoveMany write each keyword with the rebuild
// deferred and then commit a single rebuild + publish, turning N per-keyword
// automaton rebuilds into one. Modes without a per-write local rebuild (V1, V2)
// do not implement it and fall back to the plain per-keyword path.
type batchWriter interface {
	addDeferred(ctx context.Context, keyword string) (int, error)
	removeDeferred(ctx context.Context, keyword string) (int, error)
	commitBatch(ctx context.Context)
}

// batchAddFns returns the per-keyword add function and a commit function for a
// batch. In a batchWriter mode (preset) the add is deferred and commit runs the
// single coalesced rebuild+publish; otherwise add is the plain per-keyword add
// (which already rebuilt+published) and commit is a no-op. Callers gate commit on
// whether anything actually changed, so no-op batches never trigger a rebuild.
func (ac *AhoCorasick) batchAddFns() (add func(context.Context, string) (int, error), commit func(context.Context)) {
	if bw, ok := ac.ops.(batchWriter); ok {
		return bw.addDeferred, bw.commitBatch
	}
	return ac.ops.add, func(context.Context) {}
}

// batchRemoveFns is batchAddFns for the remove side.
func (ac *AhoCorasick) batchRemoveFns() (remove func(context.Context, string) (int, error), commit func(context.Context)) {
	if bw, ok := ac.ops.(batchWriter); ok {
		return bw.removeDeferred, bw.commitBatch
	}
	return ac.ops.remove, func(context.Context) {}
}

// AddMany adds multiple keywords to the Aho-Corasick automaton in batch mode.
// This is more efficient than calling Add repeatedly for large keyword sets.
//
// The opts parameter controls error handling behavior:
//   - nil or BatchModeBestEffort: continues on errors, returns partial results
//   - BatchModeTransactional: rolls back on first error
//
// Duplicate keywords in the input are skipped and recorded in BatchResult.Skipped.
//
// Example:
//
//	result, err := ac.AddMany([]string{"foo", "bar", "baz"}, nil)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Printf("Added %d, Failed %d\n", len(result.Added), len(result.Failed))
func (ac *AhoCorasick) AddMany(keywords []string, opts *BatchOptions) (*BatchResult, error) {
	return ac.AddManyContext(ac.ctx, keywords, opts)
}

// screenBatch applies the checks that need no Redis access: blank keywords go to
// result.Failed, repeats within the same call are skipped, and the survivors are
// returned in input order alongside their normalized forms. Every batch path
// shares it, so blanks and duplicates behave the same whichever write path runs.
//
// Duplicates are detected on the normalized form, not the caller's spelling: on a
// case-insensitive collection "Foo" and "foo" are one keyword, so only one of them
// can be added. Screening on the raw spelling let both through, and partitionApplied
// then matched both against the single applied keyword and reported two additions
// for one write.
//
// ErrEmptyKeyword is the only error it records, so a transactional caller — which
// must abort the batch on a blank rather than record it — tests
// len(result.Failed) > 0 and discards result.
func (ac *AhoCorasick) screenBatch(keywords []string, result *BatchResult) (
	candidates, normalizedKeywords []string) {
	seen := make(map[string]bool, len(keywords))
	candidates = make([]string, 0, len(keywords))
	if !ac.caseSensitive {
		normalizedKeywords = make([]string, 0, len(keywords))
	}

	for _, keyword := range keywords {
		keyword = strings.TrimSpace(keyword)
		if keyword == "" {
			result.Failed = append(result.Failed, KeywordError{
				Keyword: keyword,
				Error:   ErrEmptyKeyword,
			})
			continue
		}
		normalizedKeyword := normalizeKeyword(keyword, ac.caseSensitive)
		if seen[normalizedKeyword] {
			result.Skipped = append(result.Skipped, keyword)
			continue
		}
		seen[normalizedKeyword] = true
		candidates = append(candidates, keyword)
		if !ac.caseSensitive {
			normalizedKeywords = append(normalizedKeywords, normalizedKeyword)
		}
	}

	if ac.caseSensitive {
		normalizedKeywords = candidates
	}
	return candidates, normalizedKeywords
}

// partitionApplied splits screened candidates by what the write actually
// changed. applied is what the write path reports as having taken effect;
// everything else was already in the desired state.
//
// The write path reports normalized keywords while candidates keep the caller's
// spelling, so membership is tested against the normalized slice computed during
// screening. Comparing raw against normalized reported every keyword with an
// uppercase rune as Skipped on a case-insensitive collection, even though it had
// just been written.
func partitionApplied(candidates, normalized, applied []string) (changed, unchanged []string) {
	appliedSet := make(map[string]struct{}, len(applied))
	for _, kw := range applied {
		appliedSet[kw] = struct{}{}
	}
	for i, kw := range candidates {
		if _, ok := appliedSet[normalized[i]]; ok {
			changed = append(changed, kw)
		} else {
			unchanged = append(unchanged, kw)
		}
	}
	return changed, unchanged
}

func (ac *AhoCorasick) addManyBestEffort(ctx context.Context, keywords []string, result *BatchResult) (*BatchResult, error) {
	candidates, normalized := ac.screenBatch(keywords, result)

	if bp, ok := ac.ops.(batchPlanner); ok {
		if len(candidates) == 0 {
			return result, nil
		}
		added, err := bp.addManyAtomic(ctx, normalized)
		if err != nil {
			// One transaction means one outcome: nothing was written, so every
			// candidate failed. A partial success cannot happen here.
			for _, keyword := range candidates {
				result.Failed = append(result.Failed, KeywordError{Keyword: keyword, Error: err})
			}
			return result, nil
		}
		changed, unchanged := partitionApplied(candidates, normalized, added)
		result.Added = append(result.Added, changed...)
		result.Skipped = append(result.Skipped, unchanged...)
		return result, nil
	}

	add, commit := ac.batchAddFns()
	for _, keyword := range candidates {
		count, err := add(ctx, keyword)
		if err != nil {
			result.Failed = append(result.Failed, KeywordError{
				Keyword: keyword,
				Error:   err,
			})
			continue
		}

		if count == 0 {
			result.Skipped = append(result.Skipped, keyword)
		} else {
			result.Added = append(result.Added, keyword)
		}
	}

	if len(result.Added) > 0 {
		commit(ctx)
	}

	return result, nil
}

func (ac *AhoCorasick) addManyTransactional(ctx context.Context, keywords []string, result *BatchResult) (*BatchResult, error) {
	if bp, ok := ac.ops.(batchPlanner); ok {
		candidates, normalized := ac.screenBatch(keywords, result)
		if len(result.Failed) > 0 {
			// Transactional means all-or-nothing, so a blank keyword aborts the
			// batch instead of being recorded alongside successful writes.
			return nil, ErrEmptyKeyword
		}
		if len(candidates) == 0 {
			result.Added = []string{}
			return result, nil
		}
		// A single CAS write is already all-or-nothing, so there is nothing to
		// roll back: either the whole batch committed or none of it did.
		applied, err := bp.addManyAtomic(ctx, normalized)
		if err != nil {
			return nil, fmt.Errorf("batch add failed: %w", err)
		}
		changed, unchanged := partitionApplied(candidates, normalized, applied)
		result.Added = append(result.Added, changed...)
		result.Skipped = append(result.Skipped, unchanged...)
		return result, nil
	}

	added := make([]string, 0)
	seen := make(map[string]bool)
	add, commit := ac.batchAddFns()

	rollbackCtx := context.WithoutCancel(ctx)
	for _, keyword := range keywords {
		keyword = strings.TrimSpace(keyword)
		if keyword == "" {
			ac.rollbackAdded(rollbackCtx, added)
			return nil, ErrEmptyKeyword
		}

		if seen[keyword] {
			result.Skipped = append(result.Skipped, keyword)
			continue
		}
		seen[keyword] = true

		count, err := add(ctx, keyword)
		if err != nil {
			// rollbackAdded also repairs the deferred, not-yet-committed local
			// engine (it ends with its own commit) before returning.
			ac.rollbackAdded(rollbackCtx, added)
			return nil, fmt.Errorf("batch add failed at keyword %q: %w", keyword, err)
		}

		if count > 0 {
			added = append(added, keyword)
		} else {
			result.Skipped = append(result.Skipped, keyword)
		}
	}

	if len(added) > 0 {
		commit(ctx)
	}

	result.Added = added
	return result, nil
}

// rollbackAdded undoes the adds a failed transactional batch already committed.
func (ac *AhoCorasick) rollbackAdded(ctx context.Context, keywords []string) {
	remove, commit := ac.batchRemoveFns()
	ac.rollbackBatch(ctx, keywords, "remove", remove, commit)
}

// rollbackRemoved re-adds the removes a failed transactional batch already committed.
func (ac *AhoCorasick) rollbackRemoved(ctx context.Context, keywords []string) {
	add, commit := ac.batchAddFns()
	ac.rollbackBatch(ctx, keywords, "re-add", add, commit)
}

// rollbackBatchWorkers caps how many undo operations run at once. Rollback is
// off the hot path and hits Redis per keyword, so a small fixed pool is enough.
const rollbackBatchWorkers = 10

// rollbackBatch applies undo to every keyword concurrently and then commits once.
// It uses the deferred write plus a single commit so a failed batch triggers one
// rebuild and publish rather than one per keyword (commit is a no-op outside
// batchWriter modes, where each write already rebuilt and published).
//
// Errors are logged, not returned: this is the failure path already, and the
// caller is about to return the original error. ctx is not checked for
// cancellation either: callers pass context.WithoutCancel so the undo still runs
// when the batch failed because the caller's context went away.
func (ac *AhoCorasick) rollbackBatch(ctx context.Context, keywords []string, op string,
	undo func(context.Context, string) (int, error), commit func(context.Context)) {
	if len(keywords) == 0 {
		return
	}

	var g errgroup.Group
	g.SetLimit(rollbackBatchWorkers)
	for _, keyword := range keywords {
		g.Go(func() error {
			if _, err := undo(ctx, keyword); err != nil && ac.logger != nil {
				ac.logger.Printf("rollback: failed to %s %q: %v", op, keyword, err)
			}
			return nil
		})
	}
	_ = g.Wait()
	commit(ctx)
}

// RemoveMany removes multiple keywords from the Aho-Corasick automaton.
// This is more efficient than calling Remove repeatedly for large keyword sets.
// Uses best-effort mode by default. Use RemoveManyWithOptions for batch mode control.
//
// Example:
//
//	result, err := ac.RemoveMany([]string{"foo", "bar"})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Printf("Removed %d keywords\n", len(result.Removed))
func (ac *AhoCorasick) RemoveMany(keywords []string) (*BatchResult, error) {
	return ac.RemoveManyContext(ac.ctx, keywords, nil)
}

// RemoveManyWithOptions removes multiple keywords with batch options.
func (ac *AhoCorasick) RemoveManyWithOptions(keywords []string, opts *BatchOptions) (*BatchResult, error) {
	return ac.RemoveManyContext(ac.ctx, keywords, opts)
}

func (ac *AhoCorasick) removeManyBestEffort(ctx context.Context, keywords []string, result *BatchResult) (*BatchResult, error) {
	candidates, normalized := ac.screenBatch(keywords, result)

	if bp, ok := ac.ops.(batchPlanner); ok {
		if len(candidates) == 0 {
			return result, nil
		}
		removed, err := bp.removeManyAtomic(ctx, normalized)
		if err != nil {
			for _, keyword := range candidates {
				result.Failed = append(result.Failed, KeywordError{Keyword: keyword, Error: err})
			}
			return result, nil
		}
		changed, unchanged := partitionApplied(candidates, normalized, removed)
		result.Removed = append(result.Removed, changed...)
		result.Skipped = append(result.Skipped, unchanged...)
		return result, nil
	}

	remove, commit := ac.batchRemoveFns()
	for _, keyword := range candidates {
		count, err := remove(ctx, keyword)
		if err != nil {
			result.Failed = append(result.Failed, KeywordError{
				Keyword: keyword,
				Error:   err,
			})
			continue
		}

		// Only count as removed when a keyword was actually present (count > 0),
		// matching AddMany and single Remove. This keeps a no-op RemoveMany from
		// reporting phantom removals and from firing a needless cluster-wide commit.
		if count > 0 {
			result.Removed = append(result.Removed, keyword)
		} else {
			result.Skipped = append(result.Skipped, keyword)
		}
	}

	if len(result.Removed) > 0 {
		commit(ctx)
	}

	return result, nil
}

func (ac *AhoCorasick) removeManyTransactional(ctx context.Context, keywords []string, result *BatchResult) (*BatchResult, error) {
	if bp, ok := ac.ops.(batchPlanner); ok {
		candidates, normalized := ac.screenBatch(keywords, result)
		if len(result.Failed) > 0 {
			// See addManyTransactional: a blank aborts rather than being recorded.
			return nil, ErrEmptyKeyword
		}
		if len(candidates) == 0 {
			result.Removed = []string{}
			return result, nil
		}
		// As in addManyTransactional: one CAS write is already atomic.
		applied, err := bp.removeManyAtomic(ctx, normalized)
		if err != nil {
			return nil, fmt.Errorf("batch remove failed: %w", err)
		}
		changed, unchanged := partitionApplied(candidates, normalized, applied)
		result.Removed = append(result.Removed, changed...)
		result.Skipped = append(result.Skipped, unchanged...)
		return result, nil
	}

	removed := make([]string, 0)
	seen := make(map[string]bool)
	remove, commit := ac.batchRemoveFns()

	rollbackCtx := context.WithoutCancel(ctx)
	for _, keyword := range keywords {
		keyword = strings.TrimSpace(keyword)
		if keyword == "" {
			ac.rollbackRemoved(rollbackCtx, removed)
			return nil, ErrEmptyKeyword
		}

		if seen[keyword] {
			result.Skipped = append(result.Skipped, keyword)
			continue
		}
		seen[keyword] = true

		count, err := remove(ctx, keyword)
		if err != nil {
			// rollbackRemoved re-adds the actually-removed keywords and ends with its
			// own commit, repairing the deferred local engine before returning.
			ac.rollbackRemoved(rollbackCtx, removed)
			return nil, fmt.Errorf("batch remove failed at keyword %q: %w", keyword, err)
		}

		// Track only keywords that were present, so rollback re-adds exactly those
		// and a no-op transactional RemoveMany does not fire a commit.
		if count > 0 {
			removed = append(removed, keyword)
		} else {
			result.Skipped = append(result.Skipped, keyword)
		}
	}

	if len(removed) > 0 {
		commit(ctx)
	}

	result.Removed = removed
	return result, nil
}

// FindMany searches for keywords in multiple texts and returns a map of text to matches.
// This is convenient when you need to match against many texts at once.
//
// Note: If the same text appears multiple times in the input slice, only one result
// entry will be stored (last occurrence wins). For large batches, consider using
// parallel processing with individual FindParallel calls.
//
// Example:
//
//	results, err := ac.FindMany([]string{"hello world", "goodbye world"})
//	// results["hello world"] contains matches in that text
func (ac *AhoCorasick) FindMany(texts []string) (map[string][]string, error) {
	return ac.FindManyContext(ac.ctx, texts)
}
