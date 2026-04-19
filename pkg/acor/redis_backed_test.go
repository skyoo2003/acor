// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"strings"
	"sync"
	"testing"
	"time"

	miniredis "github.com/alicebob/miniredis/v2"
)

func newTestPresetRedis(t *testing.T, preset Preset) *AhoCorasick {
	t.Helper()
	mr := miniredis.RunT(t)
	ac, err := Create(&AhoCorasickArgs{
		Addr:   mr.Addr(),
		Name:   t.Name(),
		Preset: preset,
	})
	if err != nil {
		t.Fatalf("Create preset-redis: %v", err)
	}
	t.Cleanup(func() { _ = ac.Close() })
	return ac
}

func TestPresetNew(t *testing.T) {
	mr := miniredis.RunT(t)
	ac, err := Create(&AhoCorasickArgs{
		Addr:   mr.Addr(),
		Name:   "test-new",
		Preset: PresetBalanced,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer ac.Close()

	info, err := ac.Info()
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if info.Keywords != 0 {
		t.Errorf("Keywords = %d, want 0", info.Keywords)
	}
	if info.Preset != PresetBalanced {
		t.Errorf("Preset = %d, want %d", info.Preset, PresetBalanced)
	}
}

func TestPresetAddFind(t *testing.T) {
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
			ac := newTestPresetRedis(t, tt.preset)

			for _, kw := range tt.keywords {
				added, err := ac.Add(kw)
				if err != nil {
					t.Fatalf("Add(%q): %v", kw, err)
				}
				if added != 1 {
					t.Errorf("Add(%q) = %d, want 1", kw, added)
				}
			}

			added, err := ac.Add(tt.keywords[0])
			if err != nil {
				t.Fatalf("Add duplicate: %v", err)
			}
			if added != 0 {
				t.Errorf("Add duplicate = %d, want 0", added)
			}

			matched, err := ac.Find(tt.text)
			if err != nil {
				t.Fatalf("Find: %v", err)
			}
			if !stringSlicesEqual(matched, tt.want) {
				t.Errorf("Find = %v, want %v", matched, tt.want)
			}
		})
	}
}

func TestPresetEmptyText(t *testing.T) {
	ac := newTestPresetRedis(t, PresetBalanced)

	ac.Add("hello") //nolint:errcheck

	matched, err := ac.Find("")
	if err != nil {
		t.Fatalf("Find empty: %v", err)
	}
	if len(matched) != 0 {
		t.Errorf("Find empty = %v, want []", matched)
	}

	idx, err := ac.FindIndex("")
	if err != nil {
		t.Fatalf("FindIndex empty: %v", err)
	}
	if len(idx) != 0 {
		t.Errorf("FindIndex empty = %v, want {}", idx)
	}
}

