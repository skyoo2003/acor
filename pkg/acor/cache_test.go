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

func TestSelfSkipClaim_AcceptsKnownID(t *testing.T) {
	c := &trieCache{}
	c.selfSkip.add("msg-1")

	if !c.selfSkip.claim("msg-1") {
		t.Error("expected claim to return true for known ID")
	}
}

func TestSelfSkipClaim_RejectsUnknownID(t *testing.T) {
	c := &trieCache{}

	if c.selfSkip.claim("unknown") {
		t.Error("expected claim to return false for unknown ID")
	}
}

func TestSelfSkipClaim_RemovesOnMatch(t *testing.T) {
	c := &trieCache{}
	c.selfSkip.add("msg-1")

	c.selfSkip.claim("msg-1")

	if c.selfSkip.claim("msg-1") {
		t.Error("expected second claim to return false (ID was consumed)")
	}
}

func TestSelfSkipClaim_DoesNotLeakAcrossIDs(t *testing.T) {
	c := &trieCache{}
	c.selfSkip.add("msg-1")

	if c.selfSkip.claim("msg-2") {
		t.Error("msg-2 should not match msg-1's pending entry")
	}
	if !c.selfSkip.claim("msg-1") {
		t.Error("msg-1 should still be available after msg-2 check failed")
	}
}

func TestSelfSkipForget(t *testing.T) {
	c := &trieCache{}
	c.selfSkip.add("msg-1")
	c.selfSkip.forget("msg-1")

	if c.selfSkip.claim("msg-1") {
		t.Error("expected claim to return false after forget")
	}
}

func TestSelfSkipClaim_RejectsExpiredID(t *testing.T) {
	c := &trieCache{}
	expiredID := "expired-msg" //nolint:goconst // test value
	c.selfSkip.ids.Store(expiredID, time.Now().Add(-2*selfSkipTTL))

	if c.selfSkip.claim(expiredID) {
		t.Error("expected claim to return false for expired ID")
	}
}

func TestSelfSkipSweep(t *testing.T) {
	c := &trieCache{}
	now := time.Now()
	freshID := "fresh-msg"
	expiredID := "expired-msg" //nolint:goconst // test value

	c.selfSkip.ids.Store(freshID, now)
	c.selfSkip.ids.Store(expiredID, now.Add(-selfSkipTTL).Add(-time.Second))

	c.selfSkip.sweep()

	if c.selfSkip.claim(expiredID) {
		t.Errorf("expected expired self-invalidation %q to be pruned by sweep", expiredID)
	}
	if !c.selfSkip.claim(freshID) {
		t.Errorf("expected fresh self-invalidation %q to remain consumable after sweep", freshID)
	}
	if c.selfSkip.claim(freshID) {
		t.Errorf("expected fresh self-invalidation %q to be single-consumption", freshID)
	}
}

func TestSelfSkipClaim_ConcurrentAccess(t *testing.T) {
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
			c.selfSkip.add(msgID)
		}(id)

		for j := 0; j < checksPerID; j++ {
			wg.Add(1)
			go func(msgID string) {
				defer wg.Done()
				if c.selfSkip.claim(msgID) {
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
