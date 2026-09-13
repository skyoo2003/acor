// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// BenchmarkOverlayCosts separates the added.Contains pre-scan, the second
// automaton traversal, and the former per-rune collect/sort path. It is a
// diagnostic benchmark, not a release gate: the real-server matrix measures
// the public V3 calls and process RSS.
func BenchmarkOverlayCosts(b *testing.B) {
	words := make(map[string]struct{}, 10000)
	for i := range 10000 {
		words[fmt.Sprintf("common-prefix-keyword-%05d", i)] = struct{}{}
	}
	base := New(PresetMemoryEfficient)
	base.Build(words)
	added := New(PresetMemoryEfficient)
	added.Build(map[string]struct{}{"delta-keyword": {}, "delta": {}, "delta-keyword-long": {}})
	overlay := Overlay(base, added, nil)
	for name, query := range map[string]string{
		"added_hit":  strings.Repeat("common-prefix-keyword-00123 delta-keyword ", 8),
		"single_hit": "common-prefix-keyword-00123 common-prefix-keyword-04123 delta-keyword common-prefix-keyword-09999",
		"delta_miss": strings.Repeat("common-prefix-keyword-00123 no-change-here ", 8),
	} {
		b.Run(name, func(b *testing.B) {
			b.Run("base", func(b *testing.B) {
				b.ReportAllocs()
				for range b.N {
					_ = base.Find(query)
				}
			})
			b.Run("added_precheck", func(b *testing.B) {
				b.ReportAllocs()
				for range b.N {
					_ = added.Contains(query)
				}
			})
			b.Run("two_traversals", func(b *testing.B) {
				b.ReportAllocs()
				for range b.N {
					_ = base.Find(query)
					_ = added.Find(query)
				}
			})
			b.Run("collect_sort", func(b *testing.B) {
				b.ReportAllocs()
				for range b.N {
					_ = oldOverlayFind(base.impl.(*memEfficientEngine), added.impl.(*memEfficientEngine), query)
				}
			})
			b.Run("merged", func(b *testing.B) {
				b.ReportAllocs()
				for range b.N {
					_ = overlay.Find(query)
				}
			})
			b.Run("merged_scan", func(b *testing.B) {
				b.ReportAllocs()
				for range b.N {
					var out []string
					overlay.MatchString(query, func(k string, _, _ int) bool {
						out = append(out, k)
						return true
					})
				}
			})
		})
	}
}

// oldOverlayFind models the original additional-hit branch so its allocation
// and CPU cost can be compared on the same dictionary and query.
func oldOverlayFind(base, added *memEfficientEngine, text string) []string {
	baseState, addedState, end := 0, 0, 0
	matches := make([]overlayMatch, 0, 8)
	out := []string{}
	for _, ch := range text {
		end++
		matches = matches[:0]
		baseState = base.advance(baseState, ch)
		base.trie.out.emitChain(baseState, end, func(k string, start, end int) bool {
			matches = append(matches, overlayMatch{k, start, end, 0, len(matches)})
			return true
		})
		addedState = added.advance(addedState, ch)
		added.trie.out.emitChain(addedState, end, func(k string, start, end int) bool {
			matches = append(matches, overlayMatch{k, start, end, 1, len(matches)})
			return true
		})
		if len(matches) > 1 {
			slices.SortStableFunc(matches, overlayMatchOrder)
		}
		for _, m := range matches {
			out = append(out, m.keyword)
		}
	}
	return out
}
