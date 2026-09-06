// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/alicebob/miniredis/v2"

	matchengine "github.com/skyoo2003/acor/internal/engine"
)

func sourceTestAC(words []string, preset Preset, sensitive bool) *AhoCorasick {
	set := make(map[string]struct{})
	for _, w := range words {
		set[normalizeText(w, sensitive)] = struct{}{}
	}
	e := matchengine.New(enginePreset(preset))
	e.Build(set)
	return &AhoCorasick{ctx: context.Background(), caseSensitive: sensitive, ops: &v3SearchOps{engine: e, sensitive: sensitive}}
}
func TestScanOriginalUnicodeSpans(t *testing.T) {
	text := "A İSTANBUL 한국어 café 😀 abc"
	words := []string{"istanbul", "한국", "한국어", "café", "😀", "ab", "abc", "bc"}
	for _, preset := range []Preset{PresetSpeed, PresetBalanced, PresetMemoryEfficient} {
		ac := sourceTestAC(words, preset, false)
		for _, kind := range []MatchKind{MatchKindOverlapping, MatchKindLeftmostLongest} {
			result, err := ac.Scan(context.Background(), text, &ScanOptions{Kind: kind})
			if err != nil {
				t.Fatal(err)
			}
			expected, err := ac.FindMatches(text, &MatchOptions{Kind: kind})
			if err != nil {
				t.Fatal(err)
			}
			projected := make([]Match, 0, len(result.Matches))
			for _, m := range result.Matches {
				if m.Text != text[m.ByteStart:m.ByteEnd] || m.Text != string([]rune(text)[m.Start:m.End]) {
					t.Fatal("bad original positions", m)
				}
				projected = append(projected, Match{Keyword: m.Keyword, Start: m.Start, End: m.End})
			}
			if result.Truncated || !reflect.DeepEqual(projected, expected) {
				t.Fatal(preset, kind, projected, expected)
			}
		}
	}
}

