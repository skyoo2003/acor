// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/alicebob/miniredis/v2"
)

func TestVersionedShardedSearchAPIParity(t *testing.T) {
	for _, preset := range []Preset{PresetMemoryEfficient, PresetBalanced, PresetSpeed} {
		for _, sensitive := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/%t", preset, sensitive), func(t *testing.T) {
				ctx := context.Background()
				server := miniredis.RunT(t)
				collections := make([]*VersionedCollection, 2)
				for i, count := range []uint16{1, 4} {
					v, err := OpenVersioned(ctx, &VersionedOptions{Redis: AhoCorasickArgs{Addr: server.Addr(), Name: fmt.Sprint(count)},
						ShardCount: count, ShardConcurrency: 2, CaseSensitive: sensitive, Preset: preset})
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { _ = v.Close() })
					r, err := v.Replace(ctx, v.Status().ServingVersion, []string{"he", "she", "hers", "한국", "한국어", "국어", "어", "aa", "aaa", "CAFÉ", strings.Repeat("한", 40)})
					if err != nil {
						t.Fatal(err)
					}
					waitV3(t, v, r.Version)
					collections[i] = v
				}
				for _, input := range []string{"", "missing", strings.Repeat("ushers 한국어 aaaa CAFÉ café ", 3) + strings.Repeat("한", 41)} {
					compareShardedSearch(t, collections[0], collections[1], input)
				}
				// Every search remains usable when its Redis connection is unavailable.
				if err := collections[1].client.Close(); err != nil {
					t.Fatal(err)
				}
				compareShardedSearch(t, collections[0], collections[1], "ushers 한국어 aaaa CAFÉ")
			})
		}
	}
}

func compareShardedSearch(t *testing.T, legacy, sharded *VersionedCollection, text string) {
	t.Helper()
	ctx := context.Background()
	parallel := &ParallelOptions{Workers: 2, ChunkSize: 8, AutoOverlap: true}
	checks := map[string]func(*VersionedCollection) (any, error){
		"Find":      func(v *VersionedCollection) (any, error) { return v.Find(ctx, text) },
		"FindIndex": func(v *VersionedCollection) (any, error) { return v.FindIndex(ctx, text) },
		"FindSet":   func(v *VersionedCollection) (any, error) { return v.FindSet(ctx, text) },
		"Matches":   func(v *VersionedCollection) (any, error) { return v.FindMatches(ctx, text, nil) },
		"Longest": func(v *VersionedCollection) (any, error) {
			return v.FindMatches(ctx, text, &MatchOptions{Kind: MatchKindLeftmostLongest})
		},
		"WholeWord": func(v *VersionedCollection) (any, error) {
			return v.FindMatches(ctx, text, &MatchOptions{WholeWord: true})
		},
		"Contains":      func(v *VersionedCollection) (any, error) { return v.Contains(ctx, text) },
		"Parallel":      func(v *VersionedCollection) (any, error) { return v.FindParallel(ctx, text, parallel) },
		"IndexParallel": func(v *VersionedCollection) (any, error) { return v.FindIndexParallel(ctx, text, parallel) },
		"Batch":         func(v *VersionedCollection) (any, error) { return v.FindBatch(ctx, []string{text, "", text}) },
		"Scan":          func(v *VersionedCollection) (any, error) { return v.Scan(ctx, text, &ScanOptions{MaxMatches: 2}) },
		"ScanLongest": func(v *VersionedCollection) (any, error) {
			return v.Scan(ctx, text, &ScanOptions{Kind: MatchKindLeftmostLongest})
		},
		"Mask":    func(v *VersionedCollection) (any, error) { return v.MaskText(ctx, text, '*', nil) },
		"Replace": func(v *VersionedCollection) (any, error) { return v.ReplaceText(ctx, text, "[x]", nil) },
		"Stream": func(v *VersionedCollection) (any, error) {
			var matches []Match
			err := v.FindStream(ctx, iotest.OneByteReader(strings.NewReader(text)), func(m Match) bool { matches = append(matches, m); return true })
			return matches, err
		},
	}
	for name, check := range checks {
		want, ew := check(legacy)
		got, eg := check(sharded)
		if ew != nil || eg != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("%s(%q): got %v (%v), want %v (%v)", name, text, got, eg, want, ew)
		}
	}
}

