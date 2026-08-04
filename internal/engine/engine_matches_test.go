// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"fmt"
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

// uniqueInOrder is FindSet's contract stated independently of it: Find's
// occurrences with later duplicates dropped.
func uniqueInOrder(all []string) []string {
	seen := make(map[string]struct{}, len(all))
	out := make([]string, 0, len(all))
	for _, s := range all {
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func TestFindSetNoMatches(t *testing.T) {
	cases := []struct {
		name     string
		keywords map[string]struct{}
		text     string
	}{
		{"empty_keywords", map[string]struct{}{}, "non-empty text"},
		{"no_matching_text", keywordSet("keyword1", "keyword2", "another-keyword"), "nothing here matches"},
	}

	for _, p := range allPresets {
		for _, tc := range cases {
			t.Run(fmt.Sprintf("%s/%v", tc.name, p), func(t *testing.T) {
				e := New(p)
				e.Build(tc.keywords)
				got := e.FindSet(tc.text)
				if got == nil || len(got) != 0 {
					t.Fatalf("FindSet = %#v, want non-nil empty slice", got)
				}
			})
		}
	}
}

// TestFindSetAcrossDedupThreshold pins FindSet against Find on both sides of
// dedupLinearMax, so the handoff from linear scanning to hash maps cannot change
// the answer or its order. A text matching well over the threshold is the case
// the linear path is not meant to serve.
func TestFindSetAcrossDedupThreshold(t *testing.T) {
	for _, n := range []int{1, dedupLinearMax - 1, dedupLinearMax, dedupLinearMax + 1, 4 * dedupLinearMax} {
		kws := make(map[string]struct{}, n)
		var text string
		for i := 0; i < n; i++ {
			kw := fmt.Sprintf("kw%d", i)
			kws[kw] = struct{}{}
			// Each keyword appears twice, so dedup has something to do.
			text += kw + " " + kw + " "
		}
		for _, p := range allPresets {
			t.Run(fmt.Sprintf("%dkw/%v", n, p), func(t *testing.T) {
				e := New(p)
				e.Build(kws)
				want := uniqueInOrder(e.Find(text))
				got := e.FindSet(text)
				if !reflect.DeepEqual(got, want) {
					t.Errorf("FindSet = %v, want %v (unique Find order)", got, want)
				}
				if len(got) != n {
					t.Errorf("FindSet returned %d keywords, want %d", len(got), n)
				}
			})
		}
	}
}

// TestContainsAgreesWithFind pins the Contains specialization against Find on the
// paths that differ between them: the ASCII byte scan, the multibyte rune scan,
// and runes outside the alphabet.
func TestContainsAgreesWithFind(t *testing.T) {
	cases := []struct {
		keywords []string
		texts    []string
	}{
		{[]string{"he", "she", "his"}, []string{"this is here", "no match at all", "", "h", "☃ she ☃"}},
		{[]string{"한국", "안녕"}, []string{"안녕 한국", "hello", "", "한 국", "🦊안녕🦊"}},
		{[]string{"a", "aa", "aaa"}, []string{"aaaa", "bbbb", ""}},
		{[]string{"café"}, []string{"un café", "un cafe", ""}},
	}
	for _, tc := range cases {
		kws := keywordSet(tc.keywords...)
		for _, p := range allPresets {
			e := New(p)
			e.Build(kws)
			for _, txt := range tc.texts {
				want := len(e.Find(txt)) > 0
				if got := e.Contains(txt); got != want {
					t.Errorf("preset %v: Contains(%q) = %v, want %v", p, txt, got, want)
				}
			}
		}
	}
}

// TestRebuildResetsInfo guards the state that survives a rebuild. Build is
// documented as reconstructing the automaton, and TrieDepth is now recorded during
// the build rather than derived from a retained array — so a rebuild from shorter
// keywords must report the shorter depth, not the old maximum.
func TestRebuildResetsInfo(t *testing.T) {
	for _, p := range allPresets {
		t.Run(p.String(), func(t *testing.T) {
			e := New(p)

			e.Build(keywordSet("abcdefgh"))
			if got := e.Info().Keywords; got != 1 {
				t.Errorf("after first build: Keywords = %d, want 1", got)
			}
			deep := e.Info().TrieDepth

			e.Build(keywordSet("ab"))
			info := e.Info()
			if info.Keywords != 1 {
				t.Errorf("after rebuild: Keywords = %d, want 1", info.Keywords)
			}
			if info.TrieDepth >= deep {
				t.Errorf("after rebuild on shorter keywords: TrieDepth = %d, want < %d", info.TrieDepth, deep)
			}
			if got := e.Find("abcdefgh"); len(got) != 1 || got[0] != "ab" {
				t.Errorf("after rebuild: Find = %v, want [ab] only", got)
			}
		})
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
