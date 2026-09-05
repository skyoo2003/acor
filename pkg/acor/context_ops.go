// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"

	matchengine "github.com/skyoo2003/acor/internal/engine"
)

// AddContext inserts a keyword with context for cancellation and timeout
// propagation. Return values and the empty-keyword case are as documented on Add.
func (ac *AhoCorasick) AddContext(ctx context.Context, keyword string) (int, error) {
	return ac.ops.add(ctx, keyword)
}

// RemoveContext removes a keyword with context for cancellation and timeout
// propagation. Return values are as documented on Remove.
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

// FlushContext removes all keywords. On V2 and in Preset mode ctx carries
// cancellation and timeout through to Redis.
//
// On a V1 collection ctx is accepted and ignored. A V1 flush deletes a key per
// keyword and per output state, and abandoning that partway would leave a trie
// that is neither the old one nor empty, so it runs on a fresh context bounded
// by AhoCorasickArgs.RollbackTimeout instead. Passing an already-canceled ctx
// flushes the collection anyway and returns nil. Size RollbackTimeout, not ctx,
// to bound a V1 flush.
func (ac *AhoCorasick) FlushContext(ctx context.Context) error {
	return ac.ops.flush(ctx)
}

// InfoContext returns automaton statistics. On V1 and V2 it reads Redis under
// ctx. In Preset mode it reads the local engine, so there is nothing to cancel
// and ctx is ignored — a canceled one still returns the statistics.
func (ac *AhoCorasick) InfoContext(ctx context.Context) (*AhoCorasickInfo, error) {
	return ac.ops.info(ctx)
}

// SuggestContext returns keyword suggestions with context for cancellation and
// timeout propagation. Preset mode returns ErrSuggestRequiresRedis: the local
// automaton carries no prefix index, and preset mode keeps reads off Redis
// rather than falling back to it behind the caller's back.
func (ac *AhoCorasick) SuggestContext(ctx context.Context, input string) ([]string, error) {
	return ac.ops.suggest(ctx, input)
}

// SuggestIndexContext returns keyword suggestions with indices with context.
// Preset mode returns ErrSuggestRequiresRedis, as SuggestContext does.
func (ac *AhoCorasick) SuggestIndexContext(ctx context.Context, input string) (map[string][]int, error) {
	return ac.ops.suggestIndex(ctx, input)
}

// AddManyContext adds multiple keywords with context for cancellation and
// timeout propagation. Modes, duplicate handling, and what a transactional
// failure returns are as documented on AddMany.
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

// RemoveManyContext removes multiple keywords with context for cancellation and
// timeout propagation. Modes and duplicate handling are as documented on
// RemoveMany.
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

// FindManyContext searches for keywords in multiple texts with context. It
// scans them one at a time in the order given and stops at the first error,
// returning a nil map — the texts already scanned are discarded rather than
// returned as a partial result. See FindMany for the map's shape.
func (ac *AhoCorasick) FindManyContext(ctx context.Context, texts []string) (map[string][]string, error) {
	results := make(map[string][]string, len(texts))

	// One engine for the whole batch. Calling ops.find per text reloaded it every
	// time, so N texts cost N round trips where a single Find costs one — the same
	// defect the parallel paths below had (#205).
	//
	// Loaded on the first text that actually needs it, not up front: a batch that is
	// empty or all-empty-strings has never touched Redis, and loading an engine to
	// scan nothing with would newly cost a round trip (see FindParallelContext).
	var eng *matchengine.Engine

	for _, text := range texts {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if text == "" {
			results[text] = []string{}
			continue
		}
		if eng == nil {
			loaded, err := ac.ops.loadEngine(ctx)
			if err != nil {
				return nil, err
			}
			eng = loaded
		}
		results[text] = eng.Find(normalizeText(text, ac.caseSensitive))
	}

	return results, nil
}

