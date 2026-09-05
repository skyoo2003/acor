// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"runtime"
	"slices"
	"unicode"

	"golang.org/x/sync/errgroup"
)

type chunk struct {
	text       string
	textOffset int
	ownedRunes int
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
	normalized := *opts
	opts = &normalized
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

// scanChunks runs scan over every chunk with at most workers running at once,
// writing each result into its own slot so the output keeps chunk order without
// a sort. It returns the first error any chunk reported.
func scanChunks[T any](ctx context.Context, chunks []chunk, workers int, scan func(context.Context, chunk) (T, error)) ([]T, error) {
	results := make([]T, len(chunks))

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(workers)
	for i, c := range chunks {
		g.Go(func() error {
			res, err := scan(gctx, c)
			if err != nil {
				return err
			}
			results[i] = res
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return results, nil
}

// FindParallel searches for keywords in text using parallel processing.
// The text is split into chunks processed by multiple goroutines, which can
// significantly improve performance for very large texts.
//
// If opts is nil, DefaultParallelOptions() is used. A text that fits within a
// single chunk is scanned by a single worker, so it pays no parallelization cost
// while still returning the same shape as a multi-chunk scan.
//
// The automaton is loaded once per call however many chunks the text splits into,
// so the read cost matches Find's at any input size and every chunk is scanned
// against the same dictionary snapshot. FindIndexParallel does the same.
//
// Note: Due to chunk overlap for boundary handling, duplicate matches are
// automatically deduplicated in the returned slice, so each keyword appears at
// most once regardless of how many times or in how many chunks it occurs. (Find
// reports every occurrence; FindParallel reports a set.)
//
// With AutoOverlap enabled, every boundary is protected using the loaded
// dictionary, including keywords longer than ChunkSize. Results retain chunk
// order, then first-match scan order within each chunk.
//
// Limitation with AutoOverlap disabled: a keyword longer than opts.Overlap that straddles a chunk boundary
// can be missed, since it fits in no single chunk. Enable AutoOverlap or use
// FindStream (which never splits a match) when this matters.
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
	return ac.FindParallelContext(ac.ctx, text, opts)
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
// Enable AutoOverlap to protect all boundaries, including keywords longer than
// ChunkSize. Positions are rune offsets in the original text.
//
// Limitation with AutoOverlap disabled: a keyword longer than opts.Overlap that straddles
// a chunk boundary can be missed. Enable AutoOverlap to protect these matches.
//
// Example:
//
//	opts := acor.DefaultParallelOptions()
//	matches, err := ac.FindIndexParallel(largeText, opts)
//	for keyword, indices := range matches {
//	    fmt.Printf("%s found at: %v\n", keyword, indices)
//	}
func (ac *AhoCorasick) FindIndexParallel(text string, opts *ParallelOptions) (map[string][]int, error) {
	return ac.FindIndexParallelContext(ac.ctx, text, opts)
}

// mergeIndexResults folds per-chunk index maps into one, shifting each chunk's
// offsets back into the original text and dropping the duplicates that chunk
// overlap produces. Indices come back sorted.
//
// perChunk must be index-aligned with chunks; scanChunks guarantees this by
// building its result slice from the same chunks it was handed.
func mergeIndexResults(chunks []chunk, perChunk []map[string][]int) map[string][]int {
	merged := make(map[string]map[int]struct{})
	for i, matches := range perChunk {
		for keyword, indices := range matches {
			if merged[keyword] == nil {
				merged[keyword] = make(map[int]struct{})
			}
			for _, idx := range indices {
				merged[keyword][idx+chunks[i].textOffset] = struct{}{}
			}
		}
	}

	result := make(map[string][]int, len(merged))
	for keyword, indices := range merged {
		sorted := make([]int, 0, len(indices))
		for idx := range indices {
			sorted = append(sorted, idx)
		}
		slices.Sort(sorted)
		result[keyword] = sorted
	}
	return result
}

// splitAutoChunks assigns each start position to exactly one base chunk.
func splitAutoChunks(text string, opts *ParallelOptions, longest int) []chunk {
	runes := []rune(text)
	extension := max(opts.Overlap, longest-1)
	chunks := make([]chunk, 0)
	for start := 0; start < len(runes); {
		end := start + min(opts.ChunkSize, len(runes)-start)
		if end < len(runes) {
			boundary := findBoundary(runes, end, opts.Boundary, opts.ChunkSize/defaultMaxBacktrackDivisor)
			if boundary > start {
				end = boundary
			}
		}
		searchEnd := end + min(extension, len(runes)-end)
		chunks = append(chunks, chunk{text: string(runes[start:searchEnd]), textOffset: start, ownedRunes: end - start})
		start = end
	}
	return chunks
}
