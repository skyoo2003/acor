// SPDX-License-Identifier: Apache-2.0

package acor //nolint:errcheck,gosec

import (
	"fmt"
	"testing"

	miniredis "github.com/alicebob/miniredis/v2"
)

const (
	rbBenchInputText   = "ushers hello world benchmark test"
	rbBenchManyKwText  = "keyword500 keyword250 keyword750"
	rbBenchUnicodeText = "안녕하세요 한글입니다 일본어テスト 中文입니다"
)

func benchKeywords(n int) []string {
	kws := make([]string, n)
	for i := range kws {
		kws[i] = fmt.Sprintf("keyword%d", i)
	}
	return kws
}

func newBenchPreset(t testing.TB, preset Preset) *AhoCorasick {
	t.Helper()
	mr := miniredis.RunT(t)
	ac, err := Create(&AhoCorasickArgs{Addr: mr.Addr(), Name: t.Name(), Preset: preset})
	if err != nil {
		t.Fatal(err)
	}
	return ac
}

func BenchmarkEngineFind(b *testing.B) {
	keywords := []string{"he", "she", "his", "hers", "hello", "world", "benchmark"}
	for _, preset := range allPresets() {
		b.Run(preset.String(), func(b *testing.B) {
			ac := newBenchPreset(b, preset)
			for _, kw := range keywords {
				ac.Add(kw)
			}
			b.ResetTimer()
			for range b.N {
				ac.Find(rbBenchInputText)
			}
		})
	}
}

func BenchmarkEngineFindIndex(b *testing.B) {
	keywords := []string{"he", "she", "his", "hers", "hello", "world"}
	for _, preset := range allPresets() {
		b.Run(preset.String(), func(b *testing.B) {
			ac := newBenchPreset(b, preset)
			for _, kw := range keywords {
				ac.Add(kw)
			}
			b.ResetTimer()
			for range b.N {
				ac.FindIndex(rbBenchInputText)
			}
		})
	}
}

func BenchmarkEngineFindManyKeywords(b *testing.B) {
	for _, n := range []int{100, 1000, 5000} {
		kws := benchKeywords(n)
		for _, preset := range allPresets() {
			b.Run(fmt.Sprintf("%s/%dkw", preset.String(), n/1000), func(b *testing.B) {
				ac := newBenchPreset(b, preset)
				for _, kw := range kws {
					ac.Add(kw)
				}
				b.ResetTimer()
				for range b.N {
					ac.Find(rbBenchManyKwText)
				}
			})
		}
	}
}

func BenchmarkEngineBuild(b *testing.B) {
	for _, n := range []int{100, 1000, 5000, 10000} {
		kws := benchKeywords(n)
		kwSet := make(map[string]struct{}, len(kws))
		for _, kw := range kws {
			kwSet[kw] = struct{}{}
		}
		for _, preset := range allPresets() {
			b.Run(fmt.Sprintf("%s/%dkw", preset.String(), n/1000), func(b *testing.B) {
				b.ResetTimer()
				for range b.N {
					eng := newMatchEngine(preset)
					eng.buildFromKeywords(kwSet)
				}
			})
		}
	}
}

func BenchmarkPresetFind(b *testing.B) {
	keywords := []string{"he", "she", "his", "hers", "hello", "world", "benchmark"}
	for _, preset := range allPresets() {
		b.Run(preset.String(), func(b *testing.B) {
			ac := newBenchPreset(b, preset)
			for _, kw := range keywords {
				ac.Add(kw)
			}
			b.ResetTimer()
			for range b.N {
				ac.Find(rbBenchInputText)
			}
		})
	}
}

func BenchmarkPresetFindIndex(b *testing.B) {
	keywords := []string{"he", "she", "his", "hers", "hello", "world"}
	for _, preset := range allPresets() {
		b.Run(preset.String(), func(b *testing.B) {
			ac := newBenchPreset(b, preset)
			for _, kw := range keywords {
				ac.Add(kw)
			}
			b.ResetTimer()
			for range b.N {
				ac.FindIndex(rbBenchInputText)
			}
		})
	}
}

func BenchmarkPresetCreateAddClose(b *testing.B) {
	for _, preset := range allPresets() {
		b.Run(preset.String(), func(b *testing.B) {
			mr := miniredis.RunT(b)
			b.ResetTimer()
			for range b.N {
				ac, err := Create(&AhoCorasickArgs{Addr: mr.Addr(), Name: "bench-add", Preset: preset})
				if err != nil {
					b.Fatal(err)
				}
				for j := range 100 {
					ac.Add(fmt.Sprintf("keyword%d", j))
				}
				ac.Close()
			}
		})
	}
}

func BenchmarkPresetRemove(b *testing.B) {
	for _, preset := range allPresets() {
		b.Run(preset.String(), func(b *testing.B) {
			ac := newBenchPreset(b, preset)
			for i := 0; i < 100; i++ {
				ac.Add(fmt.Sprintf("keyword%d", i))
			}
			b.ResetTimer()
			for range b.N {
				ac.Remove("keyword50")
				ac.Add("keyword50")
			}
		})
	}
}

func BenchmarkPresetManyKeywords(b *testing.B) {
	for _, n := range []int{100, 1000, 5000} {
		for _, preset := range allPresets() {
			b.Run(fmt.Sprintf("%s/%dkw", preset.String(), n/1000), func(b *testing.B) {
				ac := newBenchPreset(b, preset)
				kws := benchKeywords(n)
				for _, kw := range kws {
					ac.Add(kw)
				}
				b.ResetTimer()
				for range b.N {
					ac.Find(rbBenchManyKwText)
				}
			})
		}
	}
}

func BenchmarkPresetUnicode(b *testing.B) {
	for _, preset := range allPresets() {
		b.Run(preset.String(), func(b *testing.B) {
			ac := newBenchPreset(b, preset)
			ac.Add("한글")
			ac.Add("일본어")
			b.ResetTimer()
			for range b.N {
				ac.Find(rbBenchUnicodeText)
			}
		})
	}
}

func BenchmarkMemoryUsage(b *testing.B) {
	keywords := benchKeywords(1000)
	for _, preset := range allPresets() {
		b.Run(preset.String(), func(b *testing.B) {
			b.ReportAllocs()
			ac := newBenchPreset(b, preset)
			for _, kw := range keywords {
				ac.Add(kw)
			}
			ac.Find(rbBenchInputText)
		})
	}
}
