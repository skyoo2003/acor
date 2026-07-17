// SPDX-License-Identifier: Apache-2.0

package acor

import "unicode/utf8"

// mapNode is a trie node using Go maps for children (sparse representation).
type mapNode struct {
	children map[rune]int
	fail     int
	output   []string
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
}

func newMemEfficientEngine() *memEfficientEngine {
	return &memEfficientEngine{}
}

func (e *memEfficientEngine) buildFromKeywords(keywords map[string]struct{}) {
	trie := mapTrie{
		nodes: []mapNode{
			{children: make(map[rune]int), depth: 0},
		},
	}

	for kw := range keywords {
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
			}
			state = child
		}
		trie.nodes[state].output = append(trie.nodes[state].output, kw)
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

			fail := trie.nodes[entry.state].fail
			for fail != 0 {
				if next, ok := trie.nodes[fail].children[ch]; ok {
					fail = next
					break
				}
				fail = trie.nodes[fail].fail
			}
			if next, ok := trie.nodes[fail].children[ch]; ok {
				fail = next
			}

			trie.nodes[child].fail = fail
			if len(trie.nodes[fail].output) > 0 {
				trie.nodes[child].output = append(trie.nodes[child].output, trie.nodes[fail].output...)
			}
		}
	}

	e.trie = trie

	e.bloom = newBloomFilter(len(keywords), 0.01)
	for kw := range keywords {
		if r, size := utf8.DecodeRuneInString(kw); size > 0 {
			e.bloom.add(r)
		}
	}
}

func (e *memEfficientEngine) find(text string) []string {
	if len(e.trie.nodes) <= 1 {
		return nil
	}

	matched := make([]string, 0)
	state := 0

	for _, ch := range text {
		// Bloom pre-filter: only skip when at root state and char can't start any keyword.
		if state == 0 && !e.bloom.mightContain(ch) {
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

		if len(e.trie.nodes[state].output) > 0 {
			matched = append(matched, e.trie.nodes[state].output...)
		}
	}

	return matched
}

func (e *memEfficientEngine) findIndex(text string) map[string][]int {
	if len(e.trie.nodes) <= 1 {
		return nil
	}

	matched := make(map[string][]int)
	state := 0
	runeIndex := 0

	for _, ch := range text {
		if state == 0 && !e.bloom.mightContain(ch) {
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
		for _, out := range e.trie.nodes[state].output {
			startIdx := runeIndex - len([]rune(out))
			matched[out] = append(matched[out], startIdx)
		}
	}

	return matched
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

func countUniqueOutputs(nodes []mapNode) int {
	seen := make(map[string]struct{})
	for _, n := range nodes {
		for _, out := range n.output {
			seen[out] = struct{}{}
		}
	}
	return len(seen)
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
		size += int64(24 + 16 + 16 + len(n.output)*16)
		for range n.children {
			size += 24
		}
	}
	if e.bloom != nil {
		size += e.bloom.memoryBytes()
	}
	return size
}
