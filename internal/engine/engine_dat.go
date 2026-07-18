// SPDX-License-Identifier: Apache-2.0

package engine

import "unicode/utf8"

// doubleArrayTrie implements a Double-Array Trie using base[] and check[] arrays.
// Provides O(1) state transitions with near hash-map memory efficiency.
// Used by PresetBalanced and PresetUltimate.
//
// Position 0 is unused (sentinel); root is at position 1. This avoids the
// ambiguity where check[pos]=0 could mean either "empty" or "parent is state 0".
type doubleArrayTrie struct {
	base    []int
	check   []int
	fail    []int
	output  [][]string
	depth   []int
	size    int
	cap     int
	runeMap map[rune]int
	// asciiCode is a direct-index fast path for the runeMap: for an ASCII rune r
	// in the alphabet, asciiCode[r] = code+1 (0 means "not in alphabet"). ASCII is
	// the common case, so this avoids a map hash on nearly every character.
	asciiCode [128]int32
	runes     []rune
}

const (
	datInitialCap = 1024
	datRootPos    = 1
)

func newDoubleArrayTrie() *doubleArrayTrie {
	return &doubleArrayTrie{
		base:  make([]int, datInitialCap),
		check: make([]int, datInitialCap),
		fail:  make([]int, datInitialCap),
		depth: make([]int, datInitialCap),
		cap:   datInitialCap,
		size:  datRootPos + 1,
	}
}

func (dat *doubleArrayTrie) expand() {
	newCap := dat.cap * 2
	newBase := make([]int, newCap)
	newCheck := make([]int, newCap)
	newFail := make([]int, newCap)
	newDepth := make([]int, newCap)
	copy(newBase, dat.base)
	copy(newCheck, dat.check)
	copy(newFail, dat.fail)
	copy(newDepth, dat.depth)
	dat.base = newBase
	dat.check = newCheck
	dat.fail = newFail
	dat.depth = newDepth
	dat.cap = newCap
}

func (dat *doubleArrayTrie) ensureCapacity(needed int) {
	for needed >= dat.cap {
		dat.expand()
	}
}

func (dat *doubleArrayTrie) buildFromKeywords(keywords map[string]struct{}) { //nolint:gocyclo,funlen
	dat.base = make([]int, datInitialCap)
	dat.check = make([]int, datInitialCap)
	dat.fail = make([]int, datInitialCap)
	dat.depth = make([]int, datInitialCap)
	dat.cap = datInitialCap
	dat.size = datRootPos + 1
	dat.output = nil

	runeSet := make(map[rune]struct{})
	for kw := range keywords {
		for _, ch := range kw {
			runeSet[ch] = struct{}{}
		}
	}
	dat.runes = make([]rune, 0, len(runeSet))
	for r := range runeSet {
		dat.runes = append(dat.runes, r)
	}
	sortRunes(dat.runes)
	dat.runeMap = make(map[rune]int, len(dat.runes))
	dat.asciiCode = [128]int32{}
	for i, r := range dat.runes {
		dat.runeMap[r] = i
		if r < utf8.RuneSelf {
			dat.asciiCode[r] = int32(i) + 1
		}
	}

	tmpChildren := make(map[int]map[rune]int)
	tmpOutput := make(map[int][]string)
	nextID := 1
	tmpChildren[0] = make(map[rune]int)

	for kw := range keywords {
		cur := 0
		for _, ch := range kw {
			if _, ok := tmpChildren[cur][ch]; !ok {
				if tmpChildren[cur] == nil {
					tmpChildren[cur] = make(map[rune]int)
				}
				tmpChildren[cur][ch] = nextID
				tmpChildren[nextID] = make(map[rune]int)
				nextID++
			}
			cur = tmpChildren[cur][ch]
		}
		tmpOutput[cur] = append(tmpOutput[cur], kw)
	}

	dat.ensureCapacity(nextID + 2)
	// Position 0 is unused sentinel; root is at position 1.
	dat.check[0] = -1
	dat.depth[datRootPos] = 0

	// datPos maps temp trie node IDs to their DAT array positions.
	datPos := make([]int, nextID)
	datPos[0] = datRootPos

	queue := make([]int, 0, nextID)
	queue = append(queue, 0)

	for len(queue) > 0 {
		parent := queue[0]
		queue = queue[1:]

		children := tmpChildren[parent]
		if len(children) == 0 {
			continue
		}

		codes := make([]int, 0, len(children))
		for ch := range children {
			codes = append(codes, dat.runeMap[ch])
		}

		base := dat.findBase(codes)
		dat.base[datPos[parent]] = base

		for ch, childID := range children {
			code := dat.runeMap[ch]
			pos := base + code
			dat.ensureCapacity(pos + 1)

			dat.check[pos] = datPos[parent]
			dat.depth[pos] = dat.depth[datPos[parent]] + 1
			datPos[childID] = pos

			if outs, ok := tmpOutput[childID]; ok && len(outs) > 0 {
				for pos >= len(dat.output) {
					dat.output = append(dat.output, nil)
				}
				dat.output[pos] = outs
			}

			if pos >= dat.size {
				dat.size = pos + 1
			}

			queue = append(queue, childID)
		}
	}

	dat.base = dat.base[:dat.size]
	dat.check = dat.check[:dat.size]
	dat.fail = dat.fail[:dat.size]
	dat.depth = dat.depth[:dat.size]

	if len(dat.output) < dat.size {
		newOut := make([][]string, dat.size)
		copy(newOut, dat.output)
		dat.output = newOut
	}

	dat.computeFailLinks()
}

