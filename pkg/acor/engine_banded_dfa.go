// SPDX-License-Identifier: Apache-2.0

package acor

// bandedDFA wraps a Double-Array Trie with precomputed DFA transitions for
// states at shallow depth (band). States beyond the band use standard NFA.
type bandedDFA struct {
	dat       *doubleArrayTrie
	dfaBand   [][]int
	bandDepth int
	runeMap   map[rune]int
	runes     []rune
}

// Compile-time check that balancedEngine satisfies matchEngine.
var _ matchEngine = (*balancedEngine)(nil)

// balancedEngine implements matchEngine using a DAT with Banded DFA and output
// link compression. Used by PresetBalanced. PresetUltimate uses the same engine
// with an added root-state pre-filter (see newUltimateEngine).
type balancedEngine struct {
	banded *bandedDFA
	bloom  *bloomFilter // root-state first-rune pre-filter; nil disables it (Balanced)
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

// newUltimateEngine returns a balancedEngine with a root-state first-rune
// pre-filter (a Bloom filter) that skips trie work for characters which cannot
// start any keyword. The filter never yields false negatives, so match results
// are identical to PresetBalanced.
func newUltimateEngine(bandDepth int) *balancedEngine {
	e := newBalancedEngine(bandDepth)
	e.preset = PresetUltimate
	return e
}

func (e *balancedEngine) buildFromKeywords(keywords map[string]struct{}) {
	e.banded.dat.buildFromKeywords(keywords)
	e.banded.runeMap = e.banded.dat.runeMap
	e.banded.runes = e.banded.dat.runes
	e.banded.buildDFABand()

	if e.preset == PresetUltimate {
		e.bloom = buildFirstRuneBloom(keywords)
	}
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
		if dat.depth[s] > bd.bandDepth {
			continue
		}
		bd.dfaBand[s] = make([]int, alphaSize)
		for ai, r := range dat.runes {
			child := dat.gotoState(s, r)
			if child != 0 {
				bd.dfaBand[s][ai] = child
			} else {
				f := dat.fail[s]
				depth := 0
				for f != datRootPos && depth < dat.size && dat.gotoState(f, r) == 0 {
					f = dat.fail[f]
					depth++
				}
				result := dat.gotoState(f, r)
				if result == 0 {
					result = datRootPos
				}
				bd.dfaBand[s][ai] = result
			}
		}
	}
}

func (e *balancedEngine) find(text string) []string {
	if e.banded.dat.size <= datRootPos+1 {
		return nil
	}

	matched := make([]string, 0)
	state := datRootPos

	for _, ch := range text {
		if e.bloom != nil && e.bloom.skipAtRoot(state == datRootPos, ch) {
			continue
		}

		ai, ok := e.banded.runeMap[ch]
		if !ok {
			state = datRootPos
			continue
		}

		if e.banded.dfaBand[state] != nil {
			state = e.banded.dfaBand[state][ai]
		} else {
			if next := e.banded.dat.gotoState(state, ch); next != 0 {
				state = next
			} else {
				state = e.banded.dat.followFail(state, ch)
			}
		}
		if state == 0 {
			state = datRootPos
		}

		if state < len(e.banded.dat.output) && len(e.banded.dat.output[state]) > 0 {
			matched = append(matched, e.banded.dat.output[state]...)
		}
	}

	return matched
}

func (e *balancedEngine) findIndex(text string) map[string][]int {
	if e.banded.dat.size <= datRootPos+1 {
		return nil
	}

	matched := make(map[string][]int)
	state := datRootPos
	runeIndex := 0

	for _, ch := range text {
		if e.bloom != nil && e.bloom.skipAtRoot(state == datRootPos, ch) {
			runeIndex++
			continue
		}

		ai, ok := e.banded.runeMap[ch]
		if !ok {
			state = datRootPos
			runeIndex++
			continue
		}

		if e.banded.dfaBand[state] != nil {
			state = e.banded.dfaBand[state][ai]
		} else {
			if next := e.banded.dat.gotoState(state, ch); next != 0 {
				state = next
			} else {
				state = e.banded.dat.followFail(state, ch)
			}
		}
		if state == 0 {
			state = datRootPos
		}

		runeIndex++
		if state < len(e.banded.dat.output) {
			for _, out := range e.banded.dat.output[state] {
				startIdx := runeIndex - len([]rune(out))
				matched[out] = append(matched[out], startIdx)
			}
		}
	}

	return matched
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
	if e.bloom != nil {
		mem += e.bloom.memoryBytes()
	}
	return &InMemoryInfo{
		Keywords:    dat.keywordCount(),
		Nodes:       dat.size - datRootPos,
		Preset:      e.preset,
		MemoryBytes: mem,
		TrieDepth:   dat.maxDepth(),
	}
}
