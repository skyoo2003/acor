// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// These tests run against the disposable endpoint selected by
// ACOR_INTEGRATION_ADDR. Each test deletes only its own random namespace.
func newV3Integration(t *testing.T, shards uint16) (context.Context, *VersionedCollection, *VersionedOptions) {
	t.Helper()
	addr := integrationAddr(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	opts := &VersionedOptions{
		Redis:      AhoCorasickArgs{Addr: addr, Name: fmt.Sprintf("v3-integration-%x", nonce)},
		ShardCount: shards, ShardConcurrency: 2,
		PollInterval: 10 * time.Millisecond, RefreshDebounce: time.Millisecond,
	}
	cleanup := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() {
		defer func() {
			if err := cleanup.Close(); err != nil {
				t.Error(err)
			}
		}()
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		root := "{acor-v3-" + v3Hash([]byte(opts.Redis.Name))
		for _, pattern := range []string{root + "}:*", root + "-shard-*}:*"} {
			var keys []string
			iter := cleanup.Scan(cleanupCtx, 0, pattern, 100).Iterator()
			for iter.Next(cleanupCtx) {
				keys = append(keys, iter.Val())
			}
			if err := iter.Err(); err != nil {
				t.Error(err)
				continue
			}
			if len(keys) != 0 {
				if err := cleanup.Del(cleanupCtx, keys...).Err(); err != nil {
					t.Error(err)
				}
			}
		}
	})
	return ctx, openV3Integration(t, ctx, opts), opts
}

func openV3Integration(t *testing.T, ctx context.Context, opts *VersionedOptions) *VersionedCollection {
	t.Helper()
	v, err := OpenVersioned(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := v.Close(); err != nil {
			t.Error(err)
		}
	})
	return v
}

func v3IntegrationWait(t *testing.T, ctx context.Context, v *VersionedCollection, version Version) {
	t.Helper()
	if err := v.WaitForVersion(ctx, version); err != nil {
		t.Fatal(err)
	}
}

func v3IntegrationFind(t *testing.T, ctx context.Context, v *VersionedCollection, text string, want []string) {
	t.Helper()
	got, err := v.Find(ctx, text)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("Find(%q) = %q, want %q", text, got, want)
	}
}

func v3IntegrationSnapshot(t *testing.T, ctx context.Context, v *VersionedCollection) *Snapshot {
	t.Helper()
	s, err := v.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.Close(cleanupCtx); err != nil {
			t.Error(err)
		}
	})
	return s
}

func v3IntegrationList(t *testing.T, ctx context.Context, s *Snapshot) []string {
	t.Helper()
	var words []string
	cursor := ""
	for {
		page, err := s.List(ctx, cursor, 2)
		if err != nil {
			t.Fatal(err)
		}
		if page.Version != s.Version() {
			t.Fatalf("page version = %q, snapshot = %q", page.Version, s.Version())
		}
		words = append(words, page.Keywords...)
		cursor = page.NextCursor
		if cursor == "" {
			break
		}
	}
	slices.Sort(words)
	return words
}

