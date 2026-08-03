// SPDX-License-Identifier: Apache-2.0

package engine

import "unicode/utf8"

// mapNode is a trie node using Go maps for children (sparse representation).
// own is the single keyword ending at this node, or "" for none, and outLink
// chains to the next keyword-carrying node along the failure path (outNone at the
// end). See doubleArrayTrie.own for the O(n^2) that eagerly merging output lists
// cost on a suffix-nested dictionary.
type mapNode struct {
	children map[rune]int
	fail     int
	own      string
	outLink  int32
	depth    int
}

// mapTrie is a trie backed by a slice of mapNodes.
type mapTrie struct {
	nodes []mapNode
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
			{children: make(map[rune]int), depth: 0, outLink: outNone},
		},
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
					outLink:  outNone,
				})
			}
			state = child
		}
		trie.nodes[state].own = kw
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
			if trie.nodes[fail].own != "" {
				trie.nodes[child].outLink = int32(fail) //nolint:gosec // G115: node ids index a slice built above.
			} else {
				trie.nodes[child].outLink = trie.nodes[fail].outLink
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

		for s := state; s != outNone; s = int(e.trie.nodes[s].outLink) {
			if kw := e.trie.nodes[s].own; kw != "" {
				matched = append(matched, kw)
			}
		}
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
		for s := state; s != outNone; s = int(e.trie.nodes[s].outLink) {
			out := e.trie.nodes[s].own
			if out == "" {
				continue
			}
			startIdx := runeIndex - runeLen(out, e.asciiOnly)
			matched[out] = append(matched[out], startIdx)
		}
	}

	return matched
}

// matchString is matchStream over an in-memory string; see the matchEngine
// interface for why the loop is duplicated rather than shared through a closure.
func (e *memEfficientEngine) matchString(text string, emit func(Match) bool) {
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
		for s := state; s != outNone; s = int(e.trie.nodes[s].outLink) {
			out := e.trie.nodes[s].own
			if out == "" {
				continue
			}
			start := runeIndex - runeLen(out, e.asciiOnly)
			if !emit(Match{Keyword: out, Start: start, End: runeIndex}) {
				return
			}
		}
	}
}

func (e *memEfficientEngine) matchStream(next func() (rune, bool), emit func(Match) bool) {
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
		for s := state; s != outNone; s = int(e.trie.nodes[s].outLink) {
			out := e.trie.nodes[s].own
			if out == "" {
				continue
			}
			start := runeIndex - runeLen(out, e.asciiOnly)
			if !emit(Match{Keyword: out, Start: start, End: runeIndex}) {
				return
			}
		}
	}
}

func (e *memEfficientEngine) info() *InMemoryInfo {
	return &InMemoryInfo{
		Keywords:    countUniqueOutputs(e.trie.nodes),
		Nodes:       len(e.trie.nodes),
		Preset:      PresetMemoryEfficient,
		MemoryBytes: e.estimateMemory(),
		TrieDepth:   trieMaxDepth(e.trie.nodes),
	}
}

// countUniqueOutputs counts the nodes that terminate a keyword. Each keyword is
// reached by exactly one path and so fills exactly one own slot, and the build
// input is a set, so nothing needs deduplication.
func countUniqueOutputs(nodes []mapNode) int {
	n := 0
	for _, node := range nodes {
		if node.own != "" {
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
		size += int64(24 + 16 + 16 + 16 + 4) // children hdr, fail, depth, own, outLink
		for range n.children {
			size += 24
		}
	}
	if e.bloom != nil {
		size += e.bloom.memoryBytes()
	}
	return size
}
