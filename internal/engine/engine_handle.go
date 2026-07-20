// SPDX-License-Identifier: Apache-2.0

package engine

import "unicode/utf8"

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

// Find returns the keywords found in text.
func (e *Engine) Find(text string) []string {
	return e.impl.find(text)
}

// FindIndex returns matched keywords mapped to their start offsets in text.
func (e *Engine) FindIndex(text string) map[string][]int {
	return e.impl.findIndex(text)
}

// FindMatches returns every match (overlaps included) in text, in scan order,
// each carrying its keyword and rune-offset span.
func (e *Engine) FindMatches(text string) []Match {
	out := make([]Match, 0)
	e.impl.matchStream(stringRuneSource(text), func(m Match) bool {
		out = append(out, m)
		return true
	})
	return out
}

// Contains reports whether text contains any keyword, stopping at the first hit.
func (e *Engine) Contains(text string) bool {
	found := false
	e.impl.matchStream(stringRuneSource(text), func(Match) bool {
		found = true
		return false
	})
	return found
}

// Stream pulls runes from next (rune-global offsets accumulate across calls) and
// reports every match to emit until next is exhausted or emit returns false.
// It lets callers scan an io.Reader without materializing the whole input.
func (e *Engine) Stream(next func() (rune, bool), emit func(Match) bool) {
	e.impl.matchStream(next, emit)
}

// stringRuneSource adapts a string to the rune-pull source matchStream expects.
// It decodes runes exactly like a range loop (invalid UTF-8 yields RuneError).
func stringRuneSource(s string) func() (rune, bool) {
	i := 0
	return func() (rune, bool) {
		if i >= len(s) {
			return 0, false
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		return r, true
	}
}

// Info returns statistics about the built automaton.
func (e *Engine) Info() *InMemoryInfo {
	return e.impl.info()
}
