// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"fmt"
	"strings"
	"sync"
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

func (ac *AhoCorasick) addManyBestEffort(ctx context.Context, keywords []string, result *BatchResult) (*BatchResult, error) {
	seen := make(map[string]bool)

	bw, batched := ac.ops.(batchWriter)
	add := ac.ops.add
	if batched {
		add = bw.addDeferred
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

		if seen[keyword] {
			result.Skipped = append(result.Skipped, keyword)
			continue
		}
		seen[keyword] = true

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

	if batched && len(result.Added) > 0 {
		bw.commitBatch(ctx)
	}

	return result, nil
}

func (ac *AhoCorasick) addManyTransactional(ctx context.Context, keywords []string, result *BatchResult) (*BatchResult, error) {
	added := make([]string, 0)
	seen := make(map[string]bool)

	bw, batched := ac.ops.(batchWriter)
	add := ac.ops.add
	if batched {
		add = bw.addDeferred
	}

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
			// rollbackAdded uses regular (rebuilding) removes, so it also repairs
			// the deferred, not-yet-rebuilt local engine before returning.
			ac.rollbackAdded(rollbackCtx, added)
			return nil, fmt.Errorf("batch add failed at keyword %q: %w", keyword, err)
		}

		if count > 0 {
			added = append(added, keyword)
		} else {
			result.Skipped = append(result.Skipped, keyword)
		}
	}

	if batched && len(added) > 0 {
		bw.commitBatch(ctx)
	}

	result.Added = added
	return result, nil
}

func (ac *AhoCorasick) rollbackAdded(ctx context.Context, keywords []string) {
	if len(keywords) == 0 {
		return
	}

	maxWorkers := 10
	if len(keywords) < maxWorkers {
		maxWorkers = len(keywords)
	}

	sem := make(chan struct{}, maxWorkers)
	var wg sync.WaitGroup

	for _, keyword := range keywords {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			wg.Wait()
			return
		}
		wg.Add(1)
		go func(k string) {
			defer func() {
				<-sem
				wg.Done()
			}()
			if _, err := ac.ops.remove(ctx, k); err != nil && ac.logger != nil {
				ac.logger.Printf("rollback: failed to remove %q: %v", k, err)
			}
		}(keyword)
	}
	wg.Wait()
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
	seen := make(map[string]bool)

	bw, batched := ac.ops.(batchWriter)
	remove := ac.ops.remove
	if batched {
		remove = bw.removeDeferred
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

		if seen[keyword] {
			result.Skipped = append(result.Skipped, keyword)
			continue
		}
		seen[keyword] = true

		_, err := remove(ctx, keyword)
		if err != nil {
			result.Failed = append(result.Failed, KeywordError{
				Keyword: keyword,
				Error:   err,
			})
			continue
		}

		result.Removed = append(result.Removed, keyword)
	}

	if batched && len(result.Removed) > 0 {
		bw.commitBatch(ctx)
	}

	return result, nil
}

func (ac *AhoCorasick) removeManyTransactional(ctx context.Context, keywords []string, result *BatchResult) (*BatchResult, error) {
	removed := make([]string, 0)
	seen := make(map[string]bool)

	bw, batched := ac.ops.(batchWriter)
	remove := ac.ops.remove
	if batched {
		remove = bw.removeDeferred
	}

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

		_, err := remove(ctx, keyword)
		if err != nil {
			// rollbackRemoved re-adds via regular (rebuilding) adds, repairing the
			// deferred local engine before returning.
			ac.rollbackRemoved(rollbackCtx, removed)
			return nil, fmt.Errorf("batch remove failed at keyword %q: %w", keyword, err)
		}

		removed = append(removed, keyword)
	}

	if batched && len(removed) > 0 {
		bw.commitBatch(ctx)
	}

	result.Removed = removed
	return result, nil
}

func (ac *AhoCorasick) rollbackRemoved(ctx context.Context, keywords []string) {
	if len(keywords) == 0 {
		return
	}

	maxWorkers := 10
	if len(keywords) < maxWorkers {
		maxWorkers = len(keywords)
	}

	sem := make(chan struct{}, maxWorkers)
	var wg sync.WaitGroup

	for _, keyword := range keywords {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			wg.Wait()
			return
		}
		wg.Add(1)
		go func(k string) {
			defer func() {
				<-sem
				wg.Done()
			}()
			if _, err := ac.ops.add(ctx, k); err != nil && ac.logger != nil {
				ac.logger.Printf("rollback: failed to re-add %q: %v", k, err)
			}
		}(keyword)
	}
	wg.Wait()
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
