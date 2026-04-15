package acor

import (
	"testing"

	miniredis "github.com/alicebob/miniredis/v2"
)

// createAhoCorasickCaseSensitive creates a V2 instance with CaseSensitive=true.
func createAhoCorasickCaseSensitive(t *testing.T) (*AhoCorasick, *miniredis.Miniredis) {
	t.Helper()
	mr := createTestRedisServer(t)
	ac, err := Create(&AhoCorasickArgs{
		Addr:          mr.Addr(),
		Password:      "",
		DB:            0,
		Name:          "test-cs",
		Debug:         false,
		CaseSensitive: true,
	})
	if err != nil {
		mr.Close()
		t.Fatal(err)
	}
	return ac, mr
}

// createAhoCorasickCaseSensitiveV1 creates a V1 instance with CaseSensitive=true.
func createAhoCorasickCaseSensitiveV1(t *testing.T) (*AhoCorasick, *miniredis.Miniredis) {
	t.Helper()
	mr := createTestRedisServer(t)
	ac, err := Create(&AhoCorasickArgs{
		Addr:          mr.Addr(),
		Password:      "",
		DB:            0,
		Name:          "test-cs-v1",
		Debug:         false,
		SchemaVersion: SchemaV1,
		CaseSensitive: true,
	})
	if err != nil {
		mr.Close()
		t.Fatal(err)
	}
	return ac, mr
}

func createAhoCorasickWithOpts(t *testing.T, schemaVersion int, caseSensitive bool) (*AhoCorasick, *miniredis.Miniredis) {
	t.Helper()
	mr := createTestRedisServer(t)
	ac, err := Create(&AhoCorasickArgs{
		Addr:          mr.Addr(),
		Password:      "",
		DB:            0,
		Name:          "test-opts",
		Debug:         false,
		SchemaVersion: schemaVersion,
		CaseSensitive: caseSensitive,
	})
	if err != nil {
		mr.Close()
		t.Fatal(err)
	}
	return ac, mr
}

// --- Case-insensitive Find tests (default behavior) ---

func TestFindCaseInsensitiveDefault(t *testing.T) {
	tests := []struct {
		name      string
		keywords  []string
		input     string
		wantLen   int
		wantMatch []string
	}{
		{
			name:      "uppercase keyword matches lowercase text",
			keywords:  []string{"Hello"},
			input:     "hello world",
			wantLen:   1,
			wantMatch: []string{"hello"},
		},
		{
			name:      "mixed case text matches lowercase keyword",
			keywords:  []string{"hello"},
			input:     "Hello World",
			wantLen:   1,
			wantMatch: []string{"hello"},
		},
		{
			name:      "mixed case keyword matches mixed case text",
			keywords:  []string{"HeLLo"},
			input:     "hElLo WoRlD",
			wantLen:   1,
			wantMatch: []string{"hello"},
		},
		{
			name:      "multiple mixed case keywords",
			keywords:  []string{"Hello", "World"},
			input:     "HELLO WORLD",
			wantLen:   2,
			wantMatch: []string{"hello", "world"},
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
		})
	}
}