// Removing a shard from the refresh or read path must break these assertions.
func TestIntegrationVersionedFourShardCRUDAndVersionBarrier(t *testing.T) {
	ctx, writer, opts := newV3Integration(t, 4)
	reader := openV3Integration(t, ctx, opts)
	r, err := writer.Replace(ctx, writer.Status().ServingVersion, []string{" CAFÉ ", "Москва", "한국어", "🙂", "café"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Added != 4 || r.Removed != 0 {
		t.Fatalf("replace counts: %+v", r)
	}
	v3IntegrationWait(t, ctx, reader, r.Version)
	if status := reader.Status(); status.ShardCount != 4 || status.LayoutVersion != 2 || status.ServingVersion != r.Version {
		t.Fatalf("reader did not install four-shard version: %+v", status)
	}
	v3IntegrationFind(t, ctx, reader, "CAFÉ Москва 한국어 🙂", []string{"café", "москва", "한국어", "🙂"})
	s := v3IntegrationSnapshot(t, ctx, reader)
	if got := v3IntegrationList(t, ctx, s); !slices.Equal(got, []string{"café", "москва", "한국어", "🙂"}) || s.Count() != 4 {
		t.Fatalf("snapshot dictionary = %q, count = %d", got, s.Count())
	}
	r, err = writer.AddMany(ctx, r.Version, []string{"he", "she", "café"})
	if err != nil || r.Added != 2 {
		t.Fatalf("add: %+v, %v", r, err)
	}
	v3IntegrationWait(t, ctx, reader, r.Version)
	v3IntegrationFind(t, ctx, reader, "she CAFÉ", []string{"café", "he", "she"})
	r, err = writer.RemoveMany(ctx, r.Version, []string{"café", "absent"})
	if err != nil || r.Removed != 1 {
		t.Fatalf("remove: %+v, %v", r, err)
	}
	v3IntegrationWait(t, ctx, reader, r.Version)
	v3IntegrationFind(t, ctx, reader, "she CAFÉ", []string{"he", "she"})
	r, err = writer.Replace(ctx, r.Version, nil)
	if err != nil || r.Removed != 5 {
		t.Fatalf("clear: %+v, %v", r, err)
	}
	v3IntegrationWait(t, ctx, reader, r.Version)
	v3IntegrationFind(t, ctx, reader, "she CAFÉ Москва 한국어 🙂", nil)
}

// A non-atomic expected-version check would allow both writers to commit.
func TestIntegrationVersionedConcurrentExpectedVersionConflict(t *testing.T) {
	ctx, first, opts := newV3Integration(t, 4)
	second := openV3Integration(t, ctx, opts)
	base := first.Status().ServingVersion
	type outcome struct {
		word   string
		result *WriteResult
		err    error
	}
	start := make(chan struct{})
	results := make(chan outcome, 2)
	for i, writer := range []*VersionedCollection{first, second} {
		word := []string{"alpha", "beta"}[i]
		go func() {
			<-start
			r, err := writer.Add(ctx, base, word)
			results <- outcome{word, r, err}
		}()
	}
	close(start)
	var winner outcome
	commits, conflicts := 0, 0
	for range 2 {
		select {
		case got := <-results:
			switch {
			case got.err == nil:
				commits++
				winner = got
			case errors.Is(got.err, ErrConcurrencyConflict):
				conflicts++
			default:
				t.Fatalf("unexpected writer outcome: %+v", got)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	if commits != 1 || conflicts != 1 || winner.result == nil {
		t.Fatalf("commits = %d, conflicts = %d", commits, conflicts)
	}
	for _, reader := range []*VersionedCollection{first, second} {
		v3IntegrationWait(t, ctx, reader, winner.result.Version)
		v3IntegrationFind(t, ctx, reader, "alpha beta", []string{winner.word})
	}
}

// Reopening must honor stored layouts; resharding must not reinterpret pinned pages.
//
//nolint:gocyclo // Pinned cursors span both layout transitions and a subsequent replacement.
func TestIntegrationVersionedLegacyReopenReshardSnapshot(t *testing.T) {
	ctx, v, opts := newV3Integration(t, 1)
	r, err := v.Replace(ctx, v.Status().ServingVersion, []string{"café", "한국", "한국어", "🙂"})
	if err != nil {
		t.Fatal(err)
	}
	v3IntegrationWait(t, ctx, v, r.Version)
	opts.ShardCount = 4
	reopened := openV3Integration(t, ctx, opts)
	if reopened.Status().ShardCount != 1 {
		t.Fatalf("reopen changed legacy layout: %+v", reopened.Status())
	}
	var snapshots []*Snapshot
	var firstPages []*DictionaryPage
	for _, shards := range []uint16{4, 1} {
		s := v3IntegrationSnapshot(t, ctx, v)
		page, listErr := s.List(ctx, "", 1)
		if listErr != nil || page == nil || page.NextCursor == "" {
			t.Fatalf("initial snapshot page: %+v, %v", page, listErr)
		}
		snapshots, firstPages = append(snapshots, s), append(firstPages, page)
		previous := r.Version
		r, err = v.Reshard(ctx, previous, shards)
		if err != nil || r.PreviousVersion != previous || r.Version == previous || r.Added != 0 || r.Removed != 0 {
			t.Fatalf("reshard to %d: %+v, %v", shards, r, err)
		}
		v3IntegrationWait(t, ctx, v, r.Version)
		v3IntegrationWait(t, ctx, reopened, r.Version)
		for _, reader := range []*VersionedCollection{v, reopened, openV3Integration(t, ctx, opts)} {
			if reader.Status().ShardCount != shards {
				t.Fatalf("reader did not retain layout %d: %+v", shards, reader.Status())
			}
			v3IntegrationFind(t, ctx, reader, "CAFÉ 한국어 🙂", []string{"café", "한국", "한국어", "🙂"})
		}
	}
	r, err = v.Replace(ctx, r.Version, []string{"new"})
	if err != nil {
		t.Fatal(err)
	}
	v3IntegrationWait(t, ctx, v, r.Version)
	v3IntegrationFind(t, ctx, v, "new 한국어", []string{"new"})
	for i, s := range snapshots {
		page, listErr := s.List(ctx, firstPages[i].NextCursor, 10)
		if listErr != nil || page == nil || page.NextCursor != "" || page.Version != s.Version() {
			t.Fatalf("continued pinned page: %+v, %v", page, listErr)
		}
		got := append(slices.Clone(firstPages[i].Keywords), page.Keywords...)
		slices.Sort(got)
		if !slices.Equal(got, []string{"café", "한국", "한국어", "🙂"}) || s.Count() != 4 {
			t.Fatalf("reshard changed pinned dictionary: %q", got)
		}
	}
}

// Prune must protect leased generations, then collect them after the lease closes.
func TestIntegrationVersionedPruneRetainsPinnedAgedGeneration(t *testing.T) {
	ctx, v, opts := newV3Integration(t, 4)
	r, err := v.Replace(ctx, v.Status().ServingVersion, []string{"café", "한국어"})
	if err != nil {
		t.Fatal(err)
	}
	v3IntegrationWait(t, ctx, v, r.Version)
	s := v3IntegrationSnapshot(t, ctx, v)
	next, err := v.Remove(ctx, r.Version, "한국어")
	if err != nil {
		t.Fatal(err)
	}
	v3IntegrationWait(t, ctx, v, next.Version)
	ageV3Generations(t, v)
	if _, err = v.Prune(ctx); err != nil {
		t.Fatal(err)
	}
	if got := v3IntegrationList(t, ctx, s); !slices.Equal(got, []string{"café", "한국어"}) {
		t.Fatalf("pruned leased dictionary: %q", got)
	}
	if err = s.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = v.Prune(ctx); err != nil {
		t.Fatal(err)
	}
	if exists, existsErr := v.client.Exists(ctx, v.key("gen:"+string(r.Version))).Result(); existsErr != nil || exists != 0 {
		t.Fatalf("unleased aged generation retained: %d, %v", exists, existsErr)
	}
	v3IntegrationFind(t, ctx, v, "CAFÉ 한국어", []string{"café"})
	v3IntegrationFind(t, ctx, openV3Integration(t, ctx, opts), "CAFÉ 한국어", []string{"café"})
}

// Losing the response must leave a durable receipt even after reopening and pruning.
//
//nolint:gocyclo // One receipt is verified across loss, reopen, generation removal, and replay.
func TestIntegrationVersionedLostCommitReceiptSurvivesPrune(t *testing.T) {
	ctx, writer, opts := newV3Integration(t, 4)
	hook := &v3FaultHook{}
	writer.client.AddHook(hook)
	hook.dropCommit.Store(true)
	r, err := writer.Add(ctx, writer.Status().ServingVersion, "한국어")
	if !errors.Is(err, ErrCommitUnknown) || r == nil || r.OperationID == "" {
		t.Fatalf("lost commit response: %+v, %v", r, err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	v := openV3Integration(t, ctx, opts)
	receipt, err := v.ResolveOperation(ctx, r.OperationID)
	if err != nil || receipt == nil || *receipt != *r {
		t.Fatalf("reopened durable receipt: %+v, %v", receipt, err)
	}
	v3IntegrationWait(t, ctx, v, r.Version)
	v3IntegrationFind(t, ctx, v, "한국어", []string{"한국어"})
	next, err := v.Replace(ctx, r.Version, []string{"café"})
	if err != nil {
		t.Fatal(err)
	}
	v3IntegrationWait(t, ctx, v, next.Version)
	ageV3Generations(t, v)
	if _, err = v.Prune(ctx); err != nil {
		t.Fatal(err)
	}
	if exists, existsErr := v.client.Exists(ctx, v.key("gen:"+string(r.Version))).Result(); existsErr != nil || exists != 0 {
		t.Fatalf("receipt generation not pruned: %d, %v", exists, existsErr)
	}
	receipt, err = v.ResolveOperation(ctx, r.OperationID)
	if err != nil || receipt == nil || *receipt != *r {
		t.Fatalf("prune lost durable receipt: %+v, %v", receipt, err)
	}
	v3IntegrationWait(t, ctx, v, receipt.Version)
	if v.Status().ServingVersion != next.Version {
		t.Fatalf("old receipt changed serving version: %+v", v.Status())
	}
	v3IntegrationFind(t, ctx, v, "CAFÉ 한국어", []string{"café"})
}
