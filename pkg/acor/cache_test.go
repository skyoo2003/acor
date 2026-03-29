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

func TestTrieCache_SetOverwritesPrevious(t *testing.T) {
	cache := &trieCache{}

	// Set "old" data
	cache.set([]string{"old"}, map[string][]string{"old": {"old"}})

	prefixes, _, valid := cache.get()
	if !valid {
		t.Fatal("expected cache to be valid after first set()")
	}
	if len(prefixes) != 1 || prefixes[0] != "old" {
		t.Errorf("expected prefixes [old], got %v", prefixes)
	}

	// Set "new" data — should overwrite
	cache.set([]string{"new"}, map[string][]string{"new": {"new"}})

	prefixes, outputs, valid := cache.get()
	if !valid {
		t.Fatal("expected cache to be valid after second set()")
	}
	if len(prefixes) != 1 || prefixes[0] != "new" { //nolint:goconst // test value
		t.Errorf("expected prefixes [new], got %v", prefixes)
	}
	if _, exists := outputs["old"]; exists {
		t.Error("expected old key to be gone from outputs after overwrite")
	}
	if len(outputs["new"]) != 1 || outputs["new"][0] != "new" {
		t.Errorf("expected outputs[new]=[new], got %v", outputs["new"])
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
