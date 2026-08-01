// SPDX-License-Identifier: Apache-2.0

package acor //nolint:errcheck // claim pinning focuses on round-trip counts

import (
	"fmt"
	"testing"
)

// Each test here pins one round-trip claim that ACOR publishes. When a test and
// the docs disagree, the docs are wrong: these counts are measured, the prose
// was asserted. Update README.md and docs/content/reference/benchmarks.md to
// match, never the other way around.
//
// Counts are structural, so they must be identical on miniredis and on a real
// server:
//
//	go test -run RTT ./pkg/acor
//	ACOR_INTEGRATION_ADDR=localhost:6379 go test -run RTT ./pkg/acor

// TestRTTCounterCountsPipelineAsOneRoundTrip guards the counter itself. A
// pipeline carrying N commands is one round trip; if this ever reports N, every
// published number is inflated and the evidence is worthless.
func TestRTTCounterCountsPipelineAsOneRoundTrip(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer ac.Close()

	c := countRTT(t, ac)
	c.reset()

	err := ac.storage.TxPipelined(ac.ctx, func(pipe Pipeliner) error {
		pipe.HSet(ac.ctx, "rtt-guard", "a", "1")
		pipe.HSet(ac.ctx, "rtt-guard", "b", "2")
		pipe.HSet(ac.ctx, "rtt-guard", "c", "3")
		return nil
	})
	if err != nil {
		t.Fatalf("TxPipelined() error: %v", err)
	}

	if got := c.count(); got != 1 {
		t.Fatalf("TxPipelined with 3 queued commands = %d round trips, want 1", got)
	}
}

// TestRTTCounterObservesRealWork guards against the opposite failure: a counter
// that reports 0 for everything would make every "0 RTT" claim pass while
// proving nothing.
func TestRTTCounterObservesRealWork(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer ac.Close()

	c := countRTT(t, ac)
	c.reset()

	if _, err := ac.Add("hello"); err != nil {
		t.Fatalf("Add(%q) error: %v", "hello", err)
	}

	if got := c.count(); got == 0 {
		t.Fatal("Add() = 0 round trips; the counter is not observing the storage path")
	}
}

// TestRTTV2Find pins V2 Find at one round trip.
//
// README.md and docs/content/reference/schema-v2.md claimed "3 RTT (fixed)"
// until this test measured it. fetchTrieData (v2_ops.go:177-180) pipelines both
// HGetAll calls, and a pipeline is one round trip. The published claim
// understated ACOR by 3x; the docs were corrected to 1.
func TestRTTV2Find(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer ac.Close()

	for _, kw := range []string{"he", "her", "him"} {
		if _, err := ac.Add(kw); err != nil {
			t.Fatalf("Add(%q) error: %v", kw, err)
		}
	}

	c := countRTT(t, ac)
	c.reset()

	if _, err := ac.Find("he is him"); err != nil {
		t.Fatalf("Find() error: %v", err)
	}

	if got := c.count(); got != 1 {
		t.Fatalf("V2 Find() = %d round trips, want 1 (published in docs/content/reference/benchmarks.md)", got)
	}
}

// TestRTTV2FindIsFixed pins the load-bearing half of the claim: the count does
// not grow with the dictionary. A fixed cost is the whole architectural
// argument for V2, and it is what a 50x-larger dictionary would expose.
func TestRTTV2FindIsFixed(t *testing.T) {
	small, mrSmall := createAhoCorasick(t)
	defer mrSmall.Close()
	defer small.Close()

	large, mrLarge := createAhoCorasick(t)
	defer mrLarge.Close()
	defer large.Close()

	for i := range 10 {
		if _, err := small.Add(fmt.Sprintf("keyword%d", i)); err != nil {
			t.Fatalf("small.Add() error: %v", err)
		}
	}
	for i := range 500 {
		if _, err := large.Add(fmt.Sprintf("keyword%d", i)); err != nil {
			t.Fatalf("large.Add() error: %v", err)
		}
	}

	cSmall := countRTT(t, small)
	cLarge := countRTT(t, large)
	cSmall.reset()
	cLarge.reset()

	if _, err := small.Find("keyword5 here"); err != nil {
		t.Fatalf("small.Find() error: %v", err)
	}
	if _, err := large.Find("keyword5 here"); err != nil {
		t.Fatalf("large.Find() error: %v", err)
	}

	if cSmall.count() != cLarge.count() {
		t.Fatalf("V2 Find() cost varies with dictionary size: 10 keywords = %d RTT, 500 keywords = %d RTT; the 'fixed' claim is false",
			cSmall.count(), cLarge.count())
	}
}

