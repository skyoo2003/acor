// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/alicebob/miniredis/v2"
)

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
