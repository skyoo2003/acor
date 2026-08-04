// SPDX-License-Identifier: Apache-2.0

package engine

import "unicode/utf8"

// bandedDFA wraps a Double-Array Trie with precomputed DFA transitions for
// states at shallow depth (band). States beyond the band use standard NFA.
type bandedDFA struct {
	dat       *doubleArrayTrie
	dfaBand   [][]int
	bandDepth int
}

// Compile-time check that balancedEngine satisfies matchEngine.
var _ matchEngine = (*balancedEngine)(nil)

// balancedEngine implements matchEngine using a DAT with Banded DFA and output
// link compression. Used by PresetBalanced.
type balancedEngine struct {
	banded *bandedDFA
	preset Preset
}

func newBalancedEngine(bandDepth int) *balancedEngine {
	return &balancedEngine{
		banded: &bandedDFA{
			dat:       newDoubleArrayTrie(),
			bandDepth: bandDepth,
		},
		preset: PresetBalanced,
	}
}

func (e *balancedEngine) buildFromKeywords(keywords map[string]struct{}) {
	e.banded.dat.buildFromKeywords(keywords)
	e.banded.buildDFABand()
}

func (bd *bandedDFA) buildDFABand() {
	dat := bd.dat
	if dat.size <= datRootPos+1 {
		bd.dfaBand = nil
		return
	}

	alphaSize := len(dat.runes)
	bd.dfaBand = make([][]int, dat.size)

	for s := datRootPos; s < dat.size; s++ {
		// Skip empty double-array slots (gaps in the packing): they are not real
		// states, are never reached at match time, and have fail=0 — which would
		// send followFailByCode into an infinite loop walking fail[0]=0.
		if s != datRootPos && dat.check[s] == 0 {
			continue
		}
		if dat.depth[s] > bd.bandDepth {
			continue
		}
		bd.dfaBand[s] = make([]int, alphaSize)
		for ai := range dat.runes {
			bd.dfaBand[s][ai] = dat.followFailByCode(s, ai)
		}
	}
}

func (e *balancedEngine) find(text string) []string {
	dat := e.banded.dat
	if dat.size <= datRootPos+1 {
		return []string{}
	}
	band := e.banded.dfaBand

	matched := make([]string, 0)
	state := datRootPos

	for _, ch := range text {
		code, ok := dat.code(ch)
		if !ok {
			state = datRootPos
			continue
		}

		if band[state] != nil {
			state = band[state][code]
		} else {
			if next := dat.gotoStateByCode(state, code); next != 0 {
				state = next
			} else {
				state = dat.followFailByCode(state, code)
			}
		}
		if state == 0 {
			state = datRootPos
		}

		if state < len(dat.output) && len(dat.output[state]) > 0 {
			matched = append(matched, dat.output[state]...)
		}
	}

	return matched
}

func (e *balancedEngine) findIndex(text string) map[string][]int {
	dat := e.banded.dat
	if dat.size <= datRootPos+1 {
		return map[string][]int{}
	}
	band := e.banded.dfaBand

	matched := make(map[string][]int)
	state := datRootPos
	runeIndex := 0

	for _, ch := range text {
		code, ok := dat.code(ch)
		if !ok {
			state = datRootPos
			runeIndex++
			continue
		}

		if band[state] != nil {
			state = band[state][code]
		} else {
			if next := dat.gotoStateByCode(state, code); next != 0 {
				state = next
			} else {
				state = dat.followFailByCode(state, code)
			}
		}
		if state == 0 {
			state = datRootPos
		}

		runeIndex++
		if state < len(dat.output) {
			for _, out := range dat.output[state] {
				startIdx := runeIndex - utf8.RuneCountInString(out)
				matched[out] = append(matched[out], startIdx)
			}
		}
	}

	return matched
}

func (e *balancedEngine) matchStream(next func() (rune, bool), emit func(Match) bool) {
	dat := e.banded.dat
	if dat.size <= datRootPos+1 {
		return
	}
	band := e.banded.dfaBand

	state := datRootPos
	runeIndex := 0

	for {
		ch, ok := next()
		if !ok {
			return
		}

		code, ok := dat.code(ch)
		if !ok {
			state = datRootPos
			runeIndex++
			continue
		}

		if band[state] != nil {
			state = band[state][code]
		} else {
			if nx := dat.gotoStateByCode(state, code); nx != 0 {
				state = nx
			} else {
				state = dat.followFailByCode(state, code)
			}
		}
		if state == 0 {
			state = datRootPos
		}

		runeIndex++
		if state < len(dat.output) {
			for _, out := range dat.output[state] {
				start := runeIndex - utf8.RuneCountInString(out)
				if !emit(Match{Keyword: out, Start: start, End: runeIndex}) {
					return
				}
			}
		}
	}
}

func (e *balancedEngine) info() *InMemoryInfo {
	dat := e.banded.dat
	if dat.size <= datRootPos+1 {
		return &InMemoryInfo{Preset: e.preset}
	}
	mem := dat.memoryBytes()
	for _, row := range e.banded.dfaBand {
		if row != nil {
			mem += int64(len(row)) * 8
		}
	}
	return &InMemoryInfo{
		Keywords:    dat.keywordCount(),
		Nodes:       dat.size - datRootPos,
		Preset:      e.preset,
		MemoryBytes: mem,
		TrieDepth:   dat.maxDepth(),
	}
}
