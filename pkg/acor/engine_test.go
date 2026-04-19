// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"fmt"
	"sort"
	"sync"
	"testing"
)

func allPresets() []Preset {
	return []Preset{PresetSpeed, PresetBalanced, PresetMemoryEfficient, PresetUltimate}
}

func createEngine(t *testing.T, preset Preset) *InMemoryAC {
	t.Helper()
	ac := NewInMemory(&InMemoryOptions{Preset: preset})
	if ac == nil {
		t.Fatalf("NewInMemory returned nil for preset %v", preset)
	}
	return ac
}

func TestInMemoryAdd(t *testing.T) {
	for _, preset := range allPresets() {
		t.Run(preset.String(), func(t *testing.T) {
			ac := createEngine(t, preset)

			added := ac.Add("hello")
			if added != 1 {
				t.Errorf("expected 1, got %d", added)
			}

			added = ac.Add("hello")
			if added != 0 {
				t.Errorf("expected 0 for duplicate, got %d", added)
			}

			added = ac.Add("")
			if added != 0 {
				t.Errorf("expected 0 for empty, got %d", added)
			}

			added = ac.Add("   ")
			if added != 0 {
				t.Errorf("expected 0 for whitespace, got %d", added)
			}
		})
	}
}

func TestInMemoryRemove(t *testing.T) {
	for _, preset := range allPresets() {
		t.Run(preset.String(), func(t *testing.T) {
			ac := createEngine(t, preset)
			ac.Add("hello")
			ac.Add("world")

			removed := ac.Remove("hello")
			if removed != 1 {
				t.Errorf("expected 1, got %d", removed)
			}

			removed = ac.Remove("hello")
			if removed != 0 {
				t.Errorf("expected 0 for non-existent, got %d", removed)
			}

			matches := ac.Find("hello world")
			for _, m := range matches {
				if m == testKeywordHello {
					t.Errorf("found removed keyword 'hello'")
				}
			}
		})
	}
}

func TestInMemoryFind(t *testing.T) {
	testCases := []struct {
		name     string
		keywords []string
		text     string
		expected []string
	}{
		{"single match", []string{"hello"}, "say hello world", []string{"hello"}},
		{"multiple matches", []string{"he", "she", "his", "hers"}, "ushers", []string{"he", "she", "hers"}},
		{"no match", []string{"abc", "def"}, "xyz", nil},
		{"overlapping", []string{"he", "her", "hers"}, "hers", []string{"he", "her", "hers"}},
		{"empty text", []string{"hello"}, "", nil},
		{"keyword at start", []string{"hello"}, "hello world", []string{"hello"}},
		{"keyword at end", []string{"world"}, "hello world", []string{"world"}},
		{"repeated matches", []string{"ab"}, "ababab", []string{"ab", "ab", "ab"}},
	}

	for _, preset := range allPresets() {
		t.Run(preset.String(), func(t *testing.T) {
			for _, tc := range testCases {
				t.Run(tc.name, func(t *testing.T) {
					ac := createEngine(t, preset)
					for _, kw := range tc.keywords {
						ac.Add(kw)
					}
					matches := ac.Find(tc.text)
					if !equalUnordered(matches, tc.expected) {
						t.Errorf("expected %v, got %v", tc.expected, matches)
					}
				})
			}
		})
	}
}

func TestInMemoryFindIndex(t *testing.T) {
	for _, preset := range allPresets() {
		t.Run(preset.String(), func(t *testing.T) {
			ac := createEngine(t, preset)
			ac.Add("he")
			ac.Add("she")
			ac.Add("hers")

			indexed := ac.FindIndex("ushers")

			if indices, ok := indexed["he"]; !ok {
				t.Error("missing 'he'")
			} else if len(indices) != 1 || indices[0] != 2 {
				t.Errorf("'he' expected [2], got %v", indices)
			}

			if indices, ok := indexed["she"]; !ok {
				t.Error("missing 'she'")
			} else if len(indices) != 1 || indices[0] != 1 {
				t.Errorf("'she' expected [1], got %v", indices)
			}

			if indices, ok := indexed["hers"]; !ok {
				t.Error("missing 'hers'")
			} else if len(indices) != 1 || indices[0] != 2 {
				t.Errorf("'hers' expected [2], got %v", indices)
			}
		})
	}
}