//nolint:gocyclo // A single fixture exercises independent bounded-scan contracts.
func TestScanLimitsAndWordBoundaries(t *testing.T) {
	ac := sourceTestAC([]string{"a", "aa", "aaa", "class"}, PresetMemoryEfficient, false)
	ctx := context.Background()
	for _, kind := range []MatchKind{MatchKindOverlapping, MatchKindLeftmostLongest} {
		r, err := ac.Scan(ctx, "aaaa aaaa", &ScanOptions{MaxMatches: 1, Kind: kind})
		if err != nil || !r.Truncated || len(r.Matches) != 1 {
			t.Fatal(r, err)
		}
	}
	r, err := ac.Scan(ctx, "a", &ScanOptions{MaxMatches: 1})
	if err != nil || r.Truncated {
		t.Fatal(r, err)
	}
	if _, err = ac.Scan(ctx, "aaa", &ScanOptions{MaxInputBytes: 2}); !errors.Is(err, ErrInputLimit) {
		t.Fatal(err)
	}
	if _, err = ac.Scan(ctx, "aaaaaa", &ScanOptions{MaxCandidates: 2, Kind: MatchKindLeftmostLongest}); !errors.Is(err, ErrScanWorkLimit) {
		t.Fatal(err)
	}
	r, err = ac.Scan(ctx, "classic class!", &ScanOptions{WholeWord: true})
	if err != nil || len(r.Matches) != 1 || r.Matches[0].Text != "class" {
		t.Fatal(r, err)
	}
	r, err = ac.Scan(ctx, "classic", &ScanOptions{WholeWord: true, WordRune: func(rune) bool { return false }})
	if err != nil || len(r.Matches) == 0 {
		t.Fatal(r, err)
	}
	for _, opts := range []*ScanOptions{{MaxMatches: -1}, {MaxCandidates: -1}, {MaxInputBytes: -1}, {Kind: MatchKind(99)}} {
		if _, err = ac.Scan(ctx, "a", opts); err == nil {
			t.Fatal("invalid option accepted", opts)
		}
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = ac.Scan(canceled, "a", nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

//nolint:gocyclo // Rewrite invariants share the same multilingual source spans.
func TestRewriteAtomicAndOriginalOffsets(t *testing.T) {
	ac := sourceTestAC([]string{"한국", "한국어", "istanbul", "ab", "abc", "bc"}, PresetMemoryEfficient, false)
	ctx := context.Background()
	text := "한국어 İSTANBUL abc!"
	r, err := ac.ReplaceText(ctx, text, "[x]", nil)
	if err != nil || r.Text != "[x] [x] [x]!" {
		t.Fatal(r, err)
	}
	for _, m := range r.Matches {
		if m.Text != text[m.ByteStart:m.ByteEnd] {
			t.Fatal(m)
		}
	}
	masked, err := ac.MaskText(ctx, text, '●', nil)
	if err != nil || masked.Text != "●●● ●●●●●●●● ●●●!" {
		t.Fatal(masked, err)
	}
	if utf8.RuneCountInString(masked.Text) != utf8.RuneCountInString(text) {
		t.Fatal("mask changed rune count")
	}
	nul, err := ac.MaskText(ctx, "abc", 0, nil)
	if err != nil || nul.Text != "\x00\x00\x00" {
		t.Fatal(nul, err)
	}
	if _, err = ac.MaskText(ctx, text, -1, nil); err == nil {
		t.Fatal("invalid rune accepted")
	}
	if result, e := ac.ReplaceText(ctx, text, "x", &RewriteOptions{MaxMatches: 1}); !errors.Is(e, ErrMatchLimit) || result != nil {
		t.Fatal(result, e)
	}
	if result, e := ac.MaskText(ctx, text, '●', &RewriteOptions{MaxOutputBytes: 2}); !errors.Is(e, ErrOutputLimit) || result != nil {
		t.Fatal(result, e)
	}
	if _, err = ac.ReplaceText(ctx, "abc", strings.Repeat("x", 100), &RewriteOptions{MaxOutputBytes: 10}); !errors.Is(err, ErrOutputLimit) {
		t.Fatal(err)
	}
	if _, err = ac.ReplaceText(ctx, "clean text", "x", &RewriteOptions{MaxOutputBytes: 1}); !errors.Is(err, ErrOutputLimit) {
		t.Fatal(err)
	}
	deleted, err := ac.ReplaceText(ctx, "abc abc", "", nil)
	if err != nil || deleted.Text != " " {
		t.Fatal(deleted, err)
	}
}
func TestVersionedScanAndRewrite(t *testing.T) {
	ctx := context.Background()
	server := miniredis.RunT(t)
	v := openV3Test(t, server, "source-api")
	r, err := v.Replace(ctx, v.Status().ServingVersion, []string{"한국", "hello"})
	if err != nil {
		t.Fatal(err)
	}
	waitV3(t, v, r.Version)
	if found, e := v.Scan(ctx, "HELLO 한국", nil); e != nil || len(found.Matches) != 2 {
		t.Fatal(found, e)
	}
	if rewritten, e := v.ReplaceText(ctx, "HELLO 한국", "ok", nil); e != nil || rewritten.Text != "ok ok" {
		t.Fatal(rewritten, e)
	}
	if masked, e := v.MaskText(ctx, "HELLO 한국", '*', nil); e != nil || masked.Text != "***** **" {
		t.Fatal(masked, e)
	}
}
func FuzzScanLeftmostParity(f *testing.F) {
	f.Add("ababa 한국어 İSTANBUL", "aba", "ba")
	f.Add("aaaaaa", "a", "aaa")
	f.Fuzz(func(t *testing.T, text, a, b string) {
		if len(text) > 512 || len(a) > 32 || len(b) > 32 || !utf8.ValidString(a) || !utf8.ValidString(b) {
			return
		}
		ac := sourceTestAC([]string{a, b}, PresetMemoryEfficient, false)
		got, err := ac.Scan(context.Background(), text, &ScanOptions{Kind: MatchKindLeftmostLongest, MaxCandidates: 10000})
		if err != nil {
			t.Fatal(err)
		}
		expected, err := ac.FindMatches(text, &MatchOptions{Kind: MatchKindLeftmostLongest})
		if err != nil {
			t.Fatal(err)
		}
		projected := make([]Match, 0, len(got.Matches))
		for _, m := range got.Matches {
			projected = append(projected, Match{Keyword: m.Keyword, Start: m.Start, End: m.End})
			if m.Text != text[m.ByteStart:m.ByteEnd] {
				t.Fatal(m)
			}
		}
		if !reflect.DeepEqual(projected, expected) {
			t.Fatal(text, a, b, projected, expected)
		}
	})
}

func TestScanSensitiveAndMalformedUTF8(t *testing.T) {
	ctx := context.Background()
	ac := sourceTestAC([]string{"A", "a", "�"}, PresetMemoryEfficient, true)
	text := "A a \xff"
	result, err := ac.Scan(ctx, text, nil)
	if err != nil || len(result.Matches) != 3 {
		t.Fatal(result, err)
	}
	for _, m := range result.Matches {
		if m.Text != text[m.ByteStart:m.ByteEnd] {
			t.Fatal(m)
		}
	}
	if result.Matches[0].Keyword != "A" || result.Matches[2].Text != "\xff" {
		t.Fatal(result)
	}
}
