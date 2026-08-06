// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
)

// assertStats compares the fields that are deterministic. RebuildDuration and
// LastInvalidationLag are wall-clock and get their own targeted assertions.
func assertStats(t *testing.T, got CacheStats, hits, misses, rebuilds uint64) {
	t.Helper()
	if got.Hits != hits || got.Misses != misses || got.Rebuilds != rebuilds {
		t.Errorf("stats = {hits:%d misses:%d rebuilds:%d}, want {hits:%d misses:%d rebuilds:%d}",
			got.Hits, got.Misses, got.Rebuilds, hits, misses, rebuilds)
	}
}

// TestCacheStatsUncachedV2 covers the mode most likely to look broken: without
// EnableCache or Preset there is no cache to hit, yet the memo still skips the
// rebuild, so the counters must move rather than sit at zero.
func TestCacheStatsUncachedV2(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()

	if _, err := ac.Add("he"); err != nil {
		t.Fatal(err)
	}
	if s := ac.CacheStats(); s.Hits != 0 || s.Misses != 0 {
		t.Fatalf("a write must not count as a read: %+v", s)
	}

	if _, err := ac.Find("she"); err != nil {
		t.Fatal(err)
	}
	assertStats(t, ac.CacheStats(), 0, 1, 1)

	// Same keywords, so the raw payload digests identically and the automaton is
	// reused even though the freshness read happened again.
	if _, err := ac.Find("she"); err != nil {
		t.Fatal(err)
	}
	assertStats(t, ac.CacheStats(), 1, 1, 1)

	if got := ac.CacheStats().RebuildDuration; got <= 0 {
		t.Errorf("RebuildDuration = %v, want > 0 after a rebuild", got)
	}
}

// TestCacheStatsUncachedV2Rebuilds proves the digest is doing real work: a changed
// keyword set must miss, not hit.
func TestCacheStatsUncachedV2Rebuilds(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()

	if _, err := ac.Add("he"); err != nil {
		t.Fatal(err)
	}
	if _, err := ac.Find("she"); err != nil {
		t.Fatal(err)
	}
	if _, err := ac.Add("her"); err != nil {
		t.Fatal(err)
	}
	if _, err := ac.Find("she"); err != nil {
		t.Fatal(err)
	}
	assertStats(t, ac.CacheStats(), 0, 2, 2)
}

func TestCacheStatsCachedV2(t *testing.T) {
	mr := createTestRedisServer(t)
	defer mr.Close()

	ac, err := Create(&AhoCorasickArgs{Addr: mr.Addr(), Name: "stats-cached", EnableCache: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ac.Close() }()

	if _, err := ac.Add("he"); err != nil {
		t.Fatal(err)
	}
	if _, err := ac.Find("she"); err != nil {
		t.Fatal(err)
	}
	if _, err := ac.Find("she"); err != nil {
		t.Fatal(err)
	}
	assertStats(t, ac.CacheStats(), 1, 1, 1)

	// Stand in for a peer's write. Going through ac.Add would invalidate too, but it
	// would also publish and rebuild through the write path, which is not what this
	// assertion is about.
	ac.cache.invalidate()
	if _, err := ac.Find("she"); err != nil {
		t.Fatal(err)
	}
	assertStats(t, ac.CacheStats(), 1, 2, 2)
}

func TestCacheStatsPreset(t *testing.T) {
	mr := createTestRedisServer(t)
	defer mr.Close()

	ac, err := Create(&AhoCorasickArgs{Addr: mr.Addr(), Name: "stats-preset", Preset: PresetBalanced})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ac.Close() }()

	// Preset builds during Create, before any read: one rebuild, no reads.
	assertStats(t, ac.CacheStats(), 0, 0, 1)

	if _, err := ac.Find("she"); err != nil {
		t.Fatal(err)
	}
	assertStats(t, ac.CacheStats(), 1, 0, 1)

	rb, ok := ac.ops.(*redisBackedAC)
	if !ok {
		t.Fatalf("expected *redisBackedAC, got %T", ac.ops)
	}
	rb.markStale()
	if _, err := ac.Find("she"); err != nil {
		t.Fatal(err)
	}
	assertStats(t, ac.CacheStats(), 1, 1, 2)

	// A single-keyword write rebuilds directly rather than going through the reload
	// path, and a flush does too. Both move Rebuilds without touching either read
	// counter — miss these and the reported mean rebuild cost drifts high on any
	// instance that writes.
	if _, err := ac.Add("he"); err != nil {
		t.Fatal(err)
	}
	assertStats(t, ac.CacheStats(), 1, 1, 3)

	if err := ac.Flush(); err != nil {
		t.Fatal(err)
	}
	assertStats(t, ac.CacheStats(), 1, 1, 4)
}

