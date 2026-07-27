// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"sort"
	"strings"
	"testing"

	miniredis "github.com/alicebob/miniredis/v2"
)

// V1's find/findIndex used to walk the trie in Redis one character at a time.
// They now build the same in-memory engine the other modes use. These tests pin
// the two against each other so the swap cannot change what a V1 collection
// reports.

// v1TrieWalkFind is the pre-refactor V1 matcher, kept here as the reference
// implementation: goto/fail/output resolved per character against Redis.
func v1TrieWalkFind(ctx context.Context, ac *AhoCorasick, text string, caseSensitive bool) ([]string, error) {
	if text == "" {
		return []string{}, nil
	}
	if !caseSensitive {
		text = strings.ToLower(text)
	}

	state := ""
	matched := make([]string, 0)
	for _, char := range text {
		nextState, err := ac.goWithContext(ctx, state, char)
		if err != nil {
			return nil, err
		}
		if nextState == "" {
			nextState, err = ac.failWithContext(ctx, state)
			if err != nil {
				return nil, err
			}
			var afterNextState string
			afterNextState, err = ac.goWithContext(ctx, nextState, char)
			if err != nil {
				return nil, err
			}
			if afterNextState == "" {
				afterNextState, err = ac.failWithContext(ctx, nextState+string(char))
				if err != nil {
					return nil, err
				}
			}
			nextState = afterNextState
		}

		outputs, err := ac.outputWithContext(ctx, nextState)
		if err != nil {
			return nil, err
		}
		matched = append(matched, outputs...)
		state = nextState
	}
	return matched, nil
}

// v1TrieWalkFindIndex is v1TrieWalkFind with start offsets, the pre-refactor
// findIndex.
func v1TrieWalkFindIndex(ctx context.Context, ac *AhoCorasick, text string, caseSensitive bool) (map[string][]int, error) {
	if text == "" {
		return map[string][]int{}, nil
	}
	if !caseSensitive {
		text = strings.ToLower(text)
	}

	matched := make(map[string][]int)
	state := ""
	runeIndex := 0
	for _, char := range text {
		nextState, err := ac.goWithContext(ctx, state, char)
		if err != nil {
			return nil, err
		}
		if nextState == "" {
			nextState, err = ac.failWithContext(ctx, state)
			if err != nil {
				return nil, err
			}
			var afterNextState string
			afterNextState, err = ac.goWithContext(ctx, nextState, char)
			if err != nil {
				return nil, err
			}
			if afterNextState == "" {
				afterNextState, err = ac.failWithContext(ctx, nextState+string(char))
				if err != nil {
					return nil, err
				}
			}
			nextState = afterNextState
		}

		outputs, err := ac.outputWithContext(ctx, nextState)
		if err != nil {
			return nil, err
		}
		ac.appendMatchedIndexesWithContext(ctx, matched, outputs, runeIndex+1)
		state = nextState
		runeIndex++
	}
	return matched, nil
}

var parityCases = []struct {
	name     string
	keywords []string
	texts    []string
}{
	{
		name:     "classic overlapping",
		keywords: []string{"he", "she", "his", "hers"},
		texts:    []string{"ushers", "he is his", "hershey", "", "xyz"},
	},
	{
		name:     "nested prefixes",
		keywords: []string{"a", "aa", "aaa"},
		texts:    []string{"aaaa", "aa", "baaab"},
	},
	{
		name:     "shared suffix",
		keywords: []string{"bar", "foobar", "obar"},
		texts:    []string{"foobar", "xfoobarx", "obar bar"},
	},
	{
		name:     "multibyte",
		keywords: []string{"한글", "글자", "가나다"},
		texts:    []string{"한글자입니다", "가나다라 한글", "no match here"},
	},
	{
		name:     "single char keywords",
		keywords: []string{"x", "y"},
		texts:    []string{"xyxy", "zzz"},
	},
}

func newV1Parity(t *testing.T, keywords []string) *AhoCorasick {
	t.Helper()
	mr := miniredis.RunT(t)
	ac, err := Create(&AhoCorasickArgs{
		Addr:          mr.Addr(),
		Name:          "parity",
		SchemaVersion: SchemaV1,
	})
	if err != nil {
		t.Fatalf("Create V1: %v", err)
	}
	t.Cleanup(func() { _ = ac.Close() })

	for _, kw := range keywords {
		if _, addErr := ac.Add(kw); addErr != nil {
			t.Fatalf("Add(%q): %v", kw, addErr)
		}
	}
	return ac
}

func TestV1FindMatchesTrieWalk(t *testing.T) {
	for _, tc := range parityCases {
		t.Run(tc.name, func(t *testing.T) {
			ac := newV1Parity(t, tc.keywords)
			ctx := context.Background()

			for _, text := range tc.texts {
				want, err := v1TrieWalkFind(ctx, ac, text, false)
				if err != nil {
					t.Fatalf("trie walk find(%q): %v", text, err)
				}
				got, err := ac.ops.find(ctx, text)
				if err != nil {
					t.Fatalf("find(%q): %v", text, err)
				}
				// The trie walk emits per end position, the engine per scan
				// position; both report the same multiset of matches.
				if !sameMultiset(got, want) {
					t.Errorf("find(%q) = %v, trie walk = %v", text, got, want)
				}
			}
		})
	}
}

func TestV1FindIndexMatchesTrieWalk(t *testing.T) {
	for _, tc := range parityCases {
		t.Run(tc.name, func(t *testing.T) {
			ac := newV1Parity(t, tc.keywords)
			ctx := context.Background()

			for _, text := range tc.texts {
				want, err := v1TrieWalkFindIndex(ctx, ac, text, false)
				if err != nil {
					t.Fatalf("trie walk findIndex(%q): %v", text, err)
				}
				got, err := ac.ops.findIndex(ctx, text)
				if err != nil {
					t.Fatalf("findIndex(%q): %v", text, err)
				}
				if len(got) != len(want) {
					t.Fatalf("findIndex(%q) = %v, trie walk = %v", text, got, want)
				}
				for kw, wantIdx := range want {
					gotIdx, ok := got[kw]
					if !ok {
						t.Errorf("findIndex(%q) missing keyword %q (trie walk found it at %v)", text, kw, wantIdx)
						continue
					}
					if !sameIntSet(gotIdx, wantIdx) {
						t.Errorf("findIndex(%q)[%q] = %v, trie walk = %v", text, kw, gotIdx, wantIdx)
					}
				}
			}
		})
	}
}

func sameMultiset(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	as := append([]string(nil), a...)
	bs := append([]string(nil), b...)
	sort.Strings(as)
	sort.Strings(bs)
	for i := range as {
		if as[i] != bs[i] {
			return false
		}
	}
	return true
}

func sameIntSet(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	as := append([]int(nil), a...)
	bs := append([]int(nil), b...)
	sort.Ints(as)
	sort.Ints(bs)
	for i := range as {
		if as[i] != bs[i] {
			return false
		}
	}
	return true
}
