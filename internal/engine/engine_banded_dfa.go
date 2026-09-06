// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"math"
)

// bandedDFA wraps a Double-Array Trie with precomputed DFA transitions for
// states at shallow depth (band). States beyond the band use standard NFA.
//
// The band is one flat slice rather than a slice per state: a [][]int cost the
// scan loop a slice-header load plus two bounds checks per character, where a
// flat []int32 indexed by a per-state offset costs two loads and one branch and
// fits in far fewer cache lines.
type bandedDFA struct {
	dat *doubleArrayTrie
	// band holds the DFA rows back to back. For a banded state s the row starts at
	// bandOff[s], so band[bandOff[s]+code] is the next state, with hasOutputBit set
	// when that state has outputs. The scan can then skip the output lookup on the
	// common miss.
	band []int32
	// bandOff[s] is s's row offset, or bandNotBanded when s is outside the band
	// and has to fall back to the NFA walk.
	bandOff   []int32
	bandDepth int
}

const (
	// bandNotBanded marks a state with no precomputed DFA row.
	bandNotBanded int32 = -1
)

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
	// depth is only used to select the band, and maxDepth reads the value recorded
	// during the build, so releasing it saves 4 bytes per state.
	e.banded.dat.depth = nil
}

func (bd *bandedDFA) buildDFABand() {
	dat := bd.dat
	if dat.size <= datRootPos+1 {
		bd.band, bd.bandOff = nil, nil
		return
	}

	alphaSize := len(dat.runes)
	bd.bandOff = make([]int32, dat.size)
	for s := range bd.bandOff {
		dat.guard.check()
		bd.bandOff[s] = bandNotBanded
	}

	// State ids share an int32 with hasOutputBit, and row offsets must index an
	// int32-addressed slice. A dictionary too large for either bound still works:
	// it runs entirely on the NFA path, which needs no packing. Every int32
	// conversion below is inside this guard.
	rows := bandRows(dat, bd.bandDepth)
	if dat.size >= int(hasOutputBit) || alphaSize == 0 || rows > math.MaxInt32/alphaSize {
		bd.band = nil
		return
	}

	// Assign row offsets in the same pass that fills them: a running counter is all
	// a separate pass would compute.
	bd.band = make([]int32, rows*alphaSize)
	off := int32(0)
	for s := datRootPos; s < dat.size; s++ {
		dat.guard.check()
		if !inBand(dat, s, bd.bandDepth) {
			continue
		}
		bd.bandOff[s] = off
		for ai := range dat.runes {
			dat.guard.check()
			next := dat.followFailByCode(s, ai)
			bd.band[off+int32(ai)] = packState(next, next < len(dat.hasOutput) && dat.hasOutput[next]) //nolint:gosec // G115: ai < alphaSize, bounded above.
		}
		off += int32(alphaSize) //nolint:gosec // G115: rows*alphaSize fits int32 per the guard.
	}
}

// inBand reports whether state s gets a precomputed DFA row.
//
// The check[s] == 0 test skips empty double-array slots (gaps in the packing).
// They are not real states and are never reached at match time, and their fail=0
// would send followFailByCode into an infinite loop.
func inBand(dat *doubleArrayTrie, s, bandDepth int) bool {
	if s != datRootPos && dat.check[s] == 0 {
		return false
	}
	return int(dat.depth[s]) <= bandDepth
}

// bandRows counts the rows inBand grants, so the caller can check the size before
// allocating.
func bandRows(dat *doubleArrayTrie, bandDepth int) int {
	rows := 0
	for s := datRootPos; s < dat.size; s++ {
		dat.guard.check()
		if inBand(dat, s, bandDepth) {
			rows++
		}
	}
	return rows
}

// step advances one character, returning the next state and whether it carries
// outputs. Callers then test one bool instead of indexing the output table on
// every character.
//
// Kept small enough to inline: it is the whole per-character cost of a scan.
func (bd *bandedDFA) step(state, code int) (int, bool) {
	if off := bd.bandOff[state]; off != bandNotBanded {
		entry := bd.band[off+int32(code)] //nolint:gosec // G115: code < alphaSize, bounded at build.
		return int(entry &^ hasOutputBit), entry&hasOutputBit != 0
	}
	dat := bd.dat
	next := dat.gotoStateByCode(state, code)
	if next == 0 {
		next = dat.followFailByCode(state, code)
	}
	if next == 0 {
		next = datRootPos
	}
	return next, next < len(dat.hasOutput) && dat.hasOutput[next]
}

