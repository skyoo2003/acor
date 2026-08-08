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

// setCollector folds matched states' output chains into the unique keyword set
// behind FindSet, preserving first-match order. Keyword ids are dense, so dedup
// is one bitset test per chain entry: no string hashing, no linear rescans, and
// no size-based strategy handoff.
//
// It stays unallocated until the first hit: text matching nothing is the common
// case for a filter, and it should not pay for the bookkeeping of a hit that
// never happened.
//
// ponytail: seen is len(keywords)/8 bytes per matching query — ~128 KB for a
// million-pattern dictionary. Fall back to a map[int32]struct{} for huge
// dictionaries if that allocation ever shows up in profiles.
type setCollector struct {
	out  []string
	seen []uint64
}

// collectChain adds the unseen keywords on state's output chain to the set.
func (c *setCollector) collectChain(o *outputs, state int) {
	for s := state; s != outNone; s = int(o.outLink[s]) {
		id := o.own[s]
		if id == 0 {
			continue
		}
		if c.seen == nil {
			c.seen = make([]uint64, (len(o.keywords)+63)/64)
			c.out = make([]string, 0, findResultHint)
		}
		if c.seen[id>>6]&(1<<(id&63)) != 0 {
			continue
		}
		c.seen[id>>6] |= 1 << (id & 63)
		c.out = append(c.out, o.keywords[id])
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