// TestCacheStatsInvalidationLagEndToEnd is the only test that proves lag survives the
// real publish/subscribe round trip. The unit test below pins the parsing; this pins
// that the payload a writer actually publishes is the one the reader can parse, which
// is the coupling that would break silently if the ID format ever changed.
func TestCacheStatsInvalidationLagEndToEnd(t *testing.T) {
	mr := createTestRedisServer(t)
	defer mr.Close()

	newInstance := func(name string) *AhoCorasick {
		ac, err := Create(&AhoCorasickArgs{Addr: mr.Addr(), Name: name, EnableCache: true})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = ac.Close() })
		return ac
	}

	reader := newInstance("stats-lag")
	writer := newInstance("stats-lag")

	if _, err := writer.Add("he"); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		if reader.CacheStats().LastInvalidationLag > 0 {
			// No upper bound asserted: the value includes the gap between two clock
			// reads, and pinning a ceiling would make this flaky on a loaded machine.
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("reader never recorded an invalidation lag from the writer's publish")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestInvalidationLag(t *testing.T) {
	published := time.Now().Add(-50 * time.Millisecond)
	tests := []struct {
		name    string
		payload string
		wantOK  bool
	}{
		{"real payload", fmt.Sprintf("coll:%d:a1b2c3d4", published.UnixNano()), true},
		{"no id at all", "coll", false},
		{"id without random suffix", "coll:12345", false},
		{"non-numeric timestamp", "coll:notanumber:a1b2", false},
		{"empty", "", false},
		// Screened for the same reason isSelfEcho screens it: a payload naming another
		// collection is not this instance's peer traffic, so it must not move the gauge.
		{"other collection", fmt.Sprintf("other:%d:a1b2c3d4", published.UnixNano()), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lag, ok := invalidationLag(tc.payload, "coll")
			if ok != tc.wantOK {
				t.Fatalf("invalidationLag(%q) ok = %v, want %v", tc.payload, ok, tc.wantOK)
			}
			if !ok {
				// A corrupt payload must not read as instant delivery.
				if lag != 0 {
					t.Errorf("lag = %v on unparseable payload, want 0", lag)
				}
				return
			}
			if lag < 50*time.Millisecond {
				t.Errorf("lag = %v, want at least the 50ms the payload was backdated by", lag)
			}
		})
	}
}

// TestCacheStatsDiscardsNegativeLag pins the clock-skew rule: a publisher whose clock
// is ahead of ours produces a negative lag, which is meaningless rather than merely
// imprecise, so it must not overwrite a good value.
func TestCacheStatsDiscardsNegativeLag(t *testing.T) {
	var s cacheStats

	s.recordInvalidationLag(-time.Second)
	if got := s.snapshot().LastInvalidationLag; got != 0 {
		t.Errorf("after a negative lag, LastInvalidationLag = %v, want 0", got)
	}

	s.recordInvalidationLag(30 * time.Millisecond)
	s.recordInvalidationLag(-time.Hour)
	if got := s.snapshot().LastInvalidationLag; got != 30*time.Millisecond {
		t.Errorf("a negative lag overwrote a good one: got %v, want 30ms", got)
	}
}

// TestCacheStatsNilReceiver covers the construction paths that never wire counters —
// tests build v2Operations literals directly (see v2_cache_test.go), and a nil check
// at every record site would be noise.
func TestCacheStatsNilReceiver(t *testing.T) {
	var s *cacheStats
	s.hit()
	s.miss()
	s.recordRebuild(time.Second)
	s.recordInvalidationLag(time.Second)
	if got := s.snapshot(); got != (CacheStats{}) {
		t.Errorf("nil cacheStats snapshot = %+v, want zero value", got)
	}
}

// BenchmarkCacheStatsOverhead measures what the counters cost the read path, by
// running the same Find with recording on and off. Setting stats to nil is what the
// nil-receiver contract is for, so the two arms differ only in whether the atomics
// execute — no build tag, no second code path.
//
// The counters sit on the cache-check path, not inside the scan loop, so the cost is
// per Find rather than per matched byte. If this gap ever grows past a few percent of
// Preset/Cached, move the counter rather than keeping it.
func BenchmarkCacheStatsOverhead(b *testing.B) {
	modes := []struct {
		name string
		args *AhoCorasickArgs
	}{
		{"Preset", &AhoCorasickArgs{Preset: PresetBalanced}},
		{"Cached", &AhoCorasickArgs{EnableCache: true}},
	}
	for _, mode := range modes {
		for _, recording := range []bool{true, false} {
			label := "on"
			if !recording {
				label = "off"
			}
			b.Run(mode.name+"/recording="+label, func(b *testing.B) {
				mr := miniredis.RunT(b)
				defer mr.Close()

				args := *mode.args
				args.Addr = mr.Addr()
				args.Name = "bench-stats"
				ac, err := Create(&args)
				if err != nil {
					b.Fatal(err)
				}
				defer func() { _ = ac.Close() }()

				if _, err := ac.Add("greeting"); err != nil {
					b.Fatal(err)
				}
				if _, err := ac.Find("warm up the cache"); err != nil {
					b.Fatal(err)
				}
				if !recording {
					disableStats(ac)
				}

				b.ResetTimer()
				for range b.N {
					if _, err := ac.Find("a greeting in a longer sentence"); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

// disableStats nils every counter reference an instance holds, so the recording calls
// become the nil-receiver no-op. Benchmark-only: production has no way to switch the
// counters off, and none is wanted — CacheStats exists to be always available.
func disableStats(ac *AhoCorasick) {
	ac.stats = nil
	switch ops := ac.ops.(type) {
	case *redisBackedAC:
		ops.stats = nil
	case *v2Operations:
		ops.stats = nil
		ops.engines.stats = nil
	case *v1Operations:
		ops.engines.stats = nil
	}
}

// TestCacheStatsAfterClose documents that scraping a closed instance is safe: a
// metrics goroutine racing shutdown must not panic.
func TestCacheStatsAfterClose(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()

	if _, err := ac.Add("he"); err != nil {
		t.Fatal(err)
	}
	if _, err := ac.Find("she"); err != nil {
		t.Fatal(err)
	}
	before := ac.CacheStats()
	if err := ac.Close(); err != nil {
		t.Fatal(err)
	}
	if after := ac.CacheStats(); after != before {
		t.Errorf("stats changed across Close: %+v then %+v", before, after)
	}
}
