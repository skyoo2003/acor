// SPDX-License-Identifier: Apache-2.0

package engine

// Preset selects the architecture for the in-memory Aho-Corasick engine.
// Each preset optimizes for a different trade-off between speed, memory, and
// feature set. The preset is fixed at creation time and cannot be changed.
type Preset int

const (
	// PresetNone is the zero value (unset). When Preset is PresetNone, Create
	// falls through to the original V1/V2 Redis-backed mode.
	PresetNone Preset = iota

	// PresetSpeed prioritizes maximum search speed using a Full DFA with flat
	// array trie and compact alphabet mapping. Best for real-time packet
	// inspection, high-speed log scanning, and latency-critical paths.
	// Trade-off: higher memory usage proportional to states × alphabet size.
	PresetSpeed

	// PresetBalanced provides the best speed-to-memory ratio using a
	// Double-Array Trie with Banded DFA (DFA for shallow states, NFA fallback
	// for deep states) and output link compression. Best for general-purpose
	// backend keyword filtering and search engines.
	PresetBalanced

	// PresetMemoryEfficient minimizes memory usage using a map-based sparse
	// trie with Bloom filter pre-filtering and standard NFA. Best for
	// large-scale domain blocking, malware signature matching, or any scenario
	// with millions of patterns.
	// Trade-off: slower search due to failure link traversal and map lookups.
	PresetMemoryEfficient

	// PresetUltimate uses the Balanced architecture (Double-Array Trie with
	// Banded DFA) plus a root-state first-rune pre-filter (a rune-level Bloom
	// filter) that skips characters which cannot start any keyword. Best for
	// production systems that need high
	// throughput with reasonable memory usage.
	PresetUltimate

	// PresetDefault is an internal sentinel; not user-selectable.
	PresetDefault Preset = -1
)

func (p Preset) String() string {
	switch p {
	case PresetNone:
		return "None"
	case PresetSpeed:
		return "Speed"
	case PresetBalanced:
		return "Balanced"
	case PresetMemoryEfficient:
		return "MemoryEfficient"
	case PresetUltimate:
		return "Ultimate"
	case PresetDefault:
		return "Default"
	default:
		return "Unknown"
	}
}

// InMemoryInfo contains statistics about an in-memory Aho-Corasick engine.
type InMemoryInfo struct {
	// Keywords is the number of keywords in the automaton.
	Keywords int
	// Nodes is the number of trie nodes (states).
	Nodes int
	// Preset is the architecture preset used.
	Preset Preset
	// MemoryBytes is the estimated memory usage in bytes.
	MemoryBytes int64
	// TrieDepth is the maximum depth of the trie.
	TrieDepth int
}

// matchEngine is the unexported strategy interface for in-memory Aho-Corasick
// implementations. Each architecture preset provides a concrete implementation.
type matchEngine interface {
	buildFromKeywords(keywords map[string]struct{})
	find(text string) []string
	findIndex(text string) map[string][]int
	info() *InMemoryInfo
}