func (dat *doubleArrayTrie) findBase(codes []int) int {
	if len(codes) == 0 {
		return 1
	}
	minCode := codes[0]
	for _, c := range codes[1:] {
		if c < minCode {
			minCode = c
		}
	}

	// Start from a base that places minCode at position datRootPos+1 (skip sentinel).
	for base := (datRootPos + 1) - minCode; ; base++ {
		conflict := false
		for _, code := range codes {
			pos := base + code
			if pos >= dat.cap {
				dat.expand()
			}
			if pos < 0 || pos == 0 {
				conflict = true
				break
			}
			if dat.check[pos] != 0 {
				conflict = true
				break
			}
		}
		if !conflict {
			return base
		}
	}
}

func (dat *doubleArrayTrie) computeFailLinks() {
	queue := make([]int, 0, dat.size)

	// Root's direct children: check[pos] == datRootPos.
	for i := datRootPos + 1; i < dat.size; i++ {
		if dat.check[i] == datRootPos {
			dat.fail[i] = datRootPos
			queue = append(queue, i)
		}
	}
	dat.fail[datRootPos] = datRootPos

	for len(queue) > 0 {
		state := queue[0]
		queue = queue[1:]

		for _, r := range dat.runes {
			next := dat.gotoState(state, r)
			if next == 0 {
				continue
			}
			queue = append(queue, next)

			f := dat.fail[state]
			for f != datRootPos && dat.gotoState(f, r) == 0 {
				f = dat.fail[f]
			}
			failState := dat.gotoState(f, r)
			if failState == 0 {
				failState = datRootPos
			}
			dat.fail[next] = failState

			if len(dat.output[dat.fail[next]]) > 0 {
				dat.output[next] = append(dat.output[next], dat.output[dat.fail[next]]...)
			}
		}
	}
}

// code resolves a rune to its alphabet index via the ASCII fast path, falling
// back to the runeMap for non-ASCII runes. ok is false if ch is not in the
// alphabet.
func (dat *doubleArrayTrie) code(ch rune) (int, bool) {
	if ch < utf8.RuneSelf {
		c := dat.asciiCode[ch]
		return int(c) - 1, c != 0
	}
	c, ok := dat.runeMap[ch]
	return c, ok
}

// gotoStateByCode is gotoState with the rune already resolved to its alphabet
// index, so callers in the hot loop avoid re-resolving the rune on every fail hop.
func (dat *doubleArrayTrie) gotoStateByCode(state, code int) int {
	pos := dat.base[state] + code
	if pos < 0 || pos >= dat.size {
		return 0
	}
	if dat.check[pos] != state {
		return 0
	}
	return pos
}

func (dat *doubleArrayTrie) gotoState(state int, ch rune) int {
	code, ok := dat.code(ch)
	if !ok {
		return 0
	}
	return dat.gotoStateByCode(state, code)
}

func (dat *doubleArrayTrie) followFailByCode(state, code int) int {
	for state != datRootPos && dat.gotoStateByCode(state, code) == 0 {
		state = dat.fail[state]
	}
	next := dat.gotoStateByCode(state, code)
	if next == 0 {
		next = datRootPos
	}
	return next
}

func (dat *doubleArrayTrie) memoryBytes() int64 {
	return int64(len(dat.base)+len(dat.check)+len(dat.fail)+len(dat.depth)) * 8
}

func (dat *doubleArrayTrie) maxDepth() int {
	d := 0
	for _, v := range dat.depth {
		if v > d {
			d = v
		}
	}
	return d
}

func (dat *doubleArrayTrie) keywordCount() int {
	seen := make(map[string]struct{})
	for _, outs := range dat.output {
		for _, o := range outs {
			seen[o] = struct{}{}
		}
	}
	return len(seen)
}
