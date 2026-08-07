// SPDX-License-Identifier: Apache-2.0

package acor //nolint:errcheck // benchmarks focus on timing, not error paths

import (
	"fmt"
	"strings"
	"testing"
)

// Timing evidence published on docs/content/reference/benchmarks.md.
//
// These run against a real server only. Every other benchmark in this package
// uses miniredis, where a round trip costs almost nothing — which is exactly
// where V1's per-node write cost lives, so miniredis understates the V2
// advantage while appearing to measure it. Absolute times are hardware-bound;
// the reproducible quantity is the V1:V2 ratio.
//
//	ACOR_INTEGRATION_ADDR=localhost:6379 make bench

func newRealServerAC(tb testing.TB, name string, args *AhoCorasickArgs) *AhoCorasick {
	tb.Helper()
	args.Addr = integrationAddr(tb)
	args.Name = name
	ac, err := Create(args)
	if err != nil {
		tb.Fatalf("Create(%s) error: %v", name, err)
	}
	if err := ac.Flush(); err != nil {
		_ = ac.Close()
		tb.Fatalf("Flush(%s) error: %v", name, err)
	}
	tb.Cleanup(func() {
		_ = ac.Flush()
		_ = ac.Close()
	})
	return ac
}

// BenchmarkRealServerFind measures Find against a real server across schema
// versions and the cached path. This is the comparison the README's headline
// speedup claim rests on.
func BenchmarkRealServerFind(b *testing.B) {
	text := strings.Repeat("the quick brown keyword50 fox keyword99 jumps over the lazy dog ", 10)

	cases := []struct {
		name string
		args *AhoCorasickArgs
	}{
		{"V1", &AhoCorasickArgs{SchemaVersion: SchemaV1}},
		{"V2", &AhoCorasickArgs{SchemaVersion: SchemaV2}},
		{"V2Cached", &AhoCorasickArgs{SchemaVersion: SchemaV2, EnableCache: true}},
		{"PresetBalanced", &AhoCorasickArgs{Preset: PresetBalanced}},
	}

	for _, n := range []int{100, 1000} {
		for _, tc := range cases {
			b.Run(fmt.Sprintf("%s/%dkw", tc.name, n), func(b *testing.B) {
				args := *tc.args
				ac := newRealServerAC(b, fmt.Sprintf("bench-find-%s-%d", tc.name, n), &args)
				if args.SchemaVersion == SchemaV1 {
					ac = v1Writable(b, ac)
				}
				for i := range n {
					if _, err := ac.Add(fmt.Sprintf("keyword%d", i)); err != nil {
						b.Fatalf("Add() error: %v", err)
					}
				}

				b.ResetTimer()
				for range b.N {
					if _, err := ac.Find(text); err != nil {
						b.Fatalf("Find() error: %v", err)
					}
				}
			})
		}
	}
}

// BenchmarkRealServerAdd measures Add, where V1's per-node round trips actually
// bite. Each iteration adds a distinct keyword so no run is a no-op update.
func BenchmarkRealServerAdd(b *testing.B) {
	cases := []struct {
		name string
		args *AhoCorasickArgs
	}{
		{"V1", &AhoCorasickArgs{SchemaVersion: SchemaV1}},
		{"V2", &AhoCorasickArgs{SchemaVersion: SchemaV2}},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			args := *tc.args
			ac := newRealServerAC(b, "bench-add-"+tc.name, &args)
			// V1 is measured through the fixture writer rather than dropped from the
			// comparison. docs/content/reference/benchmarks.md uses V1 as the baseline
			// for every table and rests its case for V2 on V1's per-node write cost,
			// so deleting this case would leave those published numbers unverifiable.
			// The wrapper calls the same writeKeyword that Add used to reach.
			if args.SchemaVersion == SchemaV1 {
				ac = v1Writable(b, ac)
			}

			b.ResetTimer()
			for i := range b.N {
				if _, err := ac.Add(fmt.Sprintf("benchkeyword%d", i)); err != nil {
					b.Fatalf("Add() error: %v", err)
				}
			}
		})
	}
}

// TestRTTAgainstRealServer proves the round-trip counts are structural rather
// than an artifact of miniredis. The counter wraps the kvStorage seam, which
// sits above the wire, so a real server must produce the identical numbers
// pinned in rtt_claims_test.go. If it ever doesn't, the backend is taking a
// different code path and the published table describes only miniredis.
func TestRTTAgainstRealServer(t *testing.T) {
	integrationAddr(t) // skips unless a real server is configured

	find := func(ac *AhoCorasick) error { _, err := ac.Find("he is him"); return err }
	add := func(ac *AhoCorasick) error { _, err := ac.Add("hello"); return err }

	cases := []struct {
		name string
		args *AhoCorasickArgs
		seed []string
		op   func(*AhoCorasick) error
		want int
	}{
		{"V2Find", &AhoCorasickArgs{}, []string{"he", "her", "him"}, find, 1},
		{"V2Add", &AhoCorasickArgs{}, []string{"seed"}, add, 2},
		{"V1Find", &AhoCorasickArgs{SchemaVersion: SchemaV1}, []string{"he", "her", "him"}, find, 1},
		{"PresetFind", &AhoCorasickArgs{Preset: PresetBalanced}, []string{"greeting"}, find, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := *tc.args
			ac := newRealServerAC(t, "rtt-real-"+tc.name, &args)
			// V1 rejects Add in every build, so its fixture goes through the writer
			// that path used to reach. The claim under measurement is a read-path
			// round-trip count, which the wrapper does not touch.
			if args.SchemaVersion == SchemaV1 {
				ac = v1Writable(t, ac)
			}
			for _, kw := range tc.seed {
				if _, err := ac.Add(kw); err != nil {
					t.Fatalf("Add(%q) error: %v", kw, err)
				}
			}

			c := countRTT(t, ac)
			c.reset()
			if err := tc.op(ac); err != nil {
				t.Fatalf("%s operation error: %v", tc.name, err)
			}

			if got := c.count(); got != tc.want {
				t.Fatalf("%s on real server = %d round trips, want %d (miniredis reports %d)", tc.name, got, tc.want, tc.want)
			}
		})
	}
}
