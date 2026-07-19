// SPDX-License-Identifier: Apache-2.0

package engine

// defaultBandDepth is the trie depth at which Balanced/Ultimate DFA transitions
// stop and NFA failure-link fallback takes over. Speed uses a full DFA and
// MemoryEfficient a pure NFA, so neither consults this.
const defaultBandDepth = 3

// newMatchEngine creates the appropriate matchEngine implementation for the given preset.
func newMatchEngine(preset Preset) matchEngine {
	switch preset {
	case PresetSpeed:
		return newSpeedEngine()
	case PresetMemoryEfficient:
		return newMemEfficientEngine()
	case PresetUltimate:
		return newUltimateEngine(defaultBandDepth)
	case PresetNone, PresetBalanced, PresetDefault:
		return newBalancedEngine(defaultBandDepth)
	default:
		return newBalancedEngine(defaultBandDepth)
	}
}
