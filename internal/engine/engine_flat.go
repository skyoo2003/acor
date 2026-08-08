// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"cmp"
	"slices"
)

// flatNode is a trie node using a map for goto transitions (flat array pool).
//
// ownID is the id of the single keyword ending at this node, or 0 for none: a
// trie node is reached by exactly one string, so at most one keyword can end
// there.
type flatNode struct {
	gotoMap map[rune]int
	ownID   int32
	fail    int
	outLink int32
	depth   int
}

// Compile-time check that speedEngine satisfies matchEngine.
var _ matchEngine = (*speedEngine)(nil)

// speedEngine implements matchEngine using a flat array trie with Full DFA
// transitions and compact alphabet mapping. Used by PresetSpeed.
type speedEngine struct {
	// dfa is the transition table flattened to state*alphaSize+alphabetIndex. A
	// [][]int cost the scan a slice-header load and two bounds checks per
	// character, where one flat []int32 costs a single indexed load. Each entry
	// carries hasOutputBit so the scan skips the output lookup on a miss.
	dfa       []int32
	alphaSize int
	// out maps matching states to the keywords they report; see outputs.
	out      outputs
	alphabet []rune // sorted unique runes from all keywords
	alphabetCoder
	numStates int
	// trieDepth is the deepest trie level, recorded during the build. Info reported
	// zero for this preset before, because the depths lived on flatNode and were
	// dropped with it.
	trieDepth int
	preset    Preset
}

func newSpeedEngine() *speedEngine {
	return &speedEngine{preset: PresetSpeed}
}

func (e *speedEngine) buildFromKeywords(keywords map[string]struct{}) { //nolint:gocyclo,funlen
	if len(keywords) == 0 {
		e.dfa = nil
		e.out = outputs{}
		e.numStates, e.trieDepth = 0, 0
		return
	}

	runeSet := make(map[rune]struct{})
	for kw := range keywords {
		for _, ch := range kw {
			runeSet[ch] = struct{}{}
		}
	}
	e.alphabet = make([]rune, 0, len(runeSet))
	for r := range runeSet {
		e.alphabet = append(e.alphabet, r)
	}
	slices.Sort(e.alphabet)
	e.build(e.alphabet)

	nodes := []flatNode{
		{gotoMap: make(map[rune]int), depth: 0, outLink: outNone},
	}
	sortedKw := make([]string, 0, len(keywords))
	for kw := range keywords {
		sortedKw = append(sortedKw, kw)
	}
	slices.Sort(sortedKw)
	outs := outputs{}
	for _, kw := range sortedKw {
		// An empty keyword would end at the root, where ownID == 0 already means
		// "no keyword". The public API rejects empty keywords; skipping keeps this
		// engine correct on its own.
		if kw == "" {
			continue
		}
		state := 0
		for _, ch := range kw {
			child, ok := nodes[state].gotoMap[ch]
			if !ok {
				child = len(nodes)
				nodes[state].gotoMap[ch] = child
				nodes = append(nodes, flatNode{
					gotoMap: make(map[rune]int),
					depth:   nodes[state].depth + 1,
					outLink: outNone,
				})
			}
			state = child
		}
		nodes[state].ownID = outs.assignID(kw)
	}

	numStates := len(nodes)
	requirePackableStateCount(numStates)
	alphaSize := len(e.alphabet)

	queue := make([]int, 0)
	// bfsOrder records non-root states in BFS (non-decreasing depth) order, used
	// below to fill the DFA so that e.dfa[fail] is always populated first.
	bfsOrder := make([]int, 0, numStates)
	sortedChildren := func(gotoMap map[rune]int) []struct {
		ch    rune
		child int
	} {
		pairs := make([]struct {
			ch    rune
			child int
		}, 0, len(gotoMap))
		for ch, child := range gotoMap {
			pairs = append(pairs, struct {
				ch    rune
				child int
			}{ch, child})
		}
		slices.SortFunc(pairs, func(a, b struct {
			ch    rune
			child int
		}) int {
			return cmp.Compare(a.ch, b.ch)
		})
		return pairs
	}

	for _, pair := range sortedChildren(nodes[0].gotoMap) {
		nodes[pair.child].fail = 0
		queue = append(queue, pair.child)
	}

	for len(queue) > 0 {
		state := queue[0]
		queue = queue[1:]
		bfsOrder = append(bfsOrder, state)

		for _, pair := range sortedChildren(nodes[state].gotoMap) {
			ch := pair.ch
			child := pair.child
			queue = append(queue, child)

			// Walk failure links to the deepest state that has a `ch` child, then
			// apply goto(fail, ch) exactly once below. Assigning inside the loop
			// and re-applying after would double-apply goto and can point a state's
			// fail link at itself (e.g. keywords {a,aa,aaa}), corrupting the DFA.
			fail := nodes[state].fail
			for fail != 0 {
				if _, ok := nodes[fail].gotoMap[ch]; ok {
					break
				}
				fail = nodes[fail].fail
			}
			if next, ok := nodes[fail].gotoMap[ch]; ok {
				fail = next
			}
			nodes[child].fail = fail
			// Link to the nearest keyword-carrying state on the failure path instead
			// of copying its outputs in. fail is strictly shallower and BFS visits by
			// depth, so nodes[fail].outLink is already final here.
			if nodes[fail].ownID != 0 {
				nodes[child].outLink = int32(fail) //nolint:gosec // G115: state ids pass requirePackableStateCount above.
			} else {
				nodes[child].outLink = nodes[fail].outLink
			}
		}
	}

	e.alphaSize = alphaSize
	e.dfa = make([]int32, numStates*alphaSize)
	outs.own = make([]int32, numStates)
	outs.outLink = make([]int32, numStates)
	for i := range nodes {
		outs.own[i] = nodes[i].ownID
		outs.outLink[i] = nodes[i].outLink
	}
	e.out = outs

	// enc packs a target state with the flag saying whether it carries outputs, so
	// the scan loop learns both from one load. Copying a fail row copies the flag
	// with it, which is correct: the flag describes the target state. A state
	// carries keywords when it terminates one or its output chain reaches one.
	//
	enc := func(state int) int32 {
		return packState(state, e.out.own[state] != 0 || e.out.outLink[state] != outNone)
	}

	for ai, r := range e.alphabet {
		if child, ok := nodes[0].gotoMap[r]; ok {
			e.dfa[ai] = enc(child)
		} else {
			e.dfa[ai] = enc(0)
		}
	}

	// Fill non-root rows in BFS (non-decreasing depth) order. A fail link points to
	// a strictly shallower state, so the fail row is already filled when we copy
	// from it. Iterating by state id is wrong: ids come from trie-insertion order,
	// so a fail link can point to a higher-id row that is still all zeros, which
	// silently drops matches.
	for _, s := range bfsOrder {
		row := s * alphaSize
		failRow := nodes[s].fail * alphaSize
		for ai, r := range e.alphabet {
			if child, ok := nodes[s].gotoMap[r]; ok {
				e.dfa[row+ai] = enc(child)
			} else {
				e.dfa[row+ai] = e.dfa[failRow+ai]
			}
		}
	}

	e.numStates = numStates
	// Reset-and-recompute, not max-with-previous: Build reconstructs the automaton,
	// so a rebuild from shorter keywords must report the shorter depth.
	e.trieDepth = 0
	for i := range nodes {
		if nodes[i].depth > e.trieDepth {
			e.trieDepth = nodes[i].depth
		}
	}
}

