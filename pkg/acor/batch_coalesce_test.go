// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"testing"
	"time"

	miniredis "github.com/alicebob/miniredis/v2"
	redis "github.com/redis/go-redis/v9"
)

func createPresetAC(t *testing.T) (*AhoCorasick, *miniredis.Miniredis) {
	t.Helper()
	mr := createTestRedisServer(t)
	ac, err := Create(&AhoCorasickArgs{
		Addr:   mr.Addr(),
		Name:   "test",
		Preset: PresetBalanced,
	})
	if err != nil {
		mr.Close()
		t.Fatal(err)
	}
	return ac, mr
}

// countInvalidations subscribes to the collection's invalidation channel and
// returns how many messages arrive within a short drain window. One message per
// commitBatch is the observable proof that a batch coalesces to a single rebuild.
func countInvalidations(t *testing.T, mr *miniredis.Miniredis, name string, during func()) int {
	t.Helper()
	ctx := context.Background()
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	sub := client.Subscribe(ctx, invalidateChannelPrefix+name)
	defer sub.Close()
	if _, err := sub.Receive(ctx); err != nil { // confirm subscription is live
		t.Fatalf("subscribe: %v", err)
	}

	during()

	count := 0
	for {
		msg, err := sub.ReceiveTimeout(ctx, 300*time.Millisecond)
		if err != nil {
			break // timeout: no more messages
		}
		if _, ok := msg.(*redis.Message); ok {
			count++
		}
	}
	return count
}

func TestAddMany_CoalescesRebuild(t *testing.T) {
	ac, mr := createPresetAC(t)
	defer mr.Close()
	defer ac.Close()

	keywords := []string{"alpha", "beta", "gamma", "delta", "epsilon", "zeta", "eta", "theta"}

	var result *BatchResult
	n := countInvalidations(t, mr, "test", func() {
		var err error
		result, err = ac.AddMany(keywords, nil)
		if err != nil {
			t.Fatalf("AddMany: %v", err)
		}
	})

	if len(result.Added) != len(keywords) {
		t.Fatalf("added %d, want %d", len(result.Added), len(keywords))
	}
	// The whole batch must publish exactly one invalidation, not one per keyword.
	if n != 1 {
		t.Errorf("AddMany of %d keywords published %d invalidations, want 1 (coalesced)", len(keywords), n)
	}

	// Correctness: every keyword is findable after the coalesced commit.
	for _, kw := range keywords {
		if ok, err := ac.Contains(kw); err != nil || !ok {
			t.Errorf("Contains(%q) = %v, %v; want true", kw, ok, err)
		}
	}
	matches, err := ac.FindMatches("alpha beta gamma", nil)
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]bool)
	for _, m := range matches {
		seen[m.Keyword] = true
	}
	for _, kw := range []string{"alpha", "beta", "gamma"} {
		if !seen[kw] {
			t.Errorf("FindMatches after AddMany missing %q (got %v)", kw, matches)
		}
	}
}

func TestRemoveMany_CoalescesRebuild(t *testing.T) {
	ac, mr := createPresetAC(t)
	defer mr.Close()
	defer ac.Close()

	keywords := []string{"alpha", "beta", "gamma", "delta", "epsilon"}
	if _, err := ac.AddMany(keywords, nil); err != nil {
		t.Fatal(err)
	}

	toRemove := []string{"beta", "delta"}
	var result *BatchResult
	n := countInvalidations(t, mr, "test", func() {
		var err error
		result, err = ac.RemoveMany(toRemove)
		if err != nil {
			t.Fatalf("RemoveMany: %v", err)
		}
	})

	if len(result.Removed) != len(toRemove) {
		t.Fatalf("removed %d, want %d", len(result.Removed), len(toRemove))
	}
	if n != 1 {
		t.Errorf("RemoveMany published %d invalidations, want 1 (coalesced)", n)
	}

	// beta/delta gone; the rest remain.
	for kw, want := range map[string]bool{
		"beta": false, "delta": false, "alpha": true, "gamma": true, "epsilon": true,
	} {
		ok, err := ac.Contains(kw)
		if err != nil {
			t.Fatal(err)
		}
		if ok != want {
			t.Errorf("after RemoveMany Contains(%q) = %v, want %v", kw, ok, want)
		}
	}
}

// TestRemoveMany_NoOpDoesNotCommit guards against the reload storm: removing
// keywords that are not present must not publish an invalidation or report them
// as removed.
func TestRemoveMany_NoOpDoesNotCommit(t *testing.T) {
	ac, mr := createPresetAC(t)
	defer mr.Close()
	defer ac.Close()

	if _, err := ac.AddMany([]string{"alpha", "beta"}, nil); err != nil {
		t.Fatal(err)
	}

	var result *BatchResult
	n := countInvalidations(t, mr, "test", func() {
		var err error
		result, err = ac.RemoveMany([]string{"absent1", "absent2"})
		if err != nil {
			t.Fatalf("RemoveMany: %v", err)
		}
	})

	if n != 0 {
		t.Errorf("no-op RemoveMany published %d invalidations, want 0", n)
	}
	if len(result.Removed) != 0 {
		t.Errorf("no-op RemoveMany reported %v as removed, want none", result.Removed)
	}
	if len(result.Skipped) != 2 {
		t.Errorf("no-op RemoveMany skipped %d, want 2 (%v)", len(result.Skipped), result.Skipped)
	}
}

func TestAddMany_Transactional_Coalesces(t *testing.T) {
	ac, mr := createPresetAC(t)
	defer mr.Close()
	defer ac.Close()

	keywords := []string{"one", "two", "three", "four"}
	n := countInvalidations(t, mr, "test", func() {
		if _, err := ac.AddMany(keywords, &BatchOptions{Mode: BatchModeTransactional}); err != nil {
			t.Fatalf("transactional AddMany: %v", err)
		}
	})
	if n != 1 {
		t.Errorf("transactional AddMany published %d invalidations, want 1", n)
	}
	for _, kw := range keywords {
		if ok, _ := ac.Contains(kw); !ok {
			t.Errorf("Contains(%q) = false after transactional AddMany", kw)
		}
	}
}
