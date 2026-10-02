// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

// Count input consumed by the real native traversal, whether Contains uses its
// string callback path or advances its cursor directly.
type compositeContainsProbe struct {
	matchEngine
	native *Engine
	read   int
}

func (p *compositeContainsProbe) matchString(text string, emit func(string, int, int) bool) {
	p.native.Stream(func() (rune, bool) {
		if text == "" {
			return 0, false
		}
		ch, size := utf8.DecodeRuneInString(text)
		text = text[size:]
		p.read++
		return ch, true
	}, emit)
}

func (p *compositeContainsProbe) cursor() runeCursor {
	cursor := p.native.cursor()
	return func(ch rune) outputCursor {
		p.read++
		return cursor(ch)
	}
}

func TestCompositeContainsStopsAtLaterShardFirstRuneHit(t *testing.T) {
	for _, preset := range allPresets {
		for _, concurrency := range []int{1, 2} {
			for _, hit := range []string{"a", "한"} {
				t.Run(preset.String()+"/"+strconv.Itoa(concurrency)+"/"+hit, func(t *testing.T) {
					words := []string{"missing", "absent", "unmatched", hit}
					shards := make([]*Engine, len(words))
					probes := make([]*compositeContainsProbe, len(words))
					for i, word := range words {
						native := New(preset)
						native.Build(map[string]struct{}{word: {}})
						probes[i] = &compositeContainsProbe{matchEngine: native.impl, native: native}
						shards[i] = &Engine{impl: probes[i], maxKeywordRunes: native.MaxKeywordRunes()}
					}
					if !NewComposite(shards, concurrency).Contains(hit + strings.Repeat("x", 100000)) {
						t.Fatal("missed the first-rune hit in the last shard")
					}
					for i, probe := range probes {
						if probe.read > 1 {
							t.Errorf("shard %d consumed %d runes despite a first-rune hit", i, probe.read)
						}
					}
				})
			}
		}
	}
}

func TestCompositeContainsPreservesNativeResults(t *testing.T) {
	tests := []struct {
		text string
		want bool
	}{
		{"", false},
		{"ab", false},
		{"none", false},
		{"ABCD", false},
		{"abcd", true},
		{"abc", true}, // Only the suffix "bc" is terminal at this state.
		{"aabc", true},
		{"xabc", true},
		{"한국", true},
		{"\xff", true},
	}
	for _, preset := range allPresets {
		t.Run(preset.String(), func(t *testing.T) {
			shards := []*Engine{New(preset), New(preset), New(preset)}
			shards[0].Build(map[string]struct{}{"abcd": {}, "bc": {}})
			shards[1].Build(map[string]struct{}{"한국": {}, "�": {}})
			e := NewComposite(shards, 2)
			for _, tt := range tests {
				if got := e.Contains(tt.text); got != tt.want {
					t.Errorf("Contains(%q) = %v, want %v", tt.text, got, tt.want)
				}
			}
			if NewComposite(shards[2:], 2).Contains("abcd 한국") {
				t.Fatal("empty composite reported a match")
			}
		})
	}
}

// The probe delegates to real native engines and counts the actual per-hit
// callbacks. FindSet must skip repeated state chains before those callbacks.
type compositeSetProbe struct {
	matchEngine
	native *Engine
	emits  int
}

func (p *compositeSetProbe) matchString(text string, emit func(string, int, int) bool) {
	p.matchEngine.matchString(text, func(keyword string, start, end int) bool {
		p.emits++
		return emit(keyword, start, end)
	})
}
func (p *compositeSetProbe) cursor() runeCursor { return p.native.cursor() }

