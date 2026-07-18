// SPDX-License-Identifier: Apache-2.0

package engine

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

// Info returns statistics about the built automaton.
func (e *Engine) Info() *InMemoryInfo {
	return e.impl.info()
}
