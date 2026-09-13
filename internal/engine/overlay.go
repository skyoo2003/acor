// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"slices"
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
	var matches []overlayMatch
	order := 0
	o.base.MatchString(text, func(k string, start, end int) bool {
		if _, deleted := o.removed[k]; !deleted {
			matches = append(matches, overlayMatch{k, start, end, 0, order})
		}
		order++
		return true
	})
	order = 0
	o.added.MatchString(text, func(k string, start, end int) bool {
		matches = append(matches, overlayMatch{k, start, end, 1, order})
		order++
		return true
	})
	slices.SortStableFunc(matches, func(a, b overlayMatch) int {
		if a.end != b.end {
			return a.end - b.end
		}
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

func (o *overlayEngine) matchStream(next func() (rune, bool), emit func(string, int, int) bool) {
	const chunk = 4096
	longest := max(o.base.MaxKeywordRunes(), o.added.MaxKeywordRunes())
	window := make([]rune, 0, chunk+longest)
	windowStart := 0
	total := 0
	stopped := false
	emittedEnd := 0
	process := func() {
		o.matchString(string(window), func(k string, from, end int) bool {
			if windowStart+end <= emittedEnd {
				return true
			}
			if !emit(k, windowStart+from, windowStart+end) {
				stopped = true
				return false
			}
			return true
		})
		emittedEnd = total
		keep := min(len(window), max(0, longest-1))
		windowStart = total - keep
		copy(window, window[len(window)-keep:])
		window = window[:keep]
	}
	for {
		r, ok := next()
		if !ok {
			break
		}
		window = append(window, r)
		total++
		if len(window) >= chunk+longest-1 {
			process()
			if stopped {
				return
			}
		}
	}
	if len(window) > 0 && !stopped {
		process()
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
