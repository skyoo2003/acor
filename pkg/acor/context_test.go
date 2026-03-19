package acor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
)

func TestGoWithContext(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()

	ac, err := Create(&AhoCorasickArgs{
		Addr:          mr.Addr(),
		Name:          "test-go-context",
		SchemaVersion: SchemaV1,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ac.Close() }()

	ctx := context.Background()

	nextState, err := ac.goWithContext(ctx, "", 'a')
	if err != nil {
		t.Errorf("goWithContext failed: %v", err)
	}
	if nextState != "" {
		t.Errorf("expected empty state for non-existent prefix, got %s", nextState)
	}

	if buildErr := ac._buildTrie("ab"); buildErr != nil {
		t.Fatal(buildErr)
	}

	nextState, err = ac.goWithContext(ctx, "", 'a')
	if err != nil {
		t.Errorf("goWithContext failed: %v", err)
	}
	if nextState != "a" {
		t.Errorf("expected state 'a', got %s", nextState)
	}
}

func TestFailWithContext(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()

	ac, err := Create(&AhoCorasickArgs{
		Addr:          mr.Addr(),
		Name:          "test-fail-context",
		SchemaVersion: SchemaV1,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ac.Close() }()

	ctx := context.Background()

	failState, err := ac.failWithContext(ctx, "abc")
	if err != nil {
		t.Errorf("failWithContext failed: %v", err)
	}
	if failState != "" {
		t.Errorf("expected empty fail state for non-existent prefix, got %s", failState)
	}
}

func TestOutputWithContext(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()

	ac, err := Create(&AhoCorasickArgs{
		Addr:          mr.Addr(),
		Name:          "test-output-context",
		SchemaVersion: SchemaV1,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ac.Close() }()

	ctx := context.Background()

	outputs, err := ac.outputWithContext(ctx, "nonexistent")
	if err != nil {
		t.Errorf("outputWithContext failed: %v", err)
	}
	if len(outputs) != 0 {
		t.Errorf("expected empty outputs, got %v", outputs)
	}
}

func TestBuildTrieWithContext(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()

	ac, err := Create(&AhoCorasickArgs{
		Addr:          mr.Addr(),
		Name:          "test-build-trie-context",
		SchemaVersion: SchemaV1,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ac.Close() }()

	ctx := context.Background()

	if buildErr := ac.buildTrieWithContext(ctx, "test"); buildErr != nil {
		t.Errorf("buildTrieWithContext failed: %v", buildErr)
	}

	pKey := prefixKey(ac.name)
	prefixes, err := ac.redisClient.ZRange(ctx, pKey, 0, -1).Result()
	if err != nil {
		t.Fatal(err)
	}

	expectedPrefixes := []string{"", "t", "te", "tes", "test"}
	for _, ep := range expectedPrefixes {
		found := false
		for _, p := range prefixes {
			if p == ep {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected prefix %q not found in trie", ep)
		}
	}
}

func TestPruneTrieWithContext(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()

	ac, err := Create(&AhoCorasickArgs{
		Addr:          mr.Addr(),
		Name:          "test-prune-trie-context",
		SchemaVersion: SchemaV1,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ac.Close() }()

	if _, addErr := ac.Add("test"); addErr != nil {
		t.Fatal(addErr)
	}

	ctx := context.Background()

	if pruneErr := ac.pruneTrieWithContext(ctx, "test"); pruneErr != nil {
		t.Errorf("pruneTrieWithContext failed: %v", pruneErr)
	}
}

func TestContextCancellation(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()

	ac, err := Create(&AhoCorasickArgs{
		Addr:          mr.Addr(),
		Name:          "test-context-cancel",
		SchemaVersion: SchemaV1,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ac.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = ac.goWithContext(ctx, "", 'a')
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled error, got: %v", err)
	}

	_, err = ac.failWithContext(ctx, "test")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled error, got: %v", err)
	}

	_, err = ac.outputWithContext(ctx, "test")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled error, got: %v", err)
	}
}

func TestContextTimeout(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()

	ac, err := Create(&AhoCorasickArgs{
		Addr:          mr.Addr(),
		Name:          "test-context-timeout",
		SchemaVersion: SchemaV1,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ac.Close() }()

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	_, err = ac.goWithContext(ctx, "", 'a')
	if err == nil {
		t.Error("expected error with expired context")
	}
}

func TestAppendMatchedIndexesWithContext(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()

	ac, err := Create(&AhoCorasickArgs{
		Addr:          mr.Addr(),
		Name:          "test-append-context",
		SchemaVersion: SchemaV1,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ac.Close() }()

	ctx := context.Background()
	matched := make(map[string][]int)
	outputs := []string{"he", "she"}

	ac.appendMatchedIndexesWithContext(ctx, matched, outputs, 5)

	if len(matched) != 2 {
		t.Errorf("expected 2 matched outputs, got %d", len(matched))
	}
	if matched["he"][0] != 3 {
		t.Errorf("expected he start index 3, got %d", matched["he"][0])
	}
	if matched["she"][0] != 2 {
		t.Errorf("expected she start index 2, got %d", matched["she"][0])
	}
}

func TestRemovePrefixAndSuffixWithContext(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()

	ac, err := Create(&AhoCorasickArgs{
		Addr:          mr.Addr(),
		Name:          "test-remove-prefix-context",
		SchemaVersion: SchemaV1,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ac.Close() }()

	if _, addErr := ac.Add("test"); addErr != nil {
		t.Fatal(addErr)
	}

	ctx := context.Background()

	if removeErr := ac.removePrefixAndSuffixWithContext(ctx, "test", "t", "t"); removeErr != nil {
		t.Errorf("removePrefixAndSuffixWithContext failed: %v", removeErr)
	}
}

func TestWrapperMethodsUseContext(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()

	ac, err := Create(&AhoCorasickArgs{
		Addr:          mr.Addr(),
		Name:          "test-wrappers",
		SchemaVersion: SchemaV1,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ac.Close() }()

	if buildErr := ac._buildTrie("test"); buildErr != nil {
		t.Errorf("_buildTrie wrapper failed: %v", buildErr)
	}

	nextState, err := ac._go("", 't')
	if err != nil {
		t.Errorf("_go wrapper failed: %v", err)
	}
	if nextState != "t" {
		t.Errorf("expected state 't', got %s", nextState)
	}

	failState, err := ac._fail("test")
	if err != nil {
		t.Errorf("_fail wrapper failed: %v", err)
	}
	_ = failState

	outputs, err := ac._output("test")
	if err != nil {
		t.Errorf("_output wrapper failed: %v", err)
	}
	_ = outputs

	matched := make(map[string][]int)
	ac.appendMatchedIndexes(matched, []string{"test"}, 4)
	if len(matched) != 1 {
		t.Errorf("expected 1 matched output, got %d", len(matched))
	}
}
