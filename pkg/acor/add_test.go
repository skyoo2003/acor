// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	miniredis "github.com/alicebob/miniredis/v2"
)

//nolint:funlen
func TestAdd(t *testing.T) {
	tests := []struct {
		name      string
		keywords  []string
		wantCount int
		wantErr   bool
		setupHook func(ac *AhoCorasick)
		cleanup   func(ac *AhoCorasick)
	}{
		{
			name:      "single keyword",
			keywords:  []string{"hello"},
			wantCount: 1,
			wantErr:   false,
		},
		{
			name:      "multiple keywords",
			keywords:  []string{"her", "he", "his"},
			wantCount: 3,
			wantErr:   false,
		},
		{
			name:      "unicode keywords",
			keywords:  []string{"한글", "日本語", "中文"},
			wantCount: 3,
			wantErr:   false,
		},
		{
			name:      "emoji keywords",
			keywords:  []string{"😀", "🎉", "🚀"},
			wantCount: 3,
			wantErr:   false,
		},
		{
			name:      "special characters",
			keywords:  []string{"@user", "#tag", "$var", "a*b+c"},
			wantCount: 4,
			wantErr:   false,
		},
		{
			name:      "duplicate keywords returns idempotent count",
			keywords:  []string{"test", "test", "test"},
			wantCount: 1,
			wantErr:   false,
		},
		{
			name:      "very long keyword",
			keywords:  []string{strings.Repeat("a", 1000)},
			wantCount: 1,
			wantErr:   false,
		},
		{
			name:      "whitespace keyword is trimmed",
			keywords:  []string{"   ", "\t", "\n"},
			wantCount: 0,
			wantErr:   false,
		},
		{
			name:      "mixed case keywords treated case-insensitively",
			keywords:  []string{"Hello", "HELLO", "hello"},
			wantCount: 1,
			wantErr:   false,
		},
		{
			name:      "keywords with numbers",
			keywords:  []string{"test1", "test2", "123"},
			wantCount: 3,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ac, mr := createAhoCorasick(t)
			defer mr.Close()
			defer func() { _ = ac.Close() }()
			defer func() { _ = ac.Flush() }()

			if tt.setupHook != nil {
				tt.setupHook(ac)
			}
			if tt.cleanup != nil {
				defer tt.cleanup(ac)
			}

			addedCount := 0
			for _, keyword := range tt.keywords {
				count, err := ac.Add(keyword)
				if tt.wantErr {
					if err == nil {
						t.Errorf("Add(%q) expected error, got nil", keyword)
					}
					return
				}
				if err != nil {
					t.Errorf("Add(%q) unexpected error: %v", keyword, err)
					return
				}
				addedCount += count
			}

			if addedCount != tt.wantCount {
				t.Errorf("Add() total count = %d, want %d", addedCount, tt.wantCount)
			}
		})
	}
}

func TestRemove(t *testing.T) {
	tests := []struct {
		name        string
		addFirst    []string
		remove      []string
		wantCount   int
		wantErr     bool
		findAfter   string
		wantFindLen int
	}{
		{
			name:        "remove single keyword",
			addFirst:    []string{"hello", "world"},
			remove:      []string{"hello"},
			wantCount:   1,
			wantErr:     false,
			findAfter:   "hello",
			wantFindLen: 0,
		},
		{
			name:      "remove multiple keywords",
			addFirst:  []string{"her", "he", "his"},
			remove:    []string{"her", "he", "his"},
			wantCount: 3,
			wantErr:   false,
		},
		{
			name:      "remove non-existent keyword returns 0",
			addFirst:  []string{"hello"},
			remove:    []string{"world"},
			wantCount: 0,
			wantErr:   false,
		},
		{
			name:      "remove unicode keywords",
			addFirst:  []string{"한글", "日本語", "中文"},
			remove:    []string{"한글"},
			wantCount: 1,
			wantErr:   false,
		},
		{
			name:      "remove duplicate keywords returns 1 on first remove",
			addFirst:  []string{"test", "test"},
			remove:    []string{"test"},
			wantCount: 1,
			wantErr:   false,
		},
		{
			name:      "remove partial match",
			addFirst:  []string{"he", "her", "here"},
			remove:    []string{"he"},
			wantCount: 1,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ac, mr := createAhoCorasick(t)
			defer mr.Close()
			defer func() { _ = ac.Close() }()
			defer func() { _ = ac.Flush() }()

			for _, kw := range tt.addFirst {
				if _, err := ac.Add(kw); err != nil {
					t.Fatalf("Add(%q) error: %v", kw, err)
				}
			}

			removedCount := 0
			for _, kw := range tt.remove {
				count, err := ac.Remove(kw)
				if tt.wantErr {
					if err == nil {
						t.Errorf("Remove(%q) expected error, got nil", kw)
					}
					return
				}
				if err != nil {
					t.Errorf("Remove(%q) unexpected error: %v", kw, err)
					return
				}
				removedCount += count
			}

			if removedCount != tt.wantCount {
				t.Errorf("Remove() total count = %d, want %d", removedCount, tt.wantCount)
			}

			if tt.findAfter != "" {
				results, err := ac.Find(tt.findAfter)
				if err != nil {
					t.Fatalf("Find(%q) error: %v", tt.findAfter, err)
				}
				if len(results) != tt.wantFindLen {
					t.Errorf("Find(%q) after remove = %d results, want %d", tt.findAfter, len(results), tt.wantFindLen)
				}
			}
		})
	}
}

