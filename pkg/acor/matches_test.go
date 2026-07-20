// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
	"unicode"
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

// TestFindMatches_WholeWord_CombiningMark guards against a false positive on
// decomposed (NFD) text: "cafe" must not be reported as a whole word inside
// "café" written as "cafe" + U+0301 (combining acute), because the combining
// mark belongs to the preceding letter.
func TestFindMatches_WholeWord_CombiningMark(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer ac.Close()

	addAll(t, ac, "cafe")
	decomposed := "café" // café in NFD
	if got, err := ac.FindMatches(decomposed, &MatchOptions{WholeWord: true}); err != nil {
		t.Fatal(err)
	} else if len(got) != 0 {
		t.Errorf("whole-word should reject 'cafe' before a combining mark, got %v", got)
	}
	// Control: a real standalone "cafe" still matches.
	if got, err := ac.FindMatches("the cafe opens", &MatchOptions{WholeWord: true}); err != nil {
		t.Fatal(err)
	} else if len(got) != 1 {
		t.Errorf("whole-word should match standalone 'cafe', got %v", got)
	}
}

// TestFindMatches_WholeWord_CustomWordRune shows the WordRune override making
// WholeWord usable for a script the default misclassifies (CJK): the default
// drops the match, a Han-aware predicate keeps it.
func TestFindMatches_WholeWord_CustomWordRune(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer ac.Close()

	addAll(t, ac, "違禁")

	// Default WholeWord drops it: the following '詞' is a letter (word rune).
	if got, err := ac.FindMatches("違禁詞", &MatchOptions{WholeWord: true}); err != nil {
		t.Fatal(err)
	} else if len(got) != 0 {
		t.Errorf("default WholeWord should drop the CJK-adjacent match, got %v", got)
	}

	// Treat Han ideographs as non-word so a term bounded by other Han still counts.
	nonHan := func(r rune) bool { return isWordRune(r) && !unicode.Is(unicode.Han, r) }
	got, err := ac.FindMatches("違禁詞", &MatchOptions{WholeWord: true, WordRune: nonHan})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Keyword != "違禁" {
		t.Errorf("custom WordRune should match the CJK term, got %v", got)
	}
}

// wrappedEOFReader returns its data, then a wrapped io.EOF (not the bare
// sentinel) — as some decorator/decompressor readers do.
type wrappedEOFReader struct {
	data []byte
	pos  int
}

func (r *wrappedEOFReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, fmt.Errorf("stream done: %w", io.EOF)
	}
	n := copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

// TestFindStream_WrappedEOF confirms a wrapped io.EOF at end of input is treated
// as normal completion, not a scan error.
func TestFindStream_WrappedEOF(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer ac.Close()

	addAll(t, ac, "abc")
	var found []Match
	err := ac.FindStream(&wrappedEOFReader{data: []byte("xabcx")}, func(m Match) bool {
		found = append(found, m)
		return true
	})
	if err != nil {
		t.Errorf("wrapped io.EOF should be normal completion, got err %v", err)
	}
	if len(found) != 1 || found[0].Keyword != "abc" {
		t.Errorf("FindStream over wrapped-EOF reader = %v, want single abc", found)
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
