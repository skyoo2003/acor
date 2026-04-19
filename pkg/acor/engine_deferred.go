// SPDX-License-Identifier: Apache-2.0

package acor

// Compile-time check that ultimateEngine satisfies matchEngine.
var _ matchEngine = (*ultimateEngine)(nil)

// ultimateEngine implements matchEngine combining SIMD pre-filter, DAT with
// Banded DFA. Used by PresetUltimate.
type ultimateEngine struct {
	banded  *bandedDFA
	scanner *simdScanner
	preset  Preset
}

func newUltimateEngine(bandDepth int) *ultimateEngine {
	return &ultimateEngine{
		banded: &bandedDFA{
			dat:       newDoubleArrayTrie(),
			bandDepth: bandDepth,
		},
		preset: PresetUltimate,
	}
}

func (e *ultimateEngine) buildFromKeywords(keywords map[string]struct{}) {
	e.banded.dat.buildFromKeywords(keywords)
	e.banded.runeMap = e.banded.dat.runeMap
	e.banded.runes = e.banded.dat.runes
	e.banded.buildDFABand()

	e.scanner = newSIMDScanner(keywords)
}

func (e *ultimateEngine) find(text string) []string {
	if e.banded.dat.size <= datRootPos+1 {
		return nil
	}

	matched := make([]string, 0)
	state := datRootPos

	for _, ch := range text {
		if state == datRootPos && !e.scanner.mightMatch(ch) {
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

func (e *ultimateEngine) findIndex(text string) map[string][]int {
	if e.banded.dat.size <= datRootPos+1 {
		return nil
	}

	matched := make(map[string][]int)
	state := datRootPos
	runeIndex := 0

	for _, ch := range text {
		if state == datRootPos && !e.scanner.mightMatch(ch) {
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

func (e *ultimateEngine) info() *InMemoryInfo {
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
	if e.scanner != nil {
		mem += e.scanner.memoryBytes()
	}
	return &InMemoryInfo{
		Keywords:    dat.keywordCount(),
		Nodes:       dat.size - datRootPos,
		Preset:      e.preset,
		MemoryBytes: mem,
		TrieDepth:   dat.maxDepth(),
	}
}
