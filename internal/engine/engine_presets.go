// SPDX-License-Identifier: Apache-2.0

package engine

// presetConfig holds tuning parameters for each architecture preset.
type presetConfig struct {
	name      string
	bandDepth int // depth at which DFA transitions stop (0 = full DFA, -1 = no DFA)
}

var presetConfigs = map[Preset]presetConfig{
	PresetSpeed:           {name: "speed", bandDepth: 0},
	PresetBalanced:        {name: "balanced", bandDepth: 3},
	PresetMemoryEfficient: {name: "memory-efficient", bandDepth: -1},
	PresetUltimate:        {name: "ultimate", bandDepth: 3},
}

// newMatchEngine creates the appropriate matchEngine implementation for the given preset.
func newMatchEngine(preset Preset) matchEngine {
	switch preset {
	case PresetNone, PresetDefault:
		return newBalancedEngine(presetConfigs[PresetBalanced].bandDepth)
	case PresetSpeed:
		return newSpeedEngine()
	case PresetBalanced:
		return newBalancedEngine(presetConfigs[PresetBalanced].bandDepth)
	case PresetMemoryEfficient:
		return newMemEfficientEngine()
	case PresetUltimate:
		return newUltimateEngine(presetConfigs[PresetUltimate].bandDepth)
	default:
		return newBalancedEngine(presetConfigs[PresetBalanced].bandDepth)
	}
}
