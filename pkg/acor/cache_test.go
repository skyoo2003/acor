package acor

import (
	"sync"
	"testing"
)

func TestTrieCache_Invalidate(t *testing.T) {
	cache := &trieCache{
		prefixes: []string{"a", "ab"},
		outputs:  map[string][]string{"ab": {"ab"}},
		valid:    true,
	}

	cache.invalidate()

	if cache.valid {
		t.Error("expected cache to be invalid after invalidate()")
	}
}

func TestTrieCache_SetAndGet(t *testing.T) {
	cache := &trieCache{}

	prefixes := []string{"a", "ab", "abc"}
	outputs := map[string][]string{
		"ab":  {"ab"},
		"abc": {"abc"},
	}

	cache.set(prefixes, outputs)

	gotPrefixes, gotOutputs, valid := cache.get()

	if !valid {
		t.Error("expected cache to be valid after set()")
	}
	if len(gotPrefixes) != 3 {
		t.Errorf("expected 3 prefixes, got %d", len(gotPrefixes))
	}
	if len(gotOutputs) != 2 {
		t.Errorf("expected 2 outputs, got %d", len(gotOutputs))
	}
}

func TestTrieCache_GetAfterInvalidate(t *testing.T) {
	cache := &trieCache{
		prefixes: []string{"a"},
		outputs:  map[string][]string{"a": {"a"}},
		valid:    true,
	}

	cache.invalidate()

	_, _, valid := cache.get()

	if valid {
		t.Error("expected valid=false after invalidate")
	}
}

func TestTrieCache_ConcurrentAccess(t *testing.T) {
	cache := &trieCache{}

	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(3)

		go func() {
			defer wg.Done()
			cache.set([]string{"a"}, map[string][]string{"a": {"a"}})
		}()

		go func() {
			defer wg.Done()
			cache.invalidate()
		}()

		go func() {
			defer wg.Done()
			cache.get()
		}()
	}

	wg.Wait()
}
