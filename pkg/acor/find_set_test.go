// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"fmt"
	"slices"
	"testing"

	miniredis "github.com/alicebob/miniredis/v2"
)

func newSetTestAC(t *testing.T, preset Preset) *AhoCorasick {
	t.Helper()
	mr := miniredis.RunT(t)
	ac, err := Create(&AhoCorasickArgs{
		Addr:          mr.Addr(),
		Name:          t.Name(),
		Preset:        preset,
		CaseSensitive: true,
	})
	if err != nil {
		t.Fatalf("Create(preset=%v) error: %v", preset, err)
	}
	t.Cleanup(func() { _ = ac.Close() })
	return ac
}

// TestFindSetDedupesInFirstMatchOrder pins the contract that separates FindSet
// from Find: Find reports every occurrence, FindSet reports each keyword once.
func TestFindSetDedupesInFirstMatchOrder(t *testing.T) {
	for _, preset := range []Preset{PresetSpeed, PresetBalanced, PresetMemoryEfficient} {
		t.Run(preset.String(), func(t *testing.T) {
			ac := newSetTestAC(t, preset)
			if _, err := ac.AddMany([]string{"he", "her", "she"}, nil); err != nil {
				t.Fatalf("AddMany error: %v", err)
			}

			const text = "he she her he she"

			all, err := ac.Find(text)
			if err != nil {
				t.Fatalf("Find(%q) error: %v", text, err)
			}
			set, err := ac.FindSet(text)
			if err != nil {
				t.Fatalf("FindSet(%q) error: %v", text, err)
			}

			// Find must still report duplicates: FindSet is a different question,
			// not a bug fix to Find, and callers counting occurrences rely on it.
			if len(all) <= len(set) {
				t.Fatalf("Find(%q) = %v (len %d) should report more occurrences than "+
					"FindSet's %v (len %d)", text, all, len(all), set, len(set))
			}

			seen := make(map[string]struct{}, len(set))
			for _, kw := range set {
				if _, dup := seen[kw]; dup {
					t.Errorf("FindSet(%q) = %v contains duplicate %q", text, set, kw)
				}
				seen[kw] = struct{}{}
			}

			// Same membership as folding Find's output, and in first-match order.
			var want []string
			for _, kw := range all {
				if !slices.Contains(want, kw) {
					want = append(want, kw)
				}
			}
			if !slices.Equal(set, want) {
				t.Errorf("FindSet(%q) = %v, want %v (first-match order of Find)", text, set, want)
			}
		})
	}
}

// TestFindMatchesAppendReusesBuffer pins the contract that makes the buffer
// worth passing: results land in the caller's backing array, and dst[:0] reuse
// across texts yields the same matches a fresh call would.
func TestFindMatchesAppendReusesBuffer(t *testing.T) {
	ac := newSetTestAC(t, PresetBalanced)
	if _, err := ac.AddMany([]string{"he", "her", "she"}, nil); err != nil {
		t.Fatalf("AddMany error: %v", err)
	}

	texts := []string{"he she her", "nothing at all", "sherhe"}
	buf := make([]Match, 1, 16)
	backing := &buf[0]

	for _, text := range texts {
		fresh, err := ac.FindMatches(text, nil)
		if err != nil {
			t.Fatalf("FindMatches(%q) error: %v", text, err)
		}
		reused, err := ac.FindMatchesAppend(buf[:0], text, nil)
		if err != nil {
			t.Fatalf("FindMatchesAppend(%q) error: %v", text, err)
		}
		if !slices.Equal(fresh, reused) {
			t.Errorf("FindMatchesAppend(%q) = %v, want %v", text, reused, fresh)
		}
		if len(reused) > 0 && &reused[0] != backing {
			t.Errorf("FindMatchesAppend(%q) allocated instead of writing into dst", text)
		}
	}
}

