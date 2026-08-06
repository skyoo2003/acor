// SPDX-License-Identifier: Apache-2.0

package engine

import "unicode/utf8"

// mapNode is a trie node using Go maps for children (sparse representation).
type mapNode struct {
	children map[rune]int
	fail     int
	depth    int
}

// mapTrie is a trie backed by a slice of mapNodes.
type mapTrie struct {
	nodes []mapNode
	// own and outLink use the same output-chain representation as the flat and
	// double-array tries, so all engines share the matching helpers.
	own     []string
	outLink []int32
}

// Compile-time check that memEfficientEngine satisfies matchEngine.
var _ matchEngine = (*memEfficientEngine)(nil)

// memEfficientEngine implements matchEngine using a map-based sparse trie
// with standard NFA (failure links). Used by PresetMemoryEfficient.
type memEfficientEngine struct {
	trie  mapTrie
	bloom *bloomFilter
	// asciiOnly reports whether every keyword is pure ASCII, which lets runeLen take
	// a keyword's byte length instead of rescanning it. This engine has no
	// alphabetCoder to carry the flag, so it derives it at build time.
	asciiOnly bool
}

func newMemEfficientEngine() *memEfficientEngine {
	return &memEfficientEngine{}
}

func (e *memEfficientEngine) buildFromKeywords(keywords map[string]struct{}) {
	trie := mapTrie{
		nodes: []mapNode{
			{children: make(map[rune]int), depth: 0},
		},
		own:     []string{""},
		outLink: []int32{outNone},
	}

	for kw := range keywords {
		// An empty keyword would end at the root, where own == "" already means "no
		// keyword". The public API rejects empty keywords; skipping keeps this engine
		// correct on its own.
		if kw == "" {
			continue
		}
		state := 0
		for _, ch := range kw {
			child, ok := trie.nodes[state].children[ch]
			if !ok {
				child = len(trie.nodes)
				trie.nodes[state].children[ch] = child
				trie.nodes = append(trie.nodes, mapNode{
					children: make(map[rune]int),
					depth:    trie.nodes[state].depth + 1,
				})
				trie.own = append(trie.own, "")
				trie.outLink = append(trie.outLink, outNone)
			}
			state = child
		}
		trie.own[state] = kw
	}

	type queueEntry struct {
		ch    rune
		state int
	}
	queue := make([]queueEntry, 0)
	for ch, child := range trie.nodes[0].children {
		trie.nodes[child].fail = 0
		queue = append(queue, queueEntry{ch, child})
	}

	for len(queue) > 0 {
		entry := queue[0]
		queue = queue[1:]

		for ch, child := range trie.nodes[entry.state].children {
			queue = append(queue, queueEntry{ch, child})

			// Walk failure links to the deepest state that has a `ch` child, then
			// apply goto(fail, ch) exactly once below. Assigning inside the loop
			// and re-applying after would double-apply goto and can point a state's
			// fail link at itself (e.g. keywords {a,aa,aaa}), causing find to loop
			// forever following that self-referential fail link.
			fail := trie.nodes[entry.state].fail
			for fail != 0 {
				if _, ok := trie.nodes[fail].children[ch]; ok {
					break
				}
				fail = trie.nodes[fail].fail
			}
			if next, ok := trie.nodes[fail].children[ch]; ok {
				fail = next
			}

			trie.nodes[child].fail = fail
			// Link to the nearest keyword-carrying state on the failure path instead
			// of copying its outputs in. fail is strictly shallower and this BFS
			// visits by depth, so its outLink is already final.
			if trie.own[fail] != "" {
				trie.outLink[child] = int32(fail) //nolint:gosec // G115: node ids index a slice built above.
			} else {
				trie.outLink[child] = trie.outLink[fail]
			}
		}
	}

	e.trie = trie
	e.bloom = buildFirstRuneBloom(keywords)

	e.asciiOnly = true
	for kw := range keywords {
		// A keyword is pure ASCII exactly when its byte length equals its rune count.
		if len(kw) != utf8.RuneCountInString(kw) {
			e.asciiOnly = false
			break
		}
	}
}

