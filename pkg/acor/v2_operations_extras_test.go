package acor

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	redis "github.com/go-redis/redis/v8"
)

type testLogger struct{}

func (l *testLogger) Printf(format string, args ...interface{}) {}
func (l *testLogger) Println(v ...interface{})                  {}

func newTestV2Ops(t *testing.T, mr *miniredis.Miniredis) *v2Operations {
	t.Helper()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	store := newRedisStorage(client)

	return &v2Operations{
		storage: store,
		client:  client,
		name:    "test",
		ctx:     context.Background(),
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

func TestV2RemoveRetryContextCancellation(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	ops := newTestV2Ops(t, mr)
	defer func() { _ = ops.client.Close() }()

	seedV2Trie(t, mr, []string{"he", "she", "his"})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := ops.remove(ctx, "he")
	if err == nil {
		t.Fatal("expected error from canceled context")
	}
	if ctx.Err() == nil {
		t.Fatal("expected context.Canceled or context.DeadlineExceeded")
	}
}

func TestV2OperationsAddCanceledContext(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	ops := newTestV2Ops(t, mr)
	defer func() { _ = ops.client.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := ops.add(ctx, "him")
	if err == nil {
		t.Fatal("expected error from canceled context")
	}
	if ctx.Err() == nil {
		t.Fatal("expected context.Canceled or context.DeadlineExceeded")
	}
}

func TestV2RemoveExhaustsRetries(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	ops := newTestV2Ops(t, mr)
	defer func() { _ = ops.client.Close() }()

	_, err := ops.remove(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("remove nonexistent keyword should not error, got: %v", err)
	}
}

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

func TestV2LocalFindContextCancellation(t *testing.T) {
	ops := &v2Operations{}

	prefixes := []string{"", "h", "he", "s", "sh", "she"}
	outputs := map[string][]string{
		"he":  {"he"},
		"she": {"he", "she"},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	longText := strings.Repeat("she sells sea shells ", 100)
	result := ops.localFind(ctx, longText, prefixes, outputs)

	if result == nil {
		t.Fatal("localFind should return non-nil slice on context cancellation")
	}
}

func TestV2LocalFindIndexContextCancellation(t *testing.T) {
	ops := &v2Operations{}

	prefixes := []string{"", "h", "he", "s", "sh", "she"}
	outputs := map[string][]string{
		"he":  {"he"},
		"she": {"he", "she"},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	longText := strings.Repeat("she sells sea shells ", 100)
	result := ops.localFindIndex(ctx, longText, prefixes, outputs)

	if result == nil {
		t.Fatal("localFindIndex should return non-nil map on context cancellation")
	}
}

func TestV2LocalFindNormal(t *testing.T) {
	ops := &v2Operations{}

	prefixes := []string{"", "h", "he", "s", "sh", "she"}
	outputs := map[string][]string{
		"he":  {"he"},
		"she": {"he", "she"},
	}

	ctx := context.Background()
	result := ops.localFind(ctx, "she", prefixes, outputs)

	if !equalStringSets(result, []string{"he", "she"}) {
		t.Errorf("localFind('she') = %v, want [he she]", result)
	}
}

func TestV2LocalFindIndexNormal(t *testing.T) {
	ops := &v2Operations{}

	prefixes := []string{"", "h", "he", "s", "sh", "she"}
	outputs := map[string][]string{
		"he":  {"he"},
		"she": {"he", "she"},
	}

	ctx := context.Background()
	result := ops.localFindIndex(ctx, "she", prefixes, outputs)

	assertIndexResults(t, result, map[string][]int{
		"he":  {1},
		"she": {0},
	})
}

func TestFindFailState(t *testing.T) {
	ops := &v2Operations{}

	prefixSet := map[string]struct{}{
		"":    {},
		"h":   {},
		"he":  {},
		"s":   {},
		"sh":  {},
		"she": {},
	}

	tests := []struct {
		state string
		want  string
	}{
		{"he", ""},
		{"she", "he"},
		{"sh", "h"},
		{"xyz", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.state, func(t *testing.T) {
			got := ops.findFailState(tt.state, prefixSet)
			if got != tt.want {
				t.Errorf("findFailState(%q) = %q, want %q", tt.state, got, tt.want)
			}
		})
	}
}

func TestFindFailStateLongestSuffix(t *testing.T) {
	ops := &v2Operations{}

	prefixSet := map[string]struct{}{
		"":    {},
		"a":   {},
		"ab":  {},
		"abc": {},
		"bc":  {},
		"c":   {},
	}

	got := ops.findFailState("babc", prefixSet)
	if got != "abc" {
		t.Errorf("findFailState('babc') = %q, want 'abc'", got)
	}
}

func TestMustJSON(t *testing.T) {
	result := mustJSON([]string{"a", "b"})
	if result != `["a","b"]` {
		t.Errorf("mustJSON([a,b]) = %q, want %q", result, `["a","b"]`)
	}

	result = mustJSON(map[string]int{"x": 1})
	if result != `{"x":1}` {
		t.Errorf("mustJSON({x:1}) = %q, want %q", result, `{"x":1}`)
	}
}

func TestMustJSONPanics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected mustJSON to panic for unmarshallable value")
		}
	}()

	mustJSON(func() {})
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

func TestV2GetOrLoadCacheNoCache(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	ops := &v2Operations{
		storage: newRedisStorage(redis.NewClient(&redis.Options{Addr: mr.Addr()})),
		client:  redis.NewClient(&redis.Options{Addr: mr.Addr()}),
		name:    "test",
		ctx:     context.Background(),
		cache:   nil,
		logger:  &testLogger{},
	}
	defer func() { _ = ops.client.Close() }()

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

	prefixes, outputs, err := ops.getOrLoadCache(context.Background())
	if err != nil {
		t.Fatalf("getOrLoadCache() error: %v", err)
	}
	if len(prefixes) != 3 {
		t.Errorf("len(prefixes) = %d, want 3", len(prefixes))
	}
	if len(outputs) != 1 {
		t.Errorf("len(outputs) = %d, want 1", len(outputs))
	}
}

func TestV2PublishInvalidate(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	cache := &trieCache{}
	cache.set([]string{"a"}, map[string][]string{"a": {"a"}})

	ops := &v2Operations{
		storage: newRedisStorage(redis.NewClient(&redis.Options{Addr: mr.Addr()})),
		client:  redis.NewClient(&redis.Options{Addr: mr.Addr()}),
		name:    "test",
		ctx:     context.Background(),
		cache:   cache,
		logger:  &testLogger{},
	}
	defer func() { _ = ops.client.Close() }()

	ops.publishInvalidate(context.Background())

	_, _, valid := cache.get()
	if valid {
		t.Error("cache should be invalid after publishInvalidate")
	}
}

func TestV2FetchTrieData(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	client.HSet(context.Background(), "{test}:trie", map[string]interface{}{
		"keywords": `["he","she"]`,
		"prefixes": `["","h","he","s","sh","she"]`,
		"version":  "100",
	})
	client.HSet(context.Background(), "{test}:outputs", map[string]interface{}{
		"he":  `["he"]`,
		"she": `["he","she"]`,
	})

	ops := &v2Operations{
		storage: newRedisStorage(client),
		client:  client,
		name:    "test",
		ctx:     context.Background(),
		logger:  &testLogger{},
	}

	prefixes, outputs, err := ops.fetchTrieData(context.Background())
	if err != nil {
		t.Fatalf("fetchTrieData() error: %v", err)
	}
	if len(prefixes) != 6 {
		t.Errorf("len(prefixes) = %d, want 6", len(prefixes))
	}
	if len(outputs) != 2 {
		t.Errorf("len(outputs) = %d, want 2", len(outputs))
	}
}

func TestV2LoadCache(t *testing.T) {
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

	cache := &trieCache{}
	ops := &v2Operations{
		storage: newRedisStorage(client),
		client:  client,
		name:    "test",
		ctx:     context.Background(),
		cache:   cache,
		logger:  &testLogger{},
	}

	if err := ops.loadCache(context.Background()); err != nil {
		t.Fatalf("loadCache() error: %v", err)
	}

	prefixes, outputs, valid := cache.get()
	if !valid {
		t.Fatal("cache should be valid after loadCache")
	}
	if len(prefixes) != 3 {
		t.Errorf("len(prefixes) = %d, want 3", len(prefixes))
	}
	if len(outputs) != 1 {
		t.Errorf("len(outputs) = %d, want 1", len(outputs))
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

func TestV2LocalFindWithLongText(t *testing.T) {
	ops := &v2Operations{}

	prefixes := []string{"", "a", "ab", "abc"}
	outputs := map[string][]string{
		"abc": {"abc"},
	}

	text := strings.Repeat("x", 1000) + "abc"
	result := ops.localFind(context.Background(), text, prefixes, outputs)

	if !containsAll(result, "abc") {
		t.Errorf("localFind should find 'abc', got %v", result)
	}
}

func TestV2LocalFindIndexWithLongText(t *testing.T) {
	ops := &v2Operations{}

	prefixes := []string{"", "a", "ab", "abc"}
	outputs := map[string][]string{
		"abc": {"abc"},
	}

	text := strings.Repeat("x", 1000) + "abc"
	result := ops.localFindIndex(context.Background(), text, prefixes, outputs)

	if _, ok := result["abc"]; !ok {
		t.Error("localFindIndex should find 'abc'")
	}
}

func TestNewTrieCache(t *testing.T) {
	cache := &trieCache{}
	_, _, valid := cache.get()
	if valid {
		t.Error("new cache should not be valid")
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

func TestV2ConcurrencyConflictRetries(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	ops := newTestV2Ops(t, mr)
	defer func() { _ = ops.client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ops2 := newTestV2Ops(t, mr)
	defer func() { _ = ops2.client.Close() }()

	_, err := ops.add(ctx, "keyword1")
	if err != nil {
		t.Fatalf("first add error: %v", err)
	}

	_, err = ops2.add(ctx, "keyword2")
	if err != nil {
		t.Fatalf("second add (different ops) error: %v", err)
	}

	matched, err := ops.find(ctx, "keyword1 keyword2")
	if err != nil {
		t.Fatalf("find() error: %v", err)
	}
	if !containsAll(matched, "keyword1", "keyword2") {
		t.Errorf("find() = %v, want both keywords", matched)
	}
}

func TestV2RemoveMaxRetriesExhausted(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	ops := newTestV2Ops(t, mr)
	defer func() { _ = ops.client.Close() }()

	ctx := context.Background()

	_, _ = ops.add(ctx, "he")

	ops2 := newTestV2Ops(t, mr)
	defer func() { _ = ops2.client.Close() }()

	_, _ = ops2.add(ctx, "she")

	_, err := ops.remove(ctx, "he")
	if err != nil {
		t.Fatalf("remove() should succeed on retry, got: %v", err)
	}
}

func TestV2GetOrLoadCacheDoubleCheck(t *testing.T) {
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

	cache := &trieCache{}
	ops := &v2Operations{
		storage: newRedisStorage(client),
		client:  client,
		name:    "test",
		ctx:     context.Background(),
		cache:   cache,
		logger:  &testLogger{},
	}

	prefixes, outputs, err := ops.getOrLoadCache(context.Background())
	if err != nil {
		t.Fatalf("getOrLoadCache() error: %v", err)
	}
	if len(prefixes) != 3 {
		t.Errorf("len(prefixes) = %d, want 3", len(prefixes))
	}
	if len(outputs) != 1 {
		t.Errorf("len(outputs) = %d, want 1", len(outputs))
	}

	prefixes2, outputs2, err := ops.getOrLoadCache(context.Background())
	if err != nil {
		t.Fatalf("second getOrLoadCache() error: %v", err)
	}
	if len(prefixes2) != len(prefixes) {
		t.Errorf("second call returned different prefixes")
	}
	if len(outputs2) != len(outputs) {
		t.Errorf("second call returned different outputs")
	}
}

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
		ctx:     context.Background(),
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
		ctx:     context.Background(),
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

func TestV2LoadCacheError(t *testing.T) {
	mr := miniredis.RunT(t)

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	cache := &trieCache{}
	ops := &v2Operations{
		storage: newRedisStorage(client),
		client:  client,
		name:    "test",
		ctx:     context.Background(),
		cache:   cache,
		logger:  &testLogger{},
	}

	mr.Close()

	err := ops.loadCache(context.Background())
	if err == nil {
		t.Fatal("expected error when Redis is closed")
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

func TestV2PublishInvalidateWithPublishError(t *testing.T) {
	mr := miniredis.RunT(t)
	mr.Close()

	cache := &trieCache{}
	cache.set([]string{"a"}, map[string][]string{"a": {"a"}})

	ops := &v2Operations{
		storage: newRedisStorage(redis.NewClient(&redis.Options{Addr: "localhost:1"})),
		client:  redis.NewClient(&redis.Options{Addr: "localhost:1"}),
		name:    "test",
		ctx:     context.Background(),
		cache:   cache,
		logger:  &testLogger{},
	}
	defer func() { _ = ops.client.Close() }()

	ops.publishInvalidate(context.Background())

	_, _, valid := cache.get()
	if valid {
		t.Error("cache should be invalid even if publish fails")
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