func TestFindSetEmptyText(t *testing.T) {
	ac := newSetTestAC(t, PresetBalanced)
	if _, err := ac.Add("he"); err != nil {
		t.Fatalf("Add error: %v", err)
	}
	got, err := ac.FindSet("")
	if err != nil {
		t.Fatalf("FindSet(\"\") error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("FindSet(\"\") = %v, want empty", got)
	}
}

// TestBatchMatchesSequentialWrites is the equivalence guard for the batched write
// path. planAddMany and planRemoveMany fold N plan passes into one, and the claim
// is that this changes cost, not outcome, so a batch must leave exactly the state
// the same keywords written one at a time would.
func TestBatchMatchesSequentialWrites(t *testing.T) {
	// Overlapping suffixes and prefixes: "he" is a suffix of "she" and a prefix of
	// "her", so a later keyword changes the output lists of states that already
	// exist.
	keywords := []string{"she", "he", "her", "hers", "his", "sher"}
	doomed := []string{"her", "his"}
	const text = "shers he his hers her she"

	cases := []struct {
		name string
		// batch performs the whole change in one call, sequential the same change
		// one keyword at a time. Both start from an empty collection.
		batch      func(*testing.T, *AhoCorasick)
		sequential func(*testing.T, *AhoCorasick)
	}{
		{
			name: "AddMany",
			batch: func(t *testing.T, ac *AhoCorasick) {
				if _, err := ac.AddMany(keywords, nil); err != nil {
					t.Fatalf("AddMany error: %v", err)
				}
			},
			sequential: func(t *testing.T, ac *AhoCorasick) {
				for _, kw := range keywords {
					if _, err := ac.Add(kw); err != nil {
						t.Fatalf("Add(%q) error: %v", kw, err)
					}
				}
			},
		},
		{
			name: "RemoveMany",
			batch: func(t *testing.T, ac *AhoCorasick) {
				if _, err := ac.AddMany(keywords, nil); err != nil {
					t.Fatalf("AddMany(seed) error: %v", err)
				}
				if _, err := ac.RemoveMany(doomed, nil); err != nil {
					t.Fatalf("RemoveMany error: %v", err)
				}
			},
			sequential: func(t *testing.T, ac *AhoCorasick) {
				if _, err := ac.AddMany(keywords, nil); err != nil {
					t.Fatalf("AddMany(seed) error: %v", err)
				}
				for _, kw := range doomed {
					if _, err := ac.Remove(kw); err != nil {
						t.Fatalf("Remove(%q) error: %v", kw, err)
					}
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			find := func(apply func(*testing.T, *AhoCorasick)) []string {
				ac := newSetTestAC(t, PresetBalanced)
				apply(t, ac)
				got, err := ac.Find(text)
				if err != nil {
					t.Fatalf("Find(%q) error: %v", text, err)
				}
				slices.Sort(got)
				return got
			}

			gotBatch, gotSeq := find(tc.batch), find(tc.sequential)
			if !slices.Equal(gotBatch, gotSeq) {
				t.Errorf("%s produced different matches than the sequential writes:"+
					"\n batch = %v\n seq   = %v", tc.name, gotBatch, gotSeq)
			}
		})
	}
}

// TestAddManyReportsSkippedForExisting keeps the BatchResult contract intact
// under the batched path: keywords already present are Skipped, not Added.
func TestAddManyReportsSkippedForExisting(t *testing.T) {
	ac := newSetTestAC(t, PresetBalanced)
	if _, err := ac.AddMany([]string{"he", "her"}, nil); err != nil {
		t.Fatalf("AddMany(first) error: %v", err)
	}

	res, err := ac.AddMany([]string{"he", "she", "he"}, nil)
	if err != nil {
		t.Fatalf("AddMany(second) error: %v", err)
	}
	if !slices.Equal(res.Added, []string{"she"}) {
		t.Errorf("Added = %v, want [she]", res.Added)
	}
	// "he" twice: one for already-present, one for the in-batch duplicate.
	if got := len(res.Skipped); got != 2 {
		t.Errorf("Skipped = %v (len %d), want 2 entries for the repeated and existing %q",
			res.Skipped, got, "he")
	}
}

// TestAddManyReportsCaseFoldedWrites guards the batched path against reporting a
// write it actually made as Skipped. The write path normalizes (lowercases on a
// case-insensitive collection), so the outcome has to be matched back to the
// caller's own spelling rather than compared against the normalized form.
func TestAddManyReportsCaseFoldedWrites(t *testing.T) {
	mr := miniredis.RunT(t)
	// CaseSensitive left at its default (false) on purpose: every other test here
	// sets it true, which is why the mismatch went unnoticed.
	ac, err := Create(&AhoCorasickArgs{Addr: mr.Addr(), Name: t.Name(), Preset: PresetBalanced})
	if err != nil {
		t.Fatalf("Create error: %v", err)
	}
	t.Cleanup(func() { _ = ac.Close() })

	res, err := ac.AddMany([]string{"Hello", "World"}, nil)
	if err != nil {
		t.Fatalf("AddMany error: %v", err)
	}
	if !slices.Equal(res.Added, []string{"Hello", "World"}) {
		t.Errorf("Added = %v (Skipped %v), want [Hello World]: both keywords were written",
			res.Added, res.Skipped)
	}

	rres, err := ac.RemoveMany([]string{"Hello"}, nil)
	if err != nil {
		t.Fatalf("RemoveMany error: %v", err)
	}
	if !slices.Equal(rres.Removed, []string{"Hello"}) {
		t.Errorf("Removed = %v (Skipped %v), want [Hello]", rres.Removed, rres.Skipped)
	}
}

// TestAddInvalidUTF8Keyword pins that planning a keyword carrying invalid UTF-8
// does not panic. Prefixes are sliced at rune boundaries, and utf8.RuneLen of the
// RuneError such a byte decodes to is 3 — enough to slice past the end.
func TestAddInvalidUTF8Keyword(t *testing.T) {
	ac := newSetTestAC(t, PresetBalanced) // case-sensitive: the bytes survive normalization
	if _, err := ac.Add("a\xff"); err != nil {
		t.Fatalf("Add(invalid UTF-8) error: %v", err)
	}
	got, err := ac.Find("xa\xffy")
	if err != nil {
		t.Fatalf("Find error: %v", err)
	}
	if !slices.Contains(got, "a\xff") {
		t.Errorf("Find = %q, want the invalid-UTF-8 keyword reported", got)
	}
}

// TestFindMatchesAppendKeepsEarlierResults pins that options apply to the current
// text only. Offsets already in dst index a different text, so filtering the whole
// buffer read past the end of the current one.
func TestFindMatchesAppendKeepsEarlierResults(t *testing.T) {
	ac := newSetTestAC(t, PresetBalanced)
	if _, err := ac.AddMany([]string{"hello", "hi"}, nil); err != nil {
		t.Fatalf("AddMany error: %v", err)
	}
	opts := &MatchOptions{WholeWord: true}

	buf, err := ac.FindMatchesAppend(nil, "a long text with hello inside it", opts)
	if err != nil {
		t.Fatalf("FindMatchesAppend(first) error: %v", err)
	}
	first := len(buf)
	if first == 0 {
		t.Fatal("first pass matched nothing; the test needs a match to preserve")
	}

	// Short second text: any offset from the first text is out of range for it.
	buf, err = ac.FindMatchesAppend(buf, "hi", opts)
	if err != nil {
		t.Fatalf("FindMatchesAppend(second) error: %v", err)
	}
	if len(buf) != first+1 {
		t.Errorf("len = %d, want %d: the append must add this text's match and keep the earlier one",
			len(buf), first+1)
	}

	// Empty text keeps the buffer as it is rather than truncating it.
	buf, err = ac.FindMatchesAppend(buf, "", opts)
	if err != nil {
		t.Fatalf("FindMatchesAppend(empty) error: %v", err)
	}
	if len(buf) != first+1 {
		t.Errorf("len after empty text = %d, want %d", len(buf), first+1)
	}
	if got, err := ac.FindMatches("", nil); err != nil || got == nil {
		t.Errorf("FindMatches(\"\") = %v, %v; want a non-nil empty slice", got, err)
	}
}

// TestAddManyLargeBatchIsLinearlyConsistent exercises the batched planner at a
// size where the old per-keyword path did quadratic work, guarding both the
// count and that every keyword is actually matchable afterwards.
func TestAddManyLargeBatchIsLinearlyConsistent(t *testing.T) {
	ac := newSetTestAC(t, PresetBalanced)

	const n = 500
	keywords := make([]string, n)
	for i := range keywords {
		keywords[i] = fmt.Sprintf("keyword%d", i)
	}

	res, err := ac.AddMany(keywords, nil)
	if err != nil {
		t.Fatalf("AddMany(%d keywords) error: %v", n, err)
	}
	if len(res.Added) != n {
		t.Fatalf("Added = %d keywords, want %d", len(res.Added), n)
	}

	for _, probe := range []string{"keyword0", "keyword250", "keyword499"} {
		got, err := ac.FindSet("text with " + probe + " inside")
		if err != nil {
			t.Fatalf("FindSet(%q) error: %v", probe, err)
		}
		if !slices.Contains(got, probe) {
			t.Errorf("FindSet did not report %q after a %d-keyword batch; got %v", probe, n, got)
		}
	}
}