// find and findSet write out both the ASCII and rune loops instead of sharing a
// scan(text, onOutput) helper the way balancedEngine does. That refactor was
// measured: capturing the result slice in a closure boxes it on the heap, so every
// match writes through a pointer, and PresetSpeed's ASCII match path lost 12-17%
// (2,979 -> 3,483 ns at 1000 keywords). It won on multibyte and no-match text,
// but not by enough to pay for the primary case.

// The two loops make this function look branch-heavy to gocyclo; see the note
// above for why they are not shared.
func (e *speedEngine) find(text string) []string { //nolint:gocyclo
	if e.dfa == nil {
		return []string{}
	}

	// Left nil until something matches: text matching nothing is the common case
	// for a filter and should not allocate for a hit that never happened.
	var matched []string
	state := 0
	alpha := e.alphaSize

	// Byte scan when the dictionary is pure ASCII. Every byte of a multibyte rune
	// is >= utf8.RuneSelf and so cannot be in the alphabet, so it resets to root
	// just as the rune scan does. Find reports no offsets, so the rune/byte index
	// distinction never surfaces.
	if e.asciiOnly {
		for i := 0; i < len(text); i++ {
			ai, ok := e.codeByte(text[i])
			if !ok {
				state = 0
				continue
			}
			v := e.dfa[state*alpha+ai]
			state = int(v &^ hasOutputBit)
			if v&hasOutputBit == 0 {
				continue
			}
			matched = e.out.appendChain(matched, state)
		}
		if matched == nil {
			return []string{}
		}
		return matched
	}

	for _, ch := range text {
		ai, ok := e.code(ch)
		if !ok {
			state = 0
			continue
		}
		v := e.dfa[state*alpha+ai]
		state = int(v &^ hasOutputBit)
		if v&hasOutputBit == 0 {
			continue
		}
		matched = e.out.appendChain(matched, state)
	}

	if matched == nil {
		return []string{}
	}
	return matched
}

