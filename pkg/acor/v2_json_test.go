// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"testing"
)

func TestToJSON(t *testing.T) {
	result, err := toJSON([]string{"a", "b"})
	if err != nil {
		t.Fatalf("toJSON failed: %v", err)
	}
	if result != `["a","b"]` {
		t.Errorf("toJSON([a,b]) = %q, want %q", result, `["a","b"]`)
	}

	result, err = toJSON(map[string]int{"x": 1})
	if err != nil {
		t.Fatalf("toJSON failed: %v", err)
	}
	if result != `{"x":1}` {
		t.Errorf("toJSON({x:1}) = %q, want %q", result, `{"x":1}`)
	}
}

func TestToJSONError(t *testing.T) {
	_, err := toJSON(func() {})
	if err == nil {
		t.Fatal("expected toJSON to return error for unmarshallable value")
	}
}

func TestComputeOutputsV2(t *testing.T) {
	ops := &v2Operations{}

	prefixSet := map[string]struct{}{
		"":    {},
		"h":   {},
		"he":  {},
		"her": {},
		"s":   {},
		"sh":  {},
		"she": {},
	}
	keywordSet := map[string]struct{}{
		"he":  {},
		"she": {},
		"her": {},
	}

	tests := []struct {
		state string
		want  []string
	}{
		{"he", []string{"he"}},
		{"she", []string{"she", "he"}},
		{"her", []string{"her"}},
		{"h", nil},
		{"s", nil},
	}

	for _, tt := range tests {
		t.Run(tt.state, func(t *testing.T) {
			got := ops.computeOutputsV2(tt.state, prefixSet, keywordSet)
			if len(got) != len(tt.want) {
				t.Fatalf("computeOutputsV2(%q) = %v, want %v", tt.state, got, tt.want)
			}
			for i, v := range got {
				if v != tt.want[i] {
					t.Errorf("computeOutputsV2(%q)[%d] = %q, want %q", tt.state, i, v, tt.want[i])
				}
			}
		})
	}
}