func TestFindIndexCaseInsensitiveDefault(t *testing.T) {
	tests := []struct {
		name     string
		keywords []string
		input    string
		want     map[string][]int
	}{
		{
			name:     "uppercase keyword matches lowercase text",
			keywords: []string{"Hello"},
			input:    "hello world",
			want:     map[string][]int{"hello": {0}},
		},
		{
			name:     "mixed case text matches lowercase keyword",
			keywords: []string{"hello"},
			input:    "Hello World",
			want:     map[string][]int{"hello": {0}},
		},
		{
			name:     "multiple occurrences with case mismatch",
			keywords: []string{"Hello"},
			input:    "hello HELLO Hello",
			want:     map[string][]int{"hello": {0, 6, 12}},
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

// --- Case-sensitive Find tests ---

func TestFindCaseSensitive(t *testing.T) {
	tests := []struct {
		name      string
		keywords  []string
		input     string
		wantLen   int
		wantMatch []string
	}{
		{
			name:      "exact case match",
			keywords:  []string{"Hello"},
			input:     "Hello World",
			wantLen:   1,
			wantMatch: []string{"Hello"},
		},
		{
			name:      "case mismatch does not match",
			keywords:  []string{"Hello"},
			input:     "hello world",
			wantLen:   0,
			wantMatch: nil,
		},
		{
			name:      "case mismatch reversed",
			keywords:  []string{"hello"},
			input:     "Hello World",
			wantLen:   0,
			wantMatch: nil,
		},
		{
			name:      "partial case match does not match",
			keywords:  []string{"Hello"},
			input:     "HELLO",
			wantLen:   0,
			wantMatch: nil,
		},
		{
			name:      "multiple case-sensitive keywords",
			keywords:  []string{"Hello", "hello", "HELLO"},
			input:     "Hello hello HELLO",
			wantLen:   3,
			wantMatch: []string{"Hello", "hello", "HELLO"},
		},
		{
			name:      "special characters preserved",
			keywords:  []string{"@User"},
			input:     "@User @user",
			wantLen:   1,
			wantMatch: []string{"@User"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ac, mr := createAhoCorasickCaseSensitive(t)
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
		})
	}
}

func TestFindIndexCaseSensitive(t *testing.T) {
	tests := []struct {
		name     string
		keywords []string
		input    string
		want     map[string][]int
	}{
		{
			name:     "exact case match with index",
			keywords: []string{"Hello"},
			input:    "Hello World",
			want:     map[string][]int{"Hello": {0}},
		},
		{
			name:     "case mismatch no match",
			keywords: []string{"Hello"},
			input:    "hello World",
			want:     map[string][]int{},
		},
		{
			name:     "multiple case variants with indices",
			keywords: []string{"Hello", "hello"},
			input:    "Hello hello",
			want:     map[string][]int{"Hello": {0}, "hello": {6}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ac, mr := createAhoCorasickCaseSensitive(t)
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

// --- Case-sensitive Add dedup tests ---

func TestAddCaseSensitive(t *testing.T) {
	ac, mr := createAhoCorasickCaseSensitive(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	// "Hello" and "hello" are distinct keywords when case-sensitive
	count1, err := ac.Add("Hello")
	if err != nil {
		t.Fatalf("Add(Hello) error: %v", err)
	}
	if count1 != 1 {
		t.Errorf("first Add = %d, want 1", count1)
	}

	count2, err := ac.Add("hello")
	if err != nil {
		t.Fatalf("Add(hello) error: %v", err)
	}
	if count2 != 1 {
		t.Errorf("second Add = %d, want 1", count2)
	}

	// Adding "Hello" again should be idempotent
	count3, err := ac.Add("Hello")
	if err != nil {
		t.Fatalf("duplicate Add error: %v", err)
	}
	if count3 != 0 {
		t.Errorf("duplicate Add = %d, want 0", count3)
	}

	// Verify both exist
	results, err := ac.Find("Hello hello")
	if err != nil {
		t.Fatalf("Find error: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("Find = %v, want 2 results", results)
	}
}

// --- Case-sensitive Suggest tests ---

func TestSuggestCaseSensitive(t *testing.T) {
	ac, mr := createAhoCorasickCaseSensitive(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	if _, err := ac.Add("Hello"); err != nil {
		t.Fatalf("Add(Hello) error: %v", err)
	}
	if _, err := ac.Add("hello"); err != nil {
		t.Fatalf("Add(hello) error: %v", err)
	}

	// Suggest with uppercase prefix should only match "Hello"
	results, err := ac.Suggest("H")
	if err != nil {
		t.Fatalf("Suggest(H) error: %v", err)
	}
	if len(results) != 1 || results[0] != testKeywordHelloUpper {
		t.Errorf("Suggest(H) = %v, want [testKeywordHelloUpper]", results)
	}

	// Suggest with lowercase prefix should only match "hello"
	results, err = ac.Suggest("h")
	if err != nil {
		t.Fatalf("Suggest(h) error: %v", err)
	}
	if len(results) != 1 || results[0] != testKeywordHello {
		t.Errorf("Suggest(h) = %v, want [testKeywordHello]", results)
	}

	// Suggest with mixed case prefix
	results, err = ac.Suggest("he")
	if err != nil {
		t.Fatalf("Suggest(he) error: %v", err)
	}
	if len(results) != 1 || results[0] != testKeywordHello {
		t.Errorf("Suggest(he) = %v, want [testKeywordHello]", results)
	}
}

// --- Schema-parameterized tests ---

func TestCaseSensitiveAcrossSchemas(t *testing.T) { //nolint:funlen
	tests := []struct {
		name          string
		schema        int
		caseSensitive bool
		keywords      []string
		text          string
		wantMatch     []string
	}{
		{
			name:          "v1/case_insensitive/uppercase keyword",
			schema:        SchemaV1,
			caseSensitive: false,
			keywords:      []string{"Hello"},
			text:          "hello world",
			wantMatch:     []string{"hello"},
		},
		{
			name:          "v1/case_insensitive/mixed text",
			schema:        SchemaV1,
			caseSensitive: false,
			keywords:      []string{"hello"},
			text:          "Hello World",
			wantMatch:     []string{"hello"},
		},
		{
			name:          "v1/case_sensitive/exact match",
			schema:        SchemaV1,
			caseSensitive: true,
			keywords:      []string{"Hello"},
			text:          "Hello world",
			wantMatch:     []string{"Hello"},
		},
		{
			name:          "v1/case_sensitive/no match",
			schema:        SchemaV1,
			caseSensitive: true,
			keywords:      []string{"Hello"},
			text:          "hello world",
			wantMatch:     nil,
		},
		{
			name:          "v2/case_insensitive/uppercase keyword",
			schema:        SchemaV2,
			caseSensitive: false,
			keywords:      []string{"Hello"},
			text:          "hello world",
			wantMatch:     []string{"hello"},
		},
		{
			name:          "v2/case_insensitive/mixed text",
			schema:        SchemaV2,
			caseSensitive: false,
			keywords:      []string{"hello"},
			text:          "Hello World",
			wantMatch:     []string{"hello"},
		},
		{
			name:          "v2/case_sensitive/exact match",
			schema:        SchemaV2,
			caseSensitive: true,
			keywords:      []string{"Hello"},
			text:          "Hello world",
			wantMatch:     []string{"Hello"},
		},
		{
			name:          "v2/case_sensitive/no match",
			schema:        SchemaV2,
			caseSensitive: true,
			keywords:      []string{"Hello"},
			text:          "hello world",
			wantMatch:     nil,
		},
		{
			name:          "v2/case_sensitive/distinct keywords",
			schema:        SchemaV2,
			caseSensitive: true,
			keywords:      []string{"Hello", "hello"},
			text:          "Hello hello",
			wantMatch:     []string{"Hello", "hello"},
		},
		{
			name:          "v2/case_insensitive/dedup keywords",
			schema:        SchemaV2,
			caseSensitive: false,
			keywords:      []string{"Hello", "HELLO", "hello"},
			text:          "hello",
			wantMatch:     []string{"hello"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ac, mr := createAhoCorasickWithOpts(t, tt.schema, tt.caseSensitive)
			defer mr.Close()
			defer func() { _ = ac.Close() }()
			defer func() { _ = ac.Flush() }()

			for _, kw := range tt.keywords {
				if _, err := ac.Add(kw); err != nil {
					t.Fatalf("Add(%q) error: %v", kw, err)
				}
			}

			results, err := ac.Find(tt.text)
			if err != nil {
				t.Fatalf("Find(%q) error: %v", tt.text, err)
			}

			if len(results) != len(tt.wantMatch) {
				t.Errorf("Find(%q) = %d results, want %d", tt.text, len(results), len(tt.wantMatch))
			}

			for _, want := range tt.wantMatch {
				found := false
				for _, got := range results {
					if got == want {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Find(%q) missing expected match %q", tt.text, want)
				}
			}
		})
	}
}

// --- Case-sensitive Remove tests ---

func TestRemoveCaseSensitive(t *testing.T) {
	ac, mr := createAhoCorasickCaseSensitive(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	if _, err := ac.Add("Hello"); err != nil {
		t.Fatalf("Add(Hello) error: %v", err)
	}
	if _, err := ac.Add("hello"); err != nil {
		t.Fatalf("Add(hello) error: %v", err)
	}

	// Remove only "Hello" — "hello" should remain
	count, err := ac.Remove("Hello")
	if err != nil {
		t.Fatalf("Remove(Hello) error: %v", err)
	}
	if count != 1 {
		t.Errorf("Remove(Hello) = %d remaining, want 1", count)
	}

	// "Hello" should not match
	results, err := ac.Find("Hello hello")
	if err != nil {
		t.Fatalf("Find error: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("Find = %v, want [testKeywordHello]", results)
	}

	// Case-insensitive remove should NOT remove "hello"
	_, err = ac.Remove("HELLO")
	if err != nil {
		t.Fatalf("Remove(HELLO) error: %v", err)
	}

	results, err = ac.Find("hello")
	if err != nil {
		t.Fatalf("Find error: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("Find after case-mismatch remove = %v, want [testKeywordHello]", results)
	}
}

// --- V1 case-sensitive tests ---

func TestCaseSensitiveV1(t *testing.T) {
	ac, mr := createAhoCorasickCaseSensitiveV1(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	if _, err := ac.Add("Hello"); err != nil {
		t.Fatalf("Add(Hello) error: %v", err)
	}
	if _, err := ac.Add("hello"); err != nil {
		t.Fatalf("Add(hello) error: %v", err)
	}

	// Exact match
	results, err := ac.Find("Hello")
	if err != nil {
		t.Fatalf("Find(Hello) error: %v", err)
	}
	if len(results) != 1 || results[0] != testKeywordHelloUpper {
		t.Errorf("Find(Hello) = %v, want [testKeywordHelloUpper]", results)
	}

	// Case mismatch
	results, err = ac.Find("hello")
	if err != nil {
		t.Fatalf("Find(hello) error: %v", err)
	}
	if len(results) != 1 || results[0] != testKeywordHello {
		t.Errorf("Find(hello) = %v, want [testKeywordHello]", results)
	}

	// Both present
	results, err = ac.Find("Hello hello")
	if err != nil {
		t.Fatalf("Find(Hello hello) error: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("Find(Hello hello) = %v, want 2 results", results)
	}
}

func TestRemoveCaseSensitiveV1(t *testing.T) {
	ac, mr := createAhoCorasickCaseSensitiveV1(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	if _, err := ac.Add("Hello"); err != nil {
		t.Fatalf("Add(Hello) error: %v", err)
	}
	if _, err := ac.Add("hello"); err != nil {
		t.Fatalf("Add(hello) error: %v", err)
	}
	// Remove only "Hello" — "hello" should remain
	count, err := ac.Remove("Hello")
	if err != nil {
		t.Fatalf("Remove(Hello) error: %v", err)
	}
	if count != 1 {
		t.Errorf("Remove(Hello) = %d remaining, want 1", count)
	}

	// Verify "hello" still matches
	results, err := ac.Find("hello")
	if err != nil {
		t.Fatalf("Find(hello) error: %v", err)
	}
	if len(results) != 1 || results[0] != testKeywordHello {
		t.Errorf("Find(hello) = %v, want [testKeywordHello]", results)
	}

	// Verify "Hello" no longer matches
	results, err = ac.Find("Hello")
	if err != nil {
		t.Fatalf("Find(Hello) error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("Find(Hello) after remove = %v, want empty", results)
	}
}