func (e *memEfficientEngine) find(text string) []string {
	if len(e.trie.nodes) <= 1 {
		return []string{}
	}

	matched := make([]string, 0)
	state := 0

	for _, ch := range text {
		if e.bloom.skipAtRoot(state == 0, ch) {
			continue
		}

		for {
			if next, ok := e.trie.nodes[state].children[ch]; ok {
				state = next
				break
			}
			if state == 0 {
				break
			}
			state = e.trie.nodes[state].fail
		}

		matched = appendOutputChain(matched, state, e.trie.own, e.trie.outLink)
	}

	return matched
}

func (e *memEfficientEngine) findIndex(text string) map[string][]int {
	if len(e.trie.nodes) <= 1 {
		return map[string][]int{}
	}

	matched := make(map[string][]int)
	state := 0
	runeIndex := 0

	for _, ch := range text {
		if e.bloom.skipAtRoot(state == 0, ch) {
			runeIndex++
			continue
		}

		for {
			if next, ok := e.trie.nodes[state].children[ch]; ok {
				state = next
				break
			}
			if state == 0 {
				break
			}
			state = e.trie.nodes[state].fail
		}

		runeIndex++
		indexOutputChain(matched, state, runeIndex, e.trie.own, e.trie.outLink, e.asciiOnly)
	}

	return matched
}

// matchString is matchStream over an in-memory string; see the matchEngine
// interface for why the loop is duplicated rather than shared through a closure.
func (e *memEfficientEngine) matchString(text string, emit func(keyword string, start, end int) bool) {
	if len(e.trie.nodes) <= 1 {
		return
	}

	state := 0
	runeIndex := 0

	for _, ch := range text {
		if e.bloom.skipAtRoot(state == 0, ch) {
			runeIndex++
			continue
		}

		for {
			if nx, ok := e.trie.nodes[state].children[ch]; ok {
				state = nx
				break
			}
			if state == 0 {
				break
			}
			state = e.trie.nodes[state].fail
		}

		runeIndex++
		if !emitOutputChain(state, runeIndex, e.trie.own, e.trie.outLink, e.asciiOnly, emit) {
			return
		}
	}
}

func (e *memEfficientEngine) matchStream(next func() (rune, bool), emit func(keyword string, start, end int) bool) {
	if len(e.trie.nodes) <= 1 {
		return
	}

	state := 0
	runeIndex := 0

	for {
		ch, ok := next()
		if !ok {
			return
		}
		if e.bloom.skipAtRoot(state == 0, ch) {
			runeIndex++
			continue
		}

		for {
			if nx, ok := e.trie.nodes[state].children[ch]; ok {
				state = nx
				break
			}
			if state == 0 {
				break
			}
			state = e.trie.nodes[state].fail
		}

		runeIndex++
		if !emitOutputChain(state, runeIndex, e.trie.own, e.trie.outLink, e.asciiOnly, emit) {
			return
		}
	}
}

func (e *memEfficientEngine) info() *InMemoryInfo {
	return &InMemoryInfo{
		Keywords:    countUniqueOutputs(e.trie.own),
		Nodes:       len(e.trie.nodes),
		Preset:      PresetMemoryEfficient,
		MemoryBytes: e.estimateMemory(),
		TrieDepth:   trieMaxDepth(e.trie.nodes),
	}
}

// countUniqueOutputs counts the nodes that terminate a keyword. Each keyword is
// reached by exactly one path and so fills exactly one own slot, and the build
// input is a set, so nothing needs deduplication.
func countUniqueOutputs(own []string) int {
	n := 0
	for _, kw := range own {
		if kw != "" {
			n++
		}
	}
	return n
}

func trieMaxDepth(nodes []mapNode) int {
	d := 0
	for _, n := range nodes {
		if n.depth > d {
			d = n.depth
		}
	}
	return d
}

func (e *memEfficientEngine) estimateMemory() int64 {
	var size int64
	for _, n := range e.trie.nodes {
		size += int64(24 + 16 + 16) // children hdr, fail, depth
		for range n.children {
			size += 24
		}
	}
	size += int64(len(e.trie.own))*16 + int64(len(e.trie.outLink))*4
	if e.bloom != nil {
		size += e.bloom.memoryBytes()
	}
	return size
}
