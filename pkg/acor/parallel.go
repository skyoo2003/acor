// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"runtime"
	"sort"
	"sync"
	"unicode"
)

type chunk struct {
	text       string
	textOffset int
}

func splitChunks(text string, opts *ParallelOptions) []chunk {
	if opts == nil {
		opts = DefaultParallelOptions()
	}

	runes := []rune(text)
	if len(runes) <= opts.ChunkSize {
		return []chunk{{text: text, textOffset: 0}}
	}

	chunks := make([]chunk, 0)
	start := 0

	for start < len(runes) {
		end := start + opts.ChunkSize
		if end >= len(runes) {
			chunks = append(chunks, chunk{
				text:       string(runes[start:]),
				textOffset: start,
			})
			break
		}

		boundary := findBoundary(runes, end, opts.Boundary, opts.ChunkSize/defaultMaxBacktrackDivisor)
		if boundary <= start {
			boundary = end
		}

		chunkText := string(runes[start:boundary])
		chunks = append(chunks, chunk{
			text:       chunkText,
			textOffset: start,
		})

		nextStart := boundary - opts.Overlap
		if nextStart <= start {
			nextStart = boundary
		}
		start = nextStart
	}

	return chunks
}

func findBoundary(runes []rune, target int, boundaryType ChunkBoundary, maxBacktrack int) int {
	for i := target; i > target-maxBacktrack && i > 0; i-- {
		if isBoundary(runes, i, boundaryType) {
			return i
		}
	}
	return target
}

func isBoundary(runes []rune, idx int, boundaryType ChunkBoundary) bool {
	if idx <= 0 || idx >= len(runes) {
		return false
	}

	switch boundaryType {
	case ChunkBoundaryWord:
		return unicode.IsSpace(runes[idx]) && !unicode.IsSpace(runes[idx-1])
	case ChunkBoundaryLine:
		return runes[idx-1] == '\n'
	case ChunkBoundarySentence:
		return (runes[idx-1] == '.' || runes[idx-1] == '!' || runes[idx-1] == '?') &&
			unicode.IsSpace(runes[idx])
	}
	return false
}

func normalizeParallelOptions(opts *ParallelOptions) *ParallelOptions {
	if opts == nil {
		return DefaultParallelOptions()
	}
	if opts.Workers <= 0 {
		opts.Workers = runtime.NumCPU()
	}
	if opts.Overlap < 0 {
		// A negative overlap would push the next chunk's start past the current
		// boundary, dropping the runes in between and silently losing any match
		// there. Clamp to 0 (no overlap) rather than error, matching how Workers
		// is corrected above.
		opts.Overlap = 0
	}
	return opts
}

// dedupPreservingOrder returns in with duplicates removed, keeping first-seen order.
func dedupPreservingOrder(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}
	return out
}

//nolint:gocritic // Returns channels for concurrent result collection
func runStringWorkers(ac *AhoCorasick, chunks []chunk, workers int) (<-chan indexedStringResult, <-chan error) {
	return runStringWorkersCtx(ac.ctx, ac, chunks, workers)
}

//nolint:gocritic // Returns channels for concurrent result collection
func runStringWorkersCtx(ctx context.Context, ac *AhoCorasick, chunks []chunk, workers int) (<-chan indexedStringResult, <-chan error) {
	results := make(chan indexedStringResult, len(chunks))
	errors := make(chan error, len(chunks))

	var wg sync.WaitGroup
	sem := make(chan struct{}, workers)

	for i, c := range chunks {
		select {
		case <-ctx.Done():
			wg.Wait()
			close(results)
			close(errors)
			return results, errors
		default:
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, c chunk) {
			defer func() { <-sem }()
			defer wg.Done()
			matches, err := ac.ops.find(ctx, c.text)
			if err != nil {
				errors <- err
				return
			}
			results <- indexedStringResult{matches: matches, chunkIndex: i}
		}(i, c)
	}

	go func() {
		wg.Wait()
		close(results)
		close(errors)
	}()

	return results, errors
}

// collectOrderedStringResults collects matches preserving chunk order and
// deduplicating across chunks (a keyword appears at most once), matching the
// FindParallel contract.
func collectOrderedStringResults(results <-chan indexedStringResult, errors <-chan error) ([]string, error) {
	var allChunkResults []indexedStringResult
	var firstErr error

	resultsOpen, errorsOpen := true, true

	for resultsOpen || errorsOpen {
		select {
		case err, ok := <-errors:
			if !ok {
				errorsOpen = false
				continue
			}
			if firstErr == nil {
				firstErr = err
			}
		case res, ok := <-results:
			if !ok {
				resultsOpen = false
				continue
			}
			allChunkResults = append(allChunkResults, res)
		}
	}

	sort.Slice(allChunkResults, func(i, j int) bool {
		return allChunkResults[i].chunkIndex < allChunkResults[j].chunkIndex
	})

	var allMatches []string
	for _, r := range allChunkResults {
		allMatches = append(allMatches, r.matches...)
	}

	return dedupPreservingOrder(allMatches), firstErr
}