func TestVersionedShardedSearchCancellationAndStreamStop(t *testing.T) {
	ctx := context.Background()
	v := openShardedV3Test(t, miniredis.RunT(t), "cancel-sharded")
	r, err := v.Replace(ctx, v.Status().ServingVersion, []string{"a", "aa", "aaa"})
	if err != nil {
		t.Fatal(err)
	}
	waitV3(t, v, r.Version)
	canceled, cancel := context.WithCancel(ctx)
	calls := 0
	err = v.FindStream(canceled, strings.NewReader(strings.Repeat("a", 100)), func(Match) bool { calls++; cancel(); return true })
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatal(calls, err)
	}
	calls = 0
	err = v.FindStream(ctx, strings.NewReader(strings.Repeat("a", 100)), func(Match) bool { calls++; return false })
	if err != nil || calls != 1 {
		t.Fatal(calls, err)
	}
	if _, err = v.Scan(ctx, "aaaa", &ScanOptions{MaxCandidates: 1}); !errors.Is(err, ErrScanWorkLimit) {
		t.Fatal(err)
	}
}

// An empty dictionary must not read an input stream, matching legacy V3.
func TestVersionedShardedSearchEmptyStreamDoesNotRead(t *testing.T) {
	v := openShardedV3Test(t, miniredis.RunT(t), "empty-sharded-stream")
	err := v.FindStream(context.Background(), iotest.ErrReader(errors.New("unexpected read")), func(Match) bool {
		t.Fatal("empty dictionary emitted a match")
		return false
	})
	if err != nil {
		t.Fatal(err)
	}
}

//nolint:gocyclo,funlen // One scenario compares every public search entry point at the same V3 version.
func TestVersionedOverlaySearchAPIParity(t *testing.T) {
	ctx := context.Background()
	server := miniredis.RunT(t)
	open := func(name string, delta bool) *VersionedCollection {
		t.Helper()
		v, err := OpenVersioned(ctx, &VersionedOptions{Redis: AhoCorasickArgs{Addr: server.Addr(), Name: name},
			DeltaSearch: delta, PollInterval: 10 * time.Millisecond})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = v.Close() })
		return v
	}
	delta := open("single-engine-api-parity", true)
	full := open("full-api-parity", false)
	base := []string{"he", "old", "한국", "aa"}
	for i := 0; i < 129; i++ {
		base = append(base, fmt.Sprintf("word-%03d", i))
	}
	for _, v := range []*VersionedCollection{delta, full} {
		r, err := v.Replace(ctx, v.Status().ServingVersion, base)
		if err != nil {
			t.Fatal(err)
		}
		waitV3(t, v, r.Version)
		r, err = v.AddMany(ctx, r.Version, []string{"she", "hers", "한국어", "aaa"})
		if err != nil {
			t.Fatal(err)
		}
		waitV3(t, v, r.Version)
		r, err = v.Remove(ctx, r.Version, "old")
		if err != nil {
			t.Fatal(err)
		}
		waitV3(t, v, r.Version)
	}
	if status := delta.Status(); status.DeltaSearch || status.DeltaKeywords != 0 {
		t.Fatalf("expected single engine, got %+v", status)
	}
	text := strings.Repeat("ushers old 한국어 aaaa word-001 ", 5)
	compare := func(name string, a, b any, errA, errB error) {
		t.Helper()
		if errA != nil || errB != nil || !reflect.DeepEqual(a, b) {
			t.Fatalf("%s: delta=%v (%v), full=%v (%v)", name, a, errA, b, errB)
		}
	}
	a, ea := delta.Find(ctx, text)
	b, eb := full.Find(ctx, text)
	compare("Find", a, b, ea, eb)
	ai, eai := delta.FindIndex(ctx, text)
	bi, ebi := full.FindIndex(ctx, text)
	compare("FindIndex", ai, bi, eai, ebi)
	as, eas := delta.FindSet(ctx, text)
	bs, ebs := full.FindSet(ctx, text)
	compare("FindSet", as, bs, eas, ebs)
	am, eam := delta.FindMatches(ctx, text, nil)
	bm, ebm := full.FindMatches(ctx, text, nil)
	compare("FindMatches", am, bm, eam, ebm)
	ac, eac := delta.Contains(ctx, "old 한국어")
	bc, ebc := full.Contains(ctx, "old 한국어")
	compare("Contains", ac, bc, eac, ebc)
	parallel := &ParallelOptions{Workers: 2, ChunkSize: 32, AutoOverlap: true}
	ap, eap := delta.FindParallel(ctx, text, parallel)
	bp, ebp := full.FindParallel(ctx, text, parallel)
	compare("FindParallel", ap, bp, eap, ebp)
	api, eapi := delta.FindIndexParallel(ctx, text, parallel)
	bpi, ebpi := full.FindIndexParallel(ctx, text, parallel)
	compare("FindIndexParallel", api, bpi, eapi, ebpi)
	batch := []string{text, "old", "한국어 aaaa", ""}
	ab, eab := delta.FindBatch(ctx, batch)
	bb, ebb := full.FindBatch(ctx, batch)
	compare("FindBatch", ab, bb, eab, ebb)
	stream := func(v *VersionedCollection) ([]Match, error) {
		var result []Match
		err := v.FindStream(ctx, iotest.OneByteReader(strings.NewReader(text)), func(m Match) bool {
			result = append(result, m)
			return true
		})
		return result, err
	}
	ast, east := stream(delta)
	bst, ebst := stream(full)
	compare("FindStream", ast, bst, east, ebst)
	for _, input := range []string{text, "old", "한국어", "miss"} {
		ascan, ae := delta.Scan(ctx, input, nil)
		bscan, be := full.Scan(ctx, input, nil)
		compare("Scan", ascan, bscan, ae, be)
		amask, ae := delta.MaskText(ctx, input, '*', nil)
		bmask, be := full.MaskText(ctx, input, '*', nil)
		compare("MaskText", amask, bmask, ae, be)
		areplace, ae := delta.ReplaceText(ctx, input, "[x]", nil)
		breplace, be := full.ReplaceText(ctx, input, "[x]", nil)
		compare("ReplaceText", areplace, breplace, ae, be)
	}
	a, ea = delta.Find(ctx, text)
	compare("post-refresh Find", a, b, ea, eb)
}

