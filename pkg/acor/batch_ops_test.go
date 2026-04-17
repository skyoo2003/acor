// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestAddManyBestEffortWithEmpty(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	keywords := []string{"he", "", "him"}
	result, err := ac.AddMany(keywords, &BatchOptions{Mode: BatchModeBestEffort})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Added) != 2 {
		t.Errorf("expected 2 added, got %d", len(result.Added))
	}
	if len(result.Failed) != 1 {
		t.Errorf("expected 1 failed (empty), got %d", len(result.Failed))
	}
	if !errors.Is(result.Failed[0].Error, ErrEmptyKeyword) {
		t.Errorf("expected ErrEmptyKeyword, got %v", result.Failed[0].Error)
	}
}

func TestAddManyBestEffortSkipped(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	if _, err := ac.Add("he"); err != nil {
		t.Fatal(err)
	}

	keywords := []string{testKeywordHE, testKeywordHim}
	result, err := ac.AddMany(keywords, &BatchOptions{Mode: BatchModeBestEffort})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Skipped) != 1 || result.Skipped[0] != testKeywordHE {
		t.Errorf("expected he to be skipped, got %v", result.Skipped)
	}
	if len(result.Added) != 1 || result.Added[0] != testKeywordHim {
		t.Errorf("expected him to be added, got %v", result.Added)
	}
}

func TestAddManyNilOpts(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	result, err := ac.AddMany([]string{"he", "her"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Added) != 2 {
		t.Errorf("expected 2 added, got %d", len(result.Added))
	}
}

func TestRemoveManyBestEffortWithError(t *testing.T) {
	ac1, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac1.Close() }()

	ac, mr2 := createAhoCorasick(t)
	defer mr2.Close()
	defer func() { _ = ac.Close() }()

	if _, err := ac.Add("he"); err != nil {
		t.Fatal(err)
	}

	mr2.Close()

	result, err := ac.RemoveManyWithOptions([]string{"he"}, &BatchOptions{Mode: BatchModeBestEffort})
	if err != nil {
		t.Fatalf("best-effort should not return error: %v", err)
	}
	if len(result.Failed) != 1 {
		t.Errorf("expected 1 failed, got %d", len(result.Failed))
	}
}

func TestRemoveManyTransactionalWithDuplicates(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	if _, err := ac.AddMany([]string{"he", "her"}, nil); err != nil {
		t.Fatal(err)
	}

	result, err := ac.RemoveManyWithOptions([]string{"he", "he", "her"}, &BatchOptions{Mode: BatchModeTransactional})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Removed) != 2 {
		t.Errorf("expected 2 removed, got %d", len(result.Removed))
	}
	if len(result.Skipped) != 1 {
		t.Errorf("expected 1 skipped duplicate, got %d", len(result.Skipped))
	}
}

func TestAddManyTransactionalSkipped(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	if _, err := ac.Add("he"); err != nil {
		t.Fatal(err)
	}

	result, err := ac.AddMany([]string{"he", "him"}, &BatchOptions{Mode: BatchModeTransactional})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Skipped) != 1 || result.Skipped[0] != "he" {
		t.Errorf("expected he to be skipped, got %v", result.Skipped)
	}
	if len(result.Added) != 1 || result.Added[0] != "him" {
		t.Errorf("expected him to be added, got %v", result.Added)
	}
}

func TestFindManyContext(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	if _, err := ac.AddMany([]string{"he", "her"}, nil); err != nil {
		t.Fatal(err)
	}

	results, err := ac.FindManyContext(context.Background(), []string{"he is here", "nothing"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if len(results["he is here"]) == 0 {
		t.Error("expected matches in 'he is here'")
	}
}

func TestBatchResultFields(t *testing.T) {
	result := &BatchResult{
		Added:   []string{"a"},
		Removed: []string{"b"},
		Failed:  []KeywordError{{Keyword: "c", Error: ErrEmptyKeyword}},
		Skipped: []string{"d"},
	}
	if len(result.Added) != 1 {
		t.Errorf("Added = %d, want 1", len(result.Added))
	}
	if len(result.Removed) != 1 {
		t.Errorf("Removed = %d, want 1", len(result.Removed))
	}
	if len(result.Failed) != 1 {
		t.Errorf("Failed = %d, want 1", len(result.Failed))
	}
	if len(result.Skipped) != 1 {
		t.Errorf("Skipped = %d, want 1", len(result.Skipped))
	}
}

func TestKeywordErrorStruct(t *testing.T) {
	ke := KeywordError{Keyword: testKeywordTest, Error: ErrEmptyKeyword}
	if ke.Keyword != testKeywordTest {
		t.Errorf("Keyword = %q, want %q", ke.Keyword, testKeywordTest)
	}
	if !errors.Is(ke.Error, ErrEmptyKeyword) {
		t.Errorf("Error = %v, want ErrEmptyKeyword", ke.Error)
	}
}

func TestSplitChunksSmallText(t *testing.T) {
	chunks := splitChunks(testKeywordHello, &ParallelOptions{ChunkSize: 100})
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk for small text, got %d", len(chunks))
	}
	if chunks[0].text != testKeywordHello {
		t.Errorf("chunk text = %q, want %q", chunks[0].text, testKeywordHello)
	}
}

func TestSplitChunksWithOverlap(t *testing.T) {
	text := strings.Repeat("hello world ", 100)
	opts := &ParallelOptions{
		ChunkSize: 50,
		Boundary:  ChunkBoundaryWord,
		Overlap:   10,
	}
	chunks := splitChunks(text, opts)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
}

func TestSplitChunksNilOpts(t *testing.T) {
	text := "short"
	chunks := splitChunks(text, nil)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk with nil opts, got %d", len(chunks))
	}
}

