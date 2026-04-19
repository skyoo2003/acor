// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"strings"
	"sync"
)

// Preset selects the architecture for the in-memory Aho-Corasick engine.
// Each preset optimizes for a different trade-off between speed, memory, and
// feature set. The preset is fixed at creation time and cannot be changed.
type Preset int

const (
	// PresetSpeed prioritizes maximum search speed using a Full DFA with flat
	// array trie and compact alphabet mapping. Best for real-time packet
	// inspection, high-speed log scanning, and latency-critical paths.
	// Trade-off: higher memory usage proportional to states × alphabet size.
	PresetSpeed Preset = iota

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

	// PresetUltimate combines the best techniques from all presets: SIMD-aware
	// byte scanning pre-filter, Double-Array Trie, Banded DFA, and deferred
	// bit-set output collection. Best for production systems that need the
	// highest throughput with reasonable memory usage.
	PresetUltimate

	// PresetDefault is an internal sentinel; not user-selectable.
	PresetDefault Preset = -1
)

func (p Preset) String() string {
	switch p {
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

// InMemoryOptions configures an in-memory Aho-Corasick engine.
type InMemoryOptions struct {
	// Preset selects the architecture. Defaults to PresetBalanced if unset.
	Preset Preset
	// CaseSensitive controls whether matching is case-sensitive.
	// When false (default), keywords are lowercased on Add and search text
	// is lowercased during Find/FindIndex.
	CaseSensitive bool
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
// Unlike the operations interface (which uses context.Context for Redis I/O),
// this is context-free since all operations are pure in-memory.
type matchEngine interface {
	// buildFromKeywords rebuilds the entire automaton from the given keyword set.
	// Called after every Add or Remove that mutates the keyword set.
	buildFromKeywords(keywords map[string]struct{})

	// find scans text and returns all matched keywords in order of occurrence.
	find(text string) []string

	// findIndex scans text and returns matched keywords mapped to their start indices.
	findIndex(text string) map[string][]int

	// info returns statistics about the current automaton state.
	info() *InMemoryInfo
}

// InMemoryAC is a pure in-memory Aho-Corasick automaton with selectable
// architecture presets. It provides the same Find/FindIndex/Add/Remove API as
// the Redis-backed AhoCorasick but operates entirely in process memory with no
// external dependencies.
//
// All methods are safe for concurrent use across multiple goroutines.
// Write operations (Add, Remove, Flush) take an exclusive lock; read operations
// (Find, FindIndex, Info) take a shared lock.
type InMemoryAC struct {
	mu            sync.RWMutex
	engine        matchEngine
	preset        Preset
	caseSensitive bool
	keywordSet    map[string]struct{}
}

// NewInMemory creates a new in-memory Aho-Corasick engine with the given options.
// If opts is nil, defaults to PresetBalanced with case-insensitive matching.
//
// Example:
//
//	ac := acor.NewInMemory(&acor.InMemoryOptions{
//	    Preset: acor.PresetBalanced,
//	})
//	ac.Add("hello")
//	ac.Add("world")
//	matches := ac.Find("hello world")
func NewInMemory(opts *InMemoryOptions) *InMemoryAC {
	if opts == nil {
		opts = &InMemoryOptions{Preset: PresetBalanced}
	}

	preset := opts.Preset
	if preset < PresetSpeed || preset > PresetUltimate {
		preset = PresetBalanced
	}

	return &InMemoryAC{
		engine:        newMatchEngine(preset),
		preset:        preset,
		caseSensitive: opts.CaseSensitive,
		keywordSet:    make(map[string]struct{}),
	}
}

// Add inserts a keyword into the automaton. Returns 1 if added, 0 if the
// keyword already exists. The automaton is fully rebuilt after each addition.
func (ac *InMemoryAC) Add(keyword string) int {
	keyword = strings.TrimSpace(keyword)
	if !ac.caseSensitive {
		keyword = strings.ToLower(keyword)
	}
	if keyword == "" {
		return 0
	}

	ac.mu.Lock()
	defer ac.mu.Unlock()

	if _, exists := ac.keywordSet[keyword]; exists {
		return 0
	}

	ac.keywordSet[keyword] = struct{}{}
	ac.engine.buildFromKeywords(ac.keywordSet)
	return 1
}

// Remove deletes a keyword from the automaton. Returns 1 if removed, 0 if the
// keyword was not found. The automaton is fully rebuilt after each removal.
func (ac *InMemoryAC) Remove(keyword string) int {
	keyword = strings.TrimSpace(keyword)
	if !ac.caseSensitive {
		keyword = strings.ToLower(keyword)
	}
	if keyword == "" {
		return 0
	}

	ac.mu.Lock()
	defer ac.mu.Unlock()

	if _, exists := ac.keywordSet[keyword]; !exists {
		return 0
	}

	delete(ac.keywordSet, keyword)
	ac.engine.buildFromKeywords(ac.keywordSet)
	return 1
}

// Find searches the text for all keywords and returns matched keywords as a
// slice of strings in order of occurrence.
func (ac *InMemoryAC) Find(text string) []string {
	if text == "" {
		return []string{}
	}
	if !ac.caseSensitive {
		text = strings.ToLower(text)
	}

	ac.mu.RLock()
	defer ac.mu.RUnlock()

	return ac.engine.find(text)
}

// FindIndex searches the text for all keywords and returns a map of keyword
// to the slice of start indices where each keyword was found.
func (ac *InMemoryAC) FindIndex(text string) map[string][]int {
	if text == "" {
		return map[string][]int{}
	}
	if !ac.caseSensitive {
		text = strings.ToLower(text)
	}

	ac.mu.RLock()
	defer ac.mu.RUnlock()

	return ac.engine.findIndex(text)
}

// Flush removes all keywords from the automaton, resetting it to empty state.
func (ac *InMemoryAC) Flush() {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	ac.keywordSet = make(map[string]struct{})
	ac.engine.buildFromKeywords(ac.keywordSet)
}

// Info returns statistics about the current automaton state.
func (ac *InMemoryAC) Info() *InMemoryInfo {
	ac.mu.RLock()
	defer ac.mu.RUnlock()

	return ac.engine.info()
}

// Preset returns the architecture preset used by this engine.
func (ac *InMemoryAC) Preset() Preset {
	return ac.preset
}
