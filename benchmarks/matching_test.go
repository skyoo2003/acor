// SPDX-License-Identifier: Apache-2.0

// Package benchmarks measures ACOR's matching throughput, retained memory, and
// cold-start cost through the public API.
//
// It is a separate module because these benchmarks need a server — real or
// emulated — and a dictionary loaded the way a caller would load it, which is
// heavier setup than the core module's own benchmarks in pkg/acor and
// internal/engine carry.
//
// Published on docs/content/reference/benchmarks.md. Absolute times are
// hardware-bound and moved 20-25% between runs on one machine, so read them as an
// order of magnitude rather than a specification.
//
// # Measurement constraints
//
// These decide whether the numbers mean anything:
//
//   - The corpus is shared-prefix keywords, mirroring
//     internal/engine/engine_bench_test.go so failure links are exercised. A
//     disjoint alphabet would flatter every preset equally and measure nothing.
//   - Every preset runs case-sensitive, so no row pays a strings.ToLower the others
//     avoid. ACOR defaults to case-insensitive, so this is set explicitly.
//   - Preset mode's hot path issues no Redis round trips, so steady-state matching
//     can run against miniredis without the backing store entering the
//     measurement. Build and bulk load are the opposite case and are gated on a
//     real server.
//   - Each query shape is its own benchmark rather than one API forced to answer
//     all of them.
package benchmarks

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"

	miniredis "github.com/alicebob/miniredis/v2"

	"github.com/skyoo2003/acor/pkg/acor"
)

// benchText holds keyword50 and keyword99, so it matches at every corpus size.
const benchText = "the quick brown keyword50 fox keyword99 jumps over the lazy dog "

// corpusSizes matches the sizes already published on
// docs/content/reference/benchmarks.md, so the two pages can be read together.
//
// It stops at 1000 so the sizes line up with the published tables and so every
// benchmark here — including the ones that repopulate a collection per iteration —
// stays inside a CI run. BenchmarkBulkLoad reports single-digit milliseconds at
// that size.
var corpusSizes = []int{100, 1000}

// acorPresets covers every preset ACOR ships, including the ones that do not look
// good: the trade-off between them is what the presets exist to express, and the
// fastest one here is not the one the docs recommend.
//
// PresetUltimate is absent because it is now an alias for PresetBalanced and would
// only duplicate that row.
var acorPresets = []struct {
	name   string
	preset acor.Preset
}{
	{"ACOR-Speed", acor.PresetSpeed},
	{"ACOR-Balanced", acor.PresetBalanced},
	{"ACOR-MemoryEfficient", acor.PresetMemoryEfficient},
}

// keywords builds n shared-prefix keywords (keyword5 ⊂ keyword50), which exercise
// failure-link traversal. A disjoint alphabet would flatter every automaton equally
// and measure nothing.
func keywords(n int) []string {
	kws := make([]string, n)
	for i := range kws {
		kws[i] = fmt.Sprintf("keyword%d", i)
	}
	return kws
}

func haystack() string { return strings.Repeat(benchText, 10) }

// realServerAddr returns the address of a real Redis/Valkey server, skipping when
// unset. It shares the variable name with pkg/acor's integration tests; the helper
// there is unexported and unreachable from this module, so the variable is re-read
// rather than the helper exported.
func realServerAddr(tb testing.TB) string {
	tb.Helper()
	addr := os.Getenv("ACOR_INTEGRATION_ADDR")
	if addr == "" {
		tb.Skip("ACOR_INTEGRATION_ADDR not set; skipping real-server benchmark")
	}
	return addr
}

// newPresetAt builds an ACOR instance in Preset mode against an already-running
// server, loading whatever dictionary that server already holds.
func newPresetAt(tb testing.TB, addr string, preset acor.Preset) *acor.AhoCorasick {
	tb.Helper()
	ac, err := acor.Create(&acor.AhoCorasickArgs{
		Addr:          addr,
		Name:          tb.Name(),
		Preset:        preset,
		CaseSensitive: true,
	})
	if err != nil {
		tb.Fatalf("Create(preset=%v) error: %v", preset, err)
	}
	tb.Cleanup(func() { _ = ac.Close() })
	return ac
}

