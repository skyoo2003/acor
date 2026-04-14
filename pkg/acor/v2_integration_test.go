package acor

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	redis "github.com/go-redis/redis/v8"
)

func newTestV2Ops(t *testing.T, mr *miniredis.Miniredis) *v2Operations {
	t.Helper()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	store := newRedisStorage(client)

	return &v2Operations{
		storage: store,
		client:  client,
		name:    "test",
		cache:   &trieCache{},
		logger:  &testLogger{},
	}
}

func seedV2Trie(t *testing.T, mr *miniredis.Miniredis, keywords []string) {
	t.Helper()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	ops := newTestV2Ops(t, mr)
	defer func() { _ = ops.client.Close() }()

	ctx := context.Background()
	for _, kw := range keywords {
		if _, err := ops.tryAddV2(ctx, kw); err != nil {
			t.Fatalf("failed to seed keyword %q: %v", kw, err)
		}
	}
}

// --- Integration tests ---

func TestV2AddEmptyKeyword(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	ops := newTestV2Ops(t, mr)
	defer func() { _ = ops.client.Close() }()

	added, err := ops.add(context.Background(), "")
	if err != nil {
		t.Fatalf("add empty keyword should not error, got: %v", err)
	}
	if added != 0 {
		t.Errorf("add empty keyword should return 0, got %d", added)
	}
}

func TestV2AddWhitespaceKeyword(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	ops := newTestV2Ops(t, mr)
	defer func() { _ = ops.client.Close() }()

	added, err := ops.add(context.Background(), "   ")
	if err != nil {
		t.Fatalf("add whitespace keyword should not error, got: %v", err)
	}
	if added != 0 {
		t.Errorf("add whitespace keyword should return 0, got %d", added)
	}
}

func TestV2RemoveEmptyKeyword(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	ops := newTestV2Ops(t, mr)
	defer func() { _ = ops.client.Close() }()

	removed, err := ops.remove(context.Background(), "")
	if err != nil {
		t.Fatalf("remove empty keyword should not error, got: %v", err)
	}
	if removed != 0 {
		t.Errorf("remove empty keyword should return 0, got %d", removed)
	}
}

func TestV2OperationsFlush(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	ops := newTestV2Ops(t, mr)
	defer func() { _ = ops.client.Close() }()

	seedV2Trie(t, mr, []string{"he", "she"})

	ctx := context.Background()
	if err := ops.flush(ctx); err != nil {
		t.Fatalf("flush() error: %v", err)
	}

	info, err := ops.info(ctx)
	if err != nil {
		t.Fatalf("info() error: %v", err)
	}
	if info.Keywords != 0 {
		t.Errorf("after flush, Keywords = %d, want 0", info.Keywords)
	}
}

func TestV2OperationsInfo(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	ops := newTestV2Ops(t, mr)
	defer func() { _ = ops.client.Close() }()

	seedV2Trie(t, mr, []string{"he", "she", "his"})

	info, err := ops.info(context.Background())
	if err != nil {
		t.Fatalf("info() error: %v", err)
	}
	if info.Keywords != 3 {
		t.Errorf("info().Keywords = %d, want 3", info.Keywords)
	}
}

