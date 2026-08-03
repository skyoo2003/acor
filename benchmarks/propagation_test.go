// SPDX-License-Identifier: Apache-2.0

package benchmarks

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/skyoo2003/acor/pkg/acor"
)

// This file measures the one axis an in-memory Aho-Corasick library cannot serve at
// any speed: propagating a dictionary change to another running process.
//
// For cloudflare/ahocorasick or petar-dambovaliev/aho-corasick there is no
// equivalent number. Their automaton is built once from a slice in the local
// process, so the answer to "instance B is serving a stale dictionary, how long
// until it isn't?" is a redeploy. That is the trade ACOR makes: it gives up raw
// throughput to keep this number finite. README claims Pub/Sub invalidation without
// putting a duration on it; this is that duration.
//
//	ACOR_INTEGRATION_ADDR=localhost:6379 go test -run Propagation -v ./...

const (
	// propagationSamples is enough for a meaningful p99 without turning the run
	// into a soak test.
	propagationSamples = 100
	// propagationTimeout bounds a single sample. Reaching it means invalidation
	// never arrived, which is a failure rather than a slow sample.
	propagationTimeout = 10 * time.Second
)

// TestPropagationLatency measures the interval between Add() returning on one
// instance and a second instance's Find() reporting the new keyword.
//
// The clock starts after Add() returns, so this is propagation alone and excludes
// the write's own round trips: a user asking "when do my other pods see this?" means
// the interval that begins once their write call has come back.
//
// Measurement floor: instance B is polled with Find(), which in Preset mode is a
// local scan issuing no Redis traffic, so resolution is roughly one Find call
// (single-digit microseconds at this corpus size). A figure at or below that floor
// means "faster than this test can resolve", not a precise value.
func TestPropagationLatency(t *testing.T) {
	addr := realServerAddr(t)

	collection := fmt.Sprintf("propagation-%d", time.Now().UnixNano())
	newInstance := func(role string) *acor.AhoCorasick {
		ac, err := acor.Create(&acor.AhoCorasickArgs{
			Addr:          addr,
			Name:          collection,
			Preset:        acor.PresetBalanced,
			CaseSensitive: true,
		})
		if err != nil {
			t.Fatalf("Create(%s) error: %v", role, err)
		}
		t.Cleanup(func() { _ = ac.Close() })
		return ac
	}

	writer := newInstance("writer")
	reader := newInstance("reader")
	t.Cleanup(func() { _ = writer.Flush() })

	if err := writer.Flush(); err != nil {
		t.Fatalf("Flush error: %v", err)
	}
	// Seed both instances so each sample exercises an incremental update to a
	// live automaton rather than the first-ever build.
	if _, err := writer.AddMany(keywords(100), nil); err != nil {
		t.Fatalf("AddMany(seed) error: %v", err)
	}
	if _, err := reader.Find(benchText); err != nil {
		t.Fatalf("Find(reader warm-up) error: %v", err)
	}

	latencies := make([]time.Duration, 0, propagationSamples)
	var stalePolls int

	for i := range propagationSamples {
		keyword := fmt.Sprintf("propagated%d", i)
		text := "the quick brown " + keyword + " fox"

		if _, err := writer.Add(keyword); err != nil {
			t.Fatalf("Add(%q) error: %v", keyword, err)
		}
		start := time.Now()

		var visible bool
		for time.Since(start) < propagationTimeout {
			matched, err := reader.Find(text)
			// The staleness window must present as a stale answer, never as an
			// error. If this fires, the documented consistency model is wrong.
			if err != nil {
				t.Fatalf("Find(%q) on the reader errored during the staleness "+
					"window after %v: %v", keyword, time.Since(start), err)
			}
			if slices.Contains(matched, keyword) {
				visible = true
				break
			}
			stalePolls++
		}
		if !visible {
			t.Fatalf("Add(%q) never propagated to the reader within %v",
				keyword, propagationTimeout)
		}
		latencies = append(latencies, time.Since(start))
	}

	slices.Sort(latencies)
	t.Logf("cross-instance propagation over %d samples (Preset mode, Pub/Sub invalidation)",
		len(latencies))
	t.Logf("  p50 %v", percentile(latencies, 50))
	t.Logf("  p95 %v", percentile(latencies, 95))
	t.Logf("  p99 %v", percentile(latencies, 99))
	t.Logf("  max %v", latencies[len(latencies)-1])
	t.Logf("  %d stale reads observed across the windows, 0 errors — the reader "+
		"serves the previous dictionary while invalidation is in flight", stalePolls)
}

// percentile returns the p-th percentile of an already-sorted slice by nearest rank
// rounded up, which at this sample size keeps the resolution visible where
// interpolation would obscure it. Callers pass p >= 50, so the rank is always at
// least 1 and only the upper clamp is reachable.
func percentile(sorted []time.Duration, p int) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	return sorted[min((p*len(sorted)+99)/100, len(sorted))-1]
}
