// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"cmp"
	"container/heap"
	"slices"
	"sync"
	"sync/atomic"
)

// NewComposite presents immutable, disjoint shard engines as one engine. Search
// work is bounded by concurrency; no keywords or automata are rebuilt or copied.
func NewComposite(shards []*Engine, concurrency int) *Engine {
	c := &compositeEngine{}
	e := &Engine{impl: c}
	for _, shard := range shards {
		if shard.MaxKeywordRunes() == 0 {
			continue
		}
		c.shards = append(c.shards, shard)
		e.maxKeywordRunes = max(e.maxKeywordRunes, shard.MaxKeywordRunes())
	}
	c.concurrency = max(1, min(concurrency, len(c.shards)))
	c.slots = make(chan struct{}, c.concurrency)
	return e
}

type compositeMatch struct {
	keyword string
	start   int
	end     int
}

type compositeEngine struct {
	shards      []*Engine
	concurrency int
	slots       chan struct{}
}

// Single-engine output chains visit the deepest terminal first, then suffixes.
// Therefore scan order is end ascending, then start ascending (longest first).
func compareCompositeMatch(a, b compositeMatch) int {
	if n := cmp.Compare(a.end, b.end); n != 0 {
		return n
	}
	if n := cmp.Compare(a.start, b.start); n != 0 {
		return n
	}
	return cmp.Compare(a.keyword, b.keyword)
}

func (e *compositeEngine) eachShard(work func(int)) {
	if e.concurrency == 1 {
		for i := range e.shards {
			e.shardWork(func() { work(i) })
		}
		return
	}
	var wg sync.WaitGroup
	for worker := range e.concurrency {
		wg.Go(func() {
			for i := worker; i < len(e.shards); i += e.concurrency {
				e.shardWork(func() { work(i) })
			}
		})
	}
	wg.Wait()
}

func (e *compositeEngine) shardWork(work func()) {
	e.slots <- struct{}{}
	defer func() { <-e.slots }()
	work()
}

func (e *compositeEngine) matches(text string, unique bool) []compositeMatch {
	perShard := make([][]compositeMatch, len(e.shards))
	e.eachShard(func(i int) {
		var seen map[string]struct{}
		if unique {
			seen = make(map[string]struct{})
		}
		e.shards[i].MatchString(text, func(keyword string, start, end int) bool {
			if unique {
				if _, ok := seen[keyword]; ok {
					return true
				}
				seen[keyword] = struct{}{}
			}
			perShard[i] = append(perShard[i], compositeMatch{keyword, start, end})
			return true
		})
	})
	var result []compositeMatch
	for _, found := range perShard {
		result = append(result, found...)
	}
	slices.SortFunc(result, compareCompositeMatch)
	return result
}

func (e *compositeEngine) find(text string) []string {
	return compositeKeywords(e.matches(text, false))
}
func (e *compositeEngine) findSet(text string) []string {
	return compositeKeywords(e.matches(text, true))
}
func compositeKeywords(matches []compositeMatch) []string {
	result := make([]string, len(matches))
	for i, m := range matches {
		result[i] = m.keyword
	}
	return result
}
func (e *compositeEngine) findIndex(text string) map[string][]int {
	perShard := make([]map[string][]int, len(e.shards))
	e.eachShard(func(i int) { perShard[i] = e.shards[i].FindIndex(text) })
	result := make(map[string][]int)
	for _, found := range perShard {
		for keyword, positions := range found {
			result[keyword] = positions
		}
	}
	return result
}
func (e *compositeEngine) contains(text string) bool {
	var found atomic.Bool
	e.eachShard(func(i int) {
		if !found.Load() && e.shards[i].Contains(text) {
			found.Store(true)
		}
	})
	return found.Load()
}
func (e *compositeEngine) matchString(text string, emit func(string, int, int) bool) {
	for _, m := range e.matches(text, false) {
		if !emit(m.keyword, m.start, m.end) {
			return
		}
	}
}

type runeCursor func(rune) outputCursor

type outputCursor struct {
	out   *outputs
	state int
}

func (c *outputCursor) next(end int) (compositeMatch, bool) {
	for c.out != nil && c.state != outNone {
		id := c.out.own[c.state]
		c.state = int(c.out.outLink[c.state])
		if id != 0 {
			return compositeMatch{c.out.keywords[id], end - int(c.out.runeLens[id]), end}, true
		}
	}
	return compositeMatch{}, false
}

