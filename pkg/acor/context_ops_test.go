// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"strings"
	"testing"
)

func TestAddContext(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	ctx := context.Background()

	count, err := ac.AddContext(ctx, "hello")
	if err != nil {
		t.Fatalf("AddContext() error: %v", err)
	}
	if count != 1 {
		t.Errorf("AddContext() = %d, want 1", count)
	}

	// Adding duplicate should return 0
	count, err = ac.AddContext(ctx, "hello")
	if err != nil {
		t.Fatalf("AddContext() duplicate error: %v", err)
	}
	if count != 0 {
		t.Errorf("AddContext() duplicate = %d, want 0", count)
	}
}

func TestRemoveContext(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	ctx := context.Background()

	if _, err := ac.AddContext(ctx, "test"); err != nil {
		t.Fatal(err)
	}

	count, err := ac.RemoveContext(ctx, "test")
	if err != nil {
		t.Fatalf("RemoveContext() error: %v", err)
	}
	if count < 0 {
		t.Errorf("RemoveContext() = %d, want >= 0", count)
	}

	// Verify keyword is gone
	matches, err := ac.FindContext(ctx, "test")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range matches {
		if m == "test" {
			t.Error("keyword 'test' should have been removed")
		}
	}
}

func TestFindContext(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	ctx := context.Background()

	keywords := []string{"he", "her", "him"}
	for _, k := range keywords {
		if _, err := ac.AddContext(ctx, k); err != nil {
			t.Fatal(err)
		}
	}

	matches, err := ac.FindContext(ctx, "he is her friend")
	if err != nil {
		t.Fatalf("FindContext() error: %v", err)
	}

	if !containsAll(matches, "he", "her") {
		t.Errorf("FindContext() = %v, want to contain he, her", matches)
	}

	// No matches
	matches, err = ac.FindContext(ctx, "xyz")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Errorf("FindContext() no match = %v, want empty", matches)
	}
}