func (e *balancedEngine) find(text string) []string {
	dat := e.banded.dat
	if dat.size <= datRootPos+1 {
		return []string{}
	}

	// Left nil until something matches: text matching nothing is the common case
	// for a filter, and preallocating made that path allocate for nothing.
	var matched []string
	collect := func(state int) bool {
		matched = dat.out.appendChain(matched, state)
		return true
	}

	e.scan(text, collect)
	if matched == nil {
		// Find never returns nil. A zero-length literal costs no allocation, so the
		// no-match path stays allocation-free.
		return []string{}
	}
	return matched
}

// findSet reports which keywords appear, without one entry per occurrence. The
// dedup bookkeeping lives in setCollector, shared with the other engines.
func (e *balancedEngine) findSet(text string) []string {
	dat := e.banded.dat
	if dat.size <= datRootPos+1 {
		return []string{}
	}

	var c setCollector
	e.scan(text, func(state int) bool {
		c.collectChain(&dat.out, state)
		return true
	})
	return c.result()
}

// scan walks text and calls onOutput for every state carrying keywords, stopping
// when onOutput returns false. It reports whether it stopped early, which is all
// a presence check needs, and reports no offsets, which lets the ASCII path walk
// raw bytes.
//
// onOutput is one indirect call per matching state, not per character, so it stays
// off the hot path. That is also why contains shares this loop: its callback
// captures nothing and so is not heap-boxed.
func (e *balancedEngine) scan(text string, onOutput func(state int) bool) bool {
	dat := e.banded.dat
	bd := e.banded
	state := datRootPos

	// Byte scan when the dictionary is pure ASCII. Every byte of a multibyte rune
	// is >= utf8.RuneSelf and so cannot be in the alphabet, so it resets to root
	// just as the rune scan does for a rune outside the alphabet: same states, same
	// matches, without decoding UTF-8.
	if dat.asciiOnly {
		for i := 0; i < len(text); i++ {
			code, ok := dat.codeByte(text[i])
			if !ok {
				state = datRootPos
				continue
			}
			next, hasOut := bd.step(state, code)
			state = next
			if hasOut && !onOutput(state) {
				return true
			}
		}
		return false
	}
	for _, ch := range text {
		code, ok := dat.code(ch)
		if !ok {
			state = datRootPos
			continue
		}
		next, hasOut := bd.step(state, code)
		state = next
		if hasOut && !onOutput(state) {
			return true
		}
	}
	return false
}

// contains reports presence and stops at the first hit. Its callback captures no
// variables, so it allocates nothing; see Engine.Contains for the generic path.
func (e *balancedEngine) contains(text string) bool {
	if e.banded.dat.size <= datRootPos+1 {
		return false
	}
	return e.scan(text, func(int) bool { return false })
}

func (e *balancedEngine) findIndex(text string) map[string][]int {
	dat := e.banded.dat
	if dat.size <= datRootPos+1 {
		return map[string][]int{}
	}
	bd := e.banded

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

		next, out := bd.step(state, code)
		state = next
		runeIndex++
		if !out {
			continue
		}
		dat.out.indexChain(matched, state, runeIndex)
	}

	return matched
}

// matchString is matchStream with the runes read straight off the string. The
// loop body deliberately duplicates matchStream's instead of sharing a helper
// that takes a step closure, which would reintroduce the indirect call per rune.
func (e *balancedEngine) matchString(text string, emit func(keyword string, start, end int) bool) {
	dat := e.banded.dat
	if dat.size <= datRootPos+1 {
		return
	}
	bd := e.banded

	state := datRootPos
	runeIndex := 0
	for _, ch := range text {
		code, ok := dat.code(ch)
		if !ok {
			state = datRootPos
			runeIndex++
			continue
		}

		next, hasOut := bd.step(state, code)
		state = next
		runeIndex++
		if hasOut && !dat.out.emitChain(state, runeIndex, emit) {
			return
		}
	}
}

func (e *balancedEngine) matchStream(next func() (rune, bool), emit func(keyword string, start, end int) bool) {
	dat := e.banded.dat
	if dat.size <= datRootPos+1 {
		return
	}
	bd := e.banded

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

		nx, hasOut := bd.step(state, code)
		state = nx
		runeIndex++
		if !hasOut {
			continue
		}
		if !dat.out.emitChain(state, runeIndex, emit) {
			return
		}
	}
}

func (e *balancedEngine) info() *InMemoryInfo {
	dat := e.banded.dat
	if dat.size <= datRootPos+1 {
		return &InMemoryInfo{Preset: e.preset}
	}
	mem := dat.memoryBytes()
	mem += int64(len(e.banded.band)) * 4
	mem += int64(len(e.banded.bandOff)) * 4
	return &InMemoryInfo{
		Keywords:    dat.out.keywordCount(),
		Nodes:       dat.size - datRootPos,
		Preset:      e.preset,
		MemoryBytes: mem,
		TrieDepth:   dat.maxDepth(),
	}
}