func TestV2OperationsSuggest(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	ops := newTestV2Ops(t, mr)
	defer func() { _ = ops.client.Close() }()

	seedV2Trie(t, mr, []string{"he", "her", "hello", "she"})

	results, err := ops.suggest(context.Background(), "he")
	if err != nil {
		t.Fatalf("suggest() error: %v", err)
	}
	if !containsAll(results, "he", "her", "hello") {
		t.Errorf("suggest('he') = %v, want [he her hello]", results)
	}

	results, err = ops.suggest(context.Background(), "")
	if err != nil {
		t.Fatalf("suggest('') error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("suggest('') = %v, want empty", results)
	}
}

func TestV2OperationsSuggestIndex(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	ops := newTestV2Ops(t, mr)
	defer func() { _ = ops.client.Close() }()

	seedV2Trie(t, mr, []string{"he", "her"})

	results, err := ops.suggestIndex(context.Background(), "he")
	if err != nil {
		t.Fatalf("suggestIndex() error: %v", err)
	}
	for kw, idxs := range results {
		if len(idxs) != 1 || idxs[0] != 0 {
			t.Errorf("suggestIndex['%s'] = %v, want [0]", kw, idxs)
		}
	}
}

func TestV2FindEmptyText(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	ops := newTestV2Ops(t, mr)
	defer func() { _ = ops.client.Close() }()

	seedV2Trie(t, mr, []string{"he"})

	result, err := ops.find(context.Background(), "")
	if err != nil {
		t.Fatalf("find('') error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("find('') = %v, want empty", result)
	}
}

func TestV2FindIndexEmptyText(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	ops := newTestV2Ops(t, mr)
	defer func() { _ = ops.client.Close() }()

	seedV2Trie(t, mr, []string{"he"})

	result, err := ops.findIndex(context.Background(), "")
	if err != nil {
		t.Fatalf("findIndex('') error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("findIndex('') = %v, want empty", result)
	}
}

func TestV2RemoveKeywordNotExists(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	ops := newTestV2Ops(t, mr)
	defer func() { _ = ops.client.Close() }()

	seedV2Trie(t, mr, []string{"he", "she"})

	removed, err := ops.remove(context.Background(), "him")
	if err != nil {
		t.Fatalf("remove nonexistent keyword error: %v", err)
	}
	if removed != 2 {
		t.Errorf("remove nonexistent keyword = %d, want 2 (remaining keywords)", removed)
	}
}

func TestV2AddDuplicate(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	ops := newTestV2Ops(t, mr)
	defer func() { _ = ops.client.Close() }()

	seedV2Trie(t, mr, []string{"he"})

	added, err := ops.add(context.Background(), "he")
	if err != nil {
		t.Fatalf("add duplicate keyword error: %v", err)
	}
	if added != 0 {
		t.Errorf("add duplicate keyword = %d, want 0", added)
	}
}

func TestV2RemoveThenFind(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	ops := newTestV2Ops(t, mr)
	defer func() { _ = ops.client.Close() }()

	seedV2Trie(t, mr, []string{"he", "she", "his"})

	ctx := context.Background()
	removed, err := ops.remove(ctx, "she")
	if err != nil {
		t.Fatalf("remove('she') error: %v", err)
	}
	if removed != 2 {
		t.Errorf("remove('she') = %d, want 2", removed)
	}

	ops.cache.invalidate()

	matched, err := ops.find(ctx, "she is his")
	if err != nil {
		t.Fatalf("find() error: %v", err)
	}
	if containsAll(matched, "she") {
		t.Errorf("found 'she' after removal: %v", matched)
	}
	if !containsAll(matched, "he", "his") {
		t.Errorf("missing 'he' or 'his': %v", matched)
	}
}

func TestV2OperationsSuggestNoMatch(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	ops := newTestV2Ops(t, mr)
	defer func() { _ = ops.client.Close() }()

	seedV2Trie(t, mr, []string{"he", "she"})

	results, err := ops.suggest(context.Background(), "xyz")
	if err != nil {
		t.Fatalf("suggest('xyz') error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("suggest('xyz') = %v, want empty", results)
	}
}

func TestV2AddAndFindIntegration(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	ops := newTestV2Ops(t, mr)
	defer func() { _ = ops.client.Close() }()

	ctx := context.Background()

	added, err := ops.add(ctx, "hello")
	if err != nil {
		t.Fatalf("add('hello') error: %v", err)
	}
	if added != 1 {
		t.Errorf("add('hello') = %d, want 1", added)
	}

	added, err = ops.add(ctx, "world")
	if err != nil {
		t.Fatalf("add('world') error: %v", err)
	}
	if added != 1 {
		t.Errorf("add('world') = %d, want 1", added)
	}

	matched, err := ops.find(ctx, "hello world")
	if err != nil {
		t.Fatalf("find('hello world') error: %v", err)
	}
	if !containsAll(matched, "hello", "world") {
		t.Errorf("find('hello world') = %v, want [hello world]", matched)
	}
}

func TestV2CaseInsensitiveAdd(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	ops := newTestV2Ops(t, mr)
	defer func() { _ = ops.client.Close() }()

	ctx := context.Background()

	added, err := ops.add(ctx, "HELLO")
	if err != nil {
		t.Fatalf("add('HELLO') error: %v", err)
	}
	if added != 1 {
		t.Errorf("add('HELLO') = %d, want 1", added)
	}

	matched, err := ops.find(ctx, "hello")
	if err != nil {
		t.Fatalf("find('hello') error: %v", err)
	}
	if !containsAll(matched, "hello") {
		t.Errorf("find('hello') = %v, want [hello]", matched)
	}

	added, err = ops.add(ctx, "  HELLO  ")
	if err != nil {
		t.Fatalf("add('  HELLO  ') error: %v", err)
	}
	if added != 0 {
		t.Errorf("add duplicate with whitespace = %d, want 0", added)
	}
}

func TestV2RemoveCaseInsensitive(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	ops := newTestV2Ops(t, mr)
	defer func() { _ = ops.client.Close() }()

	seedV2Trie(t, mr, []string{"hello"})

	ctx := context.Background()
	removed, err := ops.remove(ctx, "  HELLO  ")
	if err != nil {
		t.Fatalf("remove('  HELLO  ') error: %v", err)
	}
	if removed != 0 {
		t.Errorf("remove('  HELLO  ') = %d, want 0 (remaining keywords)", removed)
	}

	ops.cache.invalidate()
	matched, err := ops.find(ctx, "hello")
	if err != nil {
		t.Fatalf("find() after remove error: %v", err)
	}
	if len(matched) != 0 {
		t.Errorf("found keywords after removal: %v", matched)
	}
}

func TestV2FlushWithNodes(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	client.HSet(context.Background(), "{test}:trie", map[string]interface{}{
		"keywords": `["he"]`,
		"prefixes": `["","h","he"]`,
		"suffixes": `["","e","eh"]`,
		"version":  "100",
	})
	client.HSet(context.Background(), "{test}:outputs", map[string]interface{}{
		"he": `["he"]`,
	})
	client.HSet(context.Background(), "{test}:nodes", map[string]interface{}{
		"he": `["h","he"]`,
	})

	ops := newTestV2Ops(t, mr)
	defer func() { _ = ops.client.Close() }()

	if err := ops.flush(context.Background()); err != nil {
		t.Fatalf("flush() error: %v", err)
	}

	info, err := ops.info(context.Background())
	if err != nil {
		t.Fatalf("info() after flush error: %v", err)
	}
	if info.Keywords != 0 {
		t.Errorf("after flush with nodes, Keywords = %d, want 0", info.Keywords)
	}
}

func TestV2TryAddKeywordAlreadyExists(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	ops := newTestV2Ops(t, mr)
	defer func() { _ = ops.client.Close() }()

	seedV2Trie(t, mr, []string{"he"})

	added, err := ops.tryAddV2(context.Background(), "he")
	if err != nil {
		t.Fatalf("tryAddV2 existing keyword error: %v", err)
	}
	if added != 0 {
		t.Errorf("tryAddV2 existing keyword = %d, want 0", added)
	}
}

func TestV2TryRemoveKeywordNotExists(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	ops := newTestV2Ops(t, mr)
	defer func() { _ = ops.client.Close() }()

	seedV2Trie(t, mr, []string{"he", "she"})

	removed, err := ops.tryRemoveV2(context.Background(), "xyz")
	if err != nil {
		t.Fatalf("tryRemoveV2 nonexistent keyword error: %v", err)
	}
	if removed != 2 {
		t.Errorf("tryRemoveV2 nonexistent keyword = %d, want 2", removed)
	}
}

func TestV2RemoveAllKeywords(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	ops := newTestV2Ops(t, mr)
	defer func() { _ = ops.client.Close() }()

	seedV2Trie(t, mr, []string{"he", "she", "his"})

	ctx := context.Background()

	_, err := ops.remove(ctx, "he")
	if err != nil {
		t.Fatal(err)
	}
	_, err = ops.remove(ctx, "she")
	if err != nil {
		t.Fatal(err)
	}
	removed, err := ops.remove(ctx, "his")
	if err != nil {
		t.Fatal(err)
	}
	if removed != 0 {
		t.Errorf("removing last keyword = %d, want 0", removed)
	}

	ops.cache.invalidate()
	matched, err := ops.find(ctx, "he she his")
	if err != nil {
		t.Fatal(err)
	}
	if len(matched) != 0 {
		t.Errorf("found keywords after removing all: %v", matched)
	}
}

func TestV2FindIndexWithMatches(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	ops := newTestV2Ops(t, mr)
	defer func() { _ = ops.client.Close() }()

	seedV2Trie(t, mr, []string{"he", "she", "his"})

	ctx := context.Background()
	result, err := ops.findIndex(ctx, "she is his")
	if err != nil {
		t.Fatalf("findIndex() error: %v", err)
	}

	assertIndexResults(t, result, map[string][]int{
		"he":  {1},
		"she": {0},
		"his": {7},
	})
}

func TestV2AddMultiple(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	ops := newTestV2Ops(t, mr)
	defer func() { _ = ops.client.Close() }()

	ctx := context.Background()

	keywords := []string{"he", "she", "his", "her", "hers"}
	for _, kw := range keywords {
		added, err := ops.add(ctx, kw)
		if err != nil {
			t.Fatalf("add(%q) error: %v", kw, err)
		}
		if added != 1 {
			t.Errorf("add(%q) = %d, want 1", kw, added)
		}
	}

	matched, err := ops.find(ctx, "ushers")
	if err != nil {
		t.Fatalf("find('ushers') error: %v", err)
	}
	if !containsAll(matched, "she", "he", "hers") {
		t.Errorf("find('ushers') = %v, want she he hers", matched)
	}
}

// --- Error path tests ---

func TestV2FetchTrieDataBadJSON(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	client.HSet(context.Background(), "{test}:trie", map[string]interface{}{
		"prefixes": `not-json`,
		"version":  "100",
	})

	ops := &v2Operations{
		storage: newRedisStorage(client),
		client:  client,
		name:    "test",
		logger:  &testLogger{},
	}

	_, _, err := ops.fetchTrieData(context.Background())
	if err == nil {
		t.Fatal("expected error for bad JSON in prefixes")
	}
}

func TestV2FetchTrieDataBadOutputsJSON(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	client.HSet(context.Background(), "{test}:trie", map[string]interface{}{
		"keywords": `["he"]`,
		"prefixes": `["","h","he"]`,
		"version":  "100",
	})
	client.HSet(context.Background(), "{test}:outputs", map[string]interface{}{
		"he": `not-json`,
	})

	ops := &v2Operations{
		storage: newRedisStorage(client),
		client:  client,
		name:    "test",
		logger:  &testLogger{},
	}

	_, _, err := ops.fetchTrieData(context.Background())
	if err == nil {
		t.Fatal("expected error for bad JSON in outputs")
	}
}

func TestV2TryAddBadPrefixesJSON(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	client.HSet(context.Background(), "{test}:trie", map[string]interface{}{
		"keywords": `["he"]`,
		"prefixes": `not-json`,
		"version":  "100",
	})

	ops := newTestV2Ops(t, mr)
	defer func() { _ = ops.client.Close() }()

	_, err := ops.tryAddV2(context.Background(), "she")
	if err == nil {
		t.Fatal("expected error for bad JSON in prefixes")
	}
}

func TestV2TryAddBadSuffixesJSON(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	client.HSet(context.Background(), "{test}:trie", map[string]interface{}{
		"keywords": `[]`,
		"prefixes": `[""]`,
		"suffixes": `not-json`,
		"version":  "100",
	})

	ops := newTestV2Ops(t, mr)
	defer func() { _ = ops.client.Close() }()

	_, err := ops.tryAddV2(context.Background(), "she")
	if err == nil {
		t.Fatal("expected error for bad JSON in suffixes")
	}
}

func TestV2TryRemoveBadPrefixesJSON(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	client.HSet(context.Background(), "{test}:trie", map[string]interface{}{
		"keywords": `["he"]`,
		"prefixes": `not-json`,
		"version":  "100",
	})

	ops := newTestV2Ops(t, mr)
	defer func() { _ = ops.client.Close() }()

	_, err := ops.tryRemoveV2(context.Background(), "he")
	if err == nil {
		t.Fatal("expected error for bad JSON in prefixes")
	}
}

func TestV2InfoBadJSON(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	client.HSet(context.Background(), "{test}:trie", map[string]interface{}{
		"keywords": `not-json`,
		"prefixes": `[""]`,
	})

	ops := newTestV2Ops(t, mr)
	defer func() { _ = ops.client.Close() }()

	_, err := ops.info(context.Background())
	if err == nil {
		t.Fatal("expected error for bad JSON in info")
	}
}

func TestV2SuggestBadJSON(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	client.HSet(context.Background(), "{test}:trie", map[string]interface{}{
		"keywords": `not-json`,
	})

	ops := newTestV2Ops(t, mr)
	defer func() { _ = ops.client.Close() }()

	_, err := ops.suggest(context.Background(), "he")
	if err == nil {
		t.Fatal("expected error for bad JSON in suggest")
	}
}