// FindParallel searches for keywords in text using parallel processing.
// The text is split into chunks processed by multiple goroutines, which can
// significantly improve performance for very large texts.
//
// If opts is nil, DefaultParallelOptions() is used. For small texts that fit
// within a single chunk, this method delegates to Find without parallelization.
//
// Note: Due to chunk overlap for boundary handling, duplicate matches are
// automatically deduplicated in the returned slice, so each keyword appears at
// most once regardless of how many times or in how many chunks it occurs. (Find
// reports every occurrence; FindParallel reports a set.)
//
// Limitation: a keyword longer than opts.Overlap that straddles a chunk boundary
// can be missed, since it fits in no single chunk. Set Overlap to at least your
// longest expected keyword length, or use FindStream (which never splits a match)
// when this matters.
//
// Example:
//
//	opts := &acor.ParallelOptions{
//	    Workers:   8,
//	    ChunkSize: 5000,
//	    Boundary:  acor.ChunkBoundaryLine,
//	}
//	matches, err := ac.FindParallel(largeLogFile, opts)
func (ac *AhoCorasick) FindParallel(text string, opts *ParallelOptions) ([]string, error) {
	opts = normalizeParallelOptions(opts)
	if opts.ChunkSize <= 0 {
		return nil, ErrInvalidChunkSize
	}

	chunks := splitChunks(text, opts)
	if len(chunks) == 0 {
		return []string{}, nil
	}
	if len(chunks) == 1 {
		// Match the multi-chunk contract: dedup so the result set doesn't depend
		// on whether the text fit in one chunk.
		matches, err := ac.Find(text)
		if err != nil {
			return nil, err
		}
		return dedupPreservingOrder(matches), nil
	}

	results, errors := runStringWorkers(ac, chunks, opts.Workers)
	allMatches, err := collectOrderedStringResults(results, errors)
	if err != nil {
		return nil, err
	}
	return allMatches, nil
}

type indexedResult struct {
	matches map[string][]int
	offset  int
}

type indexedStringResult struct {
	matches    []string
	chunkIndex int
}

//nolint:gocritic // Returns channels for concurrent result collection
func runIndexWorkers(ac *AhoCorasick, chunks []chunk, workers int) (<-chan indexedResult, <-chan error) {
	return runIndexWorkersCtx(ac.ctx, ac, chunks, workers)
}

//nolint:gocritic // Returns channels for concurrent result collection
func runIndexWorkersCtx(ctx context.Context, ac *AhoCorasick, chunks []chunk, workers int) (<-chan indexedResult, <-chan error) {
	results := make(chan indexedResult, len(chunks))
	errors := make(chan error, len(chunks))

	var wg sync.WaitGroup
	sem := make(chan struct{}, workers)

	for _, c := range chunks {
		select {
		case <-ctx.Done():
			wg.Wait()
			close(results)
			close(errors)
			return results, errors
		default:
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(c chunk) {
			defer func() { <-sem }()
			defer wg.Done()
			matches, err := ac.ops.findIndex(ctx, c.text)
			if err != nil {
				errors <- err
				return
			}
			results <- indexedResult{matches: matches, offset: c.textOffset}
		}(c)
	}

	go func() {
		wg.Wait()
		close(results)
		close(errors)
	}()

	return results, errors
}

func collectIndexResults(results <-chan indexedResult, errors <-chan error) (map[string]map[int]struct{}, error) {
	allMatches := make(map[string]map[int]struct{})
	var firstErr error

	resultsOpen, errorsOpen := true, true

	for resultsOpen || errorsOpen {
		select {
		case err, ok := <-errors:
			if !ok {
				errorsOpen = false
				continue
			}
			if firstErr == nil {
				firstErr = err
			}
		case res, ok := <-results:
			if !ok {
				resultsOpen = false
				continue
			}
			for keyword, indices := range res.matches {
				if allMatches[keyword] == nil {
					allMatches[keyword] = make(map[int]struct{})
				}
				for _, idx := range indices {
					adjustedIdx := idx + res.offset
					allMatches[keyword][adjustedIdx] = struct{}{}
				}
			}
		}
	}

	return allMatches, firstErr
}

// FindIndexParallel searches for keywords with indices using parallel processing.
// Similar to FindParallel but returns start positions for each match.
// Index values are adjusted to reflect positions in the original text,
// accounting for chunk offsets.
//
// The returned map has keywords as keys and sorted slices of unique start indices.
// Due to chunk overlap, matches at chunk boundaries may have duplicate indices
// that are automatically deduplicated.
//
// Limitation: like FindParallel, a keyword longer than opts.Overlap that straddles
// a chunk boundary can be missed. Set Overlap to at least your longest expected
// keyword length.
//
// Example:
//
//	opts := acor.DefaultParallelOptions()
//	matches, err := ac.FindIndexParallel(largeText, opts)
//	for keyword, indices := range matches {
//	    fmt.Printf("%s found at: %v\n", keyword, indices)
//	}
func (ac *AhoCorasick) FindIndexParallel(text string, opts *ParallelOptions) (map[string][]int, error) {
	opts = normalizeParallelOptions(opts)
	if opts.ChunkSize <= 0 {
		return nil, ErrInvalidChunkSize
	}

	chunks := splitChunks(text, opts)
	if len(chunks) == 0 {
		return map[string][]int{}, nil
	}
	if len(chunks) == 1 {
		return ac.FindIndex(text)
	}

	results, errors := runIndexWorkers(ac, chunks, opts.Workers)
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
