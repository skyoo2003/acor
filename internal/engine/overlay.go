// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"slices"
	"strings"
	"unicode/utf8"
)

// Overlay combines an immutable base with a small replacement set. Its inputs
// must not be mutated after construction. It is used only by V3 snapshots.
func Overlay(base, added *Engine, removed map[string]struct{}) *Engine {
	if added.MaxKeywordRunes() == 0 && len(removed) == 0 {
		return base
	}
	i := &overlayEngine{base: base, added: added, removed: removed}
	if _, ok := base.impl.(*memEfficientEngine); ok {
		if a, ok := added.impl.(*memEfficientEngine); ok {
			i.literal = newLiteralDelta(&a.trie.out)
		}
	}
	return &Engine{preset: base.preset, impl: i, maxKeywordRunes: max(base.MaxKeywordRunes(), added.MaxKeywordRunes())}
}

type overlayEngine struct {
	base, added *Engine
	removed     map[string]struct{}
	literal     *literalDelta
}

// literalDelta confines a small ASCII addition set to occurrences of its
// common prefix. A map lookup per distinct byte length verifies the complete
// word, so a 128-word batch with one prefix costs one lookup at each candidate.
// The general two-automaton path remains available for other dictionaries.
type literalDelta struct {
	prefix  string
	lengths []int
	words   map[int]map[string]string
}

// Candidate collection is bounded to short strings; long strings use the
// streaming automata so match-dense input cannot retain one hit per occurrence.
const literalQueryLimit = 4096

type literalHit struct {
	endByte int
	length  int
	keyword string
}

func newLiteralDelta(out *outputs) *literalDelta {
	if out.keywordCount() == 0 || out.keywordCount() > 128 {
		return nil
	}
	var prefix string
	groups := make(map[int]map[string]string)
	for _, word := range out.keywords[1:] {
		for i := range len(word) {
			if word[i] >= utf8.RuneSelf {
				return nil
			}
		}
		if prefix == "" {
			prefix = word
		} else {
			i := 0
			for i < len(prefix) && i < len(word) && prefix[i] == word[i] {
				i++
			}
			prefix = prefix[:i]
		}
		if groups[len(word)] == nil {
			groups[len(word)] = make(map[string]string)
		}
		groups[len(word)][word] = word
	}
	if len(prefix) < 3 || len(groups) > 8 {
		return nil
	}
	lengths := make([]int, 0, len(groups))
	for length := range groups {
		lengths = append(lengths, length)
	}
	slices.Sort(lengths)
	return &literalDelta{prefix: prefix, lengths: lengths, words: groups}
}

func (d *literalDelta) matches(text string, hits []literalHit) []literalHit {
	for offset := 0; offset < len(text); {
		at := strings.Index(text[offset:], d.prefix)
		if at < 0 {
			break
		}
		at += offset
		for _, length := range d.lengths {
			if at+length <= len(text) {
				word := text[at : at+length]
				if canonical, ok := d.words[length][word]; ok {
					hits = append(hits, literalHit{endByte: at + length, length: length, keyword: canonical})
				}
			}
		}
		offset = at + 1
	}
	if len(hits) > 1 {
		slices.SortFunc(hits, func(a, b literalHit) int {
			if a.endByte != b.endByte {
				return a.endByte - b.endByte
			}
			return b.length - a.length
		})
	}
	return hits
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
	if o.literal != nil && len(text) <= literalQueryLimit {
		o.matchStringLiteral(text, emit)
		return
	}
	base, baseOK := o.base.impl.(*memEfficientEngine)
	added, addedOK := o.added.impl.(*memEfficientEngine)
	if !baseOK || !addedOK {
		i := 0
		o.matchStreamWindow(func() (rune, bool) {
			if i == len(text) {
				return 0, false
			}
			r, size := utf8.DecodeRuneInString(text[i:])
			i += size
			return r, true
		}, emit)
		return
	}
	if added.trie.out.keywordCount() == 0 {
		o.base.MatchString(text, func(k string, start, end int) bool {
			if _, removed := o.removed[k]; removed {
				return true
			}
			return emit(k, start, end)
		})
		return
	}
	baseState, addedState, end := 0, 0, 0
	for _, ch := range text {
		end++
		baseState = base.advance(baseState, ch)
		addedState = added.advance(addedState, ch)
		if base.trie.out.own[baseState] == 0 && base.trie.out.outLink[baseState] == outNone &&
			added.trie.out.own[addedState] == 0 && added.trie.out.outLink[addedState] == outNone {
			continue
		}
		if !o.emitMerged(&base.trie.out, baseState, &added.trie.out, addedState, end, emit) {
			return
		}
	}
}