func TestPresetAllPresets(t *testing.T) {
	presets := []Preset{PresetSpeed, PresetBalanced, PresetMemoryEfficient, PresetUltimate}
	for _, preset := range presets {
		t.Run(preset.String(), func(t *testing.T) {
			ac := newTestPresetRedis(t, preset)

			ac.Add("abc") //nolint:errcheck
			ac.Add("bc")  //nolint:errcheck
			ac.Add("c")   //nolint:errcheck

			matched, err := ac.Find("abc")
			if err != nil {
				t.Fatalf("Find: %v", err)
			}
			if len(matched) != 3 {
				t.Errorf("Find = %v, want 3 matches", matched)
			}

			idx, err := ac.FindIndex("abc")
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

func TestPresetConcurrent(t *testing.T) {
	ac := newTestPresetRedis(t, PresetBalanced)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			kw := strings.Repeat("x", i+1)
			ac.Add(kw) //nolint:errcheck
		}(i)
	}
	wg.Wait()

	matched, err := ac.Find(strings.Repeat("x", 10))
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(matched) == 0 {
		t.Error("expected at least one match")
	}
}

func TestPresetInvalidName(t *testing.T) {
	_, err := Create(&AhoCorasickArgs{
		Addr:   "localhost:6379",
		Name:   "bad:name",
		Preset: PresetBalanced,
	})
	if err != ErrInvalidName {
		t.Errorf("error = %v, want ErrInvalidName", err)
	}
}

func TestPresetDefaultPreset(t *testing.T) {
	ac := newTestPresetRedis(t, PresetBalanced)
	info, _ := ac.Info()
	if info.Preset != PresetBalanced {
		t.Errorf("Preset = %d, want %d", info.Preset, PresetBalanced)
	}
}

func TestPresetEmptyKeyword(t *testing.T) {
	ac := newTestPresetRedis(t, PresetBalanced)

	added, err := ac.Add("")
	if err != nil {
		t.Fatalf("Add empty: %v", err)
	}
	if added != 0 {
		t.Errorf("Add empty = %d, want 0", added)
	}

	added, err = ac.Add("  ")
	if err != nil {
		t.Fatalf("Add whitespace: %v", err)
	}
	if added != 0 {
		t.Errorf("Add whitespace = %d, want 0", added)
	}
}

func TestPresetCrossInstanceInvalidation(t *testing.T) {
	mr := miniredis.RunT(t)
	name := "test-cross-instance"

	ac1, err := Create(&AhoCorasickArgs{
		Addr:   mr.Addr(),
		Name:   name,
		Preset: PresetBalanced,
	})
	if err != nil {
		t.Fatalf("Create ac1: %v", err)
	}
	defer func() { _ = ac1.Close() }()

	ac2, err := Create(&AhoCorasickArgs{
		Addr:   mr.Addr(),
		Name:   name,
		Preset: PresetBalanced,
	})
	if err != nil {
		t.Fatalf("Create ac2: %v", err)
	}
	defer func() { _ = ac2.Close() }()

	if _, addErr := ac1.Add("hello"); addErr != nil {
		t.Fatalf("ac1.Add: %v", addErr)
	}

	time.Sleep(50 * time.Millisecond)

	matched, err := ac2.Find("hello world")
	if err != nil {
		t.Fatalf("ac2.Find: %v", err)
	}
	if len(matched) != 1 || matched[0] != testKeywordHello {
		t.Errorf("ac2.Find = %v, want [hello]", matched)
	}
}

func TestPresetDegradedMode(t *testing.T) {
	mr := miniredis.RunT(t)
	ac, err := Create(&AhoCorasickArgs{
		Addr:   mr.Addr(),
		Name:   "test-degraded",
		Preset: PresetBalanced,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer ac.Close()

	ac.Add("hello") //nolint:errcheck

	mr.Close()

	matched, err := ac.Find("hello world")
	if err != nil {
		t.Fatalf("Find degraded: %v", err)
	}
	if len(matched) != 1 || matched[0] != testKeywordHello {
		t.Errorf("Find degraded = %v, want [hello]", matched)
	}
}

func TestPresetRedisSuggestError(t *testing.T) {
	ac := newTestPresetRedis(t, PresetBalanced)
	_, err := ac.Suggest("he")
	if err != ErrSuggestRequiresRedis {
		t.Errorf("expected ErrSuggestRequiresRedis, got %v", err)
	}
}

func TestPresetRedisSuggestIndexError(t *testing.T) {
	ac := newTestPresetRedis(t, PresetBalanced)
	_, err := ac.SuggestIndex("he")
	if err != ErrSuggestRequiresRedis {
		t.Errorf("expected ErrSuggestRequiresRedis, got %v", err)
	}
}

func TestPresetRedisV1Error(t *testing.T) {
	_, err := Create(&AhoCorasickArgs{
		Addr:          "localhost:6379",
		Name:          "test-v1",
		Preset:        PresetBalanced,
		SchemaVersion: SchemaV1,
	})
	if err != ErrPresetRequiresV2 {
		t.Errorf("expected ErrPresetRequiresV2, got %v", err)
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
