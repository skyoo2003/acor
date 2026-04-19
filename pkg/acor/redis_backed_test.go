// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	miniredis "github.com/alicebob/miniredis/v2"
)

func newTestRedisBacked(t *testing.T, preset Preset) (*RedisBackedAC, *miniredis.Miniredis) { //nolint:unparam
	t.Helper()
	mr := miniredis.RunT(t)
	ac, err := NewRedisBacked(context.Background(), &RedisBackedArgs{
		AhoCorasickArgs: AhoCorasickArgs{
			Addr: mr.Addr(),
			Name: t.Name(),
		},
		Preset: preset,
	})
	if err != nil {
		t.Fatalf("NewRedisBacked: %v", err)
	}
	t.Cleanup(func() { _ = ac.Close() })
	return ac, mr
}

func TestRedisBackedNew(t *testing.T) {
	mr := miniredis.RunT(t)
	ac, err := NewRedisBacked(context.Background(), &RedisBackedArgs{
		AhoCorasickArgs: AhoCorasickArgs{
			Addr: mr.Addr(),
			Name: "test-new",
		},
		Preset: PresetBalanced,
	})
	if err != nil {
		t.Fatalf("NewRedisBacked: %v", err)
	}
	defer func() { _ = ac.Close() }()

	if ac.Preset() != PresetBalanced {
		t.Errorf("Preset() = %d, want %d", ac.Preset(), PresetBalanced)
	}

	info, err := ac.Info(context.Background())
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if info.Keywords != 0 {
		t.Errorf("Keywords = %d, want 0", info.Keywords)
	}
}

func TestRedisBackedAddFind(t *testing.T) {
	tests := []struct {
		name     string
		preset   Preset
		keywords []string
		text     string
		want     []string
	}{
		{
			name:     "single keyword",
			preset:   PresetSpeed,
			keywords: []string{"hello"},
			text:     "hello world",
			want:     []string{"hello"},
		},
		{
			name:     "multiple keywords",
			preset:   PresetBalanced,
			keywords: []string{"he", "she", "his", "hers"},
			text:     "ushers",
			want:     []string{"she", "he", "hers"},
		},
		{
			name:     "unicode",
			preset:   PresetMemoryEfficient,
			keywords: []string{"한글", "일본어"},
			text:     "한글과 일본어",
			want:     []string{"한글", "일본어"},
		},
		{
			name:     "ultimate preset",
			preset:   PresetUltimate,
			keywords: []string{"abc", "bc", "c"},
			text:     "abc",
			want:     []string{"abc", "bc", "c"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ac, _ := newTestRedisBacked(t, tt.preset)
			ctx := context.Background()

			for _, kw := range tt.keywords {
				added, err := ac.Add(ctx, kw)
				if err != nil {
					t.Fatalf("Add(%q): %v", kw, err)
				}
				if added != 1 {
					t.Errorf("Add(%q) = %d, want 1", kw, added)
				}
			}

			added, err := ac.Add(ctx, tt.keywords[0])
			if err != nil {
				t.Fatalf("Add duplicate: %v", err)
			}
			if added != 0 {
				t.Errorf("Add duplicate = %d, want 0", added)
			}

			matched, err := ac.Find(ctx, tt.text)
			if err != nil {
				t.Fatalf("Find: %v", err)
			}
			if !stringSlicesEqual(matched, tt.want) {
				t.Errorf("Find = %v, want %v", matched, tt.want)
			}
		})
	}
}

func TestRedisBackedFindIndex(t *testing.T) {
	ac, _ := newTestRedisBacked(t, PresetBalanced)
	ctx := context.Background()

	ac.Add(ctx, "he")  //nolint:errcheck,gosec
	ac.Add(ctx, "she") //nolint:errcheck,gosec

	matched, err := ac.FindIndex(ctx, "ushers")
	if err != nil {
		t.Fatalf("FindIndex: %v", err)
	}

	if len(matched["he"]) != 1 {
		t.Errorf("he count = %d, want 1", len(matched["he"]))
	}
	if len(matched["she"]) != 1 {
		t.Errorf("she count = %d, want 1", len(matched["she"]))
	}
	if matched["she"][0] != 1 {
		t.Errorf("she index = %d, want 1", matched["she"][0])
	}
	if matched["he"][0] != 2 {
		t.Errorf("he index = %d, want 2", matched["he"][0])
	}
}

