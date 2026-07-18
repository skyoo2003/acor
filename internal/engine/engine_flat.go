// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"sort"
	"unicode/utf8"
)

// flatNode is a trie node using a map for goto transitions (flat array pool).
type flatNode struct {
	gotoMap map[rune]int
	output  []string
	fail    int
	depth   int
}

// Compile-time check that speedEngine satisfies matchEngine.
var _ matchEngine = (*speedEngine)(nil)

// speedEngine implements matchEngine using a flat array trie with Full DFA
// transitions and compact alphabet mapping. Used by PresetSpeed.
type speedEngine struct {
	dfa       [][]int    // [state][alphabetIndex] -> nextState
	outputMap [][]string // [state] -> matched keywords
	alphabet  []rune     // sorted unique runes from all keywords
	alphabetCoder
	numStates int
	preset    Preset
}

func newSpeedEngine() *speedEngine {
	return &speedEngine{preset: PresetSpeed}
}

func (e *speedEngine) buildFromKeywords(keywords map[string]struct{}) { //nolint:gocyclo,funlen
	if len(keywords) == 0 {
		e.dfa = nil
		e.outputMap = nil
		e.numStates = 0
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
	sortRunes(e.alphabet)
	e.build(e.alphabet)

	nodes := []flatNode{
		{gotoMap: make(map[rune]int), depth: 0},
	}
	sortedKw := make([]string, 0, len(keywords))
	for kw := range keywords {
		sortedKw = append(sortedKw, kw)
	}
	sortStrings(sortedKw)
	for _, kw := range sortedKw {
		state := 0
		for _, ch := range kw {
			child, ok := nodes[state].gotoMap[ch]
			if !ok {
				child = len(nodes)
				nodes[state].gotoMap[ch] = child
				nodes = append(nodes, flatNode{gotoMap: make(map[rune]int), depth: nodes[state].depth + 1})
			}
			state = child
		}
		nodes[state].output = append(nodes[state].output, kw)
	}

	numStates := len(nodes)
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
		sortRunesFromPairs(pairs)
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
			if len(nodes[fail].output) > 0 {
				nodes[child].output = append(nodes[child].output, nodes[fail].output...)
			}
		}
	}

	e.dfa = make([][]int, numStates)
	e.outputMap = make([][]string, numStates)
	for i := range e.dfa {
		e.dfa[i] = make([]int, alphaSize)
		e.outputMap[i] = nodes[i].output
	}

	for ai, r := range e.alphabet {
		if child, ok := nodes[0].gotoMap[r]; ok {
			e.dfa[0][ai] = child
		} else {
			e.dfa[0][ai] = 0
		}
	}

	// Fill non-root rows in BFS (non-decreasing depth) order. A fail link always
	// points to a strictly shallower state, so e.dfa[fail] is already filled when
	// we copy from it. Iterating by state id is wrong: ids come from trie-insertion
	// order, so a fail link can point to a higher-id (not-yet-filled) state, leaving
	// the copied row all zeros and silently dropping matches.
	for _, s := range bfsOrder {
		for ai, r := range e.alphabet {
			if child, ok := nodes[s].gotoMap[r]; ok {
				e.dfa[s][ai] = child
			} else {
				e.dfa[s][ai] = e.dfa[nodes[s].fail][ai]
			}
		}
	}

	e.numStates = numStates
}

func (e *speedEngine) find(text string) []string {
	if e.dfa == nil {
		return nil
	}

	matched := make([]string, 0)
	state := 0

	for _, ch := range text {
		ai, ok := e.code(ch)
		if !ok {
			state = 0
			continue
		}
		state = e.dfa[state][ai]
		if len(e.outputMap[state]) > 0 {
			matched = append(matched, e.outputMap[state]...)
		}
	}

	return matched
}

func (e *speedEngine) findIndex(text string) map[string][]int {
	if e.dfa == nil {
		return nil
	}

	matched := make(map[string][]int)
	state := 0
	runeIndex := 0

	for _, ch := range text {
		ai, ok := e.code(ch)
		if !ok {
			state = 0
			runeIndex++
			continue
		}
		state = e.dfa[state][ai]
		runeIndex++
		for _, out := range e.outputMap[state] {
			startIdx := runeIndex - utf8.RuneCountInString(out)
			matched[out] = append(matched[out], startIdx)
		}
	}

	return matched
}

func (e *speedEngine) info() *InMemoryInfo {
	if e.dfa == nil {
		return &InMemoryInfo{Preset: e.preset}
	}
	var mem int64
	for _, row := range e.dfa {
		mem += int64(len(row)) * 8
	}
	for _, outs := range e.outputMap {
		mem += int64(16 + len(outs)*16)
	}
	mem += int64(len(e.alphabet)) * 16
	mem += int64(len(e.index)) * 24

	return &InMemoryInfo{
		Keywords:    e.countKeywords(),
		Nodes:       e.numStates,
		Preset:      e.preset,
		MemoryBytes: mem,
	}
}

func (e *speedEngine) countKeywords() int {
	seen := make(map[string]struct{})
	for _, outs := range e.outputMap {
		for _, out := range outs {
			seen[out] = struct{}{}
		}
	}
	return len(seen)
}

func sortRunes(runes []rune) {
	sort.Slice(runes, func(i, j int) bool { return runes[i] < runes[j] })
}

func sortRunesFromPairs(pairs []struct {
	ch    rune
	child int
}) {
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].ch < pairs[j].ch })
}

func sortStrings(s []string) {
	sort.Strings(s)
}
