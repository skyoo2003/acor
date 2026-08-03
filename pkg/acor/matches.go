// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"bufio"
	"cmp"
	"context"
	"errors"
	"io"
	"slices"
	"unicode"

	matchengine "github.com/skyoo2003/acor/internal/engine"
)

// Match is a single keyword occurrence in the searched text. Start and End are
// rune offsets forming the half-open span [Start, End), consistent with
// FindIndex's rune-based offsets. Re-exported from the internal engine package so
// callers depend only on the public acor API.
type Match = matchengine.Match

// MatchKind selects how overlapping matches are reported by FindMatches.
type MatchKind int

const (
	// MatchKindOverlapping reports every match, including overlapping and nested
	// ones, in scan order. This is the classic Aho-Corasick behavior and matches
	// what Find/FindIndex return. It is the default.
	MatchKindOverlapping MatchKind = iota
	// MatchKindLeftmostLongest reports only non-overlapping matches, preferring
	// the leftmost start and, among matches at the same start, the longest
	// keyword. Best for tokenization, redaction, and replace-the-match workflows.
	MatchKindLeftmostLongest
)

// MatchOptions tunes FindMatches. A nil *MatchOptions means overlapping matches
// with no whole-word constraint (identical to the raw automaton output).
type MatchOptions struct {
	// Kind selects overlapping (default) or leftmost-longest non-overlapping.
	Kind MatchKind
	// WholeWord, when true, drops matches whose neighboring runes are word
	// characters (letters, digits, combining marks, or underscore) — e.g. it stops
	// "class" from matching inside "classic". Boundaries are the string start/end
	// or any non-word rune.
	//
	// WholeWord assumes a script that delimits words with spaces or punctuation.
	// Scripts written without inter-word boundaries (CJK, Thai, …) classify every
	// adjacent character as a word rune, so a WholeWord match there is almost
	// always treated as mid-word and dropped; use FindMatches without WholeWord,
	// or supply WordRune, for such text.
	WholeWord bool
	// WordRune overrides which runes count as part of a word when WholeWord is
	// set. A match is whole-word only when the runes immediately before its start
	// and at its end are not word runes. nil uses the default (letters, digits,
	// combining marks, underscore). Supply a predicate for scripts the default
	// misclassifies — e.g. return false for CJK ideographs so a CJK term bounded
	// by spaces or ASCII is reported. Ignored unless WholeWord is true.
	WordRune func(rune) bool
}

// FindMatches searches text and returns matches carrying each keyword and its
// rune-offset span, in scan order. Unlike FindIndex (which groups start offsets
// by keyword and loses ordering and end positions), this preserves match order
// and end offsets — useful for highlighting and replacement.
//
// opts controls overlap handling and whole-word filtering; nil yields raw
// overlapping matches.
func (ac *AhoCorasick) FindMatches(text string, opts *MatchOptions) ([]Match, error) {
	return ac.FindMatchesContext(ac.ctx, text, opts)
}

// FindMatchesAppend is FindMatches writing into dst and returning the extended
// slice, so a caller scanning many texts can reuse one buffer instead of
// allocating a result per call. Pass dst[:0] to reuse the backing array; a nil dst
// behaves exactly like FindMatches.
//
// opts applies only to this call's matches: matches already in dst came from
// another text, so they are neither re-filtered nor reordered, and an empty text
// returns dst unchanged.
func (ac *AhoCorasick) FindMatchesAppend(dst []Match, text string, opts *MatchOptions) ([]Match, error) {
	return ac.findMatches(ac.ctx, dst, text, opts)
}

// FindMatchesContext is FindMatches with an explicit context for cancellation.
func (ac *AhoCorasick) FindMatchesContext(ctx context.Context, text string, opts *MatchOptions) ([]Match, error) {
	return ac.findMatches(ctx, nil, text, opts)
}