func TestVersionedDeltaOptionUsesSingleEngine(t *testing.T) {
	ctx := context.Background()
	server := miniredis.RunT(t)
	v, err := OpenVersioned(ctx, &VersionedOptions{Redis: AhoCorasickArgs{Addr: server.Addr(), Name: "overlay-limit"},
		DeltaSearch: true, PollInterval: 10 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = v.Close() })
	base := make([]string, 129)
	change := make([]string, 129)
	for i := range base {
		base[i] = fmt.Sprintf("base-%03d", i)
		change[i] = fmt.Sprintf("change-%03d", i)
	}
	r, err := v.Replace(ctx, v.Status().ServingVersion, base)
	if err != nil {
		t.Fatal(err)
	}
	waitV3(t, v, r.Version)
	r, err = v.AddMany(ctx, r.Version, change[:128])
	if err != nil {
		t.Fatal(err)
	}
	waitV3(t, v, r.Version)
	if s := v.Status(); s.DeltaSearch || s.DeltaKeywords != 0 {
		t.Fatalf("single-engine case: %+v", s)
	}
	r, err = v.Add(ctx, r.Version, change[128])
	if err != nil {
		t.Fatal(err)
	}
	waitV3(t, v, r.Version)
	if s := v.Status(); s.DeltaSearch || s.DeltaKeywords != 0 {
		t.Fatalf("second update: %+v", s)
	}
}

func TestVersionedRapidUpdatesUseSingleEngine(t *testing.T) {
	ctx := context.Background()
	server := miniredis.RunT(t)
	v, err := OpenVersioned(ctx, &VersionedOptions{Redis: AhoCorasickArgs{Addr: server.Addr(), Name: "overlay-rapid"},
		DeltaSearch: true, PollInterval: 10 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = v.Close() })
	base := make([]string, 129)
	for i := range base {
		base[i] = fmt.Sprintf("base-%03d", i)
	}
	r, err := v.Replace(ctx, v.Status().ServingVersion, base)
	if err != nil {
		t.Fatal(err)
	}
	waitV3(t, v, r.Version)
	for i := 0; i < 20; i++ {
		r, err = v.Add(ctx, r.Version, fmt.Sprintf("new-%03d", i))
		if err != nil {
			t.Fatal(err)
		}
		waitV3(t, v, r.Version)
	}
	if s := v.Status(); s.DeltaSearch || s.DeltaKeywords != 0 || s.ServingVersion != r.Version {
		t.Fatalf("latest update not served by single engine: %+v", s)
	}
	if got, err := v.FindSet(ctx, "new-000 new-019 base-001"); err != nil ||
		!reflect.DeepEqual(got, []string{"new-000", "new-019", "base-001"}) {
		t.Fatal("latest search", got, err)
	}
}
