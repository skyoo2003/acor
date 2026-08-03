// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"slices"
	"strings"
)

// setFinder is the optional specialization behind Engine.FindSet. It is separate
// from matchEngine so an engine with no cheaper way to answer a set query is not
// forced to reimplement the generic one.
type setFinder interface {
	findSet(text string) []string
}

// container is the optional specialization behind Engine.Contains. Routing a
// presence check through matchString was the most expensive of the cheap
// operations: it missed the byte-scan fast path on ASCII text and built a Match
// (including a RuneCountInString) per hit only to discard it. On 1000 keywords
// over a 640 B text it went from 1,147 to 666 ns with no match, 77 to 25 ns with
// one, and two allocations to zero.
type container interface {
	contains(text string) bool
}

// findResultHint is the starting capacity for a match-result slice. Text that
// matches nothing is the common case, so it only has to absorb the first few
// appends on text that does match; sizing it to the dictionary would allocate far
// more than a single scan needs.
const findResultHint = 8

// dedupLinearMax is the match-set size at which setCollector stops deduping by
// linear scan and switches to hash maps. Below it the scan wins: two maps cost two
// allocations up front and a string hash per hit, and a content filter typically
// finds a handful of keywords per document. On 1000 keywords over a 640 B text,
// linear scanning took findSet from 1,295 to 700 ns and 5 allocations to 3.
//
// ponytail: linear dedup is O(k^2) in the unique-match count k. Past the threshold
// the maps take over, so match-dense text does not pay the quadratic. Pattern-id
// outputs would remove the trade-off by making dedup an integer index test.
const dedupLinearMax = 32

// outNone marks the end of an output-link chain.
//
// A state's output chain is its failure chain restricted to states carrying a
// keyword, so walking it enumerates the keywords ending at the current position,
// longest first:
//
//	for s := state; s != outNone; s = int(outLink[s]) {
//		if kw := own[s]; kw != "" { ... }
//	}
//
// The cursor is an int and widens on read, so no narrowing conversion is needed.
// Each scan writes the loop out rather than sharing a callback helper: find and
// findSet run it per match, where an indirect call costs what engine_flat.go's
// header note measured.
const outNone = -1

// setCollector folds matched states into the unique keyword set behind FindSet,
// preserving first-match order.
//
// State dedup comes first because a state's keyword set is fixed: landing on it
// again can add nothing new. Keyword dedup is still needed on top because distinct
// states' output chains overlap, since every state whose failure path reaches a
// keyword reports it.
//
// It is a struct with methods rather than closures over local variables so it
// stays on the stack; a closure capturing the slice would be boxed on the heap.
type setCollector struct {
	out        []string
	seenStates []int32
	seen       map[string]struct{}
	seenState  map[int32]struct{}
}

// addState records a matching state and reports whether it is new. A state's
// keyword set is fixed, so landing there again adds nothing and the caller can
// skip walking its output chain.
func (c *setCollector) addState(state int) bool {
	if c.seenState != nil {
		if _, done := c.seenState[int32(state)]; done { //nolint:gosec // G115: state ids are bounded by hasOutputBit.
			return false
		}
		c.seenState[int32(state)] = struct{}{} //nolint:gosec // G115: as above.
		return true
	}

	if slices.Contains(c.seenStates, int32(state)) { //nolint:gosec // G115: as above.
		return false
	}
	c.seenStates = append(c.seenStates, int32(state)) //nolint:gosec // G115: as above.
	if len(c.seenStates) > dedupLinearMax {
		c.promote()
	}
	return true
}

// addKeyword records a single keyword. It is the entry point for engines that
// report keywords without a state id (the generic matchString path).
func (c *setCollector) addKeyword(kw string) {
	if c.seen != nil {
		if _, dup := c.seen[kw]; dup {
			return
		}
		c.seen[kw] = struct{}{}
		c.out = append(c.out, kw)
		return
	}
	if slices.Contains(c.out, kw) {
		return
	}
	if c.out == nil {
		// Take the whole starting capacity at the first hit instead of letting
		// append regrow 1,2,4,8, exactly as find does.
		c.out = make([]string, 0, findResultHint)
	}
	c.out = append(c.out, kw)
	if len(c.out) > dedupLinearMax {
		c.promote()
	}
}

// promote moves the linear state into hash maps once the match set is large enough
// that rescanning it per hit costs more than hashing. out keeps its order.
func (c *setCollector) promote() {
	if c.seen != nil {
		return
	}
	c.seen = make(map[string]struct{}, len(c.out)*2)
	for _, kw := range c.out {
		c.seen[kw] = struct{}{}
	}
	c.seenState = make(map[int32]struct{}, len(c.seenStates)*2)
	for _, s := range c.seenStates {
		c.seenState[s] = struct{}{}
	}
	c.seenStates = nil
}

