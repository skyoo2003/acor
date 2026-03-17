package acor

import (
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
	return opts
}

//nolint:gocritic // Returns channels for concurrent result collection
func runStringWorkers(ac *AhoCorasick, chunks []chunk, workers int) (<-chan []string, <-chan error) {
	results := make(chan []string, len(chunks))
	errors := make(chan error, len(chunks))

	var wg sync.WaitGroup
	sem := make(chan struct{}, workers)

	for _, c := range chunks {
		wg.Add(1)
		sem <- struct{}{}
		go func(c chunk) {
			defer func() { <-sem }()
			defer wg.Done()
			matches, err := ac.Find(c.text)
			if err != nil {
				errors <- err
				return
			}
			results <- matches
		}(c)
	}

	go func() {
		wg.Wait()
		close(results)
		close(errors)
	}()

	return results, errors
}

func collectStringResults(results <-chan []string, errors <-chan error) (map[string]struct{}, error) {
	allMatches := make(map[string]struct{})
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
		case matches, ok := <-results:
			if !ok {
				resultsOpen = false
				continue
			}
			for _, m := range matches {
				allMatches[m] = struct{}{}
			}
		}
	}

	return allMatches, firstErr
}

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
		return ac.Find(text)
	}

	results, errors := runStringWorkers(ac, chunks, opts.Workers)
	allMatches, err := collectStringResults(results, errors)
	if err != nil {
		return nil, err
	}

	unique := make([]string, 0, len(allMatches))
	for m := range allMatches {
		unique = append(unique, m)
	}
	return unique, nil
}

type indexedResult struct {
	matches map[string][]int
	offset  int
}

//nolint:gocritic // Returns channels for concurrent result collection
func runIndexWorkers(ac *AhoCorasick, chunks []chunk, workers int) (<-chan indexedResult, <-chan error) {
	results := make(chan indexedResult, len(chunks))
	errors := make(chan error, len(chunks))

	var wg sync.WaitGroup
	sem := make(chan struct{}, workers)

	for _, c := range chunks {
		wg.Add(1)
		sem <- struct{}{}
		go func(c chunk) {
			defer func() { <-sem }()
			defer wg.Done()
			matches, err := ac.FindIndex(c.text)
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
