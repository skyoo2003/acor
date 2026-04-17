// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"testing"
)

func TestRemoveReturnSemantics(t *testing.T) { //nolint:funlen
	tests := []struct {
		name       string
		schema     int
		addFirst   []string
		removeArgs []string
		wantCounts []int
	}{
		{
			name:       "v2 remove existing returns 1",
			schema:     SchemaV2,
			addFirst:   []string{"hello"},
			removeArgs: []string{"hello"},
			wantCounts: []int{1},
		},
		{
			name:       "v2 remove nonexistent returns 0",
			schema:     SchemaV2,
			addFirst:   []string{"hello"},
			removeArgs: []string{"world"},
			wantCounts: []int{0},
		},
		{
			name:       "v2 remove empty returns 0",
			schema:     SchemaV2,
			addFirst:   []string{"hello"},
			removeArgs: []string{""},
			wantCounts: []int{0},
		},
		{
			name:       "v2 double remove returns 1 then 0",
			schema:     SchemaV2,
			addFirst:   []string{"dup"},
			removeArgs: []string{"dup", "dup"},
			wantCounts: []int{1, 0},
		},
		{
			name:       "v2 remove from multi-keyword set returns 1",
			schema:     SchemaV2,
			addFirst:   []string{"alpha", "beta", "gamma", "delta", "epsilon"},
			removeArgs: []string{"gamma"},
			wantCounts: []int{1},
		},
		{
			name:       "v2 remove last keyword returns 1",
			schema:     SchemaV2,
			addFirst:   []string{"solo"},
			removeArgs: []string{"solo"},
			wantCounts: []int{1},
		},
		{
			name:       "v1 remove existing returns 1",
			schema:     SchemaV1,
			addFirst:   []string{"hello"},
			removeArgs: []string{"hello"},
			wantCounts: []int{1},
		},
		{
			name:       "v1 remove nonexistent returns 0",
			schema:     SchemaV1,
			addFirst:   []string{"hello"},
			removeArgs: []string{"world"},
			wantCounts: []int{0},
		},
		{
			name:       "v1 remove empty returns 0",
			schema:     SchemaV1,
			addFirst:   []string{"hello"},
			removeArgs: []string{""},
			wantCounts: []int{0},
		},
		{
			name:       "v1 double remove returns 1 then 0",
			schema:     SchemaV1,
			addFirst:   []string{"dup"},
			removeArgs: []string{"dup", "dup"},
			wantCounts: []int{1, 0},
		},
		{
			name:       "v1 remove from multi-keyword set returns 1",
			schema:     SchemaV1,
			addFirst:   []string{"alpha", "beta", "gamma", "delta", "epsilon"},
			removeArgs: []string{"gamma"},
			wantCounts: []int{1},
		},
		{
			name:       "v1 remove last keyword returns 1",
			schema:     SchemaV1,
			addFirst:   []string{"solo"},
			removeArgs: []string{"solo"},
			wantCounts: []int{1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ac, mr := createAhoCorasickWithSchema(t, tt.schema)
			defer mr.Close()
			defer func() { _ = ac.Close() }()

			for _, kw := range tt.addFirst {
				if _, err := ac.Add(kw); err != nil {
					t.Fatalf("Add(%q) failed: %v", kw, err)
				}
			}

			for i, arg := range tt.removeArgs {
				count, err := ac.Remove(arg)
				if err != nil {
					t.Fatalf("Remove(%q) error: %v", arg, err)
				}
				if count != tt.wantCounts[i] {
					t.Errorf("Remove(%q) = %d, want %d", arg, count, tt.wantCounts[i])
				}
			}
		})
	}
}