// TestRemoveWithSchemaVersion merges TestV1Remove and TestV2Remove into
// a single table-driven test parameterized by schema version.
func TestRemoveWithSchemaVersion(t *testing.T) {
	tests := []struct {
		name       string
		schema     int
		keywords   []string
		remove     string
		wantCount  int
		notFound   string
		stillFound string
	}{
		{
			name:       "v1",
			schema:     SchemaV1,
			keywords:   []string{"he", "she", "hello"},
			remove:     "he",
			wantCount:  1,
			notFound:   "he",
			stillFound: "she",
		},
		{
			name:       "v2",
			schema:     SchemaV2,
			keywords:   []string{"foo", "bar", "baz"},
			remove:     "foo",
			wantCount:  1,
			notFound:   "foo",
			stillFound: "bar",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ac, mr := createAhoCorasickWithSchema(t, tt.schema)
			defer mr.Close()
			defer func() { _ = ac.Close() }()
			defer func() { _ = ac.Flush() }()

			for _, kw := range tt.keywords {
				if _, err := ac.Add(kw); err != nil {
					t.Fatal(err)
				}
			}

			count, err := ac.Remove(tt.remove)
			if err != nil {
				t.Fatalf("Remove(%s) error: %v", tt.remove, err)
			}
			if count != tt.wantCount {
				t.Errorf("Remove(%s) = %d, want %d (removed)", tt.remove, count, tt.wantCount)
			}

			matches, err := ac.Find(tt.notFound + " " + tt.stillFound)
			if err != nil {
				t.Fatal(err)
			}
			if containsAll(matches, tt.notFound) {
				t.Errorf("Find should not match %q after removal", tt.notFound)
			}
			if !containsAll(matches, tt.stillFound) {
				t.Errorf("Find should still match %q", tt.stillFound)
			}
		})
	}
}

func TestV2RemoveNonExistent(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	if _, err := ac.Add("keep"); err != nil {
		t.Fatal(err)
	}

	count, err := ac.Remove("nonexistent")
	if err != nil {
		t.Fatalf("Remove(nonexistent) error: %v", err)
	}
	if count != 0 {
		t.Errorf("Remove(nonexistent) = %d, want 0", count)
	}
}

// TestV2RemoveSimple verifies basic V2 keyword removal.
// Concurrency retry testing requires a custom mock that induces ErrConcurrencyConflict.
func TestV2RemoveSimple(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	for i := 0; i < 10; i++ {
		if _, err := ac.Add(fmt.Sprintf("keyword%d", i)); err != nil {
			t.Fatal(err)
		}
	}

	count, err := ac.Remove("keyword5")
	if err != nil {
		t.Fatalf("Remove() error: %v", err)
	}
	if count != 1 {
		t.Errorf("Remove() = %d, want 1", count)
	}
}

