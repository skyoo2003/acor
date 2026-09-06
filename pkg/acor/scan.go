// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"errors"
	"unicode"

	matchengine "github.com/skyoo2003/acor/internal/engine"
)

const (
	defaultScanMatches    = 1000
	defaultScanInputBytes = 1 << 20
	defaultScanCandidates = 100000
	scanContextStride     = 1024
	sourceHeapArity       = 2
)

var (
	// ErrInputLimit means the input exceeds the requested byte bound.
	ErrInputLimit = errors.New("acor: input byte limit exceeded")
	// ErrScanWorkLimit means the raw automaton match budget was exhausted.
	ErrScanWorkLimit = errors.New("acor: candidate match limit exceeded")
	// ErrMatchLimit means a rewrite would exceed its match limit. No partial rewrite is returned.
	ErrMatchLimit = errors.New("acor: rewrite match limit exceeded")
	// ErrOutputLimit means the rewritten text would exceed its byte bound.
	ErrOutputLimit = errors.New("acor: output byte limit exceeded")
)

// SourceMatch identifies a match in the original input, even when Unicode case
// folding changes byte lengths. Start/End are rune offsets; ByteStart/ByteEnd are
// byte offsets. Both intervals are half-open. Text is the original substring;
// Keyword is the normalized dictionary entry. Text can retain the input string.
type SourceMatch struct {
	Keyword   string
	Text      string
	Start     int
	End       int
	ByteStart int
	ByteEnd   int
}

// ScanOptions bounds input, result storage and raw automaton output work.
// Zero limits select defaults (1 MiB input, 1,000 matches, 100,000 candidates);
// negative limits are rejected. Bounds are per call, not per worker.
type ScanOptions struct {
	MaxInputBytes int
	MaxMatches    int
	MaxCandidates int
	// Kind defaults to overlapping. LeftmostLongest selects non-overlapping matches
	// by earliest start, then longest length, without first collecting all matches.
	Kind      MatchKind
	WholeWord bool
	// WordRune follows MatchOptions' normalized-rune boundary semantics.
	WordRune func(rune) bool
}

// ScanResult contains at most MaxMatches matches. Truncated is true only when
// another eligible match was found. Input/work exhaustion returns an error and
// no result; callers must not mistake an incomplete scan for a clean document.
type ScanResult struct {
	Matches   []SourceMatch
	Truncated bool
}

// Scan searches with explicit resource bounds and original byte/rune positions.
// Existing Find/FindMatches remain unlimited and retain their existing contracts.
func (ac *AhoCorasick) Scan(ctx context.Context, text string, opts *ScanOptions) (*ScanResult, error) {
	o, err := scanOptions(ctx, text, opts)
	if err != nil {
		return nil, err
	}
	eng, err := ac.ops.loadEngine(ctx)
	if err != nil {
		return nil, err
	}
	return scanSource(ctx, eng, text, ac.caseSensitive, o)
}

// Scan searches one serving engine with bounded results and original positions.
func (v *VersionedCollection) Scan(ctx context.Context, text string, opts *ScanOptions) (*ScanResult, error) {
	ac, err := v.search(ctx)
	if err != nil {
		return nil, err
	}
	return ac.Scan(ctx, text, opts)
}
func scanOptions(ctx context.Context, text string, opts *ScanOptions) (ScanOptions, error) {
	if err := ctx.Err(); err != nil {
		return ScanOptions{}, err
	}
	var o ScanOptions
	if opts != nil {
		o = *opts
	}
	if o.MaxInputBytes < 0 || o.MaxMatches < 0 || o.MaxCandidates < 0 {
		return o, errors.New("acor: negative scan limit")
	}
	if o.Kind != MatchKindOverlapping && o.Kind != MatchKindLeftmostLongest {
		return o, errors.New("acor: invalid scan match kind")
	}
	if o.MaxInputBytes == 0 {
		o.MaxInputBytes = defaultScanInputBytes
	}
	if o.MaxMatches == 0 {
		o.MaxMatches = defaultScanMatches
	}
	if o.MaxCandidates == 0 {
		o.MaxCandidates = defaultScanCandidates
	}
	if len(text) > o.MaxInputBytes {
		return o, ErrInputLimit
	}
	if o.WordRune == nil {
		o.WordRune = isWordRune
	}
	return o, nil
}

type sourceScanner struct {
	ctx        context.Context
	text       string
	runes      []rune
	offsets    []int
	sensitive  bool
	opts       ScanOptions
	result     ScanResult
	pending    sourceWindow
	consumed   int
	candidates int
	longest    int
	stopped    bool
	err        error
}