// TestRTTV2Add pins the "Add(): 2-3 RTT" claim in README.md and
// docs/content/reference/schema-v2.md.
func TestRTTV2Add(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer ac.Close()

	if _, err := ac.Add("seed"); err != nil {
		t.Fatalf("Add(seed) error: %v", err)
	}

	c := countRTT(t, ac)
	c.reset()

	if _, err := ac.Add("hello"); err != nil {
		t.Fatalf("Add(%q) error: %v", "hello", err)
	}

	if got := c.count(); got < 2 || got > 3 {
		t.Fatalf("V2 Add() = %d round trips, want 2-3 (claim in README.md and docs/content/reference/schema-v2.md)", got)
	}
}

// TestRTTCachedFind pins the "Subsequent Find() uses local cache (0 RTT)" claim
// in README.md.
func TestRTTCachedFind(t *testing.T) {
	mr := createTestRedisServer(t)
	defer mr.Close()

	ac, err := Create(&AhoCorasickArgs{Addr: mr.Addr(), Name: "rtt-cache", EnableCache: true})
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	defer ac.Close()

	if _, err := ac.Add("hello"); err != nil {
		t.Fatalf("Add() error: %v", err)
	}

	c := countRTT(t, ac)

	// First Find populates the cache and must touch Redis.
	c.reset()
	if _, err := ac.Find("hello world"); err != nil {
		t.Fatalf("cold Find() error: %v", err)
	}
	if c.count() == 0 {
		t.Fatal("cold Find() = 0 round trips; the cache was already warm, so this proves nothing")
	}

	// Subsequent reads are served locally.
	c.reset()
	if _, err := ac.Find("another text"); err != nil {
		t.Fatalf("warm Find() error: %v", err)
	}
	if got := c.count(); got != 0 {
		t.Fatalf("warm cached Find() = %d round trips, want 0 (claim in README.md)", got)
	}
}

// TestRTTPresetFind pins the "0 RTT on hot path" claim in README.md and
// docs/content/guides/redis-backed-engine.md for every preset.
func TestRTTPresetFind(t *testing.T) {
	for _, preset := range allPresets() {
		t.Run(preset.String(), func(t *testing.T) {
			mr := createTestRedisServer(t)
			defer mr.Close()

			ac, err := Create(&AhoCorasickArgs{Addr: mr.Addr(), Name: "rtt-preset", Preset: preset})
			if err != nil {
				t.Fatalf("Create(%s) error: %v", preset, err)
			}
			defer ac.Close()

			if _, err := ac.Add("hello"); err != nil {
				t.Fatalf("Add() error: %v", err)
			}

			c := countRTT(t, ac)
			c.reset()

			if _, err := ac.Find("hello world"); err != nil {
				t.Fatalf("Find() error: %v", err)
			}

			if got := c.count(); got != 0 {
				t.Fatalf("preset %s Find() = %d round trips, want 0 (claim in README.md)", preset, got)
			}
		})
	}
}

// TestRTTV1FindIsAlsoOneRoundTrip corrects the record on V1.
//
// README.md claimed V1 Find costs "O(N x 3-5) RTT". It does not: loadEngine
// (v1_ops.go:214) issues a single SMembers and matches locally, so V1 Find is
// one round trip at any dictionary size, exactly like V2.
//
// V1's real disadvantage on reads is payload, not round trips — that one
// SMembers transfers the entire keyword set on every call, where V2 transfers a
// prebuilt trie. Round-trip counting cannot see that difference, which is
// precisely why the timing benchmarks exist alongside these tests. Publishing a
// round-trip advantage that does not exist would be the same mistake the old
// README made.
func TestRTTV1FindIsAlsoOneRoundTrip(t *testing.T) {
	small, mrSmall := createAhoCorasickV1(t)
	defer mrSmall.Close()
	defer small.Close()

	large, mrLarge := createAhoCorasickV1(t)
	defer mrLarge.Close()
	defer large.Close()

	for i := range 5 {
		if _, err := small.Add(fmt.Sprintf("keyword%d", i)); err != nil {
			t.Fatalf("small.Add() error: %v", err)
		}
	}
	for i := range 100 {
		if _, err := large.Add(fmt.Sprintf("keyword%d", i)); err != nil {
			t.Fatalf("large.Add() error: %v", err)
		}
	}

	text := "keyword3 and keyword42 appear here"

	cSmall := countRTT(t, small)
	cSmall.reset()
	if _, err := small.Find(text); err != nil {
		t.Fatalf("small.Find() error: %v", err)
	}

	cLarge := countRTT(t, large)
	cLarge.reset()
	if _, err := large.Find(text); err != nil {
		t.Fatalf("large.Find() error: %v", err)
	}

	if cSmall.count() != 1 || cLarge.count() != 1 {
		t.Fatalf("V1 Find() = %d RTT (5 keywords), %d RTT (100 keywords), want 1 for both (published in docs/content/reference/benchmarks.md)",
			cSmall.count(), cLarge.count())
	}
}