func TestAddUsesCollectionScopedKeys(t *testing.T) {
	ac, mr := createAhoCorasickV1(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	if _, err := ac.Add("he"); err != nil {
		t.Fatal(err)
	}

	keys := []string{
		keywordKey(ac.name),
		prefixKey(ac.name),
		suffixKey(ac.name),
		outputKey(ac.name, "he"),
		nodeKey(ac.name, "he"),
	}
	for _, key := range keys {
		if !mr.Exists(key) {
			t.Fatalf("expected redis key %q to exist", key)
		}
	}

	if mr.Exists("he:output") {
		t.Fatal("expected output key to be collection-scoped")
	}
	if mr.Exists("he:node") {
		t.Fatal("expected node key to be collection-scoped")
	}
}

func TestAddRollsBackPartialTrieWrites(t *testing.T) {
	ac, mr := createAhoCorasickV1(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	hookErr := errors.New("forced build trie failure")
	ac.buildTrieHook = func(prefix string) error {
		if prefix == testKeywordHE {
			return hookErr
		}
		return nil
	}

	addedCount, err := ac.Add(testKeywordHE)
	if !errors.Is(err, hookErr) {
		t.Fatalf("expected add to fail with hook error, got %v", err)
	}
	if addedCount != 0 {
		t.Fatalf("expected add count to be zero on rollback, got %d", addedCount)
	}

	ac.buildTrieHook = nil

	results, err := ac.Find(testKeywordHE)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("expected no matches after rollback, got %v", results)
	}

	indexResults, err := ac.FindIndex(testKeywordHE)
	if err != nil {
		t.Fatal(err)
	}
	if len(indexResults) != 0 {
		t.Fatalf("expected no indexed matches after rollback, got %v", indexResults)
	}
}

func TestAddFailedReAddKeepsExistingKeywordState(t *testing.T) {
	ac, mr := createAhoCorasickV1(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	if _, err := ac.Add(testKeywordHE); err != nil {
		t.Fatal(err)
	}

	hookErr := errors.New("forced buildTrie failure")
	ac.buildTrieHook = func(prefix string) error {
		if prefix == "hi" {
			return hookErr
		}
		return nil
	}

	addedCount, err := ac.Add("hi")
	if !errors.Is(err, hookErr) {
		t.Fatalf("expected add to fail with hook error, got %v", err)
	}
	if addedCount != 0 {
		t.Fatalf("expected add count to be zero, got %d", addedCount)
	}

	ac.buildTrieHook = nil

	results, err := ac.Find(testKeywordHE)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0] != testKeywordHE {
		t.Fatalf("expected existing keyword 'he' to remain after failed re-add, got %v", results)
	}

	indexResults, err := ac.FindIndex(testKeywordHE)
	if err != nil {
		t.Fatal(err)
	}
	assertIndexResults(t, indexResults, map[string][]int{testKeywordHE: {0}})
}

func TestCache_AddInvalidatesCache(t *testing.T) {
	mr := miniredis.RunT(t)

	ac, err := Create(&AhoCorasickArgs{
		Addr:        mr.Addr(),
		Name:        "test-cache-invalidate",
		EnableCache: true,
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	defer func() { _ = ac.Close() }()

	if _, err := ac.Add("first"); err != nil {
		t.Fatal(err)
	}

	_, _ = ac.Find("first test")
	_, valid := ac.cache.getEngine()
	if !valid {
		t.Fatal("expected cache to be valid after Find")
	}

	if _, err := ac.Add("second"); err != nil {
		t.Fatal(err)
	}

	_, valid = ac.cache.getEngine()
	if valid {
		t.Error("expected cache to be invalidated after Add")
	}
}

func TestCache_RemoveInvalidatesCache(t *testing.T) {
	mr := miniredis.RunT(t)

	ac, err := Create(&AhoCorasickArgs{
		Addr:        mr.Addr(),
		Name:        "test-cache-remove",
		EnableCache: true,
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	defer func() { _ = ac.Close() }()

	if _, err := ac.Add("hello"); err != nil {
		t.Fatal(err)
	}

	_, _ = ac.Find("hello world")
	_, valid := ac.cache.getEngine()
	if !valid {
		t.Fatal("expected cache to be valid after Find")
	}

	if _, err := ac.Remove("hello"); err != nil {
		t.Fatal(err)
	}

	_, valid = ac.cache.getEngine()
	if valid {
		t.Error("expected cache to be invalidated after Remove")
	}
}
