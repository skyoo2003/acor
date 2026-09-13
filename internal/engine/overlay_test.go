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