func TestRedisBackedRemove(t *testing.T) {
	ac, _ := newTestRedisBacked(t, PresetBalanced)
	ctx := context.Background()

	ac.Add(ctx, "hello") //nolint:errcheck,gosec
	ac.Add(ctx, "world") //nolint:errcheck,gosec

	removed, err := ac.Remove(ctx, "hello")
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if removed != 1 {
		t.Errorf("Remove = %d, want 1", removed)
	}

	removed, err = ac.Remove(ctx, "hello")
	if err != nil {
		t.Fatalf("Remove non-existent: %v", err)
	}
	if removed != 0 {
		t.Errorf("Remove non-existent = %d, want 0", removed)
	}

	matched, err := ac.Find(ctx, "hello world")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(matched) != 1 || matched[0] != "world" {
		t.Errorf("Find = %v, want [world]", matched)
	}
}

func TestRedisBackedFlush(t *testing.T) {
	ac, _ := newTestRedisBacked(t, PresetBalanced)
	ctx := context.Background()

	ac.Add(ctx, "hello") //nolint:errcheck,gosec
	ac.Add(ctx, "world") //nolint:errcheck,gosec

	if err := ac.Flush(ctx); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	matched, err := ac.Find(ctx, "hello world")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(matched) != 0 {
		t.Errorf("Find after Flush = %v, want []", matched)
	}

	info, _ := ac.Info(ctx)
	if info.Keywords != 0 {
		t.Errorf("Keywords after Flush = %d, want 0", info.Keywords)
	}
}

func TestRedisBackedEmptyText(t *testing.T) {
	ac, _ := newTestRedisBacked(t, PresetBalanced)
	ctx := context.Background()

	ac.Add(ctx, "hello") //nolint:errcheck,gosec

	matched, err := ac.Find(ctx, "")
	if err != nil {
		t.Fatalf("Find empty: %v", err)
	}
	if len(matched) != 0 {
		t.Errorf("Find empty = %v, want []", matched)
	}

	idx, err := ac.FindIndex(ctx, "")
	if err != nil {
		t.Fatalf("FindIndex empty: %v", err)
	}
	if len(idx) != 0 {
		t.Errorf("FindIndex empty = %v, want {}", idx)
	}
}

func TestRedisBackedCaseSensitive(t *testing.T) {
	mr := miniredis.RunT(t)
	ac, err := NewRedisBacked(context.Background(), &RedisBackedArgs{
		AhoCorasickArgs: AhoCorasickArgs{
			Addr: mr.Addr(),
			Name: "test-casesens",
		},
		Preset:        PresetBalanced,
		CaseSensitive: true,
	})
	if err != nil {
		t.Fatalf("NewRedisBacked: %v", err)
	}
	defer func() { _ = ac.Close() }()

	ctx := context.Background()
	ac.Add(ctx, "Hello") //nolint:errcheck,gosec

	matched, _ := ac.Find(ctx, "hello")
	if len(matched) != 0 {
		t.Errorf("case-sensitive Find lowercase = %v, want []", matched)
	}

	matched, _ = ac.Find(ctx, "Hello")
	if len(matched) != 1 || matched[0] != "Hello" {
		t.Errorf("case-sensitive Find exact = %v, want [Hello]", matched)
	}
}

func TestRedisBackedAllPresets(t *testing.T) {
	presets := []Preset{PresetSpeed, PresetBalanced, PresetMemoryEfficient, PresetUltimate}
	for _, preset := range presets {
		t.Run(preset.String(), func(t *testing.T) {
			ac, _ := newTestRedisBacked(t, preset)
			ctx := context.Background()

			ac.Add(ctx, "abc") //nolint:errcheck,gosec
			ac.Add(ctx, "bc")  //nolint:errcheck,gosec
			ac.Add(ctx, "c")   //nolint:errcheck,gosec

			matched, err := ac.Find(ctx, "abc")
			if err != nil {
				t.Fatalf("Find: %v", err)
			}
			if len(matched) != 3 {
				t.Errorf("Find = %v, want 3 matches", matched)
			}

			idx, err := ac.FindIndex(ctx, "abc")
			if err != nil {
				t.Fatalf("FindIndex: %v", err)
			}
			if len(idx["abc"]) != 1 || idx["abc"][0] != 0 {
				t.Errorf("abc index = %v, want [0]", idx["abc"])
			}
			if len(idx["bc"]) != 1 || idx["bc"][0] != 1 {
				t.Errorf("bc index = %v, want [1]", idx["bc"])
			}
			if len(idx["c"]) != 1 || idx["c"][0] != 2 {
				t.Errorf("c index = %v, want [2]", idx["c"])
			}
		})
	}
}

