package acor

import (
	"strings"
	"testing"

	miniredis "github.com/alicebob/miniredis/v2"
)

//nolint:funlen
func TestFind(t *testing.T) {
	tests := []struct {
		name      string
		keywords  []string
		input     string
		wantLen   int
		wantMatch []string
	}{
		{
			name:      "single match",
			keywords:  []string{"her", "he", "his"},
			input:     "he",
			wantLen:   1,
			wantMatch: []string{"he"},
		},
		{
			name:      "multiple matches",
			keywords:  []string{"her", "he", "his"},
			input:     "her",
			wantLen:   2,
			wantMatch: []string{"he", "her"},
		},
		{
			name:      "no match",
			keywords:  []string{"her", "he", "his"},
			input:     "xyz",
			wantLen:   0,
			wantMatch: nil,
		},
		{
			name:      "repeated pattern finds all occurrences",
			keywords:  []string{"he"},
			input:     "hehe",
			wantLen:   2,
			wantMatch: []string{"he"},
		},
		{
			name:      "unicode input",
			keywords:  []string{"한글"},
			input:     "가한글나",
			wantLen:   1,
			wantMatch: []string{"한글"},
		},
		{
			name:      "emoji input",
			keywords:  []string{"😀"},
			input:     "hello😀world",
			wantLen:   1,
			wantMatch: []string{"😀"},
		},
		{
			name:      "special characters",
			keywords:  []string{"@user", "#tag"},
			input:     "hello @user and #tag",
			wantLen:   2,
			wantMatch: []string{"@user", "#tag"},
		},
		{
			name:      "empty input",
			keywords:  []string{"test"},
			input:     "",
			wantLen:   0,
			wantMatch: nil,
		},
		{
			name:      "long input",
			keywords:  []string{"needle"},
			input:     strings.Repeat("haystack ", 100) + "needle",
			wantLen:   1,
			wantMatch: []string{"needle"},
		},
		{
			name:      "overlapping keywords",
			keywords:  []string{"a", "ab", "abc"},
			input:     "abc",
			wantLen:   3,
			wantMatch: []string{"a", "ab", "abc"},
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

			results, err := ac.Find(tt.input)
			if err != nil {
				t.Fatalf("Find(%q) error: %v", tt.input, err)
			}

			if len(results) != tt.wantLen {
				t.Errorf("Find(%q) = %d results, want %d", tt.input, len(results), tt.wantLen)
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
						t.Errorf("Find(%q) missing expected match %q", tt.input, want)
					}
				}
			}
		})
	}
}

func TestFindIndex(t *testing.T) {
	tests := []struct {
		name     string
		keywords []string
		input    string
		want     map[string][]int
	}{
		{
			name:     "single match at start",
			keywords: []string{"he"},
			input:    "he",
			want:     map[string][]int{"he": {0}},
		},
		{
			name:     "overlapping matches",
			keywords: []string{"her", "he", "his"},
			input:    "her",
			want:     map[string][]int{"he": {0}, "her": {0}},
		},
		{
			name:     "repeated pattern",
			keywords: []string{"he"},
			input:    "hehe",
			want:     map[string][]int{"he": {0, 2}},
		},
		{
			name:     "no match",
			keywords: []string{"test"},
			input:    "xyz",
			want:     map[string][]int{},
		},
		{
			name:     "unicode match",
			keywords: []string{"한글"},
			input:    "가한글",
			want:     map[string][]int{"한글": {1}},
		},
		{
			name:     "multiple occurrences",
			keywords: []string{"ab"},
			input:    "ababab",
			want:     map[string][]int{"ab": {0, 2, 4}},
		},
		{
			name:     "emoji match",
			keywords: []string{"😀"},
			input:    "a😀b😀c",
			want:     map[string][]int{"😀": {1, 3}},
		},
		{
			name:     "nested keywords",
			keywords: []string{"a", "aa", "aaa"},
			input:    "aaa",
			want:     map[string][]int{"a": {0, 1, 2}, "aa": {0, 1}, "aaa": {0}},
		},
		{
			name:     "empty input",
			keywords: []string{"test"},
			input:    "",
			want:     map[string][]int{},
		},
		{
			name:     "special characters",
			keywords: []string{"@@"},
			input:    "@@@@",
			want:     map[string][]int{"@@": {0, 1, 2}},
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

			results, err := ac.FindIndex(tt.input)
			if err != nil {
				t.Fatalf("FindIndex(%q) error: %v", tt.input, err)
			}

			assertIndexResults(t, results, tt.want)
		})
	}
}

func TestFindReturnsErrorWhenRedisUnavailable(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer func() { _ = ac.Close() }()

	if _, err := ac.Add("he"); err != nil {
		t.Fatal(err)
	}

	mr.Close()

	if _, err := ac.Find("he"); err == nil {
		t.Fatal("expected find to return an error")
	}
}

func TestDefaultParallelOptions(t *testing.T) {
	opts := DefaultParallelOptions()
	if opts == nil {
		t.Fatal("DefaultParallelOptions() returned nil")
	}
	if opts.Workers <= 0 {
		t.Errorf("Workers = %d, want > 0", opts.Workers)
	}
	if opts.ChunkSize != DefaultChunkSize {
		t.Errorf("ChunkSize = %d, want %d", opts.ChunkSize, DefaultChunkSize)
	}
	if opts.Boundary != ChunkBoundaryWord {
		t.Errorf("Boundary = %d, want %d", opts.Boundary, ChunkBoundaryWord)
	}
	if opts.Overlap != DefaultOverlap {
		t.Errorf("Overlap = %d, want %d", opts.Overlap, DefaultOverlap)
	}
}

func TestIsBoundaryEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		idx      int
		boundary ChunkBoundary
		want     bool
	}{
		{"word boundary at space", "hello world", 5, ChunkBoundaryWord, true},
		{"word boundary at world", "hello world", 6, ChunkBoundaryWord, false},
		{"not word boundary", "hello world", 3, ChunkBoundaryWord, false},
		{"line boundary", "hello\nworld", 6, ChunkBoundaryLine, true},
		{"not line boundary", "hello world", 5, ChunkBoundaryLine, false},
		{"sentence boundary at space", "hello. world", 6, ChunkBoundarySentence, true},
		{"not sentence boundary", "hello world", 5, ChunkBoundarySentence, false},
		{"index 0", "hello", 0, ChunkBoundaryWord, false},
		{"index at end", "hello", 5, ChunkBoundaryWord, false},
		{"invalid boundary type", "hello", 2, ChunkBoundary(99), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runes := []rune(tt.text)
			got := isBoundary(runes, tt.idx, tt.boundary)
			if got != tt.want {
				t.Errorf("isBoundary(%q, %d, %v) = %v, want %v", tt.text, tt.idx, tt.boundary, got, tt.want)
			}
		})
	}
}

func TestFindParallelEdgeCases(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	keywords := []string{"test", "hello", "world"}
	for _, kw := range keywords {
		if _, err := ac.Add(kw); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		name string
		text string
		opts *ParallelOptions
	}{
		{"empty text", "", nil},
		{"short text", "test", nil},
		{"line boundaries", "test\nhello\nworld", &ParallelOptions{Workers: 2, ChunkSize: 5, Boundary: ChunkBoundaryLine}},
		{"sentence boundaries", "Test. Hello world.", &ParallelOptions{Workers: 2, ChunkSize: 10, Boundary: ChunkBoundarySentence}},
		{"nil options uses defaults", "test hello world", nil},
		{"single worker", "test hello world", &ParallelOptions{Workers: 1, ChunkSize: 100}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ac.FindParallel(tt.text, tt.opts)
			if err != nil {
				t.Errorf("FindParallel() error: %v", err)
			}
		})
	}
}

func TestFindIndexParallelEdgeCases(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	keywords := []string{"test", "hello"}
	for _, kw := range keywords {
		if _, err := ac.Add(kw); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		name string
		text string
	}{
		{"empty text", ""},
		{"short text", "test"},
		{"long text", strings.Repeat("test hello ", 100)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ac.FindIndexParallel(tt.text, nil)
			if err != nil {
				t.Errorf("FindIndexParallel() error: %v", err)
			}
		})
	}
}

func TestCache_FindUsesLocalCache(t *testing.T) {
	mr := miniredis.RunT(t)

	ac, err := Create(&AhoCorasickArgs{
		Addr:        mr.Addr(),
		Name:        "test-cache-find",
		EnableCache: true,
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	defer func() { _ = ac.Close() }()

	if _, addErr := ac.Add("hello"); addErr != nil {
		t.Fatal(addErr)
	}

	results, err := ac.Find("hello world")
	if err != nil {
		t.Fatalf("Find failed: %v", err)
	}
	const wantKeyword = "hello"
	if len(results) != 1 || results[0] != wantKeyword {
		t.Errorf("Find() = %v, want [%s]", results, wantKeyword)
	}

	_, _, valid := ac.cache.get()
	if !valid {
		t.Error("expected cache to be valid after Find")
	}

	results2, err := ac.Find("hello there")
	if err != nil {
		t.Fatalf("Second Find failed: %v", err)
	}
	if len(results2) != 1 || results2[0] != wantKeyword {
		t.Errorf("Second Find() = %v, want [%s]", results2, wantKeyword)
	}

	// Stop Redis to prove reads come from local cache
	mr.Close()

	results3, err := ac.Find("hello again")
	if err != nil {
		t.Fatalf("Find after Redis stop failed: %v", err)
	}
	if len(results3) != 1 || results3[0] != wantKeyword {
		t.Errorf("Find after Redis stop = %v, want [%s]", results3, wantKeyword)
	}
}

func TestCache_FindIndexUsesLocalCache(t *testing.T) {
	mr := miniredis.RunT(t)

	ac, err := Create(&AhoCorasickArgs{
		Addr:        mr.Addr(),
		Name:        "test-cache-findindex",
		EnableCache: true,
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	defer func() { _ = ac.Close() }()

	if _, err = ac.Add("hello"); err != nil {
		t.Fatal(err)
	}

	_, _, valid := ac.cache.get()
	if valid {
		t.Fatal("expected cache to be invalid before FindIndex")
	}

	matches, err := ac.FindIndex("hello world")
	if err != nil {
		t.Fatalf("FindIndex failed: %v", err)
	}
	if len(matches) != 1 || len(matches["hello"]) != 1 {
		t.Errorf("FindIndex() = %v, want matches with hello at one position", matches)
	}

	_, _, valid = ac.cache.get()
	if !valid {
		t.Error("expected cache to be valid after FindIndex")
	}

	// Stop Redis to prove reads come from local cache
	mr.Close()

	matches2, err := ac.FindIndex("hello world")
	if err != nil {
		t.Fatalf("FindIndex after Redis stop failed: %v", err)
	}
	if len(matches2) != 1 || len(matches2["hello"]) != 1 {
		t.Errorf("FindIndex after Redis stop = %v, want matches with hello", matches2)
	}
}
