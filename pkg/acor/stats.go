// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"sync/atomic"
	"time"
)

// CacheStats is a point-in-time snapshot of one instance's local cache activity.
// Obtain it from AhoCorasick.CacheStats; it is returned by value and never
// constructed by callers, so fields may be added in a later v1 release without
// breaking code that reads it.
//
// Every counter is process-local and cumulative since Create. Nothing here is read
// from or written to Redis, so a peer instance's activity is invisible: scrape each
// instance separately rather than expecting a fleet-wide total.
type CacheStats struct {
	// Hits is the number of reads served from the local automaton without rebuilding
	// it.
	//
	// What that saves depends on the mode. With Preset or EnableCache a hit means no
	// Redis round trip happened at all. Without either, the freshness read still
	// happens on every call and a hit means only that the automaton did not have to be
	// rebuilt from the bytes that read returned.
	Hits uint64
	// Misses is the number of reads that had to wait for the local automaton to be
	// rebuilt, whether because a peer's write invalidated it or because nothing was
	// cached yet.
	//
	// Hits+Misses is the read count, so the hit rate is Hits/(Hits+Misses). Both are
	// zero on a fresh instance, so guard that division.
	Misses uint64
	// Rebuilds is the number of automaton builds. It starts at 1 in Preset mode, which
	// builds once during Create before any read.
	//
	// It is deliberately not equal to Misses in either direction. Concurrent misses
	// coalesce onto one build, so Misses-Rebuilds is the work that coalescing saved;
	// and a local write rebuilds off the read path, which pushes Rebuilds the other way.
	Rebuilds uint64
	// RebuildDuration is the total time spent building automatons, excluding the Redis
	// fetch and any wait for the rebuild lock. RebuildDuration/Rebuilds is the mean
	// cost of one rebuild — what a write to a large collection makes every reader pay.
	RebuildDuration time.Duration
	// LastInvalidationLag is the delay between a peer publishing an invalidation and
	// this instance receiving it, for the most recent one. Zero means none has arrived:
	// a fresh instance, or a collection nobody else is writing.
	//
	// The two timestamps come from two machines' clocks, so this includes clock skew
	// and is not a pure network measurement. Under NTP skew it can exceed the real
	// delay by orders of magnitude, and a negative result is discarded rather than
	// recorded. Treat a step change as the signal and the absolute value as an upper
	// bound.
	LastInvalidationLag time.Duration
}

// cacheStats holds the counters behind CacheStats. One instance is shared by an
// AhoCorasick and its operations, so all four modes that keep local state record into
// the same place.
//
// Every method tolerates a nil receiver. The call sites sit on the read path in four
// different modes, and a nil check at each would be noise; a construction path that
// does not wire the counters — or a test building an ops struct directly — then simply
// does not record.
type cacheStats struct {
	// atomic.Uint64/Int64 rather than bare integers with atomic.AddUint64: the wrapper
	// types guarantee 8-byte alignment on 386/arm (both released by goreleaser, where a
	// misaligned 64-bit atomic panics) instead of leaving it to field order, for the
	// same reason selfSkipSet.publishCount uses one.
	hits         atomic.Uint64
	misses       atomic.Uint64
	rebuilds     atomic.Uint64
	rebuildNanos atomic.Int64
	lastLagNanos atomic.Int64
}

func (s *cacheStats) hit() {
	if s != nil {
		s.hits.Add(1)
	}
}

func (s *cacheStats) miss() {
	if s != nil {
		s.misses.Add(1)
	}
}

// recordRebuild adds one build and its duration. Callers time the build alone, not
// the lock acquisition in front of it: a goroutine queued behind another's rebuild
// waited, but it did not spend that time building.
func (s *cacheStats) recordRebuild(d time.Duration) {
	if s == nil {
		return
	}
	s.rebuilds.Add(1)
	s.rebuildNanos.Add(int64(d))
}

// recordInvalidationLag stores the delay of the invalidation just received and drops
// a negative one. Negative means the publisher's clock is ahead of ours, which makes
// the value meaningless rather than merely imprecise — the same reason
// selfSkipSet.claim distrusts a negative age instead of using it.
func (s *cacheStats) recordInvalidationLag(d time.Duration) {
	if s == nil || d < 0 {
		return
	}
	s.lastLagNanos.Store(int64(d))
}

func (s *cacheStats) snapshot() CacheStats {
	if s == nil {
		return CacheStats{}
	}
	return CacheStats{
		Hits:                s.hits.Load(),
		Misses:              s.misses.Load(),
		Rebuilds:            s.rebuilds.Load(),
		RebuildDuration:     time.Duration(s.rebuildNanos.Load()),
		LastInvalidationLag: time.Duration(s.lastLagNanos.Load()),
	}
}

// timeRebuild runs build and records how long it took, returning what build returned.
// A build that fails is not counted: nothing was rebuilt, and folding its duration in
// would inflate the mean cost of a successful one.
func timeRebuild[T any](s *cacheStats, build func() (T, error)) (T, error) {
	start := time.Now()
	out, err := build()
	if err == nil {
		s.recordRebuild(time.Since(start))
	}
	return out, err
}