func scanSource(ctx context.Context, eng *matchengine.Engine, text string, sensitive bool, o ScanOptions) (*ScanResult, error) {
	s := sourceScanner{ctx: ctx, text: text, sensitive: sensitive, opts: o, longest: eng.MaxKeywordRunes(), result: ScanResult{Matches: []SourceMatch{}}}
	for offset, r := range text {
		if len(s.runes)%scanContextStride == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		s.runes = append(s.runes, r)
		s.offsets = append(s.offsets, offset)
	}
	s.offsets = append(s.offsets, len(text))
	eng.Stream(s.next, s.emit)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.err != nil {
		return nil, s.err
	}
	if !s.stopped {
		s.flush(true)
	}
	if s.err != nil {
		return nil, s.err
	}
	return &s.result, nil
}
func (s *sourceScanner) normalized(r rune) rune {
	if s.sensitive {
		return r
	}
	return unicode.ToLower(r)
}
func (s *sourceScanner) next() (rune, bool) {
	if s.err = s.ctx.Err(); s.err != nil {
		return 0, false
	}
	if s.opts.Kind == MatchKindLeftmostLongest {
		s.flush(false)
	}
	if s.stopped || s.consumed == len(s.runes) {
		return 0, false
	}
	r := s.normalized(s.runes[s.consumed])
	s.consumed++
	return r, true
}
func (s *sourceScanner) emit(keyword string, start, end int) bool {
	if s.err = s.ctx.Err(); s.err != nil {
		return false
	}
	s.candidates++
	if s.candidates > s.opts.MaxCandidates {
		s.err = ErrScanWorkLimit
		return false
	}
	if s.opts.WholeWord && !s.wholeWord(start, end) {
		return true
	}
	m := SourceMatch{Keyword: keyword, Start: start, End: end, ByteStart: s.offsets[start], ByteEnd: s.offsets[end]}
	m.Text = s.text[m.ByteStart:m.ByteEnd]
	if s.opts.Kind == MatchKindLeftmostLongest {
		s.pending.add(m)
		return true
	}
	return s.append(m)
}
func (s *sourceScanner) wholeWord(start, end int) bool {
	return (start == 0 || !s.opts.WordRune(s.normalized(s.runes[start-1]))) &&
		(end == len(s.runes) || !s.opts.WordRune(s.normalized(s.runes[end])))
}
func (s *sourceScanner) append(m SourceMatch) bool {
	if len(s.result.Matches) == s.opts.MaxMatches {
		s.result.Truncated = true
		s.stopped = true
		return false
	}
	s.result.Matches = append(s.result.Matches, m)
	return true
}
func (s *sourceScanner) flush(final bool) {
	for len(s.pending.starts) > 0 && !s.stopped {
		if s.err = s.ctx.Err(); s.err != nil {
			s.stopped = true
			return
		}
		start := s.pending.starts[0]
		if !final && s.consumed-start < s.longest {
			return
		}
		m := s.pending.pop()
		if m.Start < s.pending.end {
			continue
		}
		if !s.append(m) {
			return
		}
		s.pending.end = m.End
	}
}

// sourceWindow keeps one best candidate per start position and a min-heap of
// starts. Memory is O(min(input runes, longest keyword)), not raw match count.
type sourceWindow struct {
	best   map[int]SourceMatch
	starts []int
	end    int
}

func (w *sourceWindow) add(m SourceMatch) {
	if m.Start < w.end {
		return
	}
	if w.best == nil {
		w.best = make(map[int]SourceMatch)
	}
	if old, ok := w.best[m.Start]; ok {
		if m.End > old.End {
			w.best[m.Start] = m
		}
		return
	}
	w.best[m.Start] = m
	w.starts = append(w.starts, m.Start)
	i := len(w.starts) - 1
	for i > 0 {
		parent := (i - 1) / sourceHeapArity
		if w.starts[parent] <= w.starts[i] {
			break
		}
		w.starts[parent], w.starts[i] = w.starts[i], w.starts[parent]
		i = parent
	}
}
func (w *sourceWindow) pop() SourceMatch {
	start := w.starts[0]
	m := w.best[start]
	delete(w.best, start)
	last := len(w.starts) - 1
	w.starts[0] = w.starts[last]
	w.starts = w.starts[:last]
	for i := 0; i < len(w.starts); {
		child := i*sourceHeapArity + 1
		if child >= len(w.starts) {
			break
		}
		if child+1 < len(w.starts) && w.starts[child+1] < w.starts[child] {
			child++
		}
		if w.starts[i] <= w.starts[child] {
			break
		}
		w.starts[i], w.starts[child] = w.starts[child], w.starts[i]
		i = child
	}
	return m
}