// matchStringLiteral traverses the large base only once. The addition matcher
// inspects only complete literal candidates at occurrences of its common
// prefix, then merges those matches with base outputs at the same rune end.
//
//nolint:gocyclo // The hot scan keeps transitions and ordered output merge in one loop.
func (o *overlayEngine) matchStringLiteral(text string, emit func(string, int, int) bool) {
	var hitBuffer [8]literalHit
	hits := o.literal.matches(text, hitBuffer[:0])
	if len(hits) == 0 {
		if len(o.removed) == 0 {
			o.base.MatchString(text, emit)
			return
		}
		o.base.MatchString(text, func(k string, start, end int) bool {
			if _, removed := o.removed[k]; removed {
				return true
			}
			return emit(k, start, end)
		})
		return
	}
	base := o.base.impl.(*memEfficientEngine)
	out := &base.trie.out
	state, end, hitIndex := 0, 0, 0
	for pos, ch := range text {
		end++
		if !base.bloom.skipAtRoot(state == 0, ch) {
			for {
				if next, ok := base.trie.nodes[state].next(ch); ok {
					state = next
					break
				}
				if state == 0 {
					break
				}
				state = base.trie.nodes[state].fail
			}
		}
		// Literal additions are ASCII, so their last rune occupies one byte.
		endByte := pos + 1
		baseState := nextOutput(out, state)
		for baseState != outNone || hitIndex < len(hits) && hits[hitIndex].endByte == endByte {
			baseID := int32(0)
			if baseState != outNone {
				baseID = out.own[baseState]
			}
			if hitIndex == len(hits) || hits[hitIndex].endByte != endByte ||
				baseID != 0 && int(out.runeLens[baseID]) >= hits[hitIndex].length {
				keyword := out.keywords[baseID]
				baseState = nextOutput(out, int(out.outLink[baseState]))
				if _, removed := o.removed[keyword]; !removed && !emit(keyword, end-int(out.runeLens[baseID]), end) {
					return
				}
			} else {
				hit := hits[hitIndex]
				hitIndex++
				if !emit(hit.keyword, end-hit.length, end) {
					return
				}
			}
		}
	}
}

// emitMerged walks each output chain once. Chains follow failure links from
// longest keyword to shortest, so comparing rune lengths preserves the full
// rebuild's start-position order. Equal lengths retain base-first order.
func (o *overlayEngine) emitMerged(base *outputs, baseState int, added *outputs, addedState, end int,
	emit func(string, int, int) bool) bool {
	for baseState != outNone || addedState != outNone {
		baseState = nextOutput(base, baseState)
		addedState = nextOutput(added, addedState)
		baseID, addedID := int32(0), int32(0)
		if baseState != outNone {
			baseID = base.own[baseState]
		}
		if addedState != outNone {
			addedID = added.own[addedState]
		}
		if baseID == 0 && addedID == 0 {
			return true
		}
		if addedID == 0 || baseID != 0 && base.runeLens[baseID] >= added.runeLens[addedID] {
			baseState = int(base.outLink[baseState])
			keyword := base.keywords[baseID]
			if _, removed := o.removed[keyword]; !removed && !emit(keyword, end-int(base.runeLens[baseID]), end) {
				return false
			}
		} else {
			addedState = int(added.outLink[addedState])
			if !emit(added.keywords[addedID], end-int(added.runeLens[addedID]), end) {
				return false
			}
		}
	}
	return true
}

func nextOutput(out *outputs, state int) int {
	for state != outNone && out.own[state] == 0 {
		state = int(out.outLink[state])
	}
	return state
}

// appendMerged is the Find collector's direct path. Avoiding a callback here
// matters because Find reports every occurrence and can call it many times.
func (o *overlayEngine) appendMerged(dst []string, base *outputs, baseState int,
	added *outputs, addedState int) []string {
	for baseState != outNone || addedState != outNone {
		baseState = nextOutput(base, baseState)
		addedState = nextOutput(added, addedState)
		if baseState == outNone && addedState == outNone {
			break
		}
		if addedState == outNone || baseState != outNone &&
			base.runeLens[base.own[baseState]] >= added.runeLens[added.own[addedState]] {
			keyword := base.keywords[base.own[baseState]]
			baseState = int(base.outLink[baseState])
			if _, removed := o.removed[keyword]; !removed {
				dst = append(dst, keyword)
			}
		} else {
			dst = append(dst, added.keywords[added.own[addedState]])
			addedState = int(added.outLink[addedState])
		}
	}
	return dst
}

func overlayMatchOrder(a, b overlayMatch) int {
	if a.start != b.start {
		return a.start - b.start
	}
	if a.source != b.source {
		return a.source - b.source
	}
	return a.order - b.order
}

