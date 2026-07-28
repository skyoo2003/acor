// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"strings"
	"testing"
)

// parallelCancellationTargets covers both ops implementations: V2 reaches Redis
// per find, the preset mode matches against a warm in-memory engine and so only
// sees cancellation if it checks ctx itself.
func parallelCancellationTargets(t *testing.T) map[string]*AhoCorasick {
	t.Helper()

	v2, mr := createAhoCorasick(t)
	t.Cleanup(func() { _ = v2.Close(); mr.Close() })

	presetMR := createTestRedisServer(t)
	preset, err := Create(&AhoCorasickArgs{Addr: presetMR.Addr(), Name: "cancel-preset", Preset: PresetBalanced})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = preset.Close() })

	for _, ac := range []*AhoCorasick{v2, preset} {
		if _, err := ac.Add("he"); err != nil {
			t.Fatal(err)
		}
	}
	return map[string]*AhoCorasick{"v2": v2, "preset": preset}
}

func TestParallelContextCancellation(t *testing.T) {
	longText := strings.Repeat("he is here ", 100)

	for name, ac := range parallelCancellationTargets(t) {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()

			got, err := ac.FindParallelContext(ctx, longText, &ParallelOptions{
				ChunkSize: 20,
				Workers:   2,
			})
			if err == nil {
				t.Fatalf("FindParallelContext() on a canceled context returned %v, want an error", got)
			}
		})
	}
}

func TestParallelIndexContextCancellation(t *testing.T) {
	longText := strings.Repeat("he is here ", 100)

	for name, ac := range parallelCancellationTargets(t) {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()

			got, err := ac.FindIndexParallelContext(ctx, longText, &ParallelOptions{
				ChunkSize: 20,
				Workers:   2,
			})
			if err == nil {
				t.Fatalf("FindIndexParallelContext() on a canceled context returned %v, want an error", got)
			}
		})
	}
}

func TestFindParallelInvalidChunkSize(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	_, err := ac.FindParallel("test", &ParallelOptions{ChunkSize: 0})
	if err == nil {
		t.Error("expected error for ChunkSize=0")
	}
}

func TestFindIndexParallelInvalidChunkSize(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	_, err := ac.FindIndexParallel("test", &ParallelOptions{ChunkSize: -1})
	if err == nil {
		t.Error("expected error for negative ChunkSize")
	}
}
