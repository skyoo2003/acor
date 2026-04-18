// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"strings"
	"testing"
)

func TestV2LocalFindContextCancellation(t *testing.T) {
	ops := &v2Operations{}

	prefixes := []string{"", "h", "he", "s", "sh", "she"}
	prefixSet := make(map[string]struct{}, len(prefixes))
	for _, p := range prefixes {
		prefixSet[p] = struct{}{}
	}
	outputs := map[string][]string{
		"he":  {"he"},
		"she": {"he", "she"},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	longText := strings.Repeat("she sells sea shells ", 100)
	result, _ := ops.localFind(ctx, longText, prefixSet, outputs)

	if result == nil {
		t.Fatal("localFind should return non-nil slice on context cancellation")
	}
}

func TestV2LocalFindIndexContextCancellation(t *testing.T) {
	ops := &v2Operations{}

	prefixes := []string{"", "h", "he", "s", "sh", "she"}
	prefixSet := make(map[string]struct{}, len(prefixes))
	for _, p := range prefixes {
		prefixSet[p] = struct{}{}
	}
	outputs := map[string][]string{
		"he":  {"he"},
		"she": {"he", "she"},
	}
	outputRuneLen := buildOutputRuneLen(outputs)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	longText := strings.Repeat("she sells sea shells ", 100)
	result, _ := ops.localFindIndex(ctx, longText, prefixSet, outputs, outputRuneLen)

	if result == nil {
		t.Fatal("localFindIndex should return non-nil map on context cancellation")
	}
}

func TestV2LocalFindNormal(t *testing.T) {
	ops := &v2Operations{}

	prefixes := []string{"", "h", "he", "s", "sh", "she"}
	prefixSet := make(map[string]struct{}, len(prefixes))
	for _, p := range prefixes {
		prefixSet[p] = struct{}{}
	}
	outputs := map[string][]string{
		"he":  {"he"},
		"she": {"he", "she"},
	}

	ctx := context.Background()
	result, _ := ops.localFind(ctx, "she", prefixSet, outputs)

	if !equalStringSets(result, []string{"he", "she"}) {
		t.Errorf("localFind('she') = %v, want [he she]", result)
	}
}

func TestV2LocalFindIndexNormal(t *testing.T) {
	ops := &v2Operations{}

	prefixes := []string{"", "h", "he", "s", "sh", "she"}
	prefixSet := make(map[string]struct{}, len(prefixes))
	for _, p := range prefixes {
		prefixSet[p] = struct{}{}
	}
	outputs := map[string][]string{
		"he":  {"he"},
		"she": {"he", "she"},
	}
	outputRuneLen := buildOutputRuneLen(outputs)

	ctx := context.Background()
	result, _ := ops.localFindIndex(ctx, "she", prefixSet, outputs, outputRuneLen)

	assertIndexResults(t, result, map[string][]int{
		"he":  {1},
		"she": {0},
	})
}

func TestFindFailState(t *testing.T) {
	ops := &v2Operations{}

	prefixSet := map[string]struct{}{
		"":    {},
		"h":   {},
		"he":  {},
		"s":   {},
		"sh":  {},
		"she": {},
	}

	tests := []struct {
		state string
		want  string
	}{
		{"he", ""},
		{"she", "he"},
		{"sh", "h"},
		{"xyz", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.state, func(t *testing.T) {
			got := ops.findFailState(tt.state, prefixSet)
			if got != tt.want {
				t.Errorf("findFailState(%q) = %q, want %q", tt.state, got, tt.want)
			}
		})
	}
}

func TestFindFailStateLongestSuffix(t *testing.T) {
	ops := &v2Operations{}

	prefixSet := map[string]struct{}{
		"":    {},
		"a":   {},
		"ab":  {},
		"abc": {},
		"bc":  {},
		"c":   {},
	}

	got := ops.findFailState("babc", prefixSet)
	if got != "abc" {
		t.Errorf("findFailState('babc') = %q, want 'abc'", got)
	}
}

func TestV2LocalFindWithLongText(t *testing.T) {
	ops := &v2Operations{}

	prefixes := []string{"", "a", "ab", "abc"}
	prefixSet := make(map[string]struct{}, len(prefixes))
	for _, p := range prefixes {
		prefixSet[p] = struct{}{}
	}
	outputs := map[string][]string{
		"abc": {"abc"},
	}

	text := strings.Repeat("x", 1000) + "abc"
	result, _ := ops.localFind(context.Background(), text, prefixSet, outputs)

	if !containsAll(result, "abc") {
		t.Errorf("localFind should find 'abc', got %v", result)
	}
}

func TestV2LocalFindIndexWithLongText(t *testing.T) {
	ops := &v2Operations{}

	prefixes := []string{"", "a", "ab", "abc"}
	prefixSet := make(map[string]struct{}, len(prefixes))
	for _, p := range prefixes {
		prefixSet[p] = struct{}{}
	}
	outputs := map[string][]string{
		"abc": {"abc"},
	}
	outputRuneLen := buildOutputRuneLen(outputs)

	text := strings.Repeat("x", 1000) + "abc"
	result, _ := ops.localFindIndex(context.Background(), text, prefixSet, outputs, outputRuneLen)

	if _, ok := result["abc"]; !ok {
		t.Error("localFindIndex should find 'abc'")
	}
}
