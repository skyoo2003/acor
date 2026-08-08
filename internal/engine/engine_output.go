// SPDX-License-Identifier: Apache-2.0

package engine

import "unicode/utf8"

const (
	// hasOutputBit shares an int32 transition entry with its target state. Valid
	// packed state IDs are [0, hasOutputBit), leaving the sign bit clear.
	hasOutputBit int32 = 1 << 30
	// outNone marks the end of an output-link chain.
	outNone = -1
)

func requirePackableStateCount(count int) {
	if count >= int(hasOutputBit) {
		panic("engine: too many states for packed transitions")
	}
}

func packState(state int, hasOutput bool) int32 {
	if state < 0 || state >= int(hasOutputBit) {
		panic("engine: state does not fit in packed transition")
	}
	entry := int32(state) //nolint:gosec // G115: guarded above.
	if hasOutput {
		entry |= hasOutputBit
	}
	return entry
}

// outputs maps matching states to the keywords they report, shared by every
// engine. own[s] is the id of the keyword ending at s (0 for none), and
// outLink[s] chains to the next keyword-carrying state along the failure path
// (outNone at the end). The link chain replaces an eagerly merged [][]string,
// which copied each state's whole suffix output list into every state failing to
// it: O(n^2) on a suffix-nested dictionary, where 500 keywords built 125,250
// entries (2 MB of string headers) against the chain's 500.
//
// Each keyword's string lives once in keywords, indexed by id. Id 0 is a
// sentinel, so a zero-valued own slot already means "no keyword" and the
// zero-filling append/resize paths in every engine stay correct without explicit
// fills. Dense ids also make FindSet's dedup a bitset test; see setCollector.
// runeLens[id] is the keyword's rune length, computed once at build time instead
// of once per emitted match (a RuneCountInString on non-ASCII dictionaries).
type outputs struct {
	own      []int32
	outLink  []int32
	keywords []string
	runeLens []int32
}

// assign interns kw for a terminal state currently holding cur and returns the
// id to store back. Distinct keywords can collide on one rune path — invalid
// UTF-8 decodes to RuneError byte by byte, and such keywords are accepted — so
// a terminal can be written twice. Reusing cur keeps last-write-wins semantics
// without orphaning a table entry, and every preset counts the terminal once.
func (o *outputs) assign(cur int32, kw string) int32 {
	if cur != 0 {
		o.keywords[cur] = kw
		o.runeLens[cur] = int32(utf8.RuneCountInString(kw)) //nolint:gosec // G115: a rune count never exceeds the byte length.
		return cur
	}
	return o.assignID(kw)
}

// assignID interns kw and returns its id; the caller stores that id in own at
// the state where kw ends.
func (o *outputs) assignID(kw string) int32 {
	if o.keywords == nil {
		// Id 0 is the "no keyword" sentinel, never read.
		o.keywords = append(o.keywords, "")
		o.runeLens = append(o.runeLens, 0)
	}
	// Every keyword terminates a distinct state, so the keyword count is bounded
	// by the state count: requirePackableStateCount keeps the packed engines well
	// inside int32, and the map engine runs out of memory long before 2^31 nodes.
	id := int32(len(o.keywords)) //nolint:gosec // G115: bounded above.
	o.keywords = append(o.keywords, kw)
	o.runeLens = append(o.runeLens, int32(utf8.RuneCountInString(kw))) //nolint:gosec // G115: a rune count never exceeds the byte length.
	return id
}

// keywordCount reports how many keywords the table holds, excluding the
// sentinel. Each keyword is interned exactly once, so no deduplication is
// needed.
func (o *outputs) keywordCount() int {
	if len(o.keywords) == 0 {
		return 0
	}
	return len(o.keywords) - 1
}

// memoryBytes estimates the table's footprint: an id and a link per state, a
// string header and a rune length per keyword.
func (o *outputs) memoryBytes() int64 {
	return int64(len(o.own))*4 + int64(len(o.outLink))*4 +
		int64(len(o.keywords))*16 + int64(len(o.runeLens))*4
}

// appendChain appends the keywords on state's output chain to dst, allocating
// only at the first hit.
func (o *outputs) appendChain(dst []string, state int) []string {
	for s := state; s != outNone; s = int(o.outLink[s]) {
		if id := o.own[s]; id != 0 {
			if dst == nil {
				dst = make([]string, 0, findResultHint)
			}
			dst = append(dst, o.keywords[id])
		}
	}
	return dst
}