func TestCompositeFindSetSkipsRepeatedSuffixChains(t *testing.T) {
	saved := dedupHashMin
	t.Cleanup(func() { dedupHashMin = saved })
	for _, mode := range []struct {
		name      string
		threshold int
	}{{"bitsets", saved}, {"maps", 1}} {
		dedupHashMin = mode.threshold
		for _, preset := range allPresets {
			t.Run(mode.name+"/"+preset.String(), func(t *testing.T) {
				sets := make([]map[string]struct{}, 4)
				for i := range sets {
					sets[i] = make(map[string]struct{})
				}
				want := make([]string, 0, 67)
				for n := 1; n <= 64; n++ {
					word := strings.Repeat("a", n)
					sets[n%4][word] = struct{}{}
					want = append(want, word)
				}
				sets[0]["한국"], sets[1]["한국어"], sets[2]["어"] = struct{}{}, struct{}{}, struct{}{}
				want = append(want, "한국", "한국어", "어")
				shards := make([]*Engine, len(sets))
				probes := make([]*compositeSetProbe, len(sets))
				for i, set := range sets {
					native := New(preset)
					native.Build(set)
					probes[i] = &compositeSetProbe{matchEngine: native.impl, native: native}
					shards[i] = &Engine{impl: probes[i], maxKeywordRunes: native.MaxKeywordRunes()}
				}
				got := NewComposite(shards, 2).FindSet(strings.Repeat("a", 4096) + " 한국어 한국어")
				if !reflect.DeepEqual(got, want) {
					t.Fatal("first-match order changed", got)
				}
				callbacks := 0
				for _, probe := range probes {
					callbacks += probe.emits
				}
				if callbacks > len(want) {
					t.Fatalf("FindSet traversed %d occurrence callbacks for %d unique keywords", callbacks, len(want))
				}
			})
		}
	}
}

func TestCompositeFirstMatchesPreservesRuneSpans(t *testing.T) {
	want := []compositeMatch{{"a", 0, 1}, {"aa", 0, 2}, {"한국", 3, 5},
		{"한국어", 3, 6}, {"어", 5, 6}, {"�", 7, 8}}
	for _, preset := range allPresets {
		t.Run(preset.String(), func(t *testing.T) {
			e := New(preset)
			e.Build(map[string]struct{}{"a": {}, "aa": {}, "한국": {}, "한국어": {}, "어": {}, "�": {}})
			if got := e.firstMatches("aa 한국어 \xff aa 한국어"); !reflect.DeepEqual(got, want) {
				t.Fatal(got)
			}
		})
	}
}

func BenchmarkCompositeFindSetSuffixNested(b *testing.B) {
	sets := make([]map[string]struct{}, 4)
	for i := range sets {
		sets[i] = make(map[string]struct{})
	}
	for n := 1; n <= 500; n++ {
		sets[n%4][strings.Repeat("a", n)] = struct{}{}
	}
	shards := make([]*Engine, len(sets))
	for i, set := range sets {
		shards[i] = New(PresetMemoryEfficient)
		shards[i].Build(set)
	}
	e := NewComposite(shards, 2)
	text := strings.Repeat("a", 100000)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if got := e.FindSet(text); len(got) != 500 {
			b.Fatal(len(got))
		}
	}
}

// Concurrent text chunks must share the request's worker limit rather than
// multiplying it by the number of chunk workers.
func TestCompositeSharesWorkerBound(t *testing.T) {
	shards := make([]*Engine, 4)
	for i := range shards {
		shards[i] = New(PresetMemoryEfficient)
		shards[i].Build(map[string]struct{}{"word": {}})
	}
	c := NewComposite(shards, 2).impl.(*compositeEngine)
	entered := make(chan struct{}, 12)
	release := make(chan struct{})
	var done sync.WaitGroup
	for range 3 {
		done.Go(func() { c.eachShard(func(int) { entered <- struct{}{}; <-release }) })
	}
	<-entered
	<-entered
	exceeded := false
	select {
	case <-entered:
		exceeded = true
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	done.Wait()
	if exceeded {
		t.Fatal("parallel chunks exceeded the shared shard worker bound")
	}
}

// Emitting a match must precede requesting its following rune. Scan relies on
// this boundary for candidate budgets and leftmost-longest window flushing.
func TestCompositeStreamingDoesNotReadAhead(t *testing.T) {
	for _, preset := range []Preset{PresetMemoryEfficient, PresetBalanced, PresetSpeed} {
		t.Run(preset.String(), func(t *testing.T) {
			shards := []*Engine{New(preset), New(preset)}
			shards[0].Build(map[string]struct{}{"she": {}})
			shards[1].Build(map[string]struct{}{"he": {}})
			engine := NewComposite(shards, 2)
			input := []rune("she more input")
			read := 0
			var found string
			engine.Stream(func() (rune, bool) {
				if read == len(input) {
					return 0, false
				}
				r := input[read]
				read++
				return r, true
			}, func(keyword string, start, end int) bool {
				found = keyword
				if start != 0 || end != 3 {
					t.Errorf("span = [%d,%d)", start, end)
				}
				return false
			})
			if read != 3 || found != "she" {
				t.Fatal(read, found)
			}
		})
	}
}