// Cursors retain only the automaton state; the immutable engines remain shared.
// Advancing them one rune at a time preserves Stream's next/emit interleaving,
// including source scan budgets and leftmost-longest flushing.
func (e *Engine) cursor() runeCursor {
	switch impl := e.impl.(type) {
	case *memEfficientEngine:
		return mapCursor(impl)
	case *speedEngine:
		return speedCursor(impl)
	case *balancedEngine:
		return balancedCursor(impl)
	default:
		panic("engine: unsupported composite shard")
	}
}
func mapCursor(e *memEfficientEngine) runeCursor {
	state := 0
	return func(ch rune) outputCursor {
		if len(e.trie.nodes) <= 1 || e.skipAtRoot(state == 0, ch) {
			return outputCursor{}
		}
		for {
			if nx, ok := e.trie.nodes[state].next(ch); ok {
				state = nx
				break
			}
			if state == 0 {
				break
			}
			state = e.trie.nodes[state].fail
		}
		return outputCursor{&e.trie.out, state}
	}
}
func speedCursor(e *speedEngine) runeCursor {
	state := 0
	return func(ch rune) outputCursor {
		if e.dfa == nil {
			return outputCursor{}
		}
		code, ok := e.code(ch)
		if !ok {
			state = 0
			return outputCursor{}
		}
		v := e.dfa[state*e.alphaSize+code]
		state = int(v &^ hasOutputBit)
		if v&hasOutputBit == 0 {
			return outputCursor{}
		}
		return outputCursor{&e.out, state}
	}
}
func balancedCursor(e *balancedEngine) runeCursor {
	state := datRootPos
	return func(ch rune) outputCursor {
		dat := e.banded.dat
		if dat.size <= datRootPos+1 {
			return outputCursor{}
		}
		code, ok := dat.code(ch)
		if !ok {
			state = datRootPos
			return outputCursor{}
		}
		nx, hasOutput := e.banded.step(state, code)
		state = nx
		if !hasOutput {
			return outputCursor{}
		}
		return outputCursor{&dat.out, state}
	}
}

func (e *compositeEngine) matchStream(next func() (rune, bool), emit func(string, int, int) bool) {
	if len(e.shards) == 0 {
		return
	}
	cursors := make([]runeCursor, len(e.shards))
	for i, shard := range e.shards {
		cursors[i] = shard.cursor()
	}
	perShard := make([]outputCursor, len(cursors))
	end := 0
	queue := make(compositeQueue, 0, len(cursors))
	for {
		ch, ok := next()
		if !ok {
			return
		}
		end++
		// A rune transition is too small to amortize goroutine dispatch. Advance
		// cursors in one worker slot, then release it before invoking callbacks.
		// Full-string scans still fan out across the bounded shard workers.
		e.shardWork(func() {
			for i := range cursors {
				perShard[i] = cursors[i](ch)
			}
		})
		queue = queue[:0]
		for shard := range perShard {
			if m, ok := perShard[shard].next(end); ok {
				queue = append(queue, compositeHead{m, shard})
			}
		}
		if !emitCompositeQueue(&queue, perShard, end, emit) {
			return
		}
	}
}

type compositeHead struct {
	match compositeMatch
	shard int
}
type compositeQueue []compositeHead

func (q compositeQueue) Len() int           { return len(q) }
func (q compositeQueue) Less(i, j int) bool { return compareCompositeMatch(q[i].match, q[j].match) < 0 }
func (q compositeQueue) Swap(i, j int)      { q[i], q[j] = q[j], q[i] }
func (q *compositeQueue) Push(value any)    { *q = append(*q, value.(compositeHead)) }
func (q *compositeQueue) Pop() any {
	last := len(*q) - 1
	value := (*q)[last]
	*q = (*q)[:last]
	return value
}

func emitCompositeQueue(q *compositeQueue, outputs []outputCursor, end int, emit func(string, int, int) bool) bool {
	heap.Init(q)
	for len(*q) != 0 {
		head := (*q)[0]
		m := head.match
		if !emit(m.keyword, m.start, m.end) {
			return false
		}
		if next, ok := outputs[head.shard].next(end); ok {
			(*q)[0].match = next
			heap.Fix(q, 0)
		} else {
			heap.Pop(q)
		}
	}
	return true
}

func (e *compositeEngine) buildFromKeywords(map[string]struct{}) {
	panic("engine: composite is immutable")
}
func (e *compositeEngine) info() *InMemoryInfo {
	result := new(InMemoryInfo)
	for _, shard := range e.shards {
		i := shard.Info()
		result.Keywords += i.Keywords
		result.Nodes += i.Nodes
		result.MemoryBytes += i.MemoryBytes
		result.TrieDepth = max(result.TrieDepth, i.TrieDepth)
		result.Preset = i.Preset
	}
	return result
}