// findSet reports which keywords appear, without one entry per occurrence. The
// dedup bookkeeping lives in setCollector, shared with the other engines.
func (e *speedEngine) findSet(text string) []string {
	if e.dfa == nil {
		return []string{}
	}

	var c setCollector
	state := 0
	alpha := e.alphaSize

	if e.asciiOnly {
		for i := 0; i < len(text); i++ {
			ai, ok := e.codeByte(text[i])
			if !ok {
				state = 0
				continue
			}
			v := e.dfa[state*alpha+ai]
			state = int(v &^ hasOutputBit)
			if v&hasOutputBit != 0 {
				c.collectChain(&e.out, state)
			}
		}
	} else {
		for _, ch := range text {
			ai, ok := e.code(ch)
			if !ok {
				state = 0
				continue
			}
			v := e.dfa[state*alpha+ai]
			state = int(v &^ hasOutputBit)
			if v&hasOutputBit != 0 {
				c.collectChain(&e.out, state)
			}
		}
	}

	return c.result()
}

// contains reports presence and stops at the first hit. It is the byte/rune scan
// with the output lookup dropped, since hasOutputBit already says whether the
// state carries keywords.
func (e *speedEngine) contains(text string) bool {
	if e.dfa == nil {
		return false
	}

	state := 0
	alpha := e.alphaSize

	if e.asciiOnly {
		for i := 0; i < len(text); i++ {
			ai, ok := e.codeByte(text[i])
			if !ok {
				state = 0
				continue
			}
			v := e.dfa[state*alpha+ai]
			if v&hasOutputBit != 0 {
				return true
			}
			// Reached only when the bit is clear, so v needs no masking.
			state = int(v)
		}
		return false
	}

	for _, ch := range text {
		ai, ok := e.code(ch)
		if !ok {
			state = 0
			continue
		}
		v := e.dfa[state*alpha+ai]
		if v&hasOutputBit != 0 {
			return true
		}
		state = int(v)
	}
	return false
}

func (e *speedEngine) findIndex(text string) map[string][]int {
	if e.dfa == nil {
		return map[string][]int{}
	}

	matched := make(map[string][]int)
	state := 0
	runeIndex := 0
	alpha := e.alphaSize

	for _, ch := range text {
		ai, ok := e.code(ch)
		if !ok {
			state = 0
			runeIndex++
			continue
		}
		v := e.dfa[state*alpha+ai]
		state = int(v &^ hasOutputBit)
		runeIndex++
		if v&hasOutputBit == 0 {
			continue
		}
		e.out.indexChain(matched, state, runeIndex)
	}

	return matched
}

// matchString is matchStream over an in-memory string; see the matchEngine
// interface for why the loop is duplicated rather than shared through a closure.
func (e *speedEngine) matchString(text string, emit func(keyword string, start, end int) bool) {
	if e.dfa == nil {
		return
	}

	state := 0
	runeIndex := 0
	alpha := e.alphaSize

	for _, ch := range text {
		ai, ok := e.code(ch)
		if !ok {
			state = 0
			runeIndex++
			continue
		}
		v := e.dfa[state*alpha+ai]
		state = int(v &^ hasOutputBit)
		runeIndex++
		if v&hasOutputBit == 0 {
			continue
		}
		if !e.out.emitChain(state, runeIndex, emit) {
			return
		}
	}
}

func (e *speedEngine) matchStream(next func() (rune, bool), emit func(keyword string, start, end int) bool) {
	if e.dfa == nil {
		return
	}

	state := 0
	runeIndex := 0
	alpha := e.alphaSize

	for {
		ch, ok := next()
		if !ok {
			return
		}
		ai, ok := e.code(ch)
		if !ok {
			state = 0
			runeIndex++
			continue
		}
		v := e.dfa[state*alpha+ai]
		state = int(v &^ hasOutputBit)
		runeIndex++
		if v&hasOutputBit == 0 {
			continue
		}
		if !e.out.emitChain(state, runeIndex, emit) {
			return
		}
	}
}

func (e *speedEngine) info() *InMemoryInfo {
	if e.dfa == nil {
		return &InMemoryInfo{Preset: e.preset}
	}
	mem := int64(len(e.dfa)) * 4
	mem += e.out.memoryBytes()
	mem += int64(len(e.alphabet)) * 16
	mem += int64(len(e.index)) * 24

	return &InMemoryInfo{
		Keywords:    e.out.keywordCount(),
		Nodes:       e.numStates,
		Preset:      e.preset,
		MemoryBytes: mem,
		TrieDepth:   e.trieDepth,
	}
}