func TestRedisBackedConcurrent(t *testing.T) {
	ac, _ := newTestRedisBacked(t, PresetBalanced)
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			kw := strings.Repeat("x", i+1)
			ac.Add(ctx, kw) //nolint:errcheck,gosec
		}(i)
	}
	wg.Wait()

	matched, err := ac.Find(ctx, strings.Repeat("x", 10))
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(matched) == 0 {
		t.Error("expected at least one match")
	}
}

func TestRedisBackedInvalidName(t *testing.T) {
	_, err := NewRedisBacked(context.Background(), &RedisBackedArgs{
		AhoCorasickArgs: AhoCorasickArgs{
			Addr: "localhost:6379",
			Name: "bad:name",
		},
	})
	if err != ErrInvalidName {
		t.Errorf("error = %v, want ErrInvalidName", err)
	}
}

func TestRedisBackedDefaultPreset(t *testing.T) {
	ac, _ := newTestRedisBacked(t, PresetDefault)
	if ac.Preset() != PresetBalanced {
		t.Errorf("Preset() = %d, want %d", ac.Preset(), PresetBalanced)
	}
}

func TestRedisBackedEmptyKeyword(t *testing.T) {
	ac, _ := newTestRedisBacked(t, PresetBalanced)
	ctx := context.Background()

	added, err := ac.Add(ctx, "")
	if err != nil {
		t.Fatalf("Add empty: %v", err)
	}
	if added != 0 {
		t.Errorf("Add empty = %d, want 0", added)
	}

	added, err = ac.Add(ctx, "  ")
	if err != nil {
		t.Fatalf("Add whitespace: %v", err)
	}
	if added != 0 {
		t.Errorf("Add whitespace = %d, want 0", added)
	}
}

func TestRedisBackedInfo(t *testing.T) {
	ac, _ := newTestRedisBacked(t, PresetSpeed)
	ctx := context.Background()

	ac.Add(ctx, "hello") //nolint:errcheck,gosec
	ac.Add(ctx, "world") //nolint:errcheck,gosec

	info, err := ac.Info(ctx)
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if info.Keywords != 2 {
		t.Errorf("Keywords = %d, want 2", info.Keywords)
	}
	if info.Nodes <= 0 {
		t.Errorf("Nodes = %d, want > 0", info.Nodes)
	}
	if info.Preset != PresetSpeed {
		t.Errorf("Preset = %d, want %d", info.Preset, PresetSpeed)
	}
}

func TestRedisBackedCrossInstanceInvalidation(t *testing.T) {
	mr := miniredis.RunT(t)
	name := "test-cross-instance"

	ac1, err := NewRedisBacked(context.Background(), &RedisBackedArgs{
		AhoCorasickArgs: AhoCorasickArgs{Addr: mr.Addr(), Name: name},
		Preset:          PresetBalanced,
	})
	if err != nil {
		t.Fatalf("NewRedisBacked ac1: %v", err)
	}
	defer func() { _ = ac1.Close() }()

	ac2, err := NewRedisBacked(context.Background(), &RedisBackedArgs{
		AhoCorasickArgs: AhoCorasickArgs{Addr: mr.Addr(), Name: name},
		Preset:          PresetBalanced,
	})
	if err != nil {
		t.Fatalf("NewRedisBacked ac2: %v", err)
	}
	defer func() { _ = ac2.Close() }()

	ctx := context.Background()
	if _, addErr := ac1.Add(ctx, "hello"); addErr != nil {
		t.Fatalf("ac1.Add: %v", addErr)
	}

	time.Sleep(50 * time.Millisecond)

	matched, err := ac2.Find(ctx, "hello world")
	if err != nil {
		t.Fatalf("ac2.Find: %v", err)
	}
	if len(matched) != 1 || matched[0] != testKeywordHello {
		t.Errorf("ac2.Find = %v, want [hello]", matched)
	}
}

func TestRedisBackedDegradedMode(t *testing.T) {
	mr := miniredis.RunT(t)
	ac, err := NewRedisBacked(context.Background(), &RedisBackedArgs{
		AhoCorasickArgs: AhoCorasickArgs{Addr: mr.Addr(), Name: "test-degraded"},
		Preset:          PresetBalanced,
	})
	if err != nil {
		t.Fatalf("NewRedisBacked: %v", err)
	}
	defer func() { _ = ac.Close() }()

	ctx := context.Background()
	ac.Add(ctx, "hello") //nolint:errcheck,gosec

	mr.Close()

	matched, err := ac.Find(ctx, "hello world")
	if err != nil {
		t.Fatalf("Find degraded: %v", err)
	}
	if len(matched) != 1 || matched[0] != testKeywordHello {
		t.Errorf("Find degraded = %v, want [hello]", matched)
	}
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