func TestInMemoryFlush(t *testing.T) {
	for _, preset := range allPresets() {
		t.Run(preset.String(), func(t *testing.T) {
			ac := createEngine(t, preset)
			ac.Add("hello")
			ac.Add("world")
			ac.Flush()

			if matches := ac.Find("hello world"); len(matches) != 0 {
				t.Errorf("expected empty after flush, got %v", matches)
			}

			info := ac.Info()
			if info.Keywords != 0 {
				t.Errorf("expected 0 keywords after flush, got %d", info.Keywords)
			}
		})
	}
}

func TestInMemoryInfo(t *testing.T) {
	for _, preset := range allPresets() {
		t.Run(preset.String(), func(t *testing.T) {
			ac := createEngine(t, preset)
			info := ac.Info()
			if info.Keywords != 0 {
				t.Errorf("expected 0 keywords, got %d", info.Keywords)
			}
			if info.Preset != preset {
				t.Errorf("expected preset %v, got %v", preset, info.Preset)
			}

			ac.Add("ab")
			ac.Add("abc")
			info = ac.Info()
			if info.Keywords != 2 {
				t.Errorf("expected 2 keywords, got %d", info.Keywords)
			}
		})
	}
}

func TestInMemoryCaseSensitive(t *testing.T) {
	for _, preset := range allPresets() {
		t.Run(preset.String(), func(t *testing.T) {
			ac := NewInMemory(&InMemoryOptions{Preset: preset})
			ac.Add("Hello")
			if matches := ac.Find("say HELLO world"); len(matches) == 0 {
				t.Error("expected match in case-insensitive mode")
			}

			ac = NewInMemory(&InMemoryOptions{Preset: preset, CaseSensitive: true})
			ac.Add("Hello")
			if matches := ac.Find("say HELLO world"); len(matches) != 0 {
				t.Error("expected no match in case-sensitive mode")
			}
			if matches := ac.Find("say Hello world"); len(matches) == 0 {
				t.Error("expected match for exact case in case-sensitive mode")
			}
		})
	}
}

func TestInMemoryEmpty(t *testing.T) {
	for _, preset := range allPresets() {
		t.Run(preset.String(), func(t *testing.T) {
			ac := createEngine(t, preset)
			if matches := ac.Find("anything"); len(matches) != 0 {
				t.Errorf("expected no matches, got %v", matches)
			}
			if indexed := ac.FindIndex("anything"); len(indexed) != 0 {
				t.Errorf("expected no indexed matches, got %v", indexed)
			}
			if ac.Remove("nonexistent") != 0 {
				t.Error("expected 0 for remove on empty engine")
			}
		})
	}
}

func TestInMemoryPresetMethod(t *testing.T) {
	ac := NewInMemory(&InMemoryOptions{Preset: PresetSpeed})
	if ac.Preset() != PresetSpeed {
		t.Errorf("expected PresetSpeed, got %v", ac.Preset())
	}
	ac = NewInMemory(nil)
	if ac.Preset() != PresetBalanced {
		t.Errorf("expected default PresetBalanced, got %v", ac.Preset())
	}
}

func TestInMemoryUnicode(t *testing.T) {
	for _, preset := range allPresets() {
		t.Run(preset.String(), func(t *testing.T) {
			ac := createEngine(t, preset)
			ac.Add("한국어")
			ac.Add("어")

			matches := ac.Find("안녕하세요 한국어입니다")
			if len(matches) == 0 {
				t.Error("expected Korean keyword match")
			}
			found := false
			for _, m := range matches {
				if m == "한국어" {
					found = true
				}
			}
			if !found {
				t.Errorf("expected '한국어' in %v", matches)
			}
		})
	}
}

