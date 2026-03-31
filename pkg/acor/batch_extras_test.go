package acor

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	redis "github.com/go-redis/redis/v8"
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

	keywords := []string{"he", "him"}
	result, err := ac.AddMany(keywords, &BatchOptions{Mode: BatchModeBestEffort})
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
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	ac, mr2 := createAhoCorasick(t)
	defer mr2.Close()
	defer func() { _ = ac.Close() }()

	if _, err := ac.Add("he"); err != nil {
		t.Fatal(err)
	}

	mr2.Close()

	result, err := ac.RemoveMany([]string{"he"}, &BatchOptions{Mode: BatchModeBestEffort})
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

	result, err := ac.RemoveMany([]string{"he", "he", "her"}, &BatchOptions{Mode: BatchModeTransactional})
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
	ke := KeywordError{Keyword: "test", Error: ErrEmptyKeyword}
	if ke.Keyword != "test" {
		t.Errorf("Keyword = %q, want %q", ke.Keyword, "test")
	}
	if !errors.Is(ke.Error, ErrEmptyKeyword) {
		t.Errorf("Error = %v, want ErrEmptyKeyword", ke.Error)
	}
}

func TestSplitChunksSmallText(t *testing.T) {
	chunks := splitChunks("hello", &ParallelOptions{ChunkSize: 100})
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk for small text, got %d", len(chunks))
	}
	if chunks[0].text != "hello" {
		t.Errorf("chunk text = %q, want %q", chunks[0].text, "hello")
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
	c := chunk{text: "hello", textOffset: 5}
	if c.text != "hello" {
		t.Errorf("chunk.text = %q, want %q", c.text, "hello")
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

	result, err := ac.RemoveMany([]string{"he", "", "him"}, &BatchOptions{Mode: BatchModeBestEffort})
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

	_, err := ac.RemoveMany([]string{"he", "", "him"}, &BatchOptions{Mode: BatchModeTransactional})
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

func TestV1Debug(t *testing.T) {
	ac, mr := createAhoCorasickV1(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	if _, err := ac.Add("test"); err != nil {
		t.Fatal(err)
	}

	ac.Debug()
}

func TestV2Debug(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	if _, err := ac.Add("test"); err != nil {
		t.Fatal(err)
	}

	ac.Debug()
}

func TestCreateWithDebug(t *testing.T) {
	mr := createTestRedisServer(t)
	defer mr.Close()

	ac, err := Create(&AhoCorasickArgs{
		Addr:  mr.Addr(),
		Name:  "test-debug",
		Debug: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ac.Close() }()

	if _, err := ac.Add("hello"); err != nil {
		t.Fatal(err)
	}

	matched, err := ac.Find("hello world")
	if err != nil {
		t.Fatal(err)
	}
	if !containsAll(matched, "hello") {
		t.Errorf("Find() = %v, want [hello]", matched)
	}
}

func TestCreateWithCustomLogger(t *testing.T) {
	mr := createTestRedisServer(t)
	defer mr.Close()

	logger := &testLogger{}
	ac, err := Create(&AhoCorasickArgs{
		Addr:   mr.Addr(),
		Name:   "test-logger",
		Logger: logger,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ac.Close() }()

	if _, err := ac.Add("test"); err != nil {
		t.Fatal(err)
	}
}

func TestCreateUnsupportedSchema(t *testing.T) {
	mr := createTestRedisServer(t)
	defer mr.Close()

	_, err := Create(&AhoCorasickArgs{
		Addr:          mr.Addr(),
		Name:          "test-bad-schema",
		SchemaVersion: 99,
	})
	if err == nil {
		t.Fatal("expected error for unsupported schema version")
	}
}

func TestCreateCacheRequiresV2(t *testing.T) {
	mr := createTestRedisServer(t)
	defer mr.Close()

	_, err := Create(&AhoCorasickArgs{
		Addr:          mr.Addr(),
		Name:          "test-cache-v1",
		SchemaVersion: SchemaV1,
		EnableCache:   true,
	})
	if err == nil {
		t.Fatal("expected error for cache with V1 schema")
	}
	if !errors.Is(err, ErrCacheRequiresV2) {
		t.Errorf("expected ErrCacheRequiresV2, got %v", err)
	}
}

func TestV1FlushWithKeywords(t *testing.T) {
	ac, mr := createAhoCorasickV1(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	if _, err := ac.Add("he"); err != nil {
		t.Fatal(err)
	}
	if _, err := ac.Add("she"); err != nil {
		t.Fatal(err)
	}

	if err := ac.Flush(); err != nil {
		t.Fatalf("Flush() error: %v", err)
	}

	info, err := ac.Info()
	if err != nil {
		t.Fatal(err)
	}
	if info.Keywords != 0 {
		t.Errorf("after flush, Keywords = %d, want 0", info.Keywords)
	}
}

func TestV1RemoveKeyword(t *testing.T) {
	ac, mr := createAhoCorasickV1(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	if _, err := ac.Add("he"); err != nil {
		t.Fatal(err)
	}
	if _, err := ac.Add("she"); err != nil {
		t.Fatal(err)
	}

	removed, err := ac.Remove("he")
	if err != nil {
		t.Fatalf("Remove() error: %v", err)
	}
	if removed < 0 {
		t.Errorf("Remove() = %d, want >= 0", removed)
	}

	matched, err := ac.Find("she is here")
	if err != nil {
		t.Fatal(err)
	}
	if containsAll(matched, "he") && !containsAll(matched, "she") {
		t.Errorf("should still find 'she', got %v", matched)
	}
}

func TestV1FindIndex(t *testing.T) {
	ac, mr := createAhoCorasickV1(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	if _, err := ac.Add("he"); err != nil {
		t.Fatal(err)
	}
	if _, err := ac.Add("she"); err != nil {
		t.Fatal(err)
	}

	results, err := ac.FindIndex("she is here")
	if err != nil {
		t.Fatalf("FindIndex() error: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected some results")
	}
}

func TestV1AddEmptyKeyword(t *testing.T) {
	ac, mr := createAhoCorasickV1(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	added, err := ac.Add("")
	if err != nil {
		t.Fatalf("Add('') error: %v", err)
	}
	if added != 0 {
		t.Errorf("Add('') = %d, want 0", added)
	}
}

func TestV1RemoveEmptyKeyword(t *testing.T) {
	ac, mr := createAhoCorasickV1(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	removed, err := ac.Remove("")
	if err != nil {
		t.Fatalf("Remove('') error: %v", err)
	}
	if removed != 0 {
		t.Errorf("Remove('') = %d, want 0", removed)
	}
}

func TestV1Info(t *testing.T) {
	ac, mr := createAhoCorasickV1(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	info, err := ac.Info()
	if err != nil {
		t.Fatalf("Info() error: %v", err)
	}
	if info.Keywords != 0 {
		t.Errorf("empty Info().Keywords = %d, want 0", info.Keywords)
	}

	if _, err := ac.Add("hello"); err != nil {
		t.Fatal(err)
	}

	info, err = ac.Info()
	if err != nil {
		t.Fatal(err)
	}
	if info.Keywords != 1 {
		t.Errorf("Info().Keywords = %d, want 1", info.Keywords)
	}
}

func TestCloseTwice(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()

	if err := ac.Close(); err != nil {
		t.Fatalf("first Close() error: %v", err)
	}

	err := ac.Close()
	if err == nil {
		t.Fatal("expected error on second Close()")
	}
	if !errors.Is(err, ErrRedisAlreadyClosed) {
		t.Errorf("expected ErrRedisAlreadyClosed, got %v", err)
	}
}

func TestSchemaVersionMethod(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	if ac.SchemaVersion() != SchemaV2 {
		t.Errorf("SchemaVersion() = %d, want %d", ac.SchemaVersion(), SchemaV2)
	}
}

func TestV1Suggest(t *testing.T) {
	ac, mr := createAhoCorasickV1(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	if _, err := ac.Add("hello"); err != nil {
		t.Fatal(err)
	}
	if _, err := ac.Add("help"); err != nil {
		t.Fatal(err)
	}

	results, err := ac.Suggest("hel")
	if err != nil {
		t.Fatalf("Suggest() error: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("Suggest('hel') = %v, want 2 results", results)
	}
}

func TestV1SuggestIndexIntegration(t *testing.T) {
	ac, mr := createAhoCorasickV1(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	if _, err := ac.Add("hello"); err != nil {
		t.Fatal(err)
	}

	results, err := ac.SuggestIndex("hel")
	if err != nil {
		t.Fatalf("SuggestIndex() error: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("SuggestIndex('hel') = %v, want 1 result", results)
	}
}

func TestV1SuggestNoMatch(t *testing.T) {
	ac, mr := createAhoCorasickV1(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	if _, err := ac.Add("hello"); err != nil {
		t.Fatal(err)
	}

	results, err := ac.Suggest("xyz")
	if err != nil {
		t.Fatalf("Suggest() error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("Suggest('xyz') = %v, want empty", results)
	}
}

func TestV1SuggestEmpty(t *testing.T) {
	ac, mr := createAhoCorasickV1(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	results, err := ac.Suggest("")
	if err != nil {
		t.Fatalf("Suggest('') error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("Suggest('') = %v, want empty", results)
	}
}

func TestV1AddDuplicate(t *testing.T) {
	ac, mr := createAhoCorasickV1(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	if _, err := ac.Add("he"); err != nil {
		t.Fatal(err)
	}

	added, err := ac.Add("he")
	if err != nil {
		t.Fatal(err)
	}
	if added != 0 {
		t.Errorf("Add duplicate = %d, want 0", added)
	}
}

func TestMigrationConstants(t *testing.T) {
	if migrationStatusError != "error" {
		t.Errorf("migrationStatusError = %q", migrationStatusError)
	}
	if migrationStatusSuccess != "success" {
		t.Errorf("migrationStatusSuccess = %q", migrationStatusSuccess)
	}
	if migrationStatusDryRun != "dry-run" {
		t.Errorf("migrationStatusDryRun = %q", migrationStatusDryRun)
	}
}

func TestMigrationLockKey(t *testing.T) {
	ac := &AhoCorasick{name: "test"}
	key := ac.migrationLockKey()
	if key != "{test}:migration_lock" {
		t.Errorf("migrationLockKey() = %q, want {test}:migration_lock", key)
	}
}

func TestParseJSON(t *testing.T) {
	var result []string
	if err := parseJSON(`["a","b"]`, &result); err != nil {
		t.Fatalf("parseJSON() error: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("parseJSON result = %v, want 2 items", result)
	}

	if err := parseJSON(`invalid`, &result); err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestV1RemoveKeywordIntegration(t *testing.T) {
	ac, mr := createAhoCorasickV1(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	added, err := ac.Add("HELLO")
	if err != nil {
		t.Fatal(err)
	}
	if added != 1 {
		t.Errorf("Add('HELLO') = %d, want 1", added)
	}

	matched, err := ac.Find("hello")
	if err != nil {
		t.Fatal(err)
	}
	if !containsAll(matched, "hello") {
		t.Errorf("Find('hello') = %v, want [hello]", matched)
	}
}

func TestV1AddWhitespaceKeyword(t *testing.T) {
	ac, mr := createAhoCorasickV1(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	added, err := ac.Add("   ")
	if err != nil {
		t.Fatal(err)
	}
	if added != 0 {
		t.Errorf("Add('   ') = %d, want 0", added)
	}
}

func TestParallelContextCancellation(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	if _, err := ac.Add("he"); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	longText := strings.Repeat("he is here ", 100)
	_, _ = ac.FindParallelContext(ctx, longText, &ParallelOptions{
		ChunkSize: 20,
		Workers:   2,
	})
}

func TestParallelIndexContextCancellation(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	if _, err := ac.Add("he"); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	longText := strings.Repeat("he is here ", 100)
	_, _ = ac.FindIndexParallelContext(ctx, longText, &ParallelOptions{
		ChunkSize: 20,
		Workers:   2,
	})
}

func TestFindParallelInvalidChunkSize(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	_, err := ac.FindParallel("test", &ParallelOptions{ChunkSize: 0})
	if err == nil {
		t.Error("expected error for ChunkSize=0")
	}
}

func TestFindIndexParallelInvalidChunkSize(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	_, err := ac.FindIndexParallel("test", &ParallelOptions{ChunkSize: -1})
	if err == nil {
		t.Error("expected error for negative ChunkSize")
	}
}

func TestMigrationLockAcquireRelease(t *testing.T) {
	mr := createTestRedisServer(t)
	defer mr.Close()

	ac, err := Create(&AhoCorasickArgs{
		Addr: mr.Addr(),
		Name: "lock-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ac.Close() }()

	acquired, err := ac.acquireMigrationLock()
	if err != nil {
		t.Fatalf("acquireMigrationLock() error: %v", err)
	}
	if !acquired {
		t.Error("expected to acquire lock")
	}

	if err := ac.releaseMigrationLock(); err != nil {
		t.Fatalf("releaseMigrationLock() error: %v", err)
	}
}

func TestMigrationLockAlreadyHeld(t *testing.T) {
	mr := createTestRedisServer(t)
	defer mr.Close()

	ac, err := Create(&AhoCorasickArgs{
		Addr: mr.Addr(),
		Name: "lock-test2",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ac.Close() }()

	acquired, err := ac.acquireMigrationLock()
	if err != nil {
		t.Fatal(err)
	}
	if !acquired {
		t.Fatal("first acquire should succeed")
	}

	acquired2, err := ac.acquireMigrationLock()
	if err != nil {
		t.Fatal(err)
	}
	if acquired2 {
		t.Error("second acquire should fail (lock already held)")
	}

	_ = ac.releaseMigrationLock()
}

func TestMigrationInProgress(t *testing.T) {
	s := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer func() { _ = client.Close() }()

	// Seed V1 data so migration doesn't fail for other reasons
	client.SAdd(context.Background(), "{test}:keyword", "he")
	client.ZAdd(context.Background(), "{test}:prefix", &redis.Z{Score: 0, Member: ""})

	// Acquire the migration lock with a separate client to simulate another process
	lockKey := "{test}:migration_lock"
	acquired, err := client.SetNX(context.Background(), lockKey, "migrating", 300*time.Second).Result()
	if err != nil {
		t.Fatalf("failed to pre-acquire lock: %v", err)
	}
	if !acquired {
		t.Fatal("expected to acquire lock on first attempt")
	}

	ac := &AhoCorasick{
		redisClient: client,
		ctx:         context.Background(),
		name:        "test",
	}

	_, err = ac.MigrateV1ToV2(nil)
	if err == nil {
		t.Fatal("expected error when migration lock is already held")
	}
	if err.Error() != "migration already in progress" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRemoveManyBestEffortError(t *testing.T) {
	ac, _ := createAhoCorasick(t)
	defer ac.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := ac.RemoveManyContext(ctx, []string{"keyword1", "keyword2"}, &BatchOptions{Mode: BatchModeBestEffort})
	if err != nil {
		t.Fatalf("best-effort should not return error, got: %v", err)
	}
	if len(result.Failed) == 0 {
		t.Error("expected some failed removals in best-effort mode with cancelled context")
	}
}

func TestRemoveManyTransactionalDuplicate(t *testing.T) {
	ac, _ := createAhoCorasick(t)
	defer ac.Close()

	_, _ = ac.Add("he")

	result, err := ac.RemoveMany([]string{"he", "he"}, &BatchOptions{Mode: BatchModeTransactional})
	if err != nil {
		t.Fatalf("RemoveMany transactional error: %v", err)
	}
	if len(result.Skipped) < 1 {
		t.Errorf("expected duplicate to be skipped, got removed=%v skipped=%v", result.Removed, result.Skipped)
	}
}

func TestRemoveManyTransactionalError(t *testing.T) {
	ac, _ := createAhoCorasick(t)
	defer ac.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := ac.RemoveManyContext(ctx, []string{"keyword1"}, &BatchOptions{Mode: BatchModeTransactional})
	if err == nil {
		t.Fatal("expected error from cancelled context in RemoveMany transactional")
	}
}

func TestRollbackRemovedWithLogger(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	log := &testLogger{}
	ac := &AhoCorasick{
		redisClient:   client,
		storage:       newRedisStorage(client),
		ctx:           context.Background(),
		name:          "test",
		logger:        log,
		schemaVersion: SchemaV2,
		ops: &v2Operations{
			storage: newRedisStorage(client),
			client:  client,
			name:    "test",
			ctx:     context.Background(),
			cache:   &trieCache{},
			logger:  log,
		},
	}

	mr.Close()

	ac.Debug()
}

func TestTryAddV2OnNonV2Ops(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	ac := &AhoCorasick{
		redisClient:   client,
		storage:       newRedisStorage(client),
		ctx:           context.Background(),
		name:          "test",
		schemaVersion: SchemaV1,
		ops: &v1Operations{
			storage: newRedisStorage(client),
			name:    "test",
			ctx:     context.Background(),
			ac:      nil,
		},
	}

	_, err := ac.tryAddV2(context.Background(), "he")
	if err == nil {
		t.Fatal("expected error when tryAddV2 called on non-v2 ops")
	}
}

func TestTryRemoveV2OnNonV2Ops(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	ac := &AhoCorasick{
		redisClient:   client,
		storage:       newRedisStorage(client),
		ctx:           context.Background(),
		name:          "test",
		schemaVersion: SchemaV1,
		ops: &v1Operations{
			storage: newRedisStorage(client),
			name:    "test",
			ctx:     context.Background(),
			ac:      nil,
		},
	}

	_, err := ac.tryRemoveV2(context.Background(), "he")
	if err == nil {
		t.Fatal("expected error when tryRemoveV2 called on non-v2 ops")
	}
}

func TestV2TryAddBadKeywordsJSON(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	client.HSet(context.Background(), "{test}:trie", map[string]interface{}{
		"keywords": `not-json`,
		"prefixes": `[""]`,
		"version":  "100",
	})

	ops := newTestV2Ops(t, mr)
	defer func() { _ = ops.client.Close() }()

	_, err := ops.tryAddV2(context.Background(), "she")
	if err == nil {
		t.Fatal("expected error for bad JSON in keywords")
	}
}

func TestV2TryRemoveBadKeywordsJSON(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	client.HSet(context.Background(), "{test}:trie", map[string]interface{}{
		"keywords": `not-json`,
		"prefixes": `[""]`,
		"version":  "100",
	})

	ops := newTestV2Ops(t, mr)
	defer func() { _ = ops.client.Close() }()

	_, err := ops.tryRemoveV2(context.Background(), "he")
	if err == nil {
		t.Fatal("expected error for bad JSON in keywords")
	}
}

func TestV2TryRemoveVersionFallback(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	client.HSet(context.Background(), "{test}:trie", map[string]interface{}{
		"keywords": `["he"]`,
		"prefixes": `["","h","he"]`,
		"version":  "not-a-number",
	})

	ops := newTestV2Ops(t, mr)
	defer func() { _ = ops.client.Close() }()

	_, err := ops.tryRemoveV2(context.Background(), "he")
	if err == nil {
		t.Fatal("expected concurrency conflict with bad version in Redis")
	}
}

func TestV2TryAddVersionFallback(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	client.HSet(context.Background(), "{test}:trie", map[string]interface{}{
		"keywords": `[]`,
		"prefixes": `[""]`,
		"suffixes": `[""]`,
		"version":  "not-a-number",
	})

	ops := newTestV2Ops(t, mr)
	defer func() { _ = ops.client.Close() }()

	_, err := ops.tryAddV2(context.Background(), "he")
	if err == nil {
		t.Fatal("expected concurrency conflict with bad version in Redis")
	}
}

func TestV2FindIndexError(t *testing.T) {
	mr := miniredis.RunT(t)
	mr.Close()

	ops := &v2Operations{
		storage: newRedisStorage(redis.NewClient(&redis.Options{Addr: "localhost:1"})),
		client:  redis.NewClient(&redis.Options{Addr: "localhost:1"}),
		name:    "test",
		ctx:     context.Background(),
		cache:   nil,
		logger:  &testLogger{},
	}
	defer func() { _ = ops.client.Close() }()

	_, err := ops.findIndex(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected error from closed Redis in findIndex")
	}
}

func TestV2GetOrLoadCacheError(t *testing.T) {
	mr := miniredis.RunT(t)
	mr.Close()

	cache := &trieCache{}
	ops := &v2Operations{
		storage: newRedisStorage(redis.NewClient(&redis.Options{Addr: "localhost:1"})),
		client:  redis.NewClient(&redis.Options{Addr: "localhost:1"}),
		name:    "test",
		ctx:     context.Background(),
		cache:   cache,
		logger:  &testLogger{},
	}
	defer func() { _ = ops.client.Close() }()

	_, _, err := ops.getOrLoadCache(context.Background())
	if err == nil {
		t.Fatal("expected error from closed Redis in getOrLoadCache")
	}
}

func TestV2FlushError(t *testing.T) {
	mr := miniredis.RunT(t)
	mr.Close()

	ops := &v2Operations{
		storage: newRedisStorage(redis.NewClient(&redis.Options{Addr: "localhost:1"})),
		client:  redis.NewClient(&redis.Options{Addr: "localhost:1"}),
		name:    "test",
		ctx:     context.Background(),
		cache:   &trieCache{},
		logger:  &testLogger{},
	}
	defer func() { _ = ops.client.Close() }()

	err := ops.flush(context.Background())
	if err == nil {
		t.Fatal("expected error from closed Redis in flush")
	}
}

func TestV2InfoBadPrefixesJSON(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	client.HSet(context.Background(), "{test}:trie", map[string]interface{}{
		"keywords": `["he"]`,
		"prefixes": `not-json`,
	})

	ops := newTestV2Ops(t, mr)
	defer func() { _ = ops.client.Close() }()

	_, err := ops.info(context.Background())
	if err == nil {
		t.Fatal("expected error for bad JSON in info prefixes")
	}
}

func TestAddManyBestEffortError(t *testing.T) {
	ac, _ := createAhoCorasick(t)
	defer ac.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := ac.AddManyContext(ctx, []string{"keyword1"}, &BatchOptions{Mode: BatchModeBestEffort})
	if err != nil {
		t.Fatalf("best-effort should not return error, got: %v", err)
	}
	if len(result.Failed) == 0 {
		t.Error("expected some failed adds in best-effort mode with cancelled context")
	}
}

func TestRollbackAddedWithLogger(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	log := &testLogger{}
	ac := &AhoCorasick{
		redisClient:   client,
		storage:       newRedisStorage(client),
		ctx:           context.Background(),
		name:          "test",
		logger:        log,
		schemaVersion: SchemaV2,
		ops: &v2Operations{
			storage: newRedisStorage(client),
			client:  client,
			name:    "test",
			ctx:     context.Background(),
			cache:   &trieCache{},
			logger:  log,
		},
	}

	mr.Close()

	ac.rollbackAdded(context.Background(), []string{"keyword1"})
}

func TestFindManyContextError(t *testing.T) {
	ac, _ := createAhoCorasick(t)
	defer ac.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := ac.FindManyContext(ctx, []string{"hello world"})
	if err == nil {
		t.Fatal("expected error from cancelled context in FindManyContext")
	}
}

func TestDebugV1WithNoData(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	ac := &AhoCorasick{
		redisClient:   client,
		storage:       newRedisStorage(client),
		ctx:           context.Background(),
		name:          "test",
		schemaVersion: SchemaV1,
		ops: &v1Operations{
			storage: newRedisStorage(client),
			name:    "test",
			ctx:     context.Background(),
			ac:      nil,
		},
	}

	ac.Debug()
}

func TestDebugV1WithClosedRedis(t *testing.T) {
	mr := miniredis.RunT(t)

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	ac := &AhoCorasick{
		redisClient:   client,
		storage:       newRedisStorage(client),
		ctx:           context.Background(),
		name:          "test",
		schemaVersion: SchemaV1,
		ops: &v1Operations{
			storage: newRedisStorage(client),
			name:    "test",
			ctx:     context.Background(),
			ac:      nil,
		},
	}

	mr.Close()

	ac.Debug()
}

func TestDebugV2WithData(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	ctx := context.Background()
	client.HSet(ctx, "{test}:trie", map[string]interface{}{
		"keywords": `["he","she"]`,
		"prefixes": `["","h","he","s","sh","she"]`,
		"suffixes": `["","e","eh","s","hs","ehs"]`,
		"version":  "100",
	})
	client.HSet(ctx, "{test}:outputs", map[string]interface{}{
		"he":  `["he"]`,
		"she": `["he","she"]`,
	})
	client.HSet(ctx, "{test}:nodes", map[string]interface{}{
		"he": `["h","he"]`,
	})

	ac := &AhoCorasick{
		redisClient:   client,
		storage:       newRedisStorage(client),
		ctx:           ctx,
		name:          "test",
		schemaVersion: SchemaV2,
		ops: &v2Operations{
			storage: newRedisStorage(client),
			client:  client,
			name:    "test",
			ctx:     ctx,
		},
	}

	ac.Debug()
}

func TestDebugV2WithClosedRedis(t *testing.T) {
	mr := miniredis.RunT(t)

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	ac := &AhoCorasick{
		redisClient:   client,
		storage:       newRedisStorage(client),
		ctx:           context.Background(),
		name:          "test",
		schemaVersion: SchemaV2,
		ops: &v2Operations{
			storage: newRedisStorage(client),
			client:  client,
			name:    "test",
			ctx:     context.Background(),
		},
	}

	mr.Close()

	ac.Debug()
}

func TestInitV2HSetError(t *testing.T) {
	mr := miniredis.RunT(t)

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	mr.Close()

	ac := &AhoCorasick{
		redisClient:   client,
		storage:       newRedisStorage(client),
		ctx:           context.Background(),
		name:          "test",
		schemaVersion: SchemaV2,
	}

	err := ac.init()
	if err == nil {
		t.Fatal("expected error from closed Redis in init()")
	}
}

func TestInitV1ZAddError(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	mr.Close()
	ac := &AhoCorasick{
		redisClient:   client,
		storage:       newRedisStorage(client),
		ctx:           context.Background(),
		name:          "test",
		schemaVersion: SchemaV1,
	}
	err := ac.init()
	if err == nil {
		t.Fatal("expected error from closed Redis in init() v1")
	}
}

func TestV2SuggestError(t *testing.T) {
	mr := miniredis.RunT(t)
	addr := mr.Addr()
	mr.Close()

	ops := &v2Operations{
		storage: newRedisStorage(redis.NewClient(&redis.Options{Addr: addr})),
		client:  redis.NewClient(&redis.Options{Addr: addr}),
		name:    "test",
		ctx:     context.Background(),
		logger:  &testLogger{},
	}
	defer func() { _ = ops.client.Close() }()

	_, err := ops.suggest(context.Background(), "he")
	if err == nil {
		t.Fatal("expected error from closed Redis in suggest")
	}
}

func TestV2SuggestIndexError(t *testing.T) {
	mr := miniredis.RunT(t)
	addr := mr.Addr()
	mr.Close()

	ops := &v2Operations{
		storage: newRedisStorage(redis.NewClient(&redis.Options{Addr: addr})),
		client:  redis.NewClient(&redis.Options{Addr: addr}),
		name:    "test",
		ctx:     context.Background(),
		logger:  &testLogger{},
	}
	defer func() { _ = ops.client.Close() }()

	_, err := ops.suggestIndex(context.Background(), "he")
	if err == nil {
		t.Fatal("expected error from closed Redis in suggestIndex")
	}
}

func TestRollbackAddedEmpty(t *testing.T) {
	ac, _ := createAhoCorasick(t)
	defer ac.Close()

	ac.rollbackAdded(context.Background(), []string{})
}