// indexChain records each chain keyword's start offset, derived from the match
// end and the keyword's rune length.
func (o *outputs) indexChain(dst map[string][]int, state, end int) {
	for s := state; s != outNone; s = int(o.outLink[s]) {
		if id := o.own[s]; id != 0 {
			kw := o.keywords[id]
			dst[kw] = append(dst[kw], end-int(o.runeLens[id]))
		}
	}
}

// emitChain reports each chain keyword with its span to emit, stopping early if
// emit returns false.
func (o *outputs) emitChain(state, end int, emit func(keyword string, start, end int) bool) bool {
	for s := state; s != outNone; s = int(o.outLink[s]) {
		if id := o.own[s]; id != 0 && !emit(o.keywords[id], end-int(o.runeLens[id]), end) {
			return false
		}
	}
	return true
}

// dedupHashMin is the combined table size (keyword ids plus state slots) at
// which setCollector dedups through hash maps instead of bitsets. Below it the
// bitsets win: at 100k keywords they cost ~26 KB zeroed once per matching
// query and every test is one indexed load. At the threshold they reach
// ~128 KB, and a million-pattern dictionary would pay ~262 KB per matching
// query (measured, MemoryEfficient) just to be forgotten at return — maps cost
// only per unique hit, which a real text keeps small.
//
// A var, not a const, so tests can force the map path without building a
// million-keyword automaton.
var dedupHashMin = 1 << 20

// setCollector folds matched states' output chains into the unique keyword set
// behind FindSet, preserving first-match order. Keyword ids and state ids are
// dense, so both dedup layers are one bitset test each; past dedupHashMin the
// bitsets would outweigh the matches they dedup, so both layers switch to
// hash maps sized by hits rather than by dictionary.
//
// It stays unallocated until the first hit: text matching nothing is the common
// case for a filter, and it should not pay for the bookkeeping of a hit that
// never happened.
type setCollector struct {
	out        []string
	seen       []uint64
	seenStates []uint64
	seenKw     map[int32]struct{}
	seenSt     map[int32]struct{}
}

// markState records a landing on state and reports whether it is new.
func (c *setCollector) markState(state int) bool {
	if c.seenStates != nil {
		if c.seenStates[state>>6]&(1<<(state&63)) != 0 {
			return false
		}
		c.seenStates[state>>6] |= 1 << (state & 63)
		return true
	}
	s := int32(state) //nolint:gosec // G115: packed engines bound states by requirePackableStateCount; map-engine node ids are memory-bounded.
	if _, dup := c.seenSt[s]; dup {
		return false
	}
	c.seenSt[s] = struct{}{}
	return true
}

// markKeyword records keyword id and reports whether it is new.
func (c *setCollector) markKeyword(id int32) bool {
	if c.seen != nil {
		if c.seen[id>>6]&(1<<(id&63)) != 0 {
			return false
		}
		c.seen[id>>6] |= 1 << (id & 63)
		return true
	}
	if _, dup := c.seenKw[id]; dup {
		return false
	}
	c.seenKw[id] = struct{}{}
	return true
}

// collectChain adds the unseen keywords on state's output chain to the set.
//
// State dedup comes first because a state's output chain is fixed: landing on
// it again can add nothing new. Without it, a run of text that keeps reaching
// the same deep state — a suffix-nested dictionary over repeated characters —
// rewalks the whole chain per character, which measured 40x on FindSet.
// Keyword dedup is still needed on top because distinct states' chains overlap,
// since every state whose failure path reaches a keyword reports it.
func (c *setCollector) collectChain(o *outputs, state int) {
	// A state carrying no outputs costs nothing: the map engine calls this for
	// every character, matched or not.
	if o.own[state] == 0 && o.outLink[state] == outNone {
		return
	}
	// Past the guard the chain holds at least one keyword, so the bookkeeping
	// is paid by a real hit.
	if c.out == nil {
		if len(o.keywords)+len(o.own) >= dedupHashMin {
			c.seenKw = make(map[int32]struct{}, findResultHint)
			c.seenSt = make(map[int32]struct{}, findResultHint)
		} else {
			c.seen = make([]uint64, (len(o.keywords)+63)/64)
			c.seenStates = make([]uint64, (len(o.own)+63)/64)
		}
		c.out = make([]string, 0, findResultHint)
	}
	if !c.markState(state) {
		return
	}
	for s := state; s != outNone; s = int(o.outLink[s]) {
		if id := o.own[s]; id != 0 && c.markKeyword(id) {
			c.out = append(c.out, o.keywords[id])
		}
	}
}

// result returns the unique keywords in first-match order. Like Find it is never
// nil, and a zero-length literal costs no allocation, so text that matched
// nothing stays allocation-free.
func (c *setCollector) result() []string {
	if c.out == nil {
		return []string{}
	}
	return c.out
}
