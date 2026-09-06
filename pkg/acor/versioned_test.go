// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	redis "github.com/redis/go-redis/v9"
)

func openV3Test(t *testing.T, server *miniredis.Miniredis, name string) *VersionedCollection {
	t.Helper()
	v, err := OpenVersioned(context.Background(), &VersionedOptions{Redis: AhoCorasickArgs{Addr: server.Addr(), Name: name}, PollInterval: 20 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = v.Close() })
	return v
}
func waitV3(t *testing.T, v *VersionedCollection, version Version) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := v.WaitForVersion(ctx, version); err != nil {
		t.Fatal(err)
	}
}

//nolint:gocyclo,funlen // One lifecycle follows the same version through successive assertions.
func TestVersionedLifecycle(t *testing.T) {
	ctx := context.Background()
	server := miniredis.RunT(t)
	v := openV3Test(t, server, "dictionary")
	initial := v.Status().ServingVersion
	r, err := v.Replace(ctx, initial, []string{" He ", "she", "hers", "한국", "한국어", "CAFÉ", "café"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Added != 6 || r.Removed != 0 {
		t.Fatal(r)
	}
	waitV3(t, v, r.Version)
	s, err := v.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close(ctx)
	var words []string
	cursor := ""
	for {
		p, e := s.List(ctx, cursor, 2)
		if e != nil {
			t.Fatal(e)
		}
		words = append(words, p.Keywords...)
		cursor = p.NextCursor
		if cursor == "" {
			break
		}
	}
	if len(words) != 6 {
		t.Fatal(words)
	}
	d, err := s.Diff(ctx, []string{"he", "new"})
	if err != nil || !reflect.DeepEqual(d.Added, []string{"new"}) || len(d.Removed) != 5 {
		t.Fatal(d, err)
	}
	same, err := v.Replace(ctx, r.Version, words)
	if err != nil || same.Version != r.Version {
		t.Fatal(same, err)
	}
	if _, err = v.AddMany(ctx, r.Version, []string{"valid", " "}); !errors.Is(err, ErrEmptyKeyword) {
		t.Fatal(err)
	}
	r2, err := v.RemoveMany(ctx, r.Version, []string{"he", "she", "missing"})
	if err != nil || r2.Removed != 2 {
		t.Fatal(r2, err)
	}
	if _, err = v.Add(ctx, r.Version, "conflict"); !errors.Is(err, ErrConcurrencyConflict) {
		t.Fatal(err)
	}
	pinned, err := s.all(ctx)
	if err != nil || !reflect.DeepEqual(words, pinned) {
		t.Fatal(pinned, err)
	}
	receipt, err := v.ResolveOperation(ctx, r2.OperationID)
	if err != nil || *receipt != *r2 {
		t.Fatal(receipt, err)
	}
	waitV3(t, v, r2.Version)
	if err = v.WaitForVersion(ctx, r.Version); err != nil {
		t.Fatal(err)
	}
	empty, err := v.Replace(ctx, r2.Version, nil)
	if err != nil {
		t.Fatal(err)
	}
	waitV3(t, v, empty.Version)
	found, err := v.Find(ctx, "she 한국")
	if err != nil || len(found) != 0 {
		t.Fatal(found, err)
	}
}
func TestVersionedSearchParity(t *testing.T) {
	ctx := context.Background()
	server := miniredis.RunT(t)
	v := openV3Test(t, server, "v3")
	words := []string{"he", "she", "hers", "한국", "한국어", "café", "ab", "abc", "bc"}
	legacy, err := Create(&AhoCorasickArgs{Addr: server.Addr(), Name: "v2", Preset: PresetMemoryEfficient})
	if err != nil {
		t.Fatal(err)
	}
	defer legacy.Close()
	if _, err = legacy.AddMany(words, nil); err != nil {
		t.Fatal(err)
	}
	r, err := v.Replace(ctx, v.Status().ServingVersion, words)
	if err != nil {
		t.Fatal(err)
	}
	waitV3(t, v, r.Version)
	text := "SHE hers 한국어 CAFÉ abc abc"
	a, err := v.Find(ctx, text)
	if err != nil {
		t.Fatal(err)
	}
	b, err := legacy.Find(text)
	if err != nil || !reflect.DeepEqual(a, b) {
		t.Fatal(a, b, err)
	}
	ai, _ := v.FindIndex(ctx, text)
	bi, _ := legacy.FindIndex(text)
	if !reflect.DeepEqual(ai, bi) {
		t.Fatal(ai, bi)
	}
	for _, o := range []*MatchOptions{nil, {WholeWord: true}, {Kind: MatchKindLeftmostLongest}} {
		am, _ := v.FindMatches(ctx, text, o)
		bm, _ := legacy.FindMatches(text, o)
		if !reflect.DeepEqual(am, bm) {
			t.Fatal(am, bm)
		}
	}
	var streamed []Match
	if err = v.FindStream(ctx, strings.NewReader(strings.Repeat(text, 300)), func(m Match) bool { streamed = append(streamed, m); return true }); err != nil {
		t.Fatal(err)
	}
	expected, _ := v.FindMatches(ctx, strings.Repeat(text, 300), nil)
	if !reflect.DeepEqual(streamed, expected) {
		t.Fatal("stream mismatch")
	}
	opts := &ParallelOptions{Workers: 3, ChunkSize: 3, AutoOverlap: true}
	ap, _ := v.FindIndexParallel(ctx, text, opts)
	bp, _ := legacy.FindIndexParallel(text, opts)
	if !reflect.DeepEqual(ap, bp) {
		t.Fatal(ap, bp)
	}
}
func TestVersionedConcurrentWriters(t *testing.T) {
	ctx := context.Background()
	server := miniredis.RunT(t)
	v := openV3Test(t, server, "race")
	other := openV3Test(t, server, "race")
	expected := v.Status().ServingVersion
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i, c := range []*VersionedCollection{v, other} {
		wg.Go(func() { _, err := c.Add(ctx, expected, fmt.Sprint(i)); results <- err })
	}
	wg.Wait()
	close(results)
	successes, conflicts := 0, 0
	for err := range results {
		if err == nil {
			successes++
		} else if errors.Is(err, ErrConcurrencyConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatal(successes, conflicts)
	}
	s, err := other.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close(ctx)
	waitV3(t, v, s.Version())
	waitV3(t, other, s.Version())
}

//nolint:gocyclo // Retention assertions share a pinned generation and its successor.
func TestVersionedReuseLeasePrune(t *testing.T) {
	ctx := context.Background()
	server := miniredis.RunT(t)
	v := openV3Test(t, server, "prune")
	r, err := v.Replace(ctx, v.Status().ServingVersion, []string{"one", "two", "three"})
	if err != nil {
		t.Fatal(err)
	}
	waitV3(t, v, r.Version)
	s, err := v.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := v.Add(ctx, r.Version, "four")
	if err != nil {
		t.Fatal(err)
	}
	waitV3(t, v, r2.Version)
	next, err := v.manifest(ctx, r2.Version)
	if err != nil {
		t.Fatal(err)
	}
	changed := v3BucketNumber("four")
	for i, b := range s.manifest.Buckets {
		if i != changed && !reflect.DeepEqual(b, next.Buckets[i]) {
			t.Fatal("unchanged bucket rewritten", i)
		}
	}
	// Age all generations without expiring the lease under test.
	gens, _ := v.client.ZRange(ctx, v.key("generations"), 0, -1).Result()
	for _, g := range gens {
		v.client.ZAdd(ctx, v.key("generations"), redis.Z{Score: 1, Member: g})
	}
	if _, err = v.Prune(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = s.all(ctx); err != nil {
		t.Fatal("leased snapshot pruned", err)
	}
	_ = s.Close(ctx)
	result, err := v.Prune(ctx)
	if err != nil || result.Generations == 0 {
		t.Fatal(result, err)
	}
	if _, err = v.manifest(ctx, r.Version); !errors.Is(err, redis.Nil) {
		t.Fatal(err)
	}
	if err = v.WaitForVersion(ctx, r.Version); err != nil {
		t.Fatal(err)
	}
	expired, err := v.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	v.client.ZAdd(ctx, v.key("leases"), redis.Z{Score: 1, Member: expired.lease.member})
	expired.lease.renew(ctx)
	if _, err = expired.List(ctx, "", 1); !errors.Is(err, ErrLeaseExpired) {
		t.Fatal(err)
	}
}
func TestVersionedFencingAndPreparationFailure(t *testing.T) {
	ctx := context.Background()
	server := miniredis.RunT(t)
	v := openV3Test(t, server, "failure")
	initial := v.Status().ServingVersion
	l, _, err := v.acquire(ctx, initial, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = v.Prune(ctx); !errors.Is(err, ErrMaintenance) {
		t.Fatal(err)
	}
	v.client.ZAdd(ctx, v.key("writers"), redis.Z{Score: 1, Member: l.member})
	if err = v.stage(ctx, l, "chunk:orphan", "chunks", "orphan", []byte("[]")); !errors.Is(err, ErrLeaseExpired) {
		t.Fatal(err)
	}
	if v.client.Exists(ctx, v.key("chunk:orphan")).Val() != 0 {
		t.Fatal("expired writer recorded data")
	}
	_ = l.close(ctx)
	v.client.Set(ctx, v.key("maintenance"), "new-owner", time.Minute)
	if _, err = v.pruneDelete(ctx, "old-owner", "generations", "gen:", []string{string(initial)}); !errors.Is(err, ErrMaintenance) {
		t.Fatal(err)
	}
	if _, err = v.manifest(ctx, initial); err != nil {
		t.Fatal(err)
	}
	v.client.Del(ctx, v.key("maintenance"))
	// Wrong-type registry fails staging after a chunk may have been prepared;
	// publication must still leave the active generation untouched.
	v.client.Set(ctx, v.key("chunks"), "wrong-type", 0)
	if _, err = v.Add(ctx, initial, "new"); err == nil {
		t.Fatal("expected failure")
	}
	if active := v.client.Get(ctx, v.key("active")).Val(); active != string(initial) {
		t.Fatal(active)
	}
}
func TestVersionedCaseAndForeignTokens(t *testing.T) {
	ctx := context.Background()
	server := miniredis.RunT(t)
	v := openV3Test(t, server, "case")
	_, err := OpenVersioned(ctx, &VersionedOptions{Redis: AhoCorasickArgs{Addr: server.Addr(), Name: "case"}, CaseSensitive: true})
	if !errors.Is(err, ErrCasePolicy) {
		t.Fatal(err)
	}
	other := openV3Test(t, server, "other")
	if _, err = v.Replace(ctx, other.Status().ServingVersion, nil); !errors.Is(err, ErrInvalidVersion) {
		t.Fatal(err)
	}
	_ = v.Close()
	if _, err = v.Find(ctx, "a"); !errors.Is(err, ErrVersionedClosed) {
		t.Fatal(err)
	}
}
func TestVersionedChunkLimit(t *testing.T) {
	ctx := context.Background()
	server := miniredis.RunT(t)
	v := openV3Test(t, server, "chunks")
	l, _, err := v.acquire(ctx, v.Status().ServingVersion, true)
	if err != nil {
		t.Fatal(err)
	}
	defer l.close(ctx)
	words := []string{"a", strings.Repeat("b", v3ChunkBytes), strings.Repeat("c", v3ChunkBytes/2), strings.Repeat("d", v3ChunkBytes/2)}
	b, err := v.stageBucket(ctx, l, words)
	if err != nil {
		t.Fatal(err)
	}
	got, err := v.bucket(ctx, b)
	if err != nil || !slices.Equal(got, words) || len(b.Chunks) != 4 {
		t.Fatal(len(b.Chunks), err)
	}
}
