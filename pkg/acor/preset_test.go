// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"testing"

	"github.com/skyoo2003/acor/internal/engine"
)

// TestEnginePresetMapping pins every public preset to the engine architecture it
// selects. The public enum is frozen by the v1 compatibility promise and the
// engine's is not, so nothing else would notice a preset quietly starting to
// build a different automaton — enginePreset's default case would absorb it.
func TestEnginePresetMapping(t *testing.T) {
	tests := []struct {
		name string
		in   Preset
		want engine.Preset
	}{
		{"None", PresetNone, engine.PresetNone},
		{"Speed", PresetSpeed, engine.PresetSpeed},
		{"Balanced", PresetBalanced, engine.PresetBalanced},
		{"MemoryEfficient", PresetMemoryEfficient, engine.PresetMemoryEfficient},
		{"default sentinel", presetDefault, engine.PresetDefault},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := enginePreset(tt.in); got != tt.want {
				t.Errorf("enginePreset(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// TestPresetFromEngineRoundTrip covers the three architectures an engine reports
// back through Info. None and Default select an implementation but are never
// reported by one, so they round-trip to Balanced by design.
func TestPresetFromEngineRoundTrip(t *testing.T) {
	for _, p := range []Preset{PresetSpeed, PresetBalanced, PresetMemoryEfficient} {
		if got := presetFromEngine(enginePreset(p)); got != p {
			t.Errorf("presetFromEngine(enginePreset(%v)) = %v, want %v", p, got, p)
		}
	}
	if got := presetFromEngine(enginePreset(PresetNone)); got != PresetBalanced {
		t.Errorf("PresetNone reported back as %v, want Balanced", got)
	}
}

// TestPresetString guards the names, which are documented behavior of a public
// type and therefore covered by the v1 promise.
func TestPresetString(t *testing.T) {
	want := map[Preset]string{
		PresetNone:            "None",
		PresetSpeed:           "Speed",
		PresetBalanced:        "Balanced",
		PresetMemoryEfficient: "MemoryEfficient",
		presetDefault:         "Default",
		Preset(99):            "Unknown",
	}
	for p, w := range want {
		if got := p.String(); got != w {
			t.Errorf("Preset(%d).String() = %q, want %q", int(p), got, w)
		}
	}
}
