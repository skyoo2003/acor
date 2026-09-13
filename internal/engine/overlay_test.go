// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"reflect"
	"strings"
	"testing"
)

func TestOverlayMatchesRebuild(t *testing.T) {
	base := New(PresetMemoryEfficient)
	base.Build(map[string]struct{}{"he": {}, "she": {}, "hers": {}, "한국": {}, "old": {}})
	added := New(PresetMemoryEfficient)
	added.Build(map[string]struct{}{"her": {}, "한국어": {}})
	overlay := Overlay(base, added, map[string]struct{}{"old": {}})
	full := New(PresetMemoryEfficient)
	full.Build(map[string]struct{}{"he": {}, "she": {}, "hers": {}, "한국": {}, "her": {}, "한국어": {}})
	text := strings.Repeat("ushers old 한국어 ", 300)
	if got, want := overlay.Find(text), full.Find(text); !reflect.DeepEqual(got, want) {
		t.Fatal("find differs")
	}
	if got, want := overlay.FindIndex(text), full.FindIndex(text); !reflect.DeepEqual(got, want) {
		t.Fatal("index differs")
	}
	if got, want := overlay.FindSet(text), full.FindSet(text); !reflect.DeepEqual(got, want) {
		t.Fatal("set differs")
	}
	collect := func(e *Engine) []string {
		var out []string
		runes := []rune(text)
		i := 0
		e.Stream(func() (rune, bool) {
			if i == len(runes) {
				return 0, false
			}
			r := runes[i]
			i++
			return r, true
		},
			func(k string, start, end int) bool { out = append(out, k); return true })
		return out
	}
	if got, want := collect(overlay), collect(full); !reflect.DeepEqual(got, want) {
		t.Fatal("stream differs")
	}
}

func TestOverlayStopsAtFirstMatch(t *testing.T) {
	base := New(PresetMemoryEfficient)
	base.Build(map[string]struct{}{"a": {}, "aa": {}})
	added := New(PresetMemoryEfficient)
	added.Build(map[string]struct{}{"aaa": {}})
	overlay := Overlay(base, added, nil)
	text := strings.Repeat("a", 100000)
	called := 0
	overlay.MatchString(text, func(k string, start, end int) bool {
		called++
		return false
	})
	if called != 1 {
		t.Fatalf("callback called %d times", called)
	}
	read := 0
	overlay.Stream(func() (rune, bool) {
		read++
		if read > 1 {
			t.Fatal("stream read beyond first match")
		}
		return 'a', true
	}, func(string, int, int) bool { return false })
	if read != 1 {
		t.Fatalf("stream read %d runes", read)
	}
}

func TestOverlayMergedOutputOrderAndSpans(t *testing.T) {
	base := New(PresetMemoryEfficient)
	base.Build(map[string]struct{}{"a": {}, "aa": {}, "aaa": {}, "ba": {}, "old": {}, "한국": {}})
	added := New(PresetMemoryEfficient)
	added.Build(map[string]struct{}{"aaaa": {}, "baa": {}, "한국어": {}})
	overlay := Overlay(base, added, map[string]struct{}{"old": {}, "aa": {}})
	full := New(PresetMemoryEfficient)
	full.Build(map[string]struct{}{"a": {}, "aaa": {}, "ba": {}, "한국": {}, "aaaa": {}, "baa": {}, "한국어": {}})
	for _, text := range []string{"baaaa old 한국어 baaaa", "aaaaa", "nothing", "한국어 aaaa"} {
		collect := func(e *Engine, stream bool) [][3]any {
			var matches [][3]any
			emit := func(k string, start, end int) bool {
				matches = append(matches, [3]any{k, start, end})
				return true
			}
			if stream {
				runes := []rune(text)
				i := 0
				e.Stream(func() (rune, bool) {
					if i == len(runes) {
						return 0, false
					}
					r := runes[i]
					i++
					return r, true
				}, emit)
			} else {
				e.MatchString(text, emit)
			}
			return matches
		}
		for _, stream := range []bool{false, true} {
			if got, want := collect(overlay, stream), collect(full, stream); !reflect.DeepEqual(got, want) {
				t.Fatalf("text=%q stream=%t: got %v, want %v", text, stream, got, want)
			}
		}
		if got, want := overlay.Find(text), full.Find(text); !reflect.DeepEqual(got, want) {
			t.Fatalf("Find(%q): got %v, want %v", text, got, want)
		}
		if got, want := overlay.FindSet(text), full.FindSet(text); !reflect.DeepEqual(got, want) {
			t.Fatalf("FindSet(%q): got %v, want %v", text, got, want)
		}
		if got, want := overlay.FindIndex(text), full.FindIndex(text); !reflect.DeepEqual(got, want) {
			t.Fatalf("FindIndex(%q): got %v, want %v", text, got, want)
		}
		if got, want := overlay.Contains(text), full.Contains(text); got != want {
			t.Fatalf("Contains(%q): got %t, want %t", text, got, want)
		}
	}
}

