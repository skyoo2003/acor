// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"fmt"
	"strings"
	"testing"

	miniredis "github.com/alicebob/miniredis/v2"
)

//nolint:errcheck,gosec
const (
	rbBenchInputText      = "ushers hello world benchmark test"
	rbBenchManyKwText     = "keyword500 keyword250 keyword750"
	rbBenchUnicodeText    = "안녕하세요 한글입니다 일본어テスト 中文입니다"
	rbBenchConcurrentText = "keyword50 keyword25 keyword75"
	rbBenchStaleReadText  = "hello world hello"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func benchKeywords(n int) []string {
	kws := make([]string, n)
	for i := range kws {
		kws[i] = fmt.Sprintf("keyword%d", i)
	}
	return kws
}

func benchText(n int) string {
	return strings.Repeat("keyword50 keyword25 keyword750 ", n)
}

func newBenchRedisBacked(t testing.TB, preset Preset) (*RedisBackedAC, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	ac, err := NewRedisBacked(context.Background(), &RedisBackedArgs{
		AhoCorasickArgs: AhoCorasickArgs{Addr: mr.Addr(), Name: t.Name()},
		Preset:          preset,
	})
	if err != nil {
		t.Fatal(err)
	}
	return ac, mr
}

// ---------------------------------------------------------------------------
// 1. Per-Feature Unit Benchmarks — InMemory engine internals
// ---------------------------------------------------------------------------

func BenchmarkEngineFind(b *testing.B) {
	keywords := []string{"he", "she", "his", "hers", "hello", "world", "benchmark"}

	for _, preset := range allPresets() {
		b.Run(preset.String(), func(b *testing.B) {
			ac := NewInMemory(&InMemoryOptions{Preset: preset})
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
			ac := NewInMemory(&InMemoryOptions{Preset: preset})
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
				ac := NewInMemory(&InMemoryOptions{Preset: preset})
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

func BenchmarkEngineFindLargeText(b *testing.B) {
	keywords := benchKeywords(100)
	for _, n := range []int{1, 10, 100, 1000} {
		text := benchText(n)

		for _, preset := range allPresets() {
			b.Run(fmt.Sprintf("%s/%drep", preset.String(), n), func(b *testing.B) {
				ac := NewInMemory(&InMemoryOptions{Preset: preset})
				for _, kw := range keywords {
					ac.Add(kw)
				}
				b.ResetTimer()
				for range b.N {
					ac.Find(text)
				}
			})
		}
	}
}

func BenchmarkEngineAdd(b *testing.B) {
	for _, preset := range allPresets() {
		b.Run(preset.String(), func(b *testing.B) {
			b.ResetTimer()
			for range b.N {
				ac := NewInMemory(&InMemoryOptions{Preset: preset})
				for j := range 100 {
					ac.Add(fmt.Sprintf("keyword%d", j))
				}
			}
		})
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

// ---------------------------------------------------------------------------
// 2. E2E Benchmarks — RedisBackedAC full lifecycle (miniredis)
// ---------------------------------------------------------------------------

func BenchmarkRedisBackedFind(b *testing.B) {
	keywords := []string{"he", "she", "his", "hers", "hello", "world", "benchmark"}

	for _, preset := range allPresets() {
		b.Run(preset.String(), func(b *testing.B) {
			ac, _ := newBenchRedisBacked(b, preset)
			ctx := context.Background()
			for _, kw := range keywords {
				ac.Add(ctx, kw) //nolint:errcheck,gosec
			}
			b.ResetTimer()
			for range b.N {
				ac.Find(ctx, rbBenchInputText) //nolint:errcheck,gosec
			}
		})
	}
}

func BenchmarkRedisBackedFindIndex(b *testing.B) {
	keywords := []string{"he", "she", "his", "hers", "hello", "world"}

	for _, preset := range allPresets() {
		b.Run(preset.String(), func(b *testing.B) {
			ac, _ := newBenchRedisBacked(b, preset)
			ctx := context.Background()
			for _, kw := range keywords {
				ac.Add(ctx, kw) //nolint:errcheck,gosec
			}
			b.ResetTimer()
			for range b.N {
				ac.FindIndex(ctx, rbBenchInputText) //nolint:errcheck,gosec
			}
		})
	}
}

func BenchmarkRedisBackedAdd(b *testing.B) {
	for _, preset := range allPresets() {
		b.Run(preset.String(), func(b *testing.B) {
			mr := miniredis.RunT(b)
			ctx := context.Background()

			b.ResetTimer()
			for range b.N {
				ac, err := NewRedisBacked(ctx, &RedisBackedArgs{
					AhoCorasickArgs: AhoCorasickArgs{Addr: mr.Addr(), Name: "bench-add"},
					Preset:          preset,
				})
				if err != nil {
					b.Fatal(err)
				}
				for j := range 100 {
					ac.Add(ctx, fmt.Sprintf("keyword%d", j)) //nolint:errcheck,gosec
				}
				ac.Close() //nolint:errcheck,gosec
			}
		})
	}
}

func BenchmarkRedisBackedRemove(b *testing.B) {
	for _, preset := range allPresets() {
		b.Run(preset.String(), func(b *testing.B) {
			ac, _ := newBenchRedisBacked(b, preset)
			ctx := context.Background()
			for j := range 100 {
				ac.Add(ctx, fmt.Sprintf("keyword%d", j)) //nolint:errcheck,gosec
			}
			b.ResetTimer()
			for i := range b.N {
				ac.Remove(ctx, fmt.Sprintf("keyword%d", i%100)) //nolint:errcheck,gosec
			}
		})
	}
}

func BenchmarkRedisBackedFlush(b *testing.B) {
	for _, preset := range allPresets() {
		b.Run(preset.String(), func(b *testing.B) {
			ac, _ := newBenchRedisBacked(b, preset)
			ctx := context.Background()
			for j := range 100 {
				ac.Add(ctx, fmt.Sprintf("keyword%d", j)) //nolint:errcheck,gosec
			}
			b.ResetTimer()
			for range b.N {
				for j := range 100 {
					ac.Add(ctx, fmt.Sprintf("keyword%d", j)) //nolint:errcheck,gosec
				}
				ac.Flush(ctx) //nolint:errcheck,gosec
			}
		})
	}
}

func BenchmarkRedisBackedE2E(b *testing.B) {
	for _, preset := range allPresets() {
		b.Run(preset.String(), func(b *testing.B) {
			ac, _ := newBenchRedisBacked(b, preset)
			ctx := context.Background()
			b.ResetTimer()
			for i := range b.N {
				kw := fmt.Sprintf("e2e_keyword%d", i%100)
				ac.Add(ctx, kw)                                             //nolint:errcheck,gosec
				ac.Find(ctx, fmt.Sprintf("some text with %s embedded", kw)) //nolint:errcheck,gosec
				ac.Remove(ctx, kw)                                          //nolint:errcheck,gosec
			}
		})
	}
}

func BenchmarkRedisBackedStaleRead(b *testing.B) {
	ac, mr := newBenchRedisBacked(b, PresetBalanced)
	ctx := context.Background()
	ac.Add(ctx, "hello") //nolint:errcheck,gosec
	mr.Close()

	b.ResetTimer()
	for range b.N {
		ac.Find(ctx, rbBenchStaleReadText) //nolint:errcheck,gosec
	}
}

func BenchmarkRedisBackedReload(b *testing.B) {
	ac, _ := newBenchRedisBacked(b, PresetBalanced)
	ctx := context.Background()
	for j := range 100 {
		ac.Add(ctx, fmt.Sprintf("keyword%d", j)) //nolint:errcheck,gosec
	}

	b.ResetTimer()
	for range b.N {
		ac.markStale()
		ac.ensureValid(ctx) //nolint:errcheck,gosec
	}
}

// ---------------------------------------------------------------------------
// 3. Scale Benchmarks — many keywords, large texts
// ---------------------------------------------------------------------------

func BenchmarkRedisBackedFindManyKeywords(b *testing.B) {
	for _, n := range []int{100, 1000, 5000} {
		kws := benchKeywords(n)

		for _, preset := range allPresets() {
			b.Run(fmt.Sprintf("%s/%dkw", preset.String(), n/1000), func(b *testing.B) {
				ac, _ := newBenchRedisBacked(b, preset)
				ctx := context.Background()
				for _, kw := range kws {
					ac.Add(ctx, kw) //nolint:errcheck,gosec
				}
				b.ResetTimer()
				for range b.N {
					ac.Find(ctx, rbBenchManyKwText) //nolint:errcheck,gosec
				}
			})
		}
	}
}

func BenchmarkRedisBackedFindLargeText(b *testing.B) {
	keywords := benchKeywords(100)
	for _, n := range []int{1, 10, 100, 1000} {
		text := benchText(n)

		for _, preset := range allPresets() {
			b.Run(fmt.Sprintf("%s/%drep", preset.String(), n), func(b *testing.B) {
				ac, _ := newBenchRedisBacked(b, preset)
				ctx := context.Background()
				for _, kw := range keywords {
					ac.Add(ctx, kw) //nolint:errcheck,gosec
				}
				b.ResetTimer()
				for range b.N {
					ac.Find(ctx, text) //nolint:errcheck,gosec
				}
			})
		}
	}
}

func BenchmarkRedisBackedConcurrentFind(b *testing.B) {
	for _, preset := range allPresets() {
		b.Run(preset.String(), func(b *testing.B) {
			ac, _ := newBenchRedisBacked(b, preset)
			ctx := context.Background()
			for j := range 100 {
				ac.Add(ctx, fmt.Sprintf("keyword%d", j)) //nolint:errcheck,gosec
			}

			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					ac.Find(ctx, rbBenchConcurrentText) //nolint:errcheck,gosec
				}
			})
		})
	}
}

// ---------------------------------------------------------------------------
// 4. Comparison — InMemory vs RedisBacked
// ---------------------------------------------------------------------------

func BenchmarkCompareFind(b *testing.B) {
	keywords := []string{"he", "she", "his", "hers", "hello", "world", "benchmark"}

	b.Run("InMemory/Balanced", func(b *testing.B) {
		ac := NewInMemory(&InMemoryOptions{Preset: PresetBalanced})
		for _, kw := range keywords {
			ac.Add(kw)
		}
		b.ResetTimer()
		for range b.N {
			ac.Find(rbBenchInputText)
		}
	})

	b.Run("RedisBacked/Balanced", func(b *testing.B) {
		ac, _ := newBenchRedisBacked(b, PresetBalanced)
		ctx := context.Background()
		for _, kw := range keywords {
			ac.Add(ctx, kw) //nolint:errcheck,gosec
		}
		b.ResetTimer()
		for range b.N {
			ac.Find(ctx, rbBenchInputText) //nolint:errcheck,gosec
		}
	})
}

func BenchmarkCompareFindMany(b *testing.B) {
	for _, n := range []int{100, 1000, 5000} {
		kws := benchKeywords(n)

		b.Run(fmt.Sprintf("InMemory/Balanced/%dkw", n/1000), func(b *testing.B) {
			ac := NewInMemory(&InMemoryOptions{Preset: PresetBalanced})
			for _, kw := range kws {
				ac.Add(kw)
			}
			b.ResetTimer()
			for range b.N {
				ac.Find(rbBenchManyKwText)
			}
		})

		b.Run(fmt.Sprintf("RedisBacked/Balanced/%dkw", n/1000), func(b *testing.B) {
			ac, _ := newBenchRedisBacked(b, PresetBalanced)
			ctx := context.Background()
			for _, kw := range kws {
				ac.Add(ctx, kw) //nolint:errcheck,gosec
			}
			b.ResetTimer()
			for range b.N {
				ac.Find(ctx, rbBenchManyKwText) //nolint:errcheck,gosec
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 5. Unicode benchmarks
// ---------------------------------------------------------------------------

func BenchmarkEngineFindUnicode(b *testing.B) {
	keywords := []string{"한글", "일본어", "中文", "テスト"}

	for _, preset := range allPresets() {
		b.Run(preset.String(), func(b *testing.B) {
			ac := NewInMemory(&InMemoryOptions{Preset: preset})
			for _, kw := range keywords {
				ac.Add(kw)
			}
			b.ResetTimer()
			for range b.N {
				ac.Find(rbBenchUnicodeText)
			}
		})
	}
}

func BenchmarkRedisBackedFindUnicode(b *testing.B) {
	keywords := []string{"한글", "일본어", "中文", "テスト"}

	for _, preset := range allPresets() {
		b.Run(preset.String(), func(b *testing.B) {
			ac, _ := newBenchRedisBacked(b, preset)
			ctx := context.Background()
			for _, kw := range keywords {
				ac.Add(ctx, kw) //nolint:errcheck,gosec
			}
			b.ResetTimer()
			for range b.N {
				ac.Find(ctx, rbBenchUnicodeText) //nolint:errcheck,gosec
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 6. Memory profiling (use with: go test -bench=BenchmarkMemoryProfile -memprofile=mem.prof)
// ---------------------------------------------------------------------------

func BenchmarkMemoryProfile(b *testing.B) {
	for _, preset := range allPresets() {
		b.Run(fmt.Sprintf("%s/10kw", preset.String()), func(b *testing.B) {
			ac := NewInMemory(&InMemoryOptions{Preset: preset})
			for _, kw := range benchKeywords(10000) {
				ac.Add(kw)
			}
			text := benchText(10)
			b.ResetTimer()
			for range b.N {
				ac.Find(text)
			}
		})
	}
}
