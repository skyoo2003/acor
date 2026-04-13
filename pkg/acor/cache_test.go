package acor

import (
	"fmt"
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

func TestSkipSelfCheck_AcceptsKnownID(t *testing.T) {
	c := &trieCache{}
	skipSelfSet(c, "msg-1")

	if !skipSelfCheck(c, "msg-1") {
		t.Error("expected skipSelfCheck to return true for known ID")
	}
}

func TestSkipSelfCheck_RejectsUnknownID(t *testing.T) {
	c := &trieCache{}

	if skipSelfCheck(c, "unknown") {
		t.Error("expected skipSelfCheck to return false for unknown ID")
	}
}

func TestSkipSelfCheck_RemovesOnMatch(t *testing.T) {
	c := &trieCache{}
	skipSelfSet(c, "msg-1")

	skipSelfCheck(c, "msg-1")

	if skipSelfCheck(c, "msg-1") {
		t.Error("expected second skipSelfCheck to return false (ID was consumed)")
	}
}

func TestSkipSelfCheck_DoesNotLeakAcrossIDs(t *testing.T) {
	c := &trieCache{}
	skipSelfSet(c, "msg-1")

	if skipSelfCheck(c, "msg-2") {
		t.Error("msg-2 should not match msg-1's pending entry")
	}
	if !skipSelfCheck(c, "msg-1") {
		t.Error("msg-1 should still be available after msg-2 check failed")
	}
}

func TestSkipSelfClear(t *testing.T) {
	c := &trieCache{}
	skipSelfSet(c, "msg-1")
	skipSelfClear(c, "msg-1")

	if skipSelfCheck(c, "msg-1") {
		t.Error("expected skipSelfCheck to return false after skipSelfClear")
	}
}

func TestSkipSelfCheck_ConcurrentAccess(t *testing.T) {
	c := &trieCache{}
	var wg sync.WaitGroup

	const numIDs = 100
	const checksPerID = 10

	var mu sync.Mutex
	truePerID := make(map[string]int)
	totalTrue := 0

	for i := 0; i < numIDs; i++ {
		id := fmt.Sprintf("msg-%d", i)

		wg.Add(1)
		go func(msgID string) {
			defer wg.Done()
			skipSelfSet(c, msgID)
		}(id)

		for j := 0; j < checksPerID; j++ {
			wg.Add(1)
			go func(msgID string) {
				defer wg.Done()
				if skipSelfCheck(c, msgID) {
					mu.Lock()
					truePerID[msgID]++
					totalTrue++

					if truePerID[msgID] > 1 {
						t.Errorf("skipSelfCheck returned true more than once for ID %q", msgID)
					}
					if totalTrue > numIDs {
						t.Errorf("total true results %d exceeded number of skipSelfSet calls %d", totalTrue, numIDs)
					}

					mu.Unlock()
				}
			}(id)
		}
	}

	wg.Wait()
}