func TestOverlayLiteralCandidatesMatchRebuild(t *testing.T) {
	base := New(PresetMemoryEfficient)
	base.Build(map[string]struct{}{
		"delta": {}, "change": {}, "-001": {}, "old": {}, "한국": {},
	})
	added := New(PresetMemoryEfficient)
	added.Build(map[string]struct{}{
		"delta-change": {}, "delta-change-001": {}, "delta-change-002": {},
	})
	overlay := Overlay(base, added, map[string]struct{}{"old": {}})
	if overlay.impl.(*overlayEngine).literal == nil {
		t.Fatal("expected literal candidate path")
	}
	full := New(PresetMemoryEfficient)
	full.Build(map[string]struct{}{
		"delta": {}, "change": {}, "-001": {}, "한국": {},
		"delta-change": {}, "delta-change-001": {}, "delta-change-002": {},
	})
	for _, text := range []string{
		"한국 delta-change-001 old delta-change-002 delta-change",
		"delta-change-001delta-change-002", "delta-change-003", "absent",
		"\xffdelta-change-001\xff", // Invalid input must retain rune offsets.
		strings.Repeat("delta-change-001 ", 20),
		strings.Repeat("delta-change-001 ", 300), // Long inputs use the automata.
	} {
		collect := func(e *Engine) [][3]any {
			var matches [][3]any
			e.MatchString(text, func(k string, start, end int) bool {
				matches = append(matches, [3]any{k, start, end})
				return true
			})
			return matches
		}
		if got, want := collect(overlay), collect(full); !reflect.DeepEqual(got, want) {
			t.Fatalf("MatchString(%q): got %v, want %v", text, got, want)
		}
		if got, want := overlay.Find(text), full.Find(text); !reflect.DeepEqual(got, want) {
			t.Fatalf("Find(%q): got %v, want %v", text, got, want)
		}
		if got, want := overlay.FindSet(text), full.FindSet(text); !reflect.DeepEqual(got, want) {
			t.Fatalf("FindSet(%q): got %v, want %v", text, got, want)
		}
		if got, want := overlay.FindIndex(text), full.FindIndex(text); !reflect.DeepEqual(got, want) {
			t.Fatalf("FindIndex(%q): got %v, want %v", text, got, want)
		}
		if got, want := overlay.Contains(text), full.Contains(text); got != want {
			t.Fatalf("Contains(%q): got %t, want %t", text, got, want)
		}
	}
	for _, e := range []*Engine{overlay, full} {
		called := 0
		e.MatchString("delta-change-001 delta-change-002", func(string, int, int) bool {
			called++
			return false
		})
		if called != 1 {
			t.Fatalf("early stop emitted %d matches", called)
		}
	}
}

func TestOverlayEmptyDeltaSharesBase(t *testing.T) {
	base := New(PresetMemoryEfficient)
	base.Build(map[string]struct{}{"base": {}})
	added := New(PresetMemoryEfficient)
	added.Build(nil)
	if got := Overlay(base, added, nil); got != base {
		t.Fatal("empty delta should reuse the base search view")
	}
}
