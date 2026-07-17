// SPDX-License-Identifier: Apache-2.0

package acor //nolint:errcheck,gosec

import (
	"fmt"
	"strings"
	"testing"

	miniredis "github.com/alicebob/miniredis/v2"
)

// BenchmarkV2CachedFind exercises the default V2 Find path with local caching
// enabled over a growing dictionary and a long text. Reads are served entirely
// by the locally-built Aho-Corasick match engine (0 Redis RTT after warm-up),
// so this measures the matching hot loop rather than Redis I/O.
func BenchmarkV2CachedFind(b *testing.B) {
	text := strings.Repeat("the quick brown keyword50 fox keyword99 jumps over the lazy dog ", 40)
	for _, n := range []int{100, 1000, 5000} {
		b.Run(fmt.Sprintf("%dkw", n), func(b *testing.B) {
			mr := miniredis.RunT(b)
			ac, err := Create(&AhoCorasickArgs{Addr: mr.Addr(), Name: "bench-v2", EnableCache: true})
			if err != nil {
				b.Fatal(err)
			}
			defer ac.Close()
			for i := range n {
				if _, err := ac.Add(fmt.Sprintf("keyword%d", i)); err != nil {
					b.Fatal(err)
				}
			}
			// Warm the local cache/engine so the timed loop is pure matching.
			if _, err := ac.Find(text); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := ac.Find(text); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
