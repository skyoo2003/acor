// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"sort"
)

// AddContext inserts a keyword with context for cancellation and timeout propagation.
func (ac *AhoCorasick) AddContext(ctx context.Context, keyword string) (int, error) {
	return ac.ops.add(ctx, keyword)
}

// RemoveContext removes a keyword with context for cancellation and timeout propagation.
func (ac *AhoCorasick) RemoveContext(ctx context.Context, keyword string) (int, error) {
	return ac.ops.remove(ctx, keyword)
}

// FindContext searches for keyword matches with context for cancellation and timeout propagation.
func (ac *AhoCorasick) FindContext(ctx context.Context, text string) ([]string, error) {
	return ac.ops.find(ctx, text)
}

// FindIndexContext searches for keyword matches with indices with context.
func (ac *AhoCorasick) FindIndexContext(ctx context.Context, text string) (map[string][]int, error) {
	return ac.ops.findIndex(ctx, text)
}

// FlushContext removes all keywords with context for cancellation and timeout propagation.
func (ac *AhoCorasick) FlushContext(ctx context.Context) error {
	return ac.ops.flush(ctx)
}

// InfoContext returns automaton statistics with context for cancellation and timeout propagation.
func (ac *AhoCorasick) InfoContext(ctx context.Context) (*AhoCorasickInfo, error) {
	return ac.ops.info(ctx)
}

// SuggestContext returns keyword suggestions with context for cancellation and timeout propagation.
func (ac *AhoCorasick) SuggestContext(ctx context.Context, input string) ([]string, error) {
	return ac.ops.suggest(ctx, input)
}

// SuggestIndexContext returns keyword suggestions with indices with context.
func (ac *AhoCorasick) SuggestIndexContext(ctx context.Context, input string) (map[string][]int, error) {
	return ac.ops.suggestIndex(ctx, input)
}

// AddManyContext adds multiple keywords with context for cancellation and timeout propagation.
func (ac *AhoCorasick) AddManyContext(ctx context.Context, keywords []string, opts *BatchOptions) (*BatchResult, error) {
	if opts == nil {
		opts = &BatchOptions{Mode: BatchModeBestEffort}
	}

	result := &BatchResult{
		Added:   make([]string, 0),
		Removed: make([]string, 0),
		Failed:  make([]KeywordError, 0),
		Skipped: make([]string, 0),
	}

	if opts.Mode == BatchModeTransactional {
		return ac.addManyTransactional(ctx, keywords, result)
	}
	return ac.addManyBestEffort(ctx, keywords, result)
}

// RemoveManyContext removes multiple keywords with context for cancellation and timeout propagation.
func (ac *AhoCorasick) RemoveManyContext(ctx context.Context, keywords []string, opts *BatchOptions) (*BatchResult, error) {
	if opts == nil {
		opts = &BatchOptions{Mode: BatchModeBestEffort}
	}

	result := &BatchResult{
		Added:   make([]string, 0),
		Removed: make([]string, 0),
		Failed:  make([]KeywordError, 0),
		Skipped: make([]string, 0),
	}

	if opts.Mode == BatchModeTransactional {
		return ac.removeManyTransactional(ctx, keywords, result)
	}
	return ac.removeManyBestEffort(ctx, keywords, result)
}

// FindManyContext searches for keywords in multiple texts with context.
func (ac *AhoCorasick) FindManyContext(ctx context.Context, texts []string) (map[string][]string, error) {
	results := make(map[string][]string)

	for _, text := range texts {
		matches, err := ac.ops.find(ctx, text)
		if err != nil {
			return nil, err
		}
		results[text] = matches
	}

	return results, nil
}

// FindParallelContext searches for keywords using parallel processing with context.
func (ac *AhoCorasick) FindParallelContext(ctx context.Context, text string, opts *ParallelOptions) ([]string, error) {
	opts = normalizeParallelOptions(opts)
	if opts.ChunkSize <= 0 {
		return nil, ErrInvalidChunkSize
	}

	chunks := splitChunks(text, opts)
	if len(chunks) == 0 {
		return []string{}, nil
	}
	if len(chunks) == 1 {
		return ac.FindContext(ctx, text)
	}

	results, errors := runStringWorkersCtx(ctx, ac, chunks, opts.Workers)
	allMatches, err := collectOrderedStringResults(results, errors)

	if err != nil {
		return nil, err
	}
	return allMatches, nil
}

// FindIndexParallelContext searches for keywords with indices using parallel processing with context.
func (ac *AhoCorasick) FindIndexParallelContext(ctx context.Context, text string, opts *ParallelOptions) (map[string][]int, error) {
	opts = normalizeParallelOptions(opts)
	if opts.ChunkSize <= 0 {
		return nil, ErrInvalidChunkSize
	}

	chunks := splitChunks(text, opts)
	if len(chunks) == 0 {
		return map[string][]int{}, nil
	}
	if len(chunks) == 1 {
		return ac.FindIndexContext(ctx, text)
	}

	results, errors := runIndexWorkersCtx(ctx, ac, chunks, opts.Workers)
	allMatches, err := collectIndexResults(results, errors)
	if err != nil {
		return nil, err
	}

	result := make(map[string][]int)
	for keyword, indices := range allMatches {
		sortedIndices := make([]int, 0, len(indices))
		for idx := range indices {
			sortedIndices = append(sortedIndices, idx)
		}
		sort.Ints(sortedIndices)
		result[keyword] = sortedIndices
	}
	return result, nil
}