func TestNormalizeParallelOptionsDefault(t *testing.T) {
	opts := normalizeParallelOptions(nil)
	if opts.Workers <= 0 {
		t.Error("Workers should be > 0")
	}
	if opts.ChunkSize != DefaultChunkSize {
		t.Errorf("ChunkSize = %d, want %d", opts.ChunkSize, DefaultChunkSize)
	}
}

func TestNormalizeParallelOptionsZeroWorkers(t *testing.T) {
	opts := normalizeParallelOptions(&ParallelOptions{Workers: 0})
	if opts.Workers <= 0 {
		t.Error("Workers should be defaulted to NumCPU")
	}
}

func TestFindBoundaryLine(t *testing.T) {
	runes := []rune("hello\nworld")
	idx := findBoundary(runes, 6, ChunkBoundaryLine, 5)
	if idx != 6 {
		t.Errorf("findBoundary(line) = %d, want 6", idx)
	}
}

func TestFindBoundarySentence(t *testing.T) {
	runes := []rune("hello. world")
	idx := findBoundary(runes, 7, ChunkBoundarySentence, 5)
	if idx != 6 {
		t.Errorf("findBoundary(sentence) = %d, want 6", idx)
	}
}

func TestFindBoundaryWord(t *testing.T) {
	runes := []rune("hello world")
	idx := findBoundary(runes, 6, ChunkBoundaryWord, 5)
	if idx != 5 {
		t.Errorf("findBoundary(word) = %d, want 5", idx)
	}
}

func TestFindBoundaryEdgeCases(t *testing.T) {
	runes := []rune("abcdef")
	idx := findBoundary(runes, 3, ChunkBoundaryWord, 5)
	if idx != 3 {
		t.Errorf("findBoundary(no match) = %d, want 3 (fallback to target)", idx)
	}
}

func TestIsBoundaryOutOfBounds(t *testing.T) {
	if isBoundary([]rune("a"), 0, ChunkBoundaryWord) {
		t.Error("isBoundary at 0 should return false")
	}
	if isBoundary([]rune("a"), 1, ChunkBoundaryWord) {
		t.Error("isBoundary at len should return false")
	}
}

func TestChunkStruct(t *testing.T) {
	c := chunk{text: testKeywordHello, textOffset: 5}
	if c.text != testKeywordHello {
		t.Errorf("chunk.text = %q, want %q", c.text, testKeywordHello)
	}
	if c.textOffset != 5 {
		t.Errorf("chunk.textOffset = %d, want 5", c.textOffset)
	}
}

func TestBatchModeConstants(t *testing.T) {
	if BatchModeBestEffort != 0 {
		t.Errorf("BatchModeBestEffort = %d, want 0", BatchModeBestEffort)
	}
	if BatchModeTransactional != 1 {
		t.Errorf("BatchModeTransactional = %d, want 1", BatchModeTransactional)
	}
}

func TestChunkBoundaryConstants(t *testing.T) {
	if ChunkBoundaryWord != 0 {
		t.Errorf("ChunkBoundaryWord = %d, want 0", ChunkBoundaryWord)
	}
	if ChunkBoundarySentence != 1 {
		t.Errorf("ChunkBoundarySentence = %d, want 1", ChunkBoundarySentence)
	}
	if ChunkBoundaryLine != 2 {
		t.Errorf("ChunkBoundaryLine = %d, want 2", ChunkBoundaryLine)
	}
}

