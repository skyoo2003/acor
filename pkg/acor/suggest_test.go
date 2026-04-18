// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"testing"
)

//nolint:funlen
func TestSuggest(t *testing.T) {
	tests := []struct {
		name      string
		keywords  []string
		input     string
		wantLen   int
		wantMatch []string
	}{
		{
			name:      "single suggestion",
			keywords:  []string{"he"},
			input:     "h",
			wantLen:   1,
			wantMatch: []string{"he"},
		},
		{
			name:      "multiple suggestions",
			keywords:  []string{"her", "he", "his"},
			input:     "he",
			wantLen:   2,
			wantMatch: []string{"he", "her"},
		},
		{
			name:      "no suggestions",
			keywords:  []string{"test"},
			input:     "xyz",
			wantLen:   0,
			wantMatch: nil,
		},
		{
			name:      "exact match",
			keywords:  []string{"hello"},
			input:     "hello",
			wantLen:   1,
			wantMatch: []string{"hello"},
		},
		{
			name:      "unicode suggestions",
			keywords:  []string{"한글", "한국"},
			input:     "한",
			wantLen:   2,
			wantMatch: []string{"한글", "한국"},
		},
		{
			name:      "empty input",
			keywords:  []string{"test"},
			input:     "",
			wantLen:   0,
			wantMatch: nil,
		},
		{
			name:      "common prefix",
			keywords:  []string{"app", "apple", "application", "apply"},
			input:     "app",
			wantLen:   4,
			wantMatch: []string{"app", "apple", "application", "apply"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ac, mr := createAhoCorasick(t)
			defer mr.Close()
			defer func() { _ = ac.Close() }()
			defer func() { _ = ac.Flush() }()

			for _, kw := range tt.keywords {
				if _, err := ac.Add(kw); err != nil {
					t.Fatalf("Add(%q) error: %v", kw, err)
				}
			}

			results, err := ac.Suggest(tt.input)
			if err != nil {
				t.Fatalf("Suggest(%q) error: %v", tt.input, err)
			}

			if len(results) != tt.wantLen {
				t.Errorf("Suggest(%q) = %d results, want %d", tt.input, len(results), tt.wantLen)
			}

			if tt.wantMatch != nil {
				for _, want := range tt.wantMatch {
					found := false
					for _, got := range results {
						if got == want {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("Suggest(%q) missing expected match %q", tt.input, want)
					}
				}
			}
		})
	}
}

func TestSuggestIndex(t *testing.T) {
	tests := []struct {
		name     string
		keywords []string
		input    string
		want     map[string][]int
	}{
		{
			name:     "single suggestion",
			keywords: []string{"he"},
			input:    "h",
			want:     map[string][]int{"he": {0}},
		},
		{
			name:     "multiple suggestions",
			keywords: []string{"her", "he", "his"},
			input:    "he",
			want:     map[string][]int{"he": {0}, "her": {0}},
		},
		{
			name:     "no suggestions",
			keywords: []string{"test"},
			input:    "xyz",
			want:     map[string][]int{},
		},
		{
			name:     "unicode suggestions",
			keywords: []string{"한글"},
			input:    "한",
			want:     map[string][]int{"한글": {0}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ac, mr := createAhoCorasick(t)
			defer mr.Close()
			defer func() { _ = ac.Close() }()
			defer func() { _ = ac.Flush() }()

			for _, kw := range tt.keywords {
				if _, err := ac.Add(kw); err != nil {
					t.Fatalf("Add(%q) error: %v", kw, err)
				}
			}

			results, err := ac.SuggestIndex(tt.input)
			if err != nil {
				t.Fatalf("SuggestIndex(%q) error: %v", tt.input, err)
			}

			assertIndexResults(t, results, tt.want)
		})
	}
}

// TestSuggestWithSchemaVersion merges TestV1Suggest and TestV2Suggest into
// a single table-driven test parameterized by schema version.
func TestSuggestWithSchemaVersion(t *testing.T) {
	tests := []struct {
		name      string
		schema    int
		keywords  []string
		input     string
		wantSome  []string
		wantEmpty bool
	}{
		{
			name: "v1/prefix_he", schema: SchemaV1,
			keywords: []string{"he", "she", "hello", "help", "her"},
			input:    "he", wantSome: []string{"he", "hello", "help", "her"},
		},
		{
			name: "v1/prefix_sh", schema: SchemaV1,
			keywords: []string{"he", "she", "hello", "help", "her"},
			input:    "sh", wantSome: []string{"she"},
		},
		{
			name: "v1/no_match", schema: SchemaV1,
			keywords: []string{"he", "she", "hello", "help", "her"},
			input:    "xyz", wantEmpty: true,
		},
		{
			name: "v2/prefix_app", schema: SchemaV2,
			keywords: []string{"apple", "application", "apply", "banana"},
			input:    "app", wantSome: []string{"apple", "application", "apply"},
		},
		{
			name: "v2/prefix_ban", schema: SchemaV2,
			keywords: []string{"apple", "application", "apply", "banana"},
			input:    "ban", wantSome: []string{"banana"},
		},
		{
			name: "v2/no_match", schema: SchemaV2,
			keywords: []string{"apple", "application", "apply", "banana"},
			input:    "xyz", wantEmpty: true,
		},
		{
			name: "v2/empty_string", schema: SchemaV2,
			keywords: []string{"apple", "application", "apply", "banana"},
			input:    "", wantEmpty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ac, mr := createAhoCorasickWithSchema(t, tt.schema)
			defer mr.Close()
			defer func() { _ = ac.Close() }()
			defer func() { _ = ac.Flush() }()

			for _, kw := range tt.keywords {
				if _, addErr := ac.Add(kw); addErr != nil {
					t.Fatal(addErr)
				}
			}

			results, err := ac.Suggest(tt.input)
			if err != nil {
				t.Fatalf("Suggest(%q) error: %v", tt.input, err)
			}

			if tt.wantEmpty {
				if len(results) != 0 {
					t.Errorf("Suggest(%q) = %v, want empty", tt.input, results)
				}
				return
			}

			for _, want := range tt.wantSome {
				if !containsAll(results, want) {
					t.Errorf("Suggest(%q) = %v, missing %q", tt.input, results, want)
				}
			}
		})
	}
}

func TestV1SuggestIndex(t *testing.T) {
	ac, mr := createAhoCorasickV1(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	keywords := []string{"he", "she", "hello"}
	for _, kw := range keywords {
		if _, err := ac.Add(kw); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		name     string
		input    string
		wantSome []string
		wantNone bool
	}{
		{"prefix he", "he", []string{"he", "hello"}, false},
		{"no match", "xyz", nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := ac.SuggestIndex(tt.input)
			if err != nil {
				t.Fatalf("SuggestIndex(%q) error: %v", tt.input, err)
			}
			if tt.wantNone {
				if len(results) != 0 {
					t.Errorf("SuggestIndex(%q) = %v, want empty", tt.input, results)
				}
				return
			}
			for _, want := range tt.wantSome {
				if _, ok := results[want]; !ok {
					t.Errorf("SuggestIndex(%q) missing %q", tt.input, want)
				}
			}
		})
	}
}
