// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"fmt"
	"strings"
	"testing"
)

// benchKeywords builds n keywords with shared prefixes (keyword5 ⊂ keyword50),
// exercising failure-link traversal rather than a flat, disjoint alphabet.
func benchKeywords(n int) map[string]struct{} {
	m := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		m[fmt.Sprintf("keyword%d", i)] = struct{}{}
	}
	return m
}

const benchTextASCII = "the quick brown keyword50 fox keyword99 jumps over the lazy dog "

// benchTextMultibyte mixes ASCII and multibyte runes to measure the non-ASCII
// path (map lookups can't use the ASCII fast index).
const benchTextMultibyte = "빠른 갈색 keyword50 여우 🦊 keyword99 게으른 개를 뛰어넘다 "

var benchPresets = []struct {
	name   string
	preset Preset
}{
	{"Speed", PresetSpeed},
	{"Balanced", PresetBalanced},
	{"MemoryEfficient", PresetMemoryEfficient},
}

func benchmarkEngine(b *testing.B, findIndex bool) {
	texts := []struct {
		name string
		text string
	}{
		{"ascii", strings.Repeat(benchTextASCII, 40)},
		{"multibyte", strings.Repeat(benchTextMultibyte, 40)},
	}
	for _, n := range []int{100, 1000, 5000} {
		kws := benchKeywords(n)
		for _, bp := range benchPresets {
			e := New(bp.preset)
			e.Build(kws)
			for _, txt := range texts {
				b.Run(fmt.Sprintf("%dkw/%s/%s", n, bp.name, txt.name), func(b *testing.B) {
					b.ReportAllocs()
					b.SetBytes(int64(len(txt.text)))
					b.ResetTimer()
					if findIndex {
						for i := 0; i < b.N; i++ {
							_ = e.FindIndex(txt.text)
						}
					} else {
						for i := 0; i < b.N; i++ {
							_ = e.Find(txt.text)
						}
					}
				})
			}
		}
	}
}

func BenchmarkEngineFind(b *testing.B)      { benchmarkEngine(b, false) }
func BenchmarkEngineFindIndex(b *testing.B) { benchmarkEngine(b, true) }

// benchTextDense matches n distinct keywords twice each, putting the load on
// FindSet's dedup rather than the scan; the sparse texts above match only two.
func benchTextDense(n int) string {
	var sb strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&sb, "keyword%d keyword%d ", i, i)
	}
	return sb.String()
}

// BenchmarkEngineFindSetMillion prices FindSet at the scale the hash-map dedup
// exists for: past dedupHashMin the bitsets would cost ~262 KB zeroed per
// matching query, where the maps cost only per unique hit. MemoryEfficient is
// the preset documented for million-pattern dictionaries, so it is the one
// measured.
func BenchmarkEngineFindSetMillion(b *testing.B) {
	kws := make(map[string]struct{}, 1_000_000)
	for i := 0; i < 1_000_000; i++ {
		kws[fmt.Sprintf("keyword%d", i)] = struct{}{}
	}
	e := New(PresetMemoryEfficient)
	e.Build(kws)
	for _, txt := range []struct {
		name string
		text string
	}{
		{"1match", "the quick keyword500000 fox"},
		{"nomatch", "no hits in this text at all"},
	} {
		b.Run(txt.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(txt.text)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = e.FindSet(txt.text)
			}
		})
	}
}

// BenchmarkEngineFindSetSuffixNested keeps landing on the deepest state of a
// suffix-nested dictionary, so nearly every character re-reports a 500-keyword
// output chain. This is the case the collector's state dedup exists for:
// without it the chain is rewalked per character (measured 40x on
// Speed/Balanced).
func BenchmarkEngineFindSetSuffixNested(b *testing.B) {
	kws := make(map[string]struct{})
	kw := ""
	for i := 0; i < 500; i++ {
		kw += "a"
		kws[kw] = struct{}{}
	}
	text := strings.Repeat("a", 100_000)
	for _, bp := range benchPresets {
		e := New(bp.preset)
		e.Build(kws)
		b.Run(bp.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(text)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = e.FindSet(text)
			}
		})
	}
}

func BenchmarkEngineFindSet(b *testing.B) {
	for _, n := range []int{100, 1000} {
		kws := benchKeywords(n)
		texts := []struct {
			name string
			text string
		}{
			{"sparse", strings.Repeat(benchTextASCII, 40)},
			{"dense", benchTextDense(n)},
		}
		for _, bp := range benchPresets {
			e := New(bp.preset)
			e.Build(kws)
			for _, txt := range texts {
				b.Run(fmt.Sprintf("%dkw/%s/%s", n, bp.name, txt.name), func(b *testing.B) {
					b.ReportAllocs()
					b.SetBytes(int64(len(txt.text)))
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						_ = e.FindSet(txt.text)
					}
				})
			}
		}
	}
}
