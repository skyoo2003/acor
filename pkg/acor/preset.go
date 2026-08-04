// SPDX-License-Identifier: Apache-2.0

package acor

import "github.com/skyoo2003/acor/internal/engine"

// Preset selects the architecture for the in-memory Aho-Corasick engine.
// See the engine presets for the trade-offs each option makes between speed,
// memory, and feature set. The preset is fixed at creation time.
type Preset = engine.Preset

// InMemoryInfo contains statistics about an in-memory Aho-Corasick engine.
// Re-exported from the internal engine package so callers depend only on the
// public acor API.
type InMemoryInfo = engine.InMemoryInfo

// Preset values re-exported from the internal engine package so callers depend
// only on the public acor API.
const (
	// PresetNone is the zero value (unset). Create falls through to the original
	// V1/V2 Redis-backed mode when Preset is PresetNone.
	PresetNone = engine.PresetNone
	// PresetSpeed prioritizes maximum search speed (Full DFA, flat array trie).
	PresetSpeed = engine.PresetSpeed
	// PresetBalanced provides the best speed-to-memory ratio (DAT + Banded DFA).
	PresetBalanced = engine.PresetBalanced
	// PresetMemoryEfficient minimizes memory usage (map-based sparse trie + Bloom).
	PresetMemoryEfficient = engine.PresetMemoryEfficient
	// PresetUltimate is an alias for PresetBalanced. It used to add a root-state
	// first-rune Bloom pre-filter, which measured 1.7-1.8x slower than Balanced on
	// every benchmark in the repository, on ASCII and multibyte text alike: the
	// per-character filter check cost more than the trie work it skipped, and it
	// blocked the byte-scan fast path an ASCII dictionary can take. Existing code
	// keeps compiling and now gets the faster engine.
	//
	// Deprecated: use PresetBalanced.
	PresetUltimate = engine.PresetBalanced
)

// presetDefault is an internal sentinel (-1) meaning "unset"; it behaves
// identically to PresetNone and is not part of the public API.
const presetDefault = engine.PresetDefault
