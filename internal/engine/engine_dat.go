// SPDX-License-Identifier: Apache-2.0

package engine

import "slices"

// doubleArrayTrie implements a Double-Array Trie using base[] and check[] arrays.
// Provides O(1) state transitions with near hash-map memory efficiency.
// Used by PresetBalanced.
//
// Position 0 is unused (sentinel); root is at position 1. This avoids the
// ambiguity where check[pos]=0 could mean either "empty" or "parent is state 0".
type doubleArrayTrie struct {
	// base, check and fail are int32 rather than int: three arrays cost 12 bytes per
	// state instead of 24. Overflowing int32 would need over 8 GiB of double-array,
	// so the allocation fails first.
	base  []int32
	check []int32
	fail  []int32
	// out maps matching states to the keywords they report; see outputs.
	out outputs
	// hasOutput[s] reports whether s or its output chain carries a keyword. Banded
	// states pack the same fact into their transition entry, so this array serves
	// the states below the band — the majority once keywords run longer than
	// bandDepth (3) — where one []bool load replaces walking the chain.
	hasOutput []bool
	// depth drives band selection at build time and nothing after it, so
	// balancedEngine.buildFromKeywords releases it once the band is chosen.
	// trieDepth keeps the one value Info still needs.
	depth     []int32
	trieDepth int
	size      int
	cap       int
	runes     []rune
	alphabetCoder
}

const (
	datInitialCap = 1024
	datRootPos    = 1
)

func newDoubleArrayTrie() *doubleArrayTrie {
	return &doubleArrayTrie{
		base:  make([]int32, datInitialCap),
		check: make([]int32, datInitialCap),
		fail:  make([]int32, datInitialCap),
		depth: make([]int32, datInitialCap),
		cap:   datInitialCap,
		size:  datRootPos + 1,
	}
}

func (dat *doubleArrayTrie) expand() {
	newCap := dat.cap * 2
	newBase := make([]int32, newCap)
	newCheck := make([]int32, newCap)
	newFail := make([]int32, newCap)
	newDepth := make([]int32, newCap)
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
	dat.base = make([]int32, datInitialCap)
	dat.check = make([]int32, datInitialCap)
	dat.fail = make([]int32, datInitialCap)
	dat.depth = make([]int32, datInitialCap)
	dat.cap = datInitialCap
	dat.size = datRootPos + 1
	dat.out = outputs{}
	// Reset with the arrays: Build reconstructs the automaton, so a rebuild from
	// shorter keywords must not keep the old maximum.
	dat.trieDepth = 0

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
	slices.Sort(dat.runes)
	dat.build(dat.runes)

	tmpChildren := make(map[int]map[rune]int)
	tmpOwn := make(map[int]string)
	nextID := 1
	tmpChildren[0] = make(map[rune]int)

	for kw := range keywords {
		// An empty keyword would end at the root, where own == 0 already means "no
		// keyword". The public API rejects empty keywords; skipping keeps this trie
		// correct on its own.
		if kw == "" {
			continue
		}
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
		tmpOwn[cur] = kw
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
			codes = append(codes, dat.index[ch])
		}

		base := dat.findBase(codes)
		dat.base[datPos[parent]] = int32(base) //nolint:gosec // G115: see the int32 note on the fields.

		for ch, childID := range children {
			code := dat.index[ch]
			pos := base + code
			dat.ensureCapacity(pos + 1)

			dat.check[pos] = int32(datPos[parent]) //nolint:gosec // G115: as above.
			dat.depth[pos] = dat.depth[datPos[parent]] + 1
			if d := int(dat.depth[pos]); d > dat.trieDepth {
				dat.trieDepth = d
			}
			datPos[childID] = pos

			if kw, ok := tmpOwn[childID]; ok && kw != "" {
				for pos >= len(dat.out.own) {
					dat.out.own = append(dat.out.own, 0)
				}
				dat.out.own[pos] = dat.out.assignID(kw)
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

	if len(dat.out.own) < dat.size {
		newOwn := make([]int32, dat.size)
		copy(newOwn, dat.out.own)
		dat.out.own = newOwn
	}

	dat.out.outLink = make([]int32, dat.size)
	for s := range dat.out.outLink {
		dat.out.outLink[s] = outNone
	}
	dat.computeFailLinks()

	// Derived after the fail links, which fill the output chain.
	dat.hasOutput = make([]bool, dat.size)
	for s := 0; s < dat.size; s++ {
		dat.hasOutput[s] = dat.out.own[s] != 0 || dat.out.outLink[s] != outNone
	}
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

		for code := range dat.runes {
			next := dat.gotoStateByCode(state, code)
			if next == 0 {
				continue
			}
			queue = append(queue, next)

			dat.fail[next] = int32(dat.followFailByCode(int(dat.fail[state]), code)) //nolint:gosec // G115: as above.
			// Link to the nearest keyword-carrying state on the failure path instead
			// of copying its outputs in. The fail target is strictly shallower and
			// this BFS visits by depth, so its outLink is already final.
			if f := int(dat.fail[next]); dat.out.own[f] != 0 {
				dat.out.outLink[next] = int32(f) //nolint:gosec // G115: DAT state ids use int32 throughout.
			} else {
				dat.out.outLink[next] = dat.out.outLink[f]
			}
		}
	}
}

// gotoStateByCode resolves a goto transition with the rune already mapped to its
// alphabet index, so the hot loop does not re-resolve the rune on every fail hop.
func (dat *doubleArrayTrie) gotoStateByCode(state, code int) int {
	pos := int(dat.base[state]) + code
	if pos < 0 || pos >= dat.size {
		return 0
	}
	if int(dat.check[pos]) != state {
		return 0
	}
	return pos
}

func (dat *doubleArrayTrie) followFailByCode(state, code int) int {
	// Compute the transition once per visited state, shared by the loop condition
	// and the post-loop value, since this runs on the fail-walk hot path.
	next := dat.gotoStateByCode(state, code)
	for state != datRootPos && next == 0 {
		state = int(dat.fail[state])
		next = dat.gotoStateByCode(state, code)
	}
	if next == 0 {
		next = datRootPos
	}
	return next
}

func (dat *doubleArrayTrie) memoryBytes() int64 {
	mem := int64(len(dat.base)+len(dat.check)+len(dat.fail)+len(dat.depth)) * 4
	mem += dat.out.memoryBytes()
	mem += int64(len(dat.hasOutput)) // one bool per state
	return mem
}

// maxDepth reports the deepest trie level from the value recorded during the
// build, since depth itself is released once the band is chosen.
func (dat *doubleArrayTrie) maxDepth() int { return dat.trieDepth }