func (ac *AhoCorasick) findMatches(ctx context.Context, dst []Match, text string, opts *MatchOptions) ([]Match, error) {
	if text == "" {
		// dst is returned untouched rather than truncated: the contract is append, so
		// a caller accumulating across texts must not lose what it already has. A nil
		// dst still yields a non-nil empty slice, as FindMatches always has.
		if dst == nil {
			return []Match{}, nil
		}
		return dst, nil
	}
	norm := normalizeText(text, ac.caseSensitive)

	eng, err := ac.ops.loadEngine(ctx)
	if err != nil {
		return nil, err
	}
	// Honor an already-canceled ctx at the match boundary; the in-memory scan
	// itself isn't ctx-threaded (mirrors find/findIndex).
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Filtering applies to this call's matches only. Anything already in dst was
	// found in a different text, so its offsets do not index norm: filterWholeWord
	// would read out of range, and leftmostLongest could drop the caller's earlier
	// results.
	base := len(dst)
	matches := eng.FindMatchesAppend(dst, norm)
	if opts != nil {
		found := matches[base:]
		// Guard the []rune conversion: on the common zero-match path (a clean doc
		// through a WholeWord gate) there is nothing to filter and the rune slice
		// would be a wasted large allocation.
		if opts.WholeWord && len(found) > 0 {
			isWord := isWordRune
			if opts.WordRune != nil {
				isWord = opts.WordRune
			}
			found = filterWholeWord(found, []rune(norm), isWord)
		}
		if opts.Kind == MatchKindLeftmostLongest {
			found = leftmostLongest(found)
		}
		// Both filters only shrink, so this writes back into dst's own array; when
		// found still aliases it, source and destination coincide and it is a no-op.
		matches = append(matches[:base], found...)
	}
	return matches, nil
}

// FindSet returns each matched keyword once, in first-match order.
//
// Find reports one entry per occurrence, which is rarely what a content filter
// wants: "which banned words does this text use" is a question about the set.
// FindSet answers it directly, folding duplicates out during the scan instead of
// building the per-occurrence slice first.
//
// Use Find when occurrence counts matter, FindIndex or FindMatches when
// positions do, and Contains when only presence does.
func (ac *AhoCorasick) FindSet(text string) ([]string, error) {
	return ac.FindSetContext(ac.ctx, text)
}

// FindSetContext is FindSet with an explicit context for cancellation.
func (ac *AhoCorasick) FindSetContext(ctx context.Context, text string) ([]string, error) {
	if text == "" {
		return []string{}, nil
	}
	norm := normalizeText(text, ac.caseSensitive)

	eng, err := ac.ops.loadEngine(ctx)
	if err != nil {
		return nil, err
	}
	// See FindMatchesContext: honor an already-canceled ctx at the match boundary.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return eng.FindSet(norm), nil
}

// Contains reports whether text contains any keyword. It stops at the first
// match instead of collecting them all, so it is cheaper than len(Find()) > 0
// for gate-style checks (e.g. "does this text contain a banned word?").
func (ac *AhoCorasick) Contains(text string) (bool, error) {
	return ac.ContainsContext(ac.ctx, text)
}

// ContainsContext is Contains with an explicit context for cancellation.
func (ac *AhoCorasick) ContainsContext(ctx context.Context, text string) (bool, error) {
	if text == "" {
		return false, nil
	}
	norm := normalizeText(text, ac.caseSensitive)

	eng, err := ac.ops.loadEngine(ctx)
	if err != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return eng.Contains(norm), nil
}

// FindStream scans an io.Reader without loading the whole input into memory,
// invoking onMatch for every match (overlaps included) in scan order. Match
// offsets are rune positions from the start of the stream. Return false from
// onMatch to stop early.
//
// Unlike FindParallel, which can miss a keyword longer than the chunk overlap at
// a chunk boundary, streaming keeps a single automaton state across the whole
// input, so no match is ever split.
//
// Whole-word and leftmost-longest options are not applied here: both need
// buffering that defeats streaming. Use FindMatches on a bounded string for
// those. Only modes with a local engine (Preset or a V2/V1 collection) are
// supported.
func (ac *AhoCorasick) FindStream(r io.Reader, onMatch func(Match) bool) error {
	return ac.FindStreamContext(ac.ctx, r, onMatch)
}

