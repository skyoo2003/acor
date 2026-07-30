// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"fmt"
	"testing"

	miniredis "github.com/alicebob/miniredis/v2"
)

// V1's find/findIndex used to walk the trie in Redis one character at a time.
// They now build the same in-memory engine the other modes use. These tests pin
// the two against each other so the swap cannot change what a V1 collection
// reports.

// v1TrieWalk is the pre-refactor V1 matcher, kept here as the reference
// implementation: goto/fail/output resolved per character against Redis. It
// reports every output with the 1-based rune position it ends at, which is what
// both pre-refactor entry points were built from — find kept the keywords,
// findIndex turned each end position into a start offset.
func v1TrieWalk(ctx context.Context, ac *AhoCorasick, text string, caseSensitive bool) (outputs []string, endIndexes []int, err error) {
	text = normalizeText(text, caseSensitive)

	state := ""
	for runeIndex, char := range []rune(text) {
		nextState, err := ac.goWithContext(ctx, state, char)
		if err != nil {
			return nil, nil, err
		}
		if nextState == "" {
			nextState, err = ac.failWithContext(ctx, state)
			if err != nil {
				return nil, nil, err
			}
			var afterNextState string
			afterNextState, err = ac.goWithContext(ctx, nextState, char)
			if err != nil {
				return nil, nil, err
			}
			if afterNextState == "" {
				afterNextState, err = ac.failWithContext(ctx, nextState+string(char))
				if err != nil {
					return nil, nil, err
				}
			}
			nextState = afterNextState
		}

		stateOutputs, err := ac.outputWithContext(ctx, nextState)
		if err != nil {
			return nil, nil, err
		}
		for _, output := range stateOutputs {
			outputs = append(outputs, output)
			endIndexes = append(endIndexes, runeIndex+1)
		}
		state = nextState
	}
	return outputs, endIndexes, nil
}

// v1TrieWalkIndexes is what the pre-refactor V1 findIndex did with a walk's
// outputs: turn each end position into a start offset.
func v1TrieWalkIndexes(ctx context.Context, ac *AhoCorasick, outputs []string, endIndexes []int) map[string][]int {
	matched := make(map[string][]int)
	for i, output := range outputs {
		ac.appendMatchedIndexesWithContext(ctx, matched, []string{output}, endIndexes[i])
	}
	return matched
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
	{
		// Case-sensitive mode stores these verbatim, so only the exact-case text
		// matches; case-insensitive mode matches all four.
		name:     "mixed case",
		keywords: []string{"GoLang", "HTTPServer"},
		texts:    []string{"GoLang and HTTPServer", "golang and httpserver", "GOLANG", "a GoLang httpserver"},
	},
	{
		// An empty collection is the one input where the engine has no states at
		// all and reports nothing; both paths must still return empty, not nil.
		name:     "no keywords",
		keywords: nil,
		texts:    []string{"anything at all", ""},
	},
}

func newV1Parity(t *testing.T, keywords []string, caseSensitive bool) *AhoCorasick {
	t.Helper()
	mr := miniredis.RunT(t)
	ac, err := Create(&AhoCorasickArgs{
		Addr:          mr.Addr(),
		Name:          "parity",
		SchemaVersion: SchemaV1,
		CaseSensitive: caseSensitive,
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

func TestV1EngineMatchesTrieWalk(t *testing.T) {
	for _, caseSensitive := range []bool{false, true} {
		t.Run(fmt.Sprintf("caseSensitive=%t", caseSensitive), func(t *testing.T) {
			for _, tc := range parityCases {
				t.Run(tc.name, func(t *testing.T) {
					ac := newV1Parity(t, tc.keywords, caseSensitive)
					ctx := context.Background()

					for _, text := range tc.texts {
						outputs, endIndexes, err := v1TrieWalk(ctx, ac, text, caseSensitive)
						if err != nil {
							t.Fatalf("trie walk(%q): %v", text, err)
						}

						got, err := ac.ops.find(ctx, text)
						if err != nil {
							t.Fatalf("find(%q): %v", text, err)
						}
						// The trie walk emits per end position, the engine per scan
						// position; both report the same multiset of matches.
						if !equalStringSets(got, outputs) {
							t.Errorf("find(%q) = %v, trie walk = %v", text, got, outputs)
						}

						gotIndexes, err := ac.ops.findIndex(ctx, text)
						if err != nil {
							t.Fatalf("findIndex(%q): %v", text, err)
						}
						assertIndexResults(t, gotIndexes, v1TrieWalkIndexes(ctx, ac, outputs, endIndexes))
					}
				})
			}
		})
	}
}

// TestV1EngineMemoReusesUnchangedSet pins both branches of the memo: an
// unchanged keyword set must hand back the same automaton (the rebuild is the
// expensive part of a V1 search), and a changed one must not.
func TestV1EngineMemoReusesUnchangedSet(t *testing.T) {
	ac := newV1Parity(t, []string{"he", "she"}, false)
	ctx := context.Background()

	first, err := ac.ops.loadEngine(ctx)
	if err != nil {
		t.Fatalf("loadEngine: %v", err)
	}
	second, err := ac.ops.loadEngine(ctx)
	if err != nil {
		t.Fatalf("loadEngine (repeat): %v", err)
	}
	if first != second {
		t.Error("loadEngine rebuilt the automaton for an unchanged keyword set")
	}

	if _, addErr := ac.Add("hers"); addErr != nil {
		t.Fatalf("Add: %v", addErr)
	}
	third, err := ac.ops.loadEngine(ctx)
	if err != nil {
		t.Fatalf("loadEngine (after add): %v", err)
	}
	if third == second {
		t.Fatal("loadEngine reused the automaton after the keyword set changed")
	}
	if got := third.Find("hers"); !equalStringSets(got, []string{"he", "hers"}) {
		t.Errorf("Find(%q) = %v, want [he hers]", "hers", got)
	}
}