// newPreset builds an ACOR instance in Preset mode against miniredis and bulk-loads
// the corpus. AddMany coalesces the local rebuild, so load is O(N) rather than one
// rebuild per keyword.
func newPreset(tb testing.TB, preset acor.Preset, kws []string) *acor.AhoCorasick {
	tb.Helper()
	mr := miniredis.RunT(tb)
	ac := newPresetAt(tb, mr.Addr(), preset)
	if _, err := ac.AddMany(kws, nil); err != nil {
		tb.Fatalf("AddMany(%d keywords) error: %v", len(kws), err)
	}
	return ac
}

// dedup reduces ACOR's per-occurrence Find output to the unique keyword set. This
// is the work a caller had to write before FindSet existed, kept by
// BenchmarkFindUniqueSet as the baseline for what FindSet is worth.
func dedup(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := in[:0:0]
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// BenchmarkFindUniqueSet measures query shape A: "which of my patterns appear in
// this text?" — the content-filtering question, answered as a set with each keyword
// reported once.
//
// It reports both shapes: FindSet, which folds duplicates out during the scan, and
// the Find-then-deduplicate a caller had to write before it existed.
func BenchmarkFindUniqueSet(b *testing.B) {
	text := haystack()

	for _, n := range corpusSizes {
		kws := keywords(n)

		for _, p := range acorPresets {
			b.Run(fmt.Sprintf("%s-FindSet/%dkw", p.name, n), func(b *testing.B) {
				ac := newPreset(b, p.preset, kws)
				if _, err := ac.FindSet(text); err != nil {
					b.Fatalf("FindSet warm-up error: %v", err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if _, err := ac.FindSet(text); err != nil {
						b.Fatalf("FindSet error: %v", err)
					}
				}
			})

			// The pre-FindSet shape, kept so the page can show what the API
			// addition is worth rather than asserting it.
			b.Run(fmt.Sprintf("%s-Find+dedup/%dkw", p.name, n), func(b *testing.B) {
				ac := newPreset(b, p.preset, kws)
				if _, err := ac.Find(text); err != nil {
					b.Fatalf("Find warm-up error: %v", err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					found, err := ac.Find(text)
					if err != nil {
						b.Fatalf("Find error: %v", err)
					}
					_ = dedup(found)
				}
			})
		}
	}
}

// BenchmarkFindOccurrences measures query shape B: "report every occurrence,
// overlaps included" — the semantics ACOR's Find has.
func BenchmarkFindOccurrences(b *testing.B) {
	text := haystack()

	for _, n := range corpusSizes {
		kws := keywords(n)

		for _, p := range acorPresets {
			b.Run(fmt.Sprintf("%s/%dkw", p.name, n), func(b *testing.B) {
				ac := newPreset(b, p.preset, kws)
				if _, err := ac.Find(text); err != nil {
					b.Fatalf("Find warm-up error: %v", err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if _, err := ac.Find(text); err != nil {
						b.Fatalf("Find error: %v", err)
					}
				}
			})
		}
	}
}

// BenchmarkFindMatches measures query shape C: "give me every match with its
// position", under leftmost-longest non-overlapping semantics.
//
// Offsets are rune positions. This corpus is ASCII, so they coincide with byte
// positions; on multibyte text the scan does strictly more work.
func BenchmarkFindMatches(b *testing.B) {
	text := haystack()
	opts := &acor.MatchOptions{Kind: acor.MatchKindLeftmostLongest}

	for _, n := range corpusSizes {
		kws := keywords(n)

		for _, p := range acorPresets {
			b.Run(fmt.Sprintf("%s/%dkw", p.name, n), func(b *testing.B) {
				ac := newPreset(b, p.preset, kws)
				if _, err := ac.FindMatches(text, opts); err != nil {
					b.Fatalf("FindMatches warm-up error: %v", err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if _, err := ac.FindMatches(text, opts); err != nil {
						b.Fatalf("FindMatches error: %v", err)
					}
				}
			})
		}
	}
}

// BenchmarkBuild measures cold start: going from "process has just started" to
// "ready to serve the first match".
//
// It is gated on a real server because this is the one axis where miniredis would
// flatter ACOR: an in-process emulator makes the dictionary fetch nearly free, and
// that fetch is most of what a cold start costs. The figure includes loading the
// dictionary from Redis, not just building the automaton from patterns already in
// the process.
func BenchmarkBuild(b *testing.B) {
	addr := realServerAddr(b)

	for _, n := range corpusSizes {
		kws := keywords(n)

		b.Run(fmt.Sprintf("ACOR-PresetBalanced-fromRedis/%dkw", n), func(b *testing.B) {
			args := func() *acor.AhoCorasickArgs {
				return &acor.AhoCorasickArgs{
					Addr:          addr,
					Name:          b.Name(),
					Preset:        acor.PresetBalanced,
					CaseSensitive: true,
				}
			}
			seed, err := acor.Create(args())
			if err != nil {
				b.Fatalf("Create(seed) error: %v", err)
			}
			if err := seed.Flush(); err != nil {
				b.Fatalf("Flush error: %v", err)
			}
			if _, err := seed.AddMany(kws, nil); err != nil {
				b.Fatalf("AddMany(%d keywords) error: %v", n, err)
			}
			b.Cleanup(func() {
				_ = seed.Flush()
				_ = seed.Close()
			})

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				ac, err := acor.Create(args())
				if err != nil {
					b.Fatalf("Create error: %v", err)
				}
				// Create alone is not the whole cold start: the automaton is
				// guaranteed loaded and built only once a match has been served.
				if _, err := ac.Find(benchText); err != nil {
					b.Fatalf("Find error: %v", err)
				}
				_ = ac.Close()
			}
		})
	}
}

// BenchmarkBulkLoad measures populating a collection from scratch — the one-time
// cost of getting a dictionary into ACOR.
//
// That cost is paid once for the whole fleet, not per process: every instance that
// later starts against the same collection pays only BenchmarkBuild's cold start.
//
// Gated on a real server: miniredis understates the write path by roughly 2x.
func BenchmarkBulkLoad(b *testing.B) {
	addr := realServerAddr(b)

	for _, n := range corpusSizes {
		kws := keywords(n)
		b.Run(fmt.Sprintf("ACOR-PresetBalanced/%dkw", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				ac, err := acor.Create(&acor.AhoCorasickArgs{
					Addr:          addr,
					Name:          fmt.Sprintf("%s-%d", b.Name(), i),
					Preset:        acor.PresetBalanced,
					CaseSensitive: true,
				})
				if err != nil {
					b.Fatalf("Create error: %v", err)
				}
				if err := ac.Flush(); err != nil {
					b.Fatalf("Flush error: %v", err)
				}
				b.StartTimer()

				if _, err := ac.AddMany(kws, nil); err != nil {
					b.Fatalf("AddMany(%d keywords) error: %v", n, err)
				}

				b.StopTimer()
				_ = ac.Flush()
				_ = ac.Close()
				b.StartTimer()
			}
		})
	}
}

// TestMemoryFootprint reports retained heap per preset, which is what decides
// whether a dictionary fits inside a pod's memory limit. It is a test rather than a
// benchmark because -benchmem reports allocation volume during the timed loop, not
// what stays resident afterwards.
//
// The figure includes the go-redis client and connection pool, because a user
// running ACOR pays for those too. It excludes the emulated Redis server: miniredis
// keeps the whole V2 trie (prefixes plus the outputs hash) on the same heap, which
// was ~40% of the reported total at 1000 keywords, and a real deployment does not
// host Redis inside the matching process. The server is therefore started and
// loaded outside the measurement, and only the reading instance is measured.
//
//	go test -run MemoryFootprint -v ./...
func TestMemoryFootprint(t *testing.T) {
	for _, n := range corpusSizes {
		kws := keywords(n)

		t.Run(fmt.Sprintf("%dkw", n), func(t *testing.T) {
			for _, p := range acorPresets {
				mr := miniredis.RunT(t)
				loader := newPresetAt(t, mr.Addr(), p.preset)
				if _, err := loader.AddMany(kws, nil); err != nil {
					t.Fatalf("AddMany(%d keywords) error: %v", n, err)
				}
				t.Logf("%-20s %8d KiB", p.name+":", retainedKiB(func() any {
					ac := newPresetAt(t, mr.Addr(), p.preset)
					// The automaton is loaded and built only once a match has been
					// served, so Create alone would under-report.
					if _, err := ac.Find(benchText); err != nil {
						t.Fatalf("Find error: %v", err)
					}
					return ac
				}))
			}
		})
	}
}

// retainedKiB reports how much heap is still held after build() returns and a GC
// has run, keeping the result alive across the measurement.
func retainedKiB(build func() any) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	held := build()
	runtime.GC()
	runtime.ReadMemStats(&after)
	runtime.KeepAlive(held)
	if after.HeapAlloc < before.HeapAlloc {
		return 0
	}
	return (after.HeapAlloc - before.HeapAlloc) / 1024
}
