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
	{"Ultimate", PresetUltimate},
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