func TestFindIndexContext(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	ctx := context.Background()

	if _, err := ac.AddContext(ctx, "he"); err != nil {
		t.Fatal(err)
	}
	if _, err := ac.AddContext(ctx, "she"); err != nil {
		t.Fatal(err)
	}

	results, err := ac.FindIndexContext(ctx, "she is here")
	if err != nil {
		t.Fatalf("FindIndexContext() error: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("expected some results")
	}

	heIndexes, ok := results["he"]
	if !ok {
		t.Error("expected 'he' in results")
	} else if len(heIndexes) == 0 {
		t.Error("expected at least one index for 'he'")
	}

	sheIndexes, ok := results["she"]
	if !ok {
		t.Error("expected 'she' in results")
	} else if len(sheIndexes) == 0 {
		t.Error("expected at least one index for 'she'")
	}
}

func TestFlushContext(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	ctx := context.Background()

	if _, err := ac.AddContext(ctx, "test"); err != nil {
		t.Fatal(err)
	}

	if err := ac.FlushContext(ctx); err != nil {
		t.Fatalf("FlushContext() error: %v", err)
	}

	info, err := ac.InfoContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if info.Keywords != 0 {
		t.Errorf("after FlushContext, Keywords = %d, want 0", info.Keywords)
	}
}

func TestInfoContext(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	ctx := context.Background()

	info, err := ac.InfoContext(ctx)
	if err != nil {
		t.Fatalf("InfoContext() error: %v", err)
	}
	if info.Keywords != 0 {
		t.Errorf("InfoContext().Keywords = %d, want 0", info.Keywords)
	}

	if _, addErr := ac.AddContext(ctx, "hello"); addErr != nil {
		t.Fatal(addErr)
	}

	info, err = ac.InfoContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if info.Keywords != 1 {
		t.Errorf("InfoContext().Keywords = %d, want 1", info.Keywords)
	}
}

func TestSuggestContext(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	ctx := context.Background()

	keywords := []string{"hello", "help", "helicopter"}
	for _, k := range keywords {
		if _, err := ac.AddContext(ctx, k); err != nil {
			t.Fatal(err)
		}
	}

	suggestions, err := ac.SuggestContext(ctx, "hel")
	if err != nil {
		t.Fatalf("SuggestContext() error: %v", err)
	}

	if len(suggestions) != 3 {
		t.Errorf("SuggestContext() = %v, want 3 suggestions", suggestions)
	}

	// No suggestions
	suggestions, err = ac.SuggestContext(ctx, "xyz")
	if err != nil {
		t.Fatal(err)
	}
	if len(suggestions) != 0 {
		t.Errorf("SuggestContext() no match = %v, want empty", suggestions)
	}
}

func TestSuggestIndexContext(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	ctx := context.Background()

	if _, err := ac.AddContext(ctx, "hello"); err != nil {
		t.Fatal(err)
	}
	if _, err := ac.AddContext(ctx, "help"); err != nil {
		t.Fatal(err)
	}

	results, err := ac.SuggestIndexContext(ctx, "hel")
	if err != nil {
		t.Fatalf("SuggestIndexContext() error: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("SuggestIndexContext() = %d results, want 2", len(results))
	}
}

func TestFindParallelContext(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	ctx := context.Background()

	keywords := []string{"he", "her", "him", "test"}
	for _, k := range keywords {
		if _, err := ac.AddContext(ctx, k); err != nil {
			t.Fatal(err)
		}
	}

	// Single chunk (small text, large chunk size) — delegates to find
	results, err := ac.FindParallelContext(ctx, "he is here", &ParallelOptions{
		Workers:   2,
		ChunkSize: 1000,
	})
	if err != nil {
		t.Fatalf("FindParallelContext() single chunk error: %v", err)
	}
	if !containsAll(results, "he") {
		t.Errorf("FindParallelContext() single chunk = %v, want to contain 'he'", results)
	}

	// Multi chunk — triggers parallel path
	text := "he is here with him. this is a test of the system. " +
		strings.Repeat("padding ", 20)
	results, err = ac.FindParallelContext(ctx, text, &ParallelOptions{
		Workers:   2,
		ChunkSize: 20,
		Boundary:  ChunkBoundaryWord,
		Overlap:   3,
	})
	if err != nil {
		t.Fatalf("FindParallelContext() multi chunk error: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected some matches in multi-chunk mode")
	}
}

func TestFindIndexParallelContext(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	ctx := context.Background()

	keywords := []string{"he", "her"}
	for _, k := range keywords {
		if _, err := ac.AddContext(ctx, k); err != nil {
			t.Fatal(err)
		}
	}

	// Single chunk — delegates to findIndex
	results, err := ac.FindIndexParallelContext(ctx, "he is her friend", &ParallelOptions{
		Workers:   2,
		ChunkSize: 1000,
	})
	if err != nil {
		t.Fatalf("FindIndexParallelContext() single chunk error: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected some results in single-chunk mode")
	}

	// Multi chunk — triggers parallel path
	text := "he is her friend. she is here. " +
		strings.Repeat("padding ", 20)
	results, err = ac.FindIndexParallelContext(ctx, text, &ParallelOptions{
		Workers:   2,
		ChunkSize: 20,
		Boundary:  ChunkBoundaryWord,
		Overlap:   3,
	})
	if err != nil {
		t.Fatalf("FindIndexParallelContext() multi chunk error: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected some results in multi-chunk mode")
	}

	// Verify indices are non-negative
	for keyword, indices := range results {
		for _, idx := range indices {
			if idx < 0 {
				t.Errorf("negative index for %s: %d", keyword, idx)
			}
		}
	}
}

func TestFindParallelContextEmptyText(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	ctx := context.Background()

	results, err := ac.FindParallelContext(ctx, "", &ParallelOptions{
		ChunkSize: 100,
	})
	if err != nil {
		t.Fatalf("FindParallelContext() empty text error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("FindParallelContext() empty text = %v, want empty", results)
	}
}

func TestFindIndexParallelContextEmptyText(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	ctx := context.Background()

	results, err := ac.FindIndexParallelContext(ctx, "", &ParallelOptions{
		ChunkSize: 100,
	})
	if err != nil {
		t.Fatalf("FindIndexParallelContext() empty text error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("FindIndexParallelContext() empty text = %v, want empty", results)
	}
}

func TestFindParallelContextInvalidChunkSize(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	ctx := context.Background()

	_, err := ac.FindParallelContext(ctx, "test", &ParallelOptions{ChunkSize: 0})
	if err == nil {
		t.Error("expected error for ChunkSize=0")
	}

	_, err = ac.FindParallelContext(ctx, "test", &ParallelOptions{ChunkSize: -1})
	if err == nil {
		t.Error("expected error for negative ChunkSize")
	}
}

func TestFindIndexParallelContextInvalidChunkSize(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	ctx := context.Background()

	_, err := ac.FindIndexParallelContext(ctx, "test", &ParallelOptions{ChunkSize: 0})
	if err == nil {
		t.Error("expected error for ChunkSize=0")
	}
}

func TestDebugV1(t *testing.T) {
	ac, mr := createAhoCorasickV1(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	if _, err := ac.Add("test"); err != nil {
		t.Fatal(err)
	}
	if _, err := ac.Add("hello"); err != nil {
		t.Fatal(err)
	}

	// Debug should not panic
	ac.Debug()
}