// result returns the unique keywords in first-match order. Like Find it is never
// nil, and a zero-length literal costs no allocation, so text that matched
// nothing stays allocation-free.
func (c *setCollector) result() []string {
	if c.out == nil {
		return []string{}
	}
	return c.out
}

// Engine is the exported handle to an in-memory Aho-Corasick match engine.
// It wraps the preset-selected internal implementation so callers outside this
// package can build and query the automaton without depending on the concrete
// engine types (which stay unexported).
type Engine struct {
	impl matchEngine
}

// New returns an Engine backed by the implementation selected for preset.
func New(preset Preset) *Engine {
	return &Engine{impl: newMatchEngine(preset)}
}

// Build (re)constructs the automaton from the given keyword set.
func (e *Engine) Build(keywords map[string]struct{}) {
	e.impl.buildFromKeywords(keywords)
}

// Find returns the keywords found in text. Like FindMatches it is never nil (an
// automaton with no keywords yields an empty slice), so callers can hand it
// straight to a JSON encoder or compare it without a nil special case.
func (e *Engine) Find(text string) []string {
	return e.impl.find(text)
}

// FindIndex returns matched keywords mapped to their start offsets in text.
// Like Find, it is never nil.
func (e *Engine) FindIndex(text string) map[string][]int {
	return e.impl.findIndex(text)
}

// FindMatches returns every match (overlaps included) in text, in scan order,
// each carrying its keyword and rune-offset span.
func (e *Engine) FindMatches(text string) []Match {
	return e.FindMatchesAppend(nil, text)
}

// FindMatchesAppend appends matches to dst and returns the extended slice, so a
// caller scanning many texts can reuse one buffer instead of allocating a result
// slice per call. dst may be nil.
func (e *Engine) FindMatchesAppend(dst []Match, text string) []Match {
	out := dst
	e.impl.matchString(text, func(m Match) bool {
		if out == nil {
			out = make([]Match, 0, findResultHint)
		}
		out = append(out, m)
		return true
	})
	if out == nil {
		return []Match{}
	}
	return out
}

// Contains reports whether text contains any keyword, stopping at the first hit.
func (e *Engine) Contains(text string) bool {
	// An engine that can answer presence without positions does so directly; the
	// generic path below pays UTF-8 decoding, a Match struct, and a heap-boxed
	// capture for a bool.
	if c, ok := e.impl.(container); ok {
		return c.contains(text)
	}

	found := false
	e.impl.matchString(text, func(Match) bool {
		found = true
		return false
	})
	return found
}

// FindSet returns each matched keyword once, in first-match order.
//
// Find reports one entry per occurrence, so callers asking "which of my patterns
// appear" have to fold that into a set themselves. Doing it during the scan skips
// the per-occurrence slice, which on match-dense text is most of the work.
func (e *Engine) FindSet(text string) []string {
	// An engine that can answer this without positions does so directly; the
	// generic path below pays for a Match struct and a closure call per occurrence,
	// all of which a set query throws away.
	if sf, ok := e.impl.(setFinder); ok {
		return sf.findSet(text)
	}

	// The collector stays unallocated until something matches: text matching nothing
	// is the common case for a filter, and it should not pay for the bookkeeping of
	// a hit that never happened.
	var c setCollector
	e.impl.matchString(text, func(m Match) bool {
		c.addKeyword(m.Keyword)
		return true
	})
	return c.result()
}

// Stream pulls runes from next (rune-global offsets accumulate across calls) and
// reports every match to emit until next is exhausted or emit returns false.
// It lets callers scan an io.Reader without materializing the whole input.
func (e *Engine) Stream(next func() (rune, bool), emit func(Match) bool) {
	e.impl.matchStream(next, emit)
}

// stringRuneSource adapts a string to the rune-pull source matchStream expects.
// strings.Reader.ReadRune decodes exactly like a range loop (invalid UTF-8 yields
// RuneError), which makes it useful for testing Stream against a string. The match
// entry points no longer route through it: that indirect call per rune is what
// matchString exists to avoid.
func stringRuneSource(s string) func() (rune, bool) {
	rd := strings.NewReader(s)
	return func() (rune, bool) {
		r, _, err := rd.ReadRune()
		if err != nil {
			return 0, false
		}
		return r, true
	}
}

// Info returns statistics about the built automaton.
func (e *Engine) Info() *InMemoryInfo {
	return e.impl.info()
}