func TestInMemoryConcurrentFind(t *testing.T) {
	for _, preset := range allPresets() {
		t.Run(preset.String(), func(t *testing.T) {
			ac := createEngine(t, preset)
			for i := 0; i < 100; i++ {
				ac.Add(fmt.Sprintf("keyword%d", i))
			}
			text := "keyword50 is here and keyword25 too"
			var wg sync.WaitGroup

			for i := 0; i < 10; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					if matches := ac.Find(text); len(matches) == 0 {
						t.Error("expected matches in concurrent read")
					}
				}()
			}
			for i := 0; i < 5; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					ac.Add(fmt.Sprintf("concurrent%d", i))
					ac.Remove(fmt.Sprintf("concurrent%d", i))
				}(i)
			}
			wg.Wait()
		})
	}
}

func TestSameAPIAcrossPresets(t *testing.T) {
	keywords := []string{"he", "she", "his", "hers", "hello", "world", "benchmark"}
	text := benchmarkInputText

	var expected []string
	{
		ac := NewInMemory(&InMemoryOptions{Preset: PresetBalanced})
		for _, kw := range keywords {
			ac.Add(kw)
		}
		expected = ac.Find(text)
	}

	for _, preset := range allPresets() {
		t.Run(preset.String(), func(t *testing.T) {
			ac := NewInMemory(&InMemoryOptions{Preset: preset})
			for _, kw := range keywords {
				ac.Add(kw)
			}
			matches := ac.Find(text)
			if !equalUnordered(matches, expected) {
				t.Errorf("preset %v: expected %v, got %v", preset, expected, matches)
			}
		})
	}
}

func BenchmarkInMemoryFindSpeed(b *testing.B) {
	ac := NewInMemory(&InMemoryOptions{Preset: PresetSpeed})
	for _, kw := range []string{"he", "she", "his", "hers", "hello", "world", "benchmark"} {
		ac.Add(kw)
	}
	text := benchmarkInputText
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ac.Find(text)
	}
}

func BenchmarkInMemoryFindBalanced(b *testing.B) {
	ac := NewInMemory(&InMemoryOptions{Preset: PresetBalanced})
	for _, kw := range []string{"he", "she", "his", "hers", "hello", "world", "benchmark"} {
		ac.Add(kw)
	}
	text := benchmarkInputText
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ac.Find(text)
	}
}

func BenchmarkInMemoryFindMemoryEfficient(b *testing.B) {
	ac := NewInMemory(&InMemoryOptions{Preset: PresetMemoryEfficient})
	for _, kw := range []string{"he", "she", "his", "hers", "hello", "world", "benchmark"} {
		ac.Add(kw)
	}
	text := benchmarkInputText
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ac.Find(text)
	}
}

func BenchmarkInMemoryFindUltimate(b *testing.B) {
	ac := NewInMemory(&InMemoryOptions{Preset: PresetUltimate})
	for _, kw := range []string{"he", "she", "his", "hers", "hello", "world", "benchmark"} {
		ac.Add(kw)
	}
	text := benchmarkInputText
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ac.Find(text)
	}
}

func BenchmarkInMemoryFindManyKeywords(b *testing.B) {
	keywords := make([]string, 1000)
	for i := range keywords {
		keywords[i] = fmt.Sprintf("keyword%d", i)
	}
	for _, preset := range allPresets() {
		b.Run(preset.String(), func(b *testing.B) {
			ac := NewInMemory(&InMemoryOptions{Preset: preset})
			for _, kw := range keywords {
				ac.Add(kw)
			}
			text := "keyword500 keyword250 keyword750"
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				ac.Find(text)
			}
		})
	}
}

func BenchmarkInMemoryAdd(b *testing.B) {
	for _, preset := range allPresets() {
		b.Run(preset.String(), func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				ac := NewInMemory(&InMemoryOptions{Preset: preset})
				for j := 0; j < 100; j++ {
					ac.Add(fmt.Sprintf("keyword%d", j))
				}
			}
		})
	}
}

func equalUnordered(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	sa := make([]string, len(a))
	sb := make([]string, len(b))
	copy(sa, a)
	copy(sb, b)
	sort.Strings(sa)
	sort.Strings(sb)
	for i := range sa {
		if sa[i] != sb[i] {
			return false
		}
	}
	return true
}
