// SPDX-License-Identifier: Apache-2.0

package acor

import "github.com/skyoo2003/acor/internal/engine"

// Preset selects the architecture for the in-memory Aho-Corasick engine. Each
// value names a different trade-off between speed, memory, and feature set,
// documented on the constant. The preset is fixed at creation time.
type Preset int

const (
	// PresetNone is the zero value (unset). Create falls through to the original
	// V1/V2 Redis-backed mode when Preset is PresetNone.
	PresetNone Preset = iota
	// PresetSpeed prioritizes maximum search speed (Full DFA, flat array trie).
	// Trade-off: memory grows with states × alphabet size.
	PresetSpeed
	// PresetBalanced provides the best speed-to-memory ratio (DAT + Banded DFA).
	PresetBalanced
	// PresetMemoryEfficient minimizes memory usage (map-based sparse trie + Bloom).
	// Trade-off: slower search from failure-link traversal and map lookups.
	PresetMemoryEfficient
)

// presetDefault is an internal sentinel (-1) meaning "unset"; it behaves
// identically to PresetNone and is not part of the public API.
const presetDefault Preset = -1

// String returns the preset name, or "Unknown" for a value outside the set.
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
	case presetDefault:
		return "Default"
	default:
		return "Unknown"
	}
}

// enginePreset maps a public preset onto the internal engine's own enum.
//
// The two enums are mapped by name, not converted numerically: this package's
// values are frozen by the v1 compatibility promise while the engine's are free
// to be reordered, and a numeric cast would silently repoint a preset at a
// different architecture the first time that happened.
func enginePreset(p Preset) engine.Preset {
	switch p {
	case PresetNone:
		return engine.PresetNone
	case PresetSpeed:
		return engine.PresetSpeed
	case PresetBalanced:
		return engine.PresetBalanced
	case PresetMemoryEfficient:
		return engine.PresetMemoryEfficient
	case presetDefault:
		return engine.PresetDefault
	default:
		// The engine resolves an unrecognized preset to Balanced; mapping it here
		// keeps that behavior explicit instead of relying on the engine's default.
		return engine.PresetBalanced
	}
}

// presetFromEngine maps an engine preset back for reporting in AhoCorasickInfo.
// Only the three architectures an engine can report reach this: None and Default
// select an implementation but are never reported back by one.
func presetFromEngine(p engine.Preset) Preset {
	switch p {
	case engine.PresetSpeed:
		return PresetSpeed
	case engine.PresetMemoryEfficient:
		return PresetMemoryEfficient
	case engine.PresetNone, engine.PresetBalanced, engine.PresetDefault:
		return PresetBalanced
	}
	// A preset added to the engine but not mapped here: Balanced is the engine's own
	// fallback for an unrecognized value, so reporting it keeps Info honest.
	return PresetBalanced
}
