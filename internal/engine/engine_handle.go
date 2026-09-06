// SPDX-License-Identifier: Apache-2.0

package engine

import "unicode/utf8"

// container is the optional specialization behind Engine.Contains. Routing a
// presence check through matchString was the most expensive of the cheap
// operations: it missed the byte-scan fast path on ASCII text and reported a match
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

// Engine is the exported handle to an in-memory Aho-Corasick match engine.
// It wraps the preset-selected internal implementation so callers outside this
// package can build and query the automaton without depending on the concrete
// engine types (which stay unexported).
type Engine struct {
	preset          Preset
	impl            matchEngine
	maxKeywordRunes int
}

// New returns an Engine backed by the implementation selected for preset.
func New(preset Preset) *Engine {
	return &Engine{impl: newMatchEngine(preset), preset: preset}
}

// Build (re)constructs the automaton from the given keyword set.
func (e *Engine) Build(keywords map[string]struct{}) {
	e.maxKeywordRunes = 0
	for keyword := range keywords {
		e.maxKeywordRunes = max(e.maxKeywordRunes, utf8.RuneCountInString(keyword))
	}
	e.impl.buildFromKeywords(keywords)
}

// Find returns the keywords found in text. It is never nil (an
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

// MatchString reports every match (overlaps included) in text to emit in scan
// order, passing each keyword with its rune-offset span [start, end). It stops
// early if emit returns false.
//
// The match type itself lives in the public acor package rather than here, so
// callers assemble their own values. Declaring it here would put an internal type
// on acor's public API, and a []Match cannot be converted at the boundary.
func (e *Engine) MatchString(text string, emit func(keyword string, start, end int) bool) {
	e.impl.matchString(text, emit)
}

// Contains reports whether text contains any keyword, stopping at the first hit.
func (e *Engine) Contains(text string) bool {
	// An engine that can answer presence without positions does so directly; the
	// generic path below pays UTF-8 decoding, a match span, and a heap-boxed
	// capture for a bool.
	if c, ok := e.impl.(container); ok {
		return c.contains(text)
	}

	found := false
	e.impl.matchString(text, func(string, int, int) bool {
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
	return e.impl.findSet(text)
}

// Stream pulls runes from next (rune-global offsets accumulate across calls) and
// reports every match to emit until next is exhausted or emit returns false.
// It lets callers scan an io.Reader without materializing the whole input.
func (e *Engine) Stream(next func() (rune, bool), emit func(keyword string, start, end int) bool) {
	e.impl.matchStream(next, emit)
}

// Info returns statistics about the built automaton.
func (e *Engine) Info() *InMemoryInfo {
	return e.impl.info()
}

// MaxKeywordRunes returns the longest keyword length in runes.
func (e *Engine) MaxKeywordRunes() int { return e.maxKeywordRunes }