// FindParallelContext searches for keywords using parallel processing with context.
// Like FindParallel, the result is a set: each keyword appears at most once.
func (ac *AhoCorasick) FindParallelContext(ctx context.Context, text string, opts *ParallelOptions) ([]string, error) {
	opts = normalizeParallelOptions(opts)
	if opts.ChunkSize <= 0 {
		return nil, ErrInvalidChunkSize
	}
	// Before loadEngine: an empty text has never touched Redis, and loading an
	// engine to scan nothing with would newly cost a round trip.
	if text == "" {
		return []string{}, nil
	}

	// One engine for the whole call, not one per chunk. Each ops.find reloaded it,
	// so an N-chunk text cost N round trips where serial Find costs one at any
	// input size — the fixed read cost V2 is built around (#205). Loading once also
	// gives every chunk the same dictionary snapshot, which per-chunk loads did not
	// guarantee.
	eng, err := ac.ops.loadEngine(ctx)
	if err != nil {
		return nil, err
	}

	var chunks []chunk
	if opts.AutoOverlap {
		chunks = splitAutoChunks(text, opts, eng.MaxKeywordRunes())
	} else {
		chunks = splitChunks(text, opts)
	}

	perChunk, err := scanChunks(ctx, chunks, opts.Workers, func(ctx context.Context, c chunk) ([]string, error) {
		// Per chunk, not once above: the in-memory scan is not ctx-threaded, and in
		// Preset mode loadEngine touches no Redis, so this is the only place a
		// canceled context is noticed. Checking here also stops chunks that have not
		// started yet, which the old per-chunk ops.find did.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		// FindSet, not Find: the result of this call is a set, so every duplicate
		// occurrence Find reports is built only to be dropped by
		// dedupPreservingOrder below. On match-dense text that per-occurrence slice
		// is most of the scan's allocation, and it is accumulated across every chunk
		// before the dedup runs.
		if opts.AutoOverlap {
			matches := make([]string, 0)
			seen := make(map[string]struct{})
			eng.MatchString(normalizeText(c.text, ac.caseSensitive), func(keyword string, start, _ int) bool {
				if start < c.ownedRunes {
					if _, exists := seen[keyword]; !exists {
						seen[keyword] = struct{}{}
						matches = append(matches, keyword)
					}
				}
				return true
			})
			return matches, nil
		}
		return eng.FindSet(normalizeText(c.text, ac.caseSensitive)), nil
	})
	if err != nil {
		return nil, err
	}

	var all []string
	for _, matches := range perChunk {
		all = append(all, matches...)
	}
	return dedupPreservingOrder(all), nil
}

// FindIndexParallelContext searches for keywords with indices using parallel processing with context.
func (ac *AhoCorasick) FindIndexParallelContext(ctx context.Context, text string, opts *ParallelOptions) (map[string][]int, error) {
	opts = normalizeParallelOptions(opts)
	if opts.ChunkSize <= 0 {
		return nil, ErrInvalidChunkSize
	}
	// See FindParallelContext: empty text stays off Redis.
	if text == "" {
		return map[string][]int{}, nil
	}

	// One engine for the whole call; see FindParallelContext.
	eng, err := ac.ops.loadEngine(ctx)
	if err != nil {
		return nil, err
	}

	var chunks []chunk
	if opts.AutoOverlap {
		chunks = splitAutoChunks(text, opts, eng.MaxKeywordRunes())
	} else {
		chunks = splitChunks(text, opts)
	}

	perChunk, err := scanChunks(ctx, chunks, opts.Workers, func(ctx context.Context, c chunk) (map[string][]int, error) {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		if opts.AutoOverlap {
			matches := make(map[string][]int)
			eng.MatchString(normalizeText(c.text, ac.caseSensitive), func(keyword string, start, _ int) bool {
				if start < c.ownedRunes {
					matches[keyword] = append(matches[keyword], start)
				}
				return true
			})
			return matches, nil
		}
		return eng.FindIndex(normalizeText(c.text, ac.caseSensitive)), nil
	})
	if err != nil {
		return nil, err
	}
	return mergeIndexResults(chunks, perChunk), nil
}