// TestRTTV1AddGrowsWithKeywordLength pins where V1's round-trip cost actually
// lives. Add walks the trie node by node (trie.go), so cost scales with the
// length of the keyword being added — not with the dictionary, which is what a
// first reading of "O(M x 3-10) RTT" in README.md invites you to assume. V2
// stays at 2 regardless.
//
// This is the real V1-vs-V2 round-trip story. The README told it about Find,
// where it is false, and buried it on Add, where it is true.
func TestRTTV1AddGrowsWithKeywordLength(t *testing.T) {
	ac, mr := createAhoCorasickV1(t)
	defer mr.Close()
	defer ac.Close()

	measure := func(keyword string) int {
		c := countRTT(t, ac)
		c.reset()
		if _, err := ac.Add(keyword); err != nil {
			t.Fatalf("Add(%q) error: %v", keyword, err)
		}
		return c.count()
	}

	short := measure("abcde")
	long := measure("abcdefghijklmnopqrstuvwxyz")

	if long <= short {
		t.Fatalf("V1 Add() did not grow with keyword length: 5 chars = %d RTT, 26 chars = %d RTT", short, long)
	}
	t.Logf("V1 Add: %d RTT for a 5-char keyword, %d RTT for a 26-char keyword", short, long)
}

// TestRTTV2AddIsFlatInKeywordLength is the other half of that comparison: V2
// pays the same 2 round trips whatever the keyword length. Without this, the V1
// growth result above has nothing to be growth *relative to*.
func TestRTTV2AddIsFlatInKeywordLength(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer ac.Close()

	measure := func(keyword string) int {
		c := countRTT(t, ac)
		c.reset()
		if _, err := ac.Add(keyword); err != nil {
			t.Fatalf("Add(%q) error: %v", keyword, err)
		}
		return c.count()
	}

	short := measure("abcde")
	long := measure("abcdefghijklmnopqrstuvwxyz")

	if short != long {
		t.Fatalf("V2 Add() varies with keyword length: 5 chars = %d RTT, 26 chars = %d RTT; the flat-cost claim is false", short, long)
	}
	t.Logf("V2 Add: %d RTT at both 5 and 26 characters", short)
}

// TestRTTPublishedTable prints the measured counts in the shape published on
// docs/content/reference/benchmarks.md. Run with -v when updating that page so
// the numbers are transcribed rather than recalled.
func TestRTTPublishedTable(t *testing.T) {
	measure := func(name string, build func(t *testing.T) *AhoCorasick, warm, op func(ac *AhoCorasick) error) {
		t.Run(name, func(t *testing.T) {
			ac := build(t)
			if warm != nil {
				if err := warm(ac); err != nil {
					t.Fatalf("warm-up error: %v", err)
				}
			}
			c := countRTT(t, ac)
			c.reset()
			if err := op(ac); err != nil {
				t.Fatalf("operation error: %v", err)
			}
			t.Logf("RTT | %-28s | %d", name, c.count())
		})
	}

	newV2 := func(t *testing.T) *AhoCorasick {
		t.Helper()
		ac, mr := createAhoCorasick(t)
		t.Cleanup(func() { ac.Close(); mr.Close() })
		return ac
	}
	newV1 := func(t *testing.T) *AhoCorasick {
		t.Helper()
		ac, mr := createAhoCorasickV1(t)
		t.Cleanup(func() { ac.Close(); mr.Close() })
		return ac
	}

	addSome := func(ac *AhoCorasick) error {
		for _, kw := range []string{"he", "her", "him"} {
			if _, err := ac.Add(kw); err != nil {
				return err
			}
		}
		return nil
	}
	find := func(ac *AhoCorasick) error { _, err := ac.Find("he is him"); return err }
	add := func(ac *AhoCorasick) error { _, err := ac.Add("new-keyword"); return err }

	measure("V2 Find", newV2, addSome, find)
	measure("V2 Add", newV2, addSome, add)
	measure("V1 Find (3 keywords)", newV1, addSome, find)
	measure("V1 Add", newV1, addSome, add)
}
