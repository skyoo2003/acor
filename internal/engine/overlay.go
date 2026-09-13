// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"slices"
	"unicode/utf8"
)

// Overlay combines an immutable base with a small replacement set. Its inputs
// must not be mutated after construction. It is used only by V3 snapshots.
func Overlay(base, added *Engine, removed map[string]struct{}) *Engine {
	i := &overlayEngine{base: base, added: added, removed: removed}
	return &Engine{preset: base.preset, impl: i, maxKeywordRunes: max(base.MaxKeywordRunes(), added.MaxKeywordRunes())}
}

type overlayEngine struct {
	base, added *Engine
	removed     map[string]struct{}
}

type overlayMatch struct {
	keyword string
	start   int
	end     int
	source  int
	order   int
}

func (o *overlayEngine) buildFromKeywords(map[string]struct{}) { panic("engine: immutable overlay") }

func (o *overlayEngine) matchString(text string, emit func(string, int, int) bool) {
	i := 0
	o.matchStream(func() (rune, bool) {
		if i == len(text) {
			return 0, false
		}
		r, size := utf8.DecodeRuneInString(text[i:])
		i += size
		return r, true
	}, emit)
}

func (o *overlayEngine) matchStream(next func() (rune, bool), emit func(string, int, int) bool) {
	base, baseOK := o.base.impl.(*memEfficientEngine)
	added, addedOK := o.added.impl.(*memEfficientEngine)
	if !baseOK || !addedOK {
		o.matchStreamWindow(next, emit)
		return
	}
	baseState, addedState, end := 0, 0, 0
	matches := make([]overlayMatch, 0, 8)
	for {
		ch, ok := next()
		if !ok {
			return
		}
		end++
		matches = matches[:0]
		baseState = base.advance(baseState, ch)
		base.trie.out.emitChain(baseState, end, func(k string, start, end int) bool {
			if _, removed := o.removed[k]; !removed {
				matches = append(matches, overlayMatch{k, start, end, 0, len(matches)})
			}
			return true
		})
		addedState = added.advance(addedState, ch)
		added.trie.out.emitChain(addedState, end, func(k string, start, end int) bool {
			matches = append(matches, overlayMatch{k, start, end, 1, len(matches)})
			return true
		})
		slices.SortStableFunc(matches, func(a, b overlayMatch) int {
			if a.start != b.start {
				return a.start - b.start
			}
			if a.source != b.source {
				return a.source - b.source
			}
			return a.order - b.order
		})
		for _, m := range matches {
			if !emit(m.keyword, m.start, m.end) {
				return
			}
		}
	}
}

// Other presets can still be overlaid. Keep only enough input to decide the
// matches ending at the current rune, so a stopped callback never reads ahead.
func (o *overlayEngine) matchStreamWindow(next func() (rune, bool), emit func(string, int, int) bool) {
	longest := max(o.base.MaxKeywordRunes(), o.added.MaxKeywordRunes())
	window := make([]rune, 0, longest)
	end := 0
	for {
		ch, ok := next()
		if !ok {
			return
		}
		end++
		window = append(window, ch)
		if len(window) > longest {
			copy(window, window[1:])
			window = window[:longest]
		}
		startOffset := end - len(window)
		matches := make([]overlayMatch, 0, 8)
		o.base.MatchString(string(window), func(k string, start, localEnd int) bool {
			if localEnd == len(window) {
				if _, removed := o.removed[k]; !removed {
					matches = append(matches, overlayMatch{k, startOffset + start, end, 0, len(matches)})
				}
			}
			return true
		})
		o.added.MatchString(string(window), func(k string, start, localEnd int) bool {
			if localEnd == len(window) {
				matches = append(matches, overlayMatch{k, startOffset + start, end, 1, len(matches)})
			}
			return true
		})
		slices.SortStableFunc(matches, func(a, b overlayMatch) int {
			if a.start != b.start {
				return a.start - b.start
			}
			if a.source != b.source {
				return a.source - b.source
			}
			return a.order - b.order
		})
		for _, m := range matches {
			if !emit(m.keyword, m.start, m.end) {
				return
			}
		}
	}
}

func (e *memEfficientEngine) advance(state int, ch rune) int {
	if e.bloom.skipAtRoot(state == 0, ch) {
		return 0
	}
	for {
		if next, ok := e.trie.nodes[state].next(ch); ok {
			return next
		}
		if state == 0 {
			return 0
		}
		state = e.trie.nodes[state].fail
	}
}

func (o *overlayEngine) find(text string) []string {
	if !o.added.Contains(text) {
		base := o.base.Find(text)
		out := base[:0]
		for _, k := range base {
			if _, deleted := o.removed[k]; !deleted {
				out = append(out, k)
			}
		}
		return out
	}
	out := []string{}
	o.matchString(text, func(k string, _, _ int) bool { out = append(out, k); return true })
	return out
}
func (o *overlayEngine) findSet(text string) []string {
	if !o.added.Contains(text) {
		base := o.base.FindSet(text)
		out := base[:0]
		for _, k := range base {
			if _, deleted := o.removed[k]; !deleted {
				out = append(out, k)
			}
		}
		return out
	}
	out := []string{}
	seen := make(map[string]struct{})
	o.matchString(text, func(k string, _, _ int) bool {
		if _, ok := seen[k]; !ok {
			seen[k] = struct{}{}
			out = append(out, k)
		}
		return true
	})
	return out
}
func (o *overlayEngine) findIndex(text string) map[string][]int {
	if !o.added.Contains(text) {
		out := o.base.FindIndex(text)
		for k := range o.removed {
			delete(out, k)
		}
		return out
	}
	out := make(map[string][]int)
	o.matchString(text, func(k string, start, _ int) bool { out[k] = append(out[k], start); return true })
	return out
}
func (o *overlayEngine) contains(text string) bool {
	if o.added.Contains(text) {
		return true
	}
	if len(o.removed) == 0 {
		return o.base.Contains(text)
	}
	found := false
	o.base.MatchString(text, func(k string, _, _ int) bool {
		if _, deleted := o.removed[k]; !deleted {
			found = true
			return false
		}
		return true
	})
	return found
}
func (o *overlayEngine) info() *InMemoryInfo {
	i := *o.base.Info()
	i.Keywords += o.added.Info().Keywords - len(o.removed)
	i.MemoryBytes += o.added.Info().MemoryBytes + int64(len(o.removed))*32
	i.TrieDepth = max(i.TrieDepth, o.added.Info().TrieDepth)
	return &i
}