// FindStreamContext is FindStream with an explicit context. The context is
// checked between runes, so a canceled context stops the scan and returns
// ctx.Err().
func (ac *AhoCorasick) FindStreamContext(ctx context.Context, r io.Reader, onMatch func(Match) bool) error {
	if r == nil || onMatch == nil {
		return nil
	}

	eng, err := ac.ops.loadEngine(ctx)
	if err != nil {
		return err
	}

	br := bufio.NewReader(r)
	caseInsensitive := !ac.caseSensitive
	var scanErr error

	// bufio.Reader.ReadRune handles runes split across buffer refills, so the
	// stream is decoded exactly like a range loop over the full string.
	next := func() (rune, bool) {
		if err := ctx.Err(); err != nil {
			scanErr = err
			return 0, false
		}
		ru, _, e := br.ReadRune()
		if e != nil {
			// errors.Is, not ==: a decorator reader may return a wrapped io.EOF at
			// end of input, which is a normal completion, not a scan failure.
			if !errors.Is(e, io.EOF) {
				scanErr = e
			}
			return 0, false
		}
		if caseInsensitive {
			// Exactly the fold the in-memory path applies: strings.ToLower is
			// strings.Map(unicode.ToLower, s), so this agrees rune for rune over the
			// whole Unicode range, not just ASCII. TestFindStream_CaseFoldParity
			// guards the agreement.
			ru = unicode.ToLower(ru)
		}
		return ru, true
	}

	eng.Stream(next, onMatch)
	return scanErr
}

// cmpLeftmostLongest orders matches by start ascending, and among matches at the
// same start by end descending, so a greedy pass keeps the longest.
func cmpLeftmostLongest(a, b Match) int {
	if a.Start != b.Start {
		return cmp.Compare(a.Start, b.Start)
	}
	return cmp.Compare(b.End, a.End)
}

// leftmostLongest reduces overlapping matches to the non-overlapping
// leftmost-longest set: sort by start ascending then end descending, then greedily
// keep a match whenever its start is at or past the previous kept match's end.
func leftmostLongest(ms []Match) []Match {
	if len(ms) <= 1 {
		return ms
	}
	// The scan emits in end order, which for a dictionary with no nested keywords is
	// start order too, so the sort is often pure overhead. Checking costs one linear
	// pass against the O(n log n) it can skip.
	//
	// slices.SortFunc, not sort.Slice: the latter swaps through reflection, which
	// measured as ~30% of a leftmost-longest FindMatches call.
	if !slices.IsSortedFunc(ms, cmpLeftmostLongest) {
		slices.SortFunc(ms, cmpLeftmostLongest)
	}
	// Selection is in place: the kept set is a subsequence of the sorted input and
	// the write cursor never passes the read cursor, so no second slice is needed.
	out := ms[:0]
	lastEnd := 0
	for _, m := range ms {
		if m.Start >= lastEnd {
			out = append(out, m)
			lastEnd = m.End
		}
	}
	return out
}

// filterWholeWord keeps only matches bounded by non-word runes (or the text
// edges), per the isWord predicate. runes is the searched text as a rune slice,
// so Match rune offsets index directly into it.
func filterWholeWord(ms []Match, runes []rune, isWord func(rune) bool) []Match {
	out := make([]Match, 0, len(ms))
	for _, m := range ms {
		beforeOK := m.Start == 0 || !isWord(runes[m.Start-1])
		afterOK := m.End >= len(runes) || !isWord(runes[m.End])
		if beforeOK && afterOK {
			out = append(out, m)
		}
	}
	return out
}

func isWordRune(r rune) bool {
	// unicode.Mark: a combining mark (e.g. U+0301) belongs to the base letter it
	// decorates, so a match ending right before one (decomposed/NFD text like
	// "cafe"+combining-acute) is mid-word, not a whole word.
	return unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.Is(unicode.Mark, r) || r == '_'
}