func TestDefaultParallelOptionsValues(t *testing.T) {
	opts := DefaultParallelOptions()
	if opts.ChunkSize != DefaultChunkSize {
		t.Errorf("ChunkSize = %d, want %d", opts.ChunkSize, DefaultChunkSize)
	}
	if opts.Overlap != DefaultOverlap {
		t.Errorf("Overlap = %d, want %d", opts.Overlap, DefaultOverlap)
	}
	if opts.Boundary != ChunkBoundaryWord {
		t.Errorf("Boundary = %d, want %d", opts.Boundary, ChunkBoundaryWord)
	}
}

func TestAddManyBestEffortDuplicateInput(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	if _, err := ac.AddMany([]string{"he", "her", "him"}, nil); err != nil {
		t.Fatal(err)
	}

	result, err := ac.RemoveManyWithOptions([]string{"he", "", "him"}, &BatchOptions{Mode: BatchModeBestEffort})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Removed) != 2 {
		t.Errorf("expected 2 removed, got %d", len(result.Removed))
	}
	if len(result.Failed) != 1 {
		t.Errorf("expected 1 failed, got %d", len(result.Failed))
	}
}

func TestRemoveManyTransactionalWithEmpty(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	if _, err := ac.AddMany([]string{"he", "her", "him"}, nil); err != nil {
		t.Fatal(err)
	}

	_, err := ac.RemoveManyWithOptions([]string{"he", "", "him"}, &BatchOptions{Mode: BatchModeTransactional})
	if !errors.Is(err, ErrEmptyKeyword) {
		t.Fatalf("expected ErrEmptyKeyword, got %v", err)
	}

	for _, kw := range []string{"he", "him"} {
		results, findErr := ac.Find(kw)
		if findErr != nil {
			t.Fatal(findErr)
		}
		if !containsAll(results, kw) {
			t.Errorf("rollback should restore %q", kw)
		}
	}
}

func TestFindManyEmpty(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	if _, err := ac.Add("he"); err != nil {
		t.Fatal(err)
	}

	results, err := ac.FindMany([]string{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for empty input, got %d", len(results))
	}
}

func TestFindManyWithMatch(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	if _, err := ac.AddMany([]string{"he", "her"}, nil); err != nil {
		t.Fatal(err)
	}

	results, err := ac.FindMany([]string{"he is here", "nothing"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if len(results["he is here"]) < 2 {
		t.Errorf("expected at least 2 matches in 'he is here', got %d", len(results["he is here"]))
	}
	if len(results["nothing"]) != 0 {
		t.Errorf("expected 0 matches in 'nothing', got %d", len(results["nothing"]))
	}
}

func TestRemoveManyBestEffortError(t *testing.T) {
	ac, _ := createAhoCorasick(t)
	defer func() { _ = ac.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := ac.RemoveManyContext(ctx, []string{"keyword1", "keyword2"}, &BatchOptions{Mode: BatchModeBestEffort})
	if err != nil {
		t.Fatalf("best-effort should not return error, got: %v", err)
	}
	if len(result.Failed) == 0 {
		t.Error("expected some failed removals in best-effort mode with canceled context")
	}
}

func TestRemoveManyTransactionalDuplicate(t *testing.T) {
	ac, _ := createAhoCorasick(t)
	defer func() { _ = ac.Close() }()

	_, _ = ac.Add("he")

	result, err := ac.RemoveManyWithOptions([]string{"he", "he"}, &BatchOptions{Mode: BatchModeTransactional})
	if err != nil {
		t.Fatalf("RemoveMany transactional error: %v", err)
	}
	if len(result.Skipped) < 1 {
		t.Errorf("expected duplicate to be skipped, got removed=%v skipped=%v", result.Removed, result.Skipped)
	}
}

func TestRemoveManyTransactionalError(t *testing.T) {
	ac, _ := createAhoCorasick(t)
	defer func() { _ = ac.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := ac.RemoveManyContext(ctx, []string{"keyword1"}, &BatchOptions{Mode: BatchModeTransactional})
	if err == nil {
		t.Fatal("expected error from canceled context in RemoveMany transactional")
	}
}

func TestAddManyBestEffortError(t *testing.T) {
	ac, _ := createAhoCorasick(t)
	defer func() { _ = ac.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := ac.AddManyContext(ctx, []string{"keyword1"}, &BatchOptions{Mode: BatchModeBestEffort})
	if err != nil {
		t.Fatalf("best-effort should not return error, got: %v", err)
	}
	if len(result.Failed) == 0 {
		t.Error("expected some failed adds in best-effort mode with canceled context")
	}
}

func TestFindManyContextError(t *testing.T) {
	ac, _ := createAhoCorasick(t)
	defer func() { _ = ac.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := ac.FindManyContext(ctx, []string{"hello world"})
	if err == nil {
		t.Fatal("expected error from canceled context in FindManyContext")
	}
}
