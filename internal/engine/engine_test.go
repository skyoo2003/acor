// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"reflect"
	"sort"
	"testing"
)

// allPresets is every user-selectable preset. Optimizations must never change
// match results, so every case below is asserted against all four.
var allPresets = []Preset{PresetSpeed, PresetBalanced, PresetMemoryEfficient, PresetUltimate}

func keywordSet(kws ...string) map[string]struct{} {
	m := make(map[string]struct{}, len(kws))
	for _, kw := range kws {
		m[kw] = struct{}{}
	}
	return m
}

// bruteFindIndex is a naive O(n*m) reference: every rune-aligned occurrence of
// each keyword. Aho-Corasick reports every occurrence (overlaps included), so
// this is the ground truth for both Find and FindIndex.
func bruteFindIndex(keywords map[string]struct{}, text string) map[string][]int {
	runes := []rune(text)
	m := make(map[string][]int)
	for kw := range keywords {
		kwr := []rune(kw)
		if len(kwr) == 0 {
			continue
		}
		for i := 0; i+len(kwr) <= len(runes); i++ {
			if string(runes[i:i+len(kwr)]) == kw {
				m[kw] = append(m[kw], i)
			}
		}
	}
	return m
}

func bruteFind(keywords map[string]struct{}, text string) []string {
	idx := bruteFindIndex(keywords, text)
	out := make([]string, 0)
	for kw, starts := range idx {
		for range starts {
			out = append(out, kw)
		}
	}
	sort.Strings(out)
	return out
}

func sortedStrings(s []string) []string {
	c := append([]string(nil), s...)
	sort.Strings(c)
	return c
}

func TestEngineResultInvariance(t *testing.T) {
	cases := []struct {
		name     string
		keywords []string
		text     string
	}{
		{"empty-text", []string{"he", "she"}, ""},
		{"no-match", []string{"abc", "xyz"}, "the quick brown fox"},
		{"overlapping", []string{"he", "she", "his", "hers", "hello"}, "ushers say hello to his heroes"},
		// Input must exit the "ab" state on an in-alphabet rune to exercise the
		// Speed DFA's fail-fallback row; "abab" does, a text like "abracadabra"
		// dodges it (always leaving "ab" via an out-of-alphabet rune).
		{"single-char", []string{"a", "b", "ab"}, "abab"},
		{"repeats", []string{"na"}, "bananana"},
		{"multibyte-hangul", []string{"한국", "국어", "안녕"}, "안녕 한국어 공부"},
		{"multibyte-emoji", []string{"🙂", "🙂🙂"}, "hi 🙂🙂 there 🙂"},
		{"mixed-alphabet", []string{"café", "안녕", "ab"}, "un café 안녕 abc"},
		{"out-of-alphabet", []string{"cat"}, "a cat zzz ☃ cat"},
		{"prefix-chain", []string{"a", "aa", "aaa"}, "aaaa"},
	}

	for _, tc := range cases {
		kws := keywordSet(tc.keywords...)
		wantFind := bruteFind(kws, tc.text)
		wantIndex := bruteFindIndex(kws, tc.text)
		for _, p := range allPresets {
			t.Run(tc.name+"/"+p.String(), func(t *testing.T) {
				e := New(p)
				e.Build(kws)

				gotFind := sortedStrings(e.Find(tc.text))
				if len(gotFind) != 0 || len(wantFind) != 0 {
					if !reflect.DeepEqual(gotFind, wantFind) {
						t.Errorf("Find = %v, want %v", gotFind, wantFind)
					}
				}

				gotIndex := e.FindIndex(tc.text)
				if len(gotIndex) == 0 && len(wantIndex) == 0 {
					return // nil vs empty map are equivalent
				}
				if !reflect.DeepEqual(gotIndex, wantIndex) {
					t.Errorf("FindIndex = %v, want %v", gotIndex, wantIndex)
				}
			})
		}
	}
}

// TestEngineEmptyKeywords guards the degenerate build (no keywords): every
// preset must return no matches rather than panic.
func TestEngineEmptyKeywords(t *testing.T) {
	for _, p := range allPresets {
		t.Run(p.String(), func(t *testing.T) {
			e := New(p)
			e.Build(keywordSet())
			if got := e.Find("anything"); len(got) != 0 {
				t.Errorf("Find on empty automaton = %v, want none", got)
			}
			if got := e.FindIndex("anything"); len(got) != 0 {
				t.Errorf("FindIndex on empty automaton = %v, want none", got)
			}
		})
	}
}
