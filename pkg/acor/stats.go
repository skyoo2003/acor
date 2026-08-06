// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"sync/atomic"
	"time"
)

// CacheStats is a point-in-time snapshot of one instance's local cache activity.
// Obtain it from AhoCorasick.CacheStats; it is returned by value and never
// constructed by callers, so fields may be added in a later v1 release without
// breaking code that reads it. Read the fields you care about rather than comparing
// a whole value against another: a snapshot taken before a new field existed does
// not compare equal to one taken after.
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
	//
	// One call into ACOR is one read, whatever it scans. FindParallel,
	// FindIndexParallel, and FindMany load the automaton once and scan every chunk
	// or text against that one snapshot, so their hit rate is directly comparable to
	// a serial workload's. (Before v1.5.0 they loaded per chunk and recorded N reads
	// for an N-chunk text, which also cost N Redis round trips.)
	Hits uint64
	// Misses is the number of reads that had to wait for the local automaton to be
	// rebuilt, whether because a peer's write invalidated it or because nothing was
	// cached yet. A read whose Redis fetch then failed is counted here too: it waited
	// and came back without an automaton.
	//
	// Hits+Misses is the read count, so the hit rate is Hits/(Hits+Misses). Both are
	// zero on a fresh instance, so guard that division. See Hits for what one read
	// means.
	Misses uint64
	// Rebuilds is the number of automaton builds. It starts at 1 in Preset mode, which
	// builds once during Create before any read.
	//
	// It is deliberately not equal to Misses in either direction. Concurrent misses
	// coalesce onto one build, so Misses-Rebuilds is the work that coalescing saved;
	// and a local write rebuilds off the read path, which pushes Rebuilds the other way.
	//
	// Both counters are unsigned, so test Misses > Rebuilds before taking that
	// difference. Rebuilds exceeding Misses is ordinary rather than exceptional — a
	// Preset instance opens at Rebuilds 1 with no reads at all — and the subtraction
	// wraps to something near 2^64 instead of going negative.
	Rebuilds uint64
	// RebuildDuration is the total time spent building automatons, excluding the Redis
	// fetch and the wait for the lock that serializes rebuilds. The brief cache write
	// lock a finished build takes to publish itself is included.
	// RebuildDuration/Rebuilds is the mean cost of one rebuild — what a write to a
	// large collection makes every reader pay.
	//
	// Where decoding the fetched payload lands differs by mode. A default V2 instance
	// parses inside the memoized build, so the decode counts here; Preset materializes
	// its keyword set in applyReload before rebuildEngine starts timing, so there it
	// does not. Read the mean against itself over time rather than across two
	// differently configured instances.
	RebuildDuration time.Duration
	// LastInvalidationLag is the delay between a peer publishing an invalidation and
	// this instance receiving it, for the most recent one.
	//
	// It is populated only where an invalidation listener runs: Preset mode, and V2
	// with EnableCache. The default V2 mode and V1 subscribe to nothing, so there it
	// stays zero however busy the peers are — read that as unavailable, not as fast.
	// Where a listener does run, zero means none has arrived yet: a fresh instance, or
	// a collection nobody else is writing.
	//
	// The two timestamps come from two machines' clocks, so the value is the real
	// delivery delay plus their offset. That offset has no known bound and no fixed
	// sign: skew that runs the publisher's clock ahead understates the delay just as
	// readily as the other direction overstates it, so this is a skew-contaminated
	// estimate and not a bound either way. Only an outright negative result is
	// discarded, which drops the samples the skew distorted most rather than
	// correcting the rest. Treat a step change as the signal.
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
// a negative one. Negative means the publisher's clock is ahead of ours by more than
// the delivery delay, which makes the value meaningless rather than merely imprecise —
// the same reason selfSkipSet.claim distrusts a negative age instead of using it. A
// positive one is not thereby correct: it carries whatever the clock offset is, in
// either direction. See CacheStats.LastInvalidationLag.
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
