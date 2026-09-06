// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"maps"
	"reflect"
	"testing"
)

func TestBuildSequenceContextParity(t *testing.T) {
	words := map[string]struct{}{"he": {}, "she": {}, "hers": {}, "한국": {}, "한국어": {}, "aa": {}, "aaa": {}}
	for _, preset := range []Preset{PresetSpeed, PresetBalanced, PresetMemoryEfficient} {
		t.Run(preset.String(), func(t *testing.T) {
			old := New(preset)
			old.Build(words)
			next := New(preset)
			if err := next.BuildSequenceContext(context.Background(), maps.Keys(words), len(words)); err != nil {
				t.Fatal(err)
			}
			text := "she hers 한국어 aaaa"
			if !reflect.DeepEqual(old.FindIndex(text), next.FindIndex(text)) {
				t.Fatal("context builder changed matches")
			}
			if next.MaxKeywordRunes() != old.MaxKeywordRunes() {
				t.Fatal("max length mismatch")
			}
		})
	}
}
func TestBuildSequenceCancellationPreservesEngine(t *testing.T) {
	for _, preset := range []Preset{PresetSpeed, PresetBalanced, PresetMemoryEfficient} {
		t.Run(preset.String(), func(t *testing.T) {
			e := New(preset)
			e.Build(map[string]struct{}{"old": {}})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var words iter.Seq[string] = func(yield func(string) bool) {
				for i := range 10000 {
					if i == 1500 {
						cancel()
					}
					if !yield(fmt.Sprintf("new-%08d", i)) {
						return
					}
				}
			}
			if err := e.BuildSequenceContext(ctx, words, 10000); !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if !e.Contains("old") || e.Contains("new-00000001") {
				t.Fatal("partially built engine published")
			}
		})
	}
}

// countContext cancels in CPU build phases after sequence ingestion finishes.
// It makes the cancellation checkpoint coverage deterministic without sleeps.
type countContext struct {
	context.Context
	checks    int
	remaining int
}

func (c *countContext) Err() error {
	c.checks++
	if c.checks >= c.remaining {
		return context.Canceled
	}
	return nil
}
func TestBuildSequenceCPUPhaseCancellation(t *testing.T) {
	words := make(map[string]struct{})
	for i := range 1000 {
		words[fmt.Sprintf("long-shared-prefix-%08d", i)] = struct{}{}
	}
	for _, preset := range []Preset{PresetSpeed, PresetBalanced, PresetMemoryEfficient} {
		ctx := &countContext{Context: context.Background(), remaining: 8}
		e := New(preset)
		e.Build(map[string]struct{}{"old": {}})
		if err := e.BuildSequenceContext(ctx, maps.Keys(words), len(words)); !errors.Is(err, context.Canceled) {
			t.Fatal(preset, err)
		}
		if !e.Contains("old") {
			t.Fatal("lost previous engine")
		}
	}
}
func TestBuildSequenceDoesNotSwallowPanics(t *testing.T) {
	defer func() {
		if recover() != "caller panic" {
			t.Fatal("unexpected panic recovery")
		}
	}()
	e := New(PresetMemoryEfficient)
	_ = e.BuildSequenceContext(context.Background(), func(func(string) bool) { panic("caller panic") }, 0)
}
