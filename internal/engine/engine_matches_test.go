// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"reflect"
	"sort"
	"testing"
	"unicode/utf8"
)

// startsByKeyword collapses matches into brute-comparable start-offset lists and
// checks each span is [start, start+runeLen(keyword)).
func startsByKeyword(t *testing.T, matches []Match) map[string][]int {
	t.Helper()
	got := make(map[string][]int)
	for _, m := range matches {
		if want := m.Start + utf8.RuneCountInString(m.Keyword); m.End != want {
			t.Errorf("match %q span [%d,%d): End should be %d", m.Keyword, m.Start, m.End, want)
		}
		got[m.Keyword] = append(got[m.Keyword], m.Start)
	}
	for _, s := range got {
		sort.Ints(s)
	}
	return got
}

func TestFindMatches_MatchesBruteForce(t *testing.T) {
	cases := []struct {
		keywords []string
		text     string
	}{
		{[]string{"he", "her", "him", "she", "hers"}, "she said he hid hers with him"},
		{[]string{"a", "aa", "aaa"}, "aaaa"},
		{[]string{"안녕", "녕하", "하세요"}, "안녕하세요 안녕"},
		{[]string{"ab", "bc", "abc"}, "xabcx"},
	}
	for _, tc := range cases {
		kws := keywordSet(tc.keywords...)
		want := bruteFindIndex(kws, tc.text)
		for _, p := range allPresets {
			e := New(p)
			e.Build(kws)
			got := startsByKeyword(t, e.FindMatches(tc.text))
			// bruteFindIndex omits keywords with no occurrence; drop empty entries.
			for k, v := range got {
				if len(v) == 0 {
					delete(got, k)
				}
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("preset %v text %q: FindMatches starts = %v, want %v", p, tc.text, got, want)
			}
		}
	}
}

func TestContains(t *testing.T) {
	kws := keywordSet("he", "she", "his")
	for _, p := range allPresets {
		e := New(p)
		e.Build(kws)
		if !e.Contains("this is here") {
			t.Errorf("preset %v: Contains should be true (matches 'he'/'his')", p)
		}
		if e.Contains("no match at all") {
			t.Errorf("preset %v: Contains should be false", p)
		}
		if e.Contains("") {
			t.Errorf("preset %v: Contains(\"\") should be false", p)
		}
	}
}

func TestStream_EarlyStop(t *testing.T) {
	kws := keywordSet("ab")
	for _, p := range allPresets {
		e := New(p)
		e.Build(kws)
		count := 0
		e.Stream(stringRuneSource("abababab"), func(Match) bool {
			count++
			return false // stop after the first
		})
		if count != 1 {
			t.Errorf("preset %v: early-stop Stream emitted %d matches, want 1", p, count)
		}
	}
}

func TestFindMatches_Empty(t *testing.T) {
	for _, p := range allPresets {
		e := New(p)
		e.Build(keywordSet())
		if got := e.FindMatches("anything"); len(got) != 0 {
			t.Errorf("preset %v: empty automaton FindMatches = %v, want none", p, got)
		}
	}
}