func (o *overlayEngine) matchStream(next func() (rune, bool), emit func(string, int, int) bool) {
	base, baseOK := o.base.impl.(*memEfficientEngine)
	added, addedOK := o.added.impl.(*memEfficientEngine)
	if !baseOK || !addedOK {
		o.matchStreamWindow(next, emit)
		return
	}
	if added.trie.out.keywordCount() == 0 {
		o.base.Stream(next, func(k string, start, end int) bool {
			if _, removed := o.removed[k]; removed {
				return true
			}
			return emit(k, start, end)
		})
		return
	}
	baseState, addedState, end := 0, 0, 0
	for {
		ch, ok := next()
		if !ok {
			return
		}
		end++
		baseState = base.advance(baseState, ch)
		addedState = added.advance(addedState, ch)
		if base.trie.out.own[baseState] == 0 && base.trie.out.outLink[baseState] == outNone &&
			added.trie.out.own[addedState] == 0 && added.trie.out.outLink[addedState] == outNone {
			continue
		}
		if !o.emitMerged(&base.trie.out, baseState, &added.trie.out, addedState, end, emit) {
			return
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
		if len(matches) > 1 {
			slices.SortStableFunc(matches, overlayMatchOrder)
		}
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
	if o.literal != nil && len(text) <= literalQueryLimit {
		return o.findLiteral(text)
	}
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
	base, baseOK := o.base.impl.(*memEfficientEngine)
	added, addedOK := o.added.impl.(*memEfficientEngine)
	if !baseOK || !addedOK {
		out := []string{}
		o.matchString(text, func(k string, _, _ int) bool { out = append(out, k); return true })
		return out
	}
	out := []string{}
	baseState, addedState := 0, 0
	for _, ch := range text {
		baseState = base.advance(baseState, ch)
		addedState = added.advance(addedState, ch)
		if base.trie.out.own[baseState] == 0 && base.trie.out.outLink[baseState] == outNone &&
			added.trie.out.own[addedState] == 0 && added.trie.out.outLink[addedState] == outNone {
			continue
		}
		out = o.appendMerged(out, &base.trie.out, baseState, &added.trie.out, addedState)
	}
	return out
}

//nolint:gocyclo // Find avoids callback overhead in the same hot scan.
func (o *overlayEngine) findLiteral(text string) []string {
	var hitBuffer [8]literalHit
	hits := o.literal.matches(text, hitBuffer[:0])
	if len(hits) == 0 {
		base := o.base.Find(text)
		if len(o.removed) == 0 {
			return base
		}
		out := base[:0]
		for _, keyword := range base {
			if _, removed := o.removed[keyword]; !removed {
				out = append(out, keyword)
			}
		}
		return out
	}
	base := o.base.impl.(*memEfficientEngine)
	outputs := &base.trie.out
	out := []string{}
	state, hitIndex := 0, 0
	for pos, ch := range text {
		if !base.bloom.skipAtRoot(state == 0, ch) {
			for {
				if next, ok := base.trie.nodes[state].next(ch); ok {
					state = next
					break
				}
				if state == 0 {
					break
				}
				state = base.trie.nodes[state].fail
			}
		}
		endByte := pos + 1
		baseState := nextOutput(outputs, state)
		for baseState != outNone || hitIndex < len(hits) && hits[hitIndex].endByte == endByte {
			baseID := int32(0)
			if baseState != outNone {
				baseID = outputs.own[baseState]
			}
			if hitIndex == len(hits) || hits[hitIndex].endByte != endByte ||
				baseID != 0 && int(outputs.runeLens[baseID]) >= hits[hitIndex].length {
				keyword := outputs.keywords[baseID]
				baseState = nextOutput(outputs, int(outputs.outLink[baseState]))
				if _, removed := o.removed[keyword]; !removed {
					out = append(out, keyword)
				}
			} else {
				out = append(out, hits[hitIndex].keyword)
				hitIndex++
			}
		}
	}
	return out
}
func (o *overlayEngine) findSet(text string) []string {
	if o.literal != nil && len(text) <= literalQueryLimit && strings.Contains(text, o.literal.prefix) {
		out := []string{}
		seen := make(map[string]struct{})
		o.matchStringLiteral(text, func(k string, _, _ int) bool {
			if _, ok := seen[k]; !ok {
				seen[k] = struct{}{}
				out = append(out, k)
			}
			return true
		})
		return out
	}
	if o.literal != nil && len(text) <= literalQueryLimit || !o.added.Contains(text) {
		base := o.base.FindSet(text)
		if len(o.removed) == 0 {
			return base
		}
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
	if o.literal != nil && len(text) <= literalQueryLimit && strings.Contains(text, o.literal.prefix) {
		out := make(map[string][]int)
		o.matchStringLiteral(text, func(k string, start, _ int) bool { out[k] = append(out[k], start); return true })
		return out
	}
	if o.literal != nil && len(text) <= literalQueryLimit || !o.added.Contains(text) {
		out := o.base.FindIndex(text)
		if len(o.removed) == 0 {
			return out
		}
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
	if len(o.removed) == 0 {
		if o.base.Contains(text) {
			return true
		}
	} else {
		found := false
		o.base.MatchString(text, func(k string, _, _ int) bool {
			if _, deleted := o.removed[k]; !deleted {
				found = true
				return false
			}
			return true
		})
		if found {
			return true
		}
	}
	if o.literal != nil && !strings.Contains(text, o.literal.prefix) {
		return false
	}
	return o.added.Contains(text)
}

func (o *overlayEngine) info() *InMemoryInfo {
	i := *o.base.Info()
	i.Keywords += o.added.Info().Keywords - len(o.removed)
	i.MemoryBytes += o.added.Info().MemoryBytes + int64(len(o.removed))*32
	i.TrieDepth = max(i.TrieDepth, o.added.Info().TrieDepth)
	return &i
}
