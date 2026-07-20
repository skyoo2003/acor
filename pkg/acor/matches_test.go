// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"reflect"
	"strings"
	"testing"
)

func addAll(t *testing.T, ac *AhoCorasick, kws ...string) {
	t.Helper()
	for _, k := range kws {
		if _, err := ac.Add(k); err != nil {
			t.Fatalf("Add(%q): %v", k, err)
		}
	}
}

func TestFindMatches_Overlapping(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer ac.Close()

	addAll(t, ac, "he", "her", "hers")
	got, err := ac.FindMatches("hers", nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []Match{
		{Keyword: "he", Start: 0, End: 2},
		{Keyword: "her", Start: 0, End: 3},
		{Keyword: "hers", Start: 0, End: 4},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("FindMatches = %v, want %v", got, want)
	}
}

func TestFindMatches_LeftmostLongest(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer ac.Close()

	addAll(t, ac, "he", "her", "hers", "she")
	// Overlapping would report he/her/hers/she all at start 0-1; leftmost-longest
	// keeps only the longest non-overlapping run: "she"(0-3) then nothing left.
	got, err := ac.FindMatches("shers", &MatchOptions{Kind: MatchKindLeftmostLongest})
	if err != nil {
		t.Fatal(err)
	}
	// "shers": she@0-3, then from index 3 "rs" has no match. hers@1-5 overlaps she.
	want := []Match{{Keyword: "she", Start: 0, End: 3}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("leftmost-longest = %v, want %v", got, want)
	}
}

func TestFindMatches_WholeWord(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer ac.Close()

	addAll(t, ac, "class")
	if got, err := ac.FindMatches("classic classroom", &MatchOptions{WholeWord: true}); err != nil {
		t.Fatal(err)
	} else if len(got) != 0 {
		t.Errorf("whole-word should reject 'class' inside 'classic'/'classroom', got %v", got)
	}
	if got, err := ac.FindMatches("the class ended", &MatchOptions{WholeWord: true}); err != nil {
		t.Fatal(err)
	} else if len(got) != 1 || got[0].Keyword != "class" {
		t.Errorf("whole-word should match standalone 'class', got %v", got)
	}
}

func TestContains_EndToEnd(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer ac.Close()

	addAll(t, ac, "banned", "forbidden")
	if ok, err := ac.Contains("this text is banned here"); err != nil || !ok {
		t.Errorf("Contains = %v, %v; want true, nil", ok, err)
	}
	if ok, err := ac.Contains("this text is clean"); err != nil || ok {
		t.Errorf("Contains = %v, %v; want false, nil", ok, err)
	}
}

// TestFindStream_NoBoundaryLoss guards the motivating bug: a keyword longer than
// the parallel chunk overlap must still be found when it straddles buffer refills.
func TestFindStream_NoBoundaryLoss(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer ac.Close()

	needle := "supercalifragilistic" // 20 runes, longer than DefaultOverlap(50)? no — but longer than a small buffer
	addAll(t, ac, needle)

	// Bury the needle deep in a long stream so it spans bufio refills.
	text := strings.Repeat("x", 10000) + needle + strings.Repeat("y", 10000)

	var found []Match
	err := ac.FindStream(strings.NewReader(text), func(m Match) bool {
		found = append(found, m)
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].Keyword != needle {
		t.Fatalf("FindStream found %v, want single %q", found, needle)
	}
	if got := found[0].Start; got != 10000 {
		t.Errorf("stream match Start = %d, want 10000", got)
	}
}

func TestFindStream_EarlyStop(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer ac.Close()

	addAll(t, ac, "ab")
	count := 0
	err := ac.FindStream(strings.NewReader("abababab"), func(Match) bool {
		count++
		return false
	})
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("early-stop stream emitted %d, want 1", count)
	}
}

func TestFindMatches_V1(t *testing.T) {
	ac, mr := createAhoCorasickV1(t)
	defer mr.Close()
	defer ac.Close()

	addAll(t, ac, "he", "her")
	got, err := ac.FindMatches("her", nil)
	if err != nil {
		t.Fatalf("V1 FindMatches: %v", err)
	}
	want := []Match{
		{Keyword: "he", Start: 0, End: 2},
		{Keyword: "her", Start: 0, End: 3},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("V1 FindMatches = %v, want %v", got, want)
	}
}

func TestLeftmostLongest_Unit(t *testing.T) {
	in := []Match{
		{Keyword: "he", Start: 0, End: 2},
		{Keyword: "her", Start: 0, End: 3},
		{Keyword: "is", Start: 4, End: 6},
	}
	got := leftmostLongest(in)
	want := []Match{
		{Keyword: "her", Start: 0, End: 3},
		{Keyword: "is", Start: 4, End: 6},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("leftmostLongest = %v, want %v", got, want)
	}
}
