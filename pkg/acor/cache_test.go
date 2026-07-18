// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestTrieCache_Invalidate(t *testing.T) {
	cache := &trieCache{valid: true}

	cache.invalidate()

	if cache.valid {
		t.Error("expected cache to be invalid after invalidate()")
	}
}

func TestTrieCache_SetBuildsEngine(t *testing.T) {
	cache := &trieCache{}

	cache.set(map[string][]string{
		"ab":  {"ab"},
		"abc": {"abc"},
	})

	engine, valid := cache.getEngine()
	if !valid {
		t.Error("expected cache to be valid after set()")
	}
	if got := engine.Find("abc"); len(got) != 2 {
		t.Errorf("expected engine to match ab and abc in \"abc\", got %v", got)
	}
}

func TestTrieCache_SetOverwritesPrevious(t *testing.T) {
	cache := &trieCache{}

	cache.set(map[string][]string{"old": {"old"}})
	engine, valid := cache.getEngine()
	if !valid {
		t.Fatal("expected cache to be valid after first set()")
	}
	if got := engine.Find("old"); len(got) != 1 || got[0] != "old" {
		t.Errorf("expected engine to match [old], got %v", got)
	}

	// Set "new" data — should overwrite the previous engine.
	cache.set(map[string][]string{"new": {"new"}})
	engine, _ = cache.getEngine()
	if got := engine.Find("new"); len(got) != 1 || got[0] != "new" {
		t.Errorf("expected engine to match [new], got %v", got)
	}
	if got := engine.Find("old"); len(got) != 0 {
		t.Errorf("expected old keyword gone after overwrite, got %v", got)
	}
}

func TestTrieCache_GetAfterInvalidate(t *testing.T) {
	cache := &trieCache{valid: true}

	cache.invalidate()

	if _, valid := cache.getEngine(); valid {
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
			cache.set(map[string][]string{"a": {"a"}})
		}()

		go func() {
			defer wg.Done()
			cache.invalidate()
		}()

		go func() {
			defer wg.Done()
			cache.getEngine()
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

func TestSkipSelfCheck_RejectsExpiredID(t *testing.T) {
	c := &trieCache{}
	expiredID := "expired-msg" //nolint:goconst // test value
	c.pendingSelfInvalidations.Store(expiredID, time.Now().Add(-2*pendingSelfInvalidationTTL))

	if skipSelfCheck(c, expiredID) {
		t.Error("expected skipSelfCheck to return false for expired ID")
	}
}

func TestCleanupExpiredSelfInvalidations(t *testing.T) {
	c := &trieCache{}
	now := time.Now()
	freshID := "fresh-msg"
	expiredID := "expired-msg" //nolint:goconst // test value

	c.pendingSelfInvalidations.Store(freshID, now)
	c.pendingSelfInvalidations.Store(expiredID, now.Add(-pendingSelfInvalidationTTL).Add(-time.Second))

	cleanupExpiredSelfInvalidations(c)

	if skipSelfCheck(c, expiredID) {
		t.Errorf("expected expired self-invalidation %q to be pruned by cleanupExpiredSelfInvalidations", expiredID)
	}
	if !skipSelfCheck(c, freshID) {
		t.Errorf("expected fresh self-invalidation %q to remain consumable after cleanupExpiredSelfInvalidations", freshID)
	}
	if skipSelfCheck(c, freshID) {
		t.Errorf("expected fresh self-invalidation %q to be single-consumption", freshID)
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
