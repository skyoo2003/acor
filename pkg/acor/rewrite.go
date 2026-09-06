// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"
)

const defaultRewriteBytes = 4 << 20

// RewriteOptions bounds an atomic leftmost-longest rewrite. Zero input, match
// and candidate limits use ScanOptions defaults; zero output limit means 4 MiB.
// Any exhausted limit returns an error without a partially rewritten result.
type RewriteOptions struct {
	MaxInputBytes  int
	MaxMatches     int
	MaxCandidates  int
	MaxOutputBytes int
	WholeWord      bool
	WordRune       func(rune) bool
}

// RewriteResult carries the transformed text and the original input spans that
// were changed. Match offsets always refer to the input, not the resulting text.
type RewriteResult struct {
	Text    string
	Matches []SourceMatch
}

// ReplaceText replaces each non-overlapping leftmost-longest match with literal
// replacement text. Replacement text is not searched recursively.
func (ac *AhoCorasick) ReplaceText(ctx context.Context, text, replacement string, opts *RewriteOptions) (*RewriteResult, error) {
	return ac.rewrite(ctx, text, replacement, 0, false, opts)
}

// MaskText replaces each matched original rune with mask, preserving rune count.
// Overlaps are resolved leftmost-longest. Invalid mask runes are rejected.
func (ac *AhoCorasick) MaskText(ctx context.Context, text string, mask rune, opts *RewriteOptions) (*RewriteResult, error) {
	if !utf8.ValidRune(mask) {
		return nil, errors.New("acor: invalid mask rune")
	}
	return ac.rewrite(ctx, text, "", mask, true, opts)
}

// ReplaceText rewrites using one serving engine; positions refer to the input.
func (v *VersionedCollection) ReplaceText(ctx context.Context, text, replacement string, opts *RewriteOptions) (*RewriteResult, error) {
	ac, err := v.search(ctx)
	if err != nil {
		return nil, err
	}
	return ac.ReplaceText(ctx, text, replacement, opts)
}

// MaskText masks using one serving engine and one mask rune per input rune.
func (v *VersionedCollection) MaskText(ctx context.Context, text string, mask rune, opts *RewriteOptions) (*RewriteResult, error) {
	ac, err := v.search(ctx)
	if err != nil {
		return nil, err
	}
	return ac.MaskText(ctx, text, mask, opts)
}
func (ac *AhoCorasick) rewrite(ctx context.Context, text, replacement string, mask rune, masking bool, opts *RewriteOptions) (*RewriteResult, error) {
	var o RewriteOptions
	if opts != nil {
		o = *opts
	}
	if o.MaxOutputBytes < 0 {
		return nil, errors.New("acor: negative output limit")
	}
	if o.MaxOutputBytes == 0 {
		o.MaxOutputBytes = defaultRewriteBytes
	}
	scan, err := ac.Scan(ctx, text, &ScanOptions{MaxInputBytes: o.MaxInputBytes, MaxMatches: o.MaxMatches, MaxCandidates: o.MaxCandidates,
		Kind: MatchKindLeftmostLongest, WholeWord: o.WholeWord, WordRune: o.WordRune})
	if err != nil {
		return nil, err
	}
	if scan.Truncated {
		return nil, ErrMatchLimit
	}
	return renderRewrite(ctx, text, replacement, mask, masking, o.MaxOutputBytes, scan.Matches)
}
func renderRewrite(ctx context.Context, text, replacement string, mask rune, masking bool, limit int, matches []SourceMatch) (*RewriteResult, error) {
	total, cursor := 0, 0
	for _, m := range matches {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		unchanged := m.ByteStart - cursor
		if unchanged > limit-total {
			return nil, ErrOutputLimit
		}
		total += unchanged
		size := len(replacement)
		if masking {
			width := utf8.RuneLen(mask)
			count := m.End - m.Start
			if count > (limit-total)/width {
				return nil, ErrOutputLimit
			}
			size = count * width
		}
		if size > limit-total {
			return nil, ErrOutputLimit
		}
		total += size
		cursor = m.ByteEnd
	}
	if len(text)-cursor > limit-total {
		return nil, ErrOutputLimit
	}
	total += len(text) - cursor
	var out strings.Builder
	out.Grow(total)
	cursor = 0
	for _, m := range matches {
		out.WriteString(text[cursor:m.ByteStart])
		if !masking {
			out.WriteString(replacement)
		} else {
			for i := m.Start; i < m.End; i++ {
				if i%scanContextStride == 0 {
					if err := ctx.Err(); err != nil {
						return nil, err
					}
				}
				out.WriteRune(mask)
			}
		}
		cursor = m.ByteEnd
	}
	out.WriteString(text[cursor:])
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &RewriteResult{Text: out.String(), Matches: matches}, nil
}
