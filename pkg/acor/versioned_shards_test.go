// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/iotest"
	"time"

	"github.com/alicebob/miniredis/v2"
	redis "github.com/redis/go-redis/v9"
)

// Changing storage must preserve normalized words, rune positions, and old cursors.
//
//nolint:funlen,gocyclo // Follows a pinned snapshot through both layout formats.
func TestVersionedReshardPreservesSearchAndSnapshotIsolation(t *testing.T) {
	ctx := context.Background()
	server := miniredis.RunT(t)
	v := openV3Test(t, server, "reshard-isolation")
	v.client.AddHook(&v3FaultHook{checkSlots: true})
	r, err := v.Replace(ctx, v.Status().ServingVersion, []string{" CAFÉ ", "한국어", "한국", "🙂"})
	if err != nil {
		t.Fatal(err)
	}
	waitV3(t, v, r.Version)
	want := []string{"café", "한국", "한국어", "🙂"}
	var pinned []*Snapshot
	var pages []*DictionaryPage
	for _, count := range []uint16{4, 8, 2, 1} {
		s, snapshotErr := v.Snapshot(ctx)
		if snapshotErr != nil {
			t.Fatal(snapshotErr)
		}
		t.Cleanup(func() { _ = s.Close(ctx) })
		page, listErr := s.List(ctx, "", 1)
		if listErr != nil || page.NextCursor == "" {
			t.Fatal(page, listErr)
		}
		pinned, pages = append(pinned, s), append(pages, page)
		previous := r.Version
		r, err = v.Reshard(ctx, previous, count)
		if err != nil || r.Version == previous || r.PreviousVersion != previous || r.Added != 0 || r.Removed != 0 {
			t.Fatal(r, err)
		}
		waitV3(t, v, r.Version)
		layoutVersion := uint16(2)
		if count == 1 {
			layoutVersion = 1
		}
		if status := v.Status(); status.ShardCount != count || status.LayoutVersion != layoutVersion {
			t.Fatal("serving layout not updated", status)
		}
		if meta := v.client.HGetAll(ctx, v.key("meta")).Val(); meta["shards"] != fmt.Sprint(count) || meta["layout"] != fmt.Sprint(layoutVersion) {
			t.Fatal("committed layout metadata not updated", meta)
		}
		got, findErr := v.FindMatches(ctx, "🙂한국어 CAFÉ", nil)
		matches := []Match{{"🙂", 0, 1}, {"한국", 1, 3}, {"한국어", 1, 4}, {"café", 5, 9}}
		if findErr != nil || !reflect.DeepEqual(got, matches) {
			t.Fatal(got, findErr)
		}
		opened := openShardedV3Test(t, server, "reshard-isolation")
		if opened.Status().ShardCount != count {
			t.Fatal("reopen changed stored layout", opened.Status())
		}
		_ = opened.Close()
	}
	r, err = v.Replace(ctx, r.Version, []string{"new"})
	if err != nil {
		t.Fatal(err)
	}
	waitV3(t, v, r.Version)
	for i, s := range pinned {
		page, listErr := s.List(ctx, pages[i].NextCursor, 10)
		if listErr != nil || page.NextCursor != "" || page.Version != s.Version() {
			t.Fatal(page, listErr)
		}
		got := append(slices.Clone(pages[i].Keywords), page.Keywords...)
		slices.Sort(got)
		if !slices.Equal(got, want) || s.Count() != 4 {
			t.Fatal("layout transition changed pinned dictionary", got)
		}
		diff, diffErr := s.Diff(ctx, []string{"new"})
		if diffErr != nil || !slices.Equal(diff.Removed, want) || !slices.Equal(diff.Added, []string{"new"}) {
			t.Fatal(diff, diffErr)
		}
	}
}

func TestVersionedReshardRejectsConflictAndInvalidCount(t *testing.T) {
	ctx := context.Background()
	v := openV3Test(t, miniredis.RunT(t), "reshard-validation")
	initial := v.Status().ServingVersion
	for _, count := range []uint16{0, 3, 257, 512} {
		if _, err := v.Reshard(ctx, initial, count); err == nil {
			t.Fatal("invalid count accepted", count)
		}
	}
	if _, err := v.Reshard(ctx, "foreign", 4); !errors.Is(err, ErrInvalidVersion) {
		t.Fatal(err)
	}
	// An empty dictionary still needs a new generation for a changed layout.
	r, err := v.Reshard(ctx, initial, 256)
	if err != nil || r.Version == initial || r.Added != 0 || r.Removed != 0 {
		t.Fatal(r, err)
	}
	if _, err = v.Reshard(ctx, initial, 2); !errors.Is(err, ErrConcurrencyConflict) {
		t.Fatal(err)
	}
	same, err := v.Reshard(ctx, r.Version, 256)
	if err != nil || same.Version != r.Version {
		t.Fatal(same, err)
	}
	receipt, err := v.ResolveOperation(ctx, same.OperationID)
	if err != nil || *receipt != *same {
		t.Fatal(receipt, err)
	}
	waitV3(t, v, r.Version)
}

// The leased global generation must protect its entire graph after migrating away.
func TestVersionedPruneRetainsLeasedShardedGeneration(t *testing.T) {
	ctx := context.Background()
	v := openShardedV3Test(t, miniredis.RunT(t), "prune-sharded-lease")
	v.client.AddHook(&v3FaultHook{checkSlots: true})
	r, err := v.Replace(ctx, v.Status().ServingVersion, []string{"café", "한국어", "🙂"})
	if err != nil {
		t.Fatal(err)
	}
	waitV3(t, v, r.Version)
	s, err := v.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close(ctx)
	r, err = v.Reshard(ctx, r.Version, 1)
	if err != nil {
		t.Fatal(err)
	}
	waitV3(t, v, r.Version)
	ageV3Generations(t, v)
	if _, err = v.Prune(ctx); err != nil {
		t.Fatal(err)
	}
	words, err := s.all(ctx)
	slices.Sort(words)
	if err != nil || !slices.Equal(words, []string{"café", "한국어", "🙂"}) {
		t.Fatal("leased shard chunks pruned", words, err)
	}
	if _, err = v.manifest(ctx, s.Version()); err != nil {
		t.Fatal("leased shard manifests pruned", err)
	}
	_ = s.Close(ctx)
	if _, err = v.Prune(ctx); err != nil {
		t.Fatal(err)
	}
	for shard := range uint16(4) {
		if n := v.client.ZCard(ctx, v.shardKey(shard, "manifests")).Val(); n != 0 {
			t.Fatal("unreachable shard manifests retained", shard, n)
		}
		if n := v.client.ZCard(ctx, v.shardKey(shard, "chunks")).Val(); n != 0 {
			t.Fatal("unreachable shard chunks retained", shard, n)
		}
	}
	active, err := v.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer active.Close(ctx)
	if words, err = active.all(ctx); err != nil || len(words) != 3 {
		t.Fatal("active legacy chunks pruned", words, err)
	}
}

func ageV3Generations(t *testing.T, v *VersionedCollection) {
	t.Helper()
	ctx := context.Background()
	gens, err := v.client.ZRange(ctx, v.key("generations"), 0, -1).Result()
	if err != nil {
		t.Fatal(err)
	}
	for _, generation := range gens {
		if err = v.client.ZAdd(ctx, v.key("generations"), redis.Z{Score: 1, Member: generation}).Err(); err != nil {
			t.Fatal(err)
		}
	}
}

// Both active reused shards and recently prepared global graphs remain reachable.
func TestVersionedPruneRetainsRecentShardedGeneration(t *testing.T) {
	ctx := context.Background()
	v := openShardedV3Test(t, miniredis.RunT(t), "prune-recent")
	r, err := v.Replace(ctx, v.Status().ServingVersion, []string{"café", "한국어"})
	if err != nil {
		t.Fatal(err)
	}
	waitV3(t, v, r.Version)
	old, err := v.manifest(ctx, r.Version)
	if err != nil {
		t.Fatal(err)
	}
	next, err := v.Remove(ctx, r.Version, "한국어")
	if err != nil {
		t.Fatal(err)
	}
	waitV3(t, v, next.Version)
	if _, err = v.Prune(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = v.manifest(ctx, r.Version); err != nil {
		t.Fatal("recent generation lost shard manifest", err)
	}
	if _, err = v.bucket(ctx, old.Buckets[v3BucketNumber("한국어")]); err != nil {
		t.Fatal("recent generation lost chunk", err)
	}
	ageV3Generations(t, v)
	if _, err = v.Prune(ctx); err != nil {
		t.Fatal(err)
	}
	if v.client.Exists(ctx, shardedV3ChunkKey(v, 3, "한국어")).Val() != 0 {
		t.Fatal("unreachable chunk retained")
	}
	if v.client.Exists(ctx, shardedV3ChunkKey(v, 0, "café")).Val() != 1 {
		t.Fatal("reused active chunk pruned")
	}
	if _, err = v.manifest(ctx, next.Version); err != nil {
		t.Fatal("reused active shard manifest pruned", err)
	}
}

// A preparation without any global manifest still needs eventual collection.
func TestVersionedPruneCollectsAbandonedTargetShard(t *testing.T) {
	ctx := context.Background()
	v := openV3Test(t, miniredis.RunT(t), "prune-abandoned-target")
	l, _, err := v.acquire(ctx, v.Status().ServingVersion, true)
	if err != nil {
		t.Fatal(err)
	}
	defer l.close(ctx)
	stages := []v3Stage{{key: "chunk:orphan", registry: "chunks", id: "orphan", data: []byte("[]"), shard: 255, sharded: true},
		{key: "manifest:orphan", registry: "manifests", id: "orphan", data: []byte("{}"), shard: 255, sharded: true}}
	if err = v.stageAll(ctx, l, stages); err != nil {
		t.Fatal(err)
	}
	if _, err = v.Prune(ctx); !errors.Is(err, ErrMaintenance) {
		t.Fatal("live preparation was not protected", err)
	}
	_ = l.close(ctx)
	if _, err = v.Prune(ctx); err != nil {
		t.Fatal(err)
	}
	for _, stage := range stages {
		if v.client.Exists(ctx, v.shardKey(255, stage.key)).Val() != 0 {
			t.Fatal("abandoned target shard record retained", stage.key)
		}
	}
}

// A stale pruner must not delete after losing either its global or shard fence.
func TestVersionedPruneShardFencesRejectStaleOwner(t *testing.T) {
	for _, cause := range []string{"global-expired", "newer-shard-fence", "shard-maintenance", "live-shard-writer"} {
		t.Run(cause, func(t *testing.T) {
			ctx := context.Background()
			v := openV3Test(t, miniredis.RunT(t), "prune-fence-"+cause)
			v.client.Set(ctx, v.key("maintenance"), "1", time.Minute)
			key := v.shardKey(255, "chunk:orphan")
			v.client.Set(ctx, key, "[]", 0)
			v.client.ZAdd(ctx, v.shardKey(255, "chunks"), redis.Z{Member: "orphan", Score: 1})
			switch cause {
			case "global-expired":
				v.client.Del(ctx, v.key("maintenance"))
			case "newer-shard-fence":
				v.client.Set(ctx, v.shardKey(255, "fence"), "2", 0)
			case "shard-maintenance":
				v.client.Set(ctx, v.shardKey(255, "maintenance"), "2", time.Minute)
			case "live-shard-writer":
				now, err := v.client.Time(ctx).Result()
				if err != nil {
					t.Fatal(err)
				}
				v.client.ZAdd(ctx, v.shardKey(255, "writers"), redis.Z{Member: "writer", Score: float64(now.Add(time.Minute).UnixMilli())})
			}
			if _, err := v.pruneShard(ctx, "1", 255, nil); !errors.Is(err, ErrMaintenance) {
				t.Fatal("stale shard prune accepted", err)
			}
			if v.client.Exists(ctx, key).Val() != 1 {
				t.Fatal("stale pruner deleted chunk")
			}
		})
	}
}

type v3PruneExpiryHook struct {
	v3FaultHook
	server      *miniredis.Miniredis
	maintenance string
	global      string
}

func (h *v3PruneExpiryHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		args := cmd.Args()
		if cmd.Name() == "eval" && strings.Contains(args[1].(string), "return #KEYS-2") && args[3] == h.maintenance {
			// Expiry between the renewal and the atomic deletion must fence it out.
			h.server.Del(h.maintenance)
			h.server.Del(h.global)
		}
		return next(ctx, cmd)
	}
}

func TestVersionedPruneShardDeletionChecksExpiredAuthority(t *testing.T) {
	ctx := context.Background()
	server := miniredis.RunT(t)
	v := openV3Test(t, server, "prune-expiry-at-delete")
	key := v.shardKey(255, "chunk:orphan")
	v.client.Set(ctx, key, "[]", 0)
	v.client.ZAdd(ctx, v.shardKey(255, "chunks"), redis.Z{Member: "orphan", Score: 1})
	v.client.AddHook(&v3PruneExpiryHook{server: server, maintenance: v.shardKey(255, "maintenance"), global: v.key("maintenance")})
	if _, err := v.Prune(ctx); !errors.Is(err, ErrMaintenance) {
		t.Fatal("expired owner deleted shard data", err)
	}
	if v.client.Exists(ctx, key).Val() != 1 {
		t.Fatal("shard deletion was not fenced atomically")
	}
}

// Reading and staging under a writer lease excludes prune, but never another writer.
func TestVersionedReshardConflictsWithConcurrentWrite(t *testing.T) {
	ctx := context.Background()
	server := miniredis.RunT(t)
	v := openV3Test(t, server, "reshard-race")
	writer := openV3Test(t, server, "reshard-race")
	r, err := v.Add(ctx, v.Status().ServingVersion, "old")
	if err != nil {
		t.Fatal(err)
	}
	waitV3(t, v, r.Version)
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	hook := &v3FaultHook{chunkEntered: make(chan struct{}, 1), chunkRelease: release}
	v.client.AddHook(hook)
	done := make(chan error, 1)
	go func() { _, reshardErr := v.Reshard(ctx, r.Version, 4); done <- reshardErr }()
	select {
	case <-hook.chunkEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("reshard did not read source chunks")
	}
	if _, err = writer.Prune(ctx); !errors.Is(err, ErrMaintenance) {
		t.Fatal("prune raced live reshard", err)
	}
	next, err := writer.Add(ctx, r.Version, "new")
	if err != nil {
		t.Fatal(err)
	}
	unblock()
	if err = <-done; !errors.Is(err, ErrConcurrencyConflict) {
		t.Fatal(err)
	}
	waitV3(t, v, next.Version)
	if got, findErr := v.FindSet(ctx, "old new"); findErr != nil || !slices.Equal(got, []string{"old", "new"}) {
		t.Fatal(got, findErr)
	}
	if v.client.HGet(ctx, v.key("meta"), "shards").Val() != "1" {
		t.Fatal("failed reshard changed layout")
	}
}

// Ambiguous layout commits reconcile durably, even after their graph is pruned.
func TestVersionedReshardLostReceiptSurvivesPrune(t *testing.T) {
	ctx := context.Background()
	v := openV3Test(t, miniredis.RunT(t), "reshard-receipt")
	r, err := v.Add(ctx, v.Status().ServingVersion, "한국어")
	if err != nil {
		t.Fatal(err)
	}
	waitV3(t, v, r.Version)
	hook := &v3FaultHook{}
	v.client.AddHook(hook)
	hook.dropCommit.Store(true)
	r, err = v.Reshard(ctx, r.Version, 4)
	if !errors.Is(err, ErrCommitUnknown) || r == nil {
		t.Fatal(r, err)
	}
	receipt, err := v.ResolveOperation(ctx, r.OperationID)
	if err != nil || *receipt != *r {
		t.Fatal(receipt, err)
	}
	waitV3(t, v, r.Version)
	next, err := v.Reshard(ctx, r.Version, 2)
	if err != nil {
		t.Fatal(err)
	}
	waitV3(t, v, next.Version)
	ageV3Generations(t, v)
	if _, err = v.Prune(ctx); err != nil {
		t.Fatal(err)
	}
	if v.client.Exists(ctx, v.key("gen:"+string(r.Version))).Val() != 0 {
		t.Fatal("receipt generation not pruned")
	}
	replayed, err := v.commit(ctx, &v3Lease{member: "expired"}, receipt, &v3Manifest{Sequence: 3})
	if err != nil || *replayed != *receipt {
		t.Fatal(replayed, err)
	}
	if v.client.Get(ctx, v.key("active")).Val() != string(next.Version) ||
		v.client.HGet(ctx, v.key("meta"), "shards").Val() != "2" {
		t.Fatal("replayed receipt changed version or layout")
	}
	if err = v.WaitForVersion(ctx, receipt.Version); err != nil {
		t.Fatal("pruning lost committed sequence", err)
	}
}

// Neither invalid source data nor a failed target shard may publish a new layout.
func TestVersionedReshardPreparationFailureKeepsLayout(t *testing.T) {
	for _, cause := range []string{"source-corrupt", "target-corrupt", "target-registry"} {
		t.Run(cause, func(t *testing.T) {
			ctx := context.Background()
			v := openV3Test(t, miniredis.RunT(t), "reshard-failure-"+cause)
			r, err := v.Add(ctx, v.Status().ServingVersion, "한국어")
			if err != nil {
				t.Fatal(err)
			}
			waitV3(t, v, r.Version)
			data, _ := json.Marshal([]string{"한국어"})
			switch cause {
			case "source-corrupt":
				v.client.Set(ctx, v.key("chunk:"+v3Hash(data)), "[]", 0)
			case "target-corrupt":
				v.client.Set(ctx, shardedV3ChunkKey(v, 3, "한국어"), "[]", 0)
			case "target-registry":
				v.client.Set(ctx, v.shardKey(3, "chunks"), "wrong-type", 0)
			}
			if _, err = v.Reshard(ctx, r.Version, 4); err == nil {
				t.Fatal("failed preparation published")
			}
			if v.client.Get(ctx, v.key("active")).Val() != string(r.Version) ||
				v.client.HGet(ctx, v.key("meta"), "shards").Val() != "1" {
				t.Fatal("failed reshard changed version or layout")
			}
			if v.client.ZCard(ctx, v.key("writers")).Val() != 0 {
				t.Fatal("failed reshard retained global writer lease")
			}
			if got, findErr := v.FindSet(ctx, "한국어"); findErr != nil || !slices.Equal(got, []string{"한국어"}) {
				t.Fatal("failed reshard changed serving generation", got, findErr)
			}
		})
	}
}

func TestVersionedPruneExpiredShardedWriterCannotResume(t *testing.T) {
	ctx := context.Background()
	v := openV3Test(t, miniredis.RunT(t), "prune-expired-writer")
	l, _, err := v.acquire(ctx, v.Status().ServingVersion, true)
	if err != nil {
		t.Fatal(err)
	}
	defer l.close(ctx)
	stage := v3Stage{key: "chunk:orphan", registry: "chunks", id: "orphan", data: []byte("[]"), shard: 255, sharded: true}
	if err = v.stageAll(ctx, l, []v3Stage{stage}); err != nil {
		t.Fatal(err)
	}
	v.client.ZAdd(ctx, v.key("writers"), redis.Z{Member: l.member, Score: 1})
	v.client.ZAdd(ctx, v.shardKey(255, "writers"), redis.Z{Member: l.member, Score: 1})
	result, err := v.Prune(ctx)
	if err != nil || result.Chunks != 1 {
		t.Fatal(result, err)
	}
	if err = v.stageBatch(ctx, l, []v3Stage{stage}); !errors.Is(err, ErrLeaseExpired) {
		t.Fatal("expired staged command resumed", err)
	}
	if err = v.mirrorWriter(ctx, l, 255); !errors.Is(err, ErrLeaseExpired) {
		t.Fatal("expired mirror resumed", err)
	}
	if v.client.Exists(ctx, v.shardKey(255, stage.key)).Val() != 0 {
		t.Fatal("expired writer recreated pruned content")
	}
}

// Legacy maintenance must not materialize hundreds of unused shard namespaces.
func TestVersionedPruneLegacyDoesNotCreateShardState(t *testing.T) {
	ctx := context.Background()
	server := miniredis.RunT(t)
	v := openV3Test(t, server, "prune-legacy-footprint")
	if _, err := v.Prune(ctx); err != nil {
		t.Fatal(err)
	}
	for _, key := range server.Keys() {
		if strings.Contains(key, "-shard-") {
			t.Fatal("legacy prune created unused shard state", key)
		}
	}
}

// Losing unchanged shard engines makes every incremental update rebuild the dictionary.
func TestVersionedShardedSearchReusesUnchangedEngines(t *testing.T) {
	ctx := context.Background()
	v := openShardedV3Test(t, miniredis.RunT(t), "engine-reuse")
	r, err := v.Replace(ctx, v.Status().ServingVersion, []string{"한국어", "café"})
	if err != nil {
		t.Fatal(err)
	}
	waitV3(t, v, r.Version)
	before := v.current.Load()
	r, err = v.Add(ctx, r.Version, "🙂")
	if err != nil {
		t.Fatal(err)
	}
	waitV3(t, v, r.Version)
	after := v.current.Load()
	if len(after.shards) != 4 {
		t.Fatal("missing complete shard engine set")
	}
	for shard := range before.shards {
		if (before.shards[shard] == after.shards[shard]) != (shard != 2) {
			t.Fatalf("shard %d engine reuse incorrect", shard)
		}
	}
}

func TestVersionedShardedSearchPreservesKoreanMatchPositions(t *testing.T) {
	ctx := context.Background()
	v := openShardedV3Test(t, miniredis.RunT(t), "korean-positions")
	r, err := v.Replace(ctx, v.Status().ServingVersion, []string{"한국어", "한국", "국어", "어"})
	if err != nil {
		t.Fatal(err)
	}
	waitV3(t, v, r.Version)
	got, err := v.FindMatches(ctx, "🙂한국어 한국어", nil)
	want := []Match{{"한국", 1, 3}, {"한국어", 1, 4}, {"국어", 2, 4}, {"어", 3, 4},
		{"한국", 5, 7}, {"한국어", 5, 8}, {"국어", 6, 8}, {"어", 7, 8}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal(got, err)
	}
}

// Refreshing inside the first callback must not switch the remaining stream's dictionary.
func TestVersionedShardedSearchUsesOneGeneration(t *testing.T) {
	ctx := context.Background()
	v := openShardedV3Test(t, miniredis.RunT(t), "stream-generation")
	r, err := v.Replace(ctx, v.Status().ServingVersion, []string{"old", "한국어"})
	if err != nil {
		t.Fatal(err)
	}
	waitV3(t, v, r.Version)
	var got []string
	err = v.FindStream(ctx, iotest.OneByteReader(strings.NewReader("old 한국어 old new café")), func(m Match) bool {
		got = append(got, m.Keyword)
		if len(got) == 1 {
			next, replaceErr := v.Replace(ctx, r.Version, []string{"new", "café"})
			if replaceErr != nil {
				t.Fatal(replaceErr)
			}
			waitV3(t, v, next.Version)
		}
		return true
	})
	if err != nil || !slices.Equal(got, []string{"old", "한국어", "old"}) {
		t.Fatal(got, err)
	}
	batch, err := v.FindBatch(ctx, []string{"old new 한국어 café", "new", "old"})
	if err != nil || !reflect.DeepEqual(batch, [][]string{{"new", "café"}, {"new"}, {}}) {
		t.Fatal(batch, err)
	}
}

func openShardedV3Test(t *testing.T, server *miniredis.Miniredis, name string) *VersionedCollection {
	t.Helper()
	v, err := OpenVersioned(context.Background(), &VersionedOptions{
		Redis: AhoCorasickArgs{Addr: server.Addr(), Name: name}, ShardCount: 4,
		ShardConcurrency: 2, PollInterval: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = v.Close() })
	return v
}

// A lost invalidation must converge through polling without WaitForVersion waking refresh.
func TestVersionedShardedPollingRecoversDroppedInvalidation(t *testing.T) {
	ctx := context.Background()
	server := miniredis.RunT(t)
	writer := openShardedV3Test(t, server, "sharded-poll")
	reader := openShardedV3Test(t, server, "sharded-poll")
	writer.client.AddHook(&v3FaultHook{suppressPublish: true})
	version := reader.Status().ServingVersion
	for _, step := range []struct {
		words      []string
		downloaded int
		reused     int
	}{
		{[]string{"café", "한국어"}, 2, 2},
		{[]string{"café", "한국어", "🙂"}, 1, 3},
		{[]string{"café", "한국어"}, 1, 3}, // An emptied shard still needs a new engine.
	} {
		r, err := writer.Replace(ctx, version, step.words)
		if err != nil {
			t.Fatal(err)
		}
		version = r.Version
		status := awaitV3Status(t, reader, func(s VersionedStatus) bool { return s.ServingVersion == version })
		if status.ActiveVersion != version || status.DownloadedShards != step.downloaded ||
			status.ReusedShards != step.reused || status.FailedShard != -1 || status.RefreshingShards != 0 {
			t.Fatalf("polling installed incorrect shard observations: %+v", status)
		}
		got, err := reader.FindSet(ctx, "café 한국어 🙂")
		if err != nil || !slices.Equal(got, step.words) {
			t.Fatal(got, err)
		}
	}
}

// A late shard download failure must retain the complete prior engine and cache.
//
//nolint:gocyclo // Follows one generation through a failed refresh and its recovery.
func TestVersionedShardedBuildFailureKeepsPreviousGeneration(t *testing.T) {
	ctx := context.Background()
	server := miniredis.RunT(t)
	writer := openShardedV3Test(t, server, "sharded-failure")
	reader := openShardedV3Test(t, server, "sharded-failure")
	r, err := writer.Replace(ctx, writer.Status().ServingVersion, []string{"café"})
	if err != nil {
		t.Fatal(err)
	}
	waitV3(t, reader, r.Version)
	previous, before := reader.current.Load(), reader.Status()
	hook := &v3FaultHook{chunkKey: shardedV3ChunkKey(reader, 3, "한국어")}
	hook.failChunks.Store(true)
	reader.client.AddHook(hook)
	r, err = writer.AddMany(ctx, r.Version, []string{"🙂", "한국어"})
	if err != nil {
		t.Fatal(err)
	}
	status := awaitV3Status(t, reader, func(s VersionedStatus) bool { return s.RefreshFailures > before.RefreshFailures })
	if status.ActiveVersion != r.Version || status.ServingVersion != before.ServingVersion ||
		status.FailedShard != 3 || status.LastError == "" || status.LastRefreshFailure.IsZero() {
		t.Fatalf("missing failed shard observations: %+v", status)
	}
	if reader.current.Load() != previous || status.CompletedBuilds != before.CompletedBuilds ||
		status.DownloadedShards != before.DownloadedShards || status.ReusedShards != before.ReusedShards {
		t.Fatal("failed refresh changed installed generation or successful build counters", status)
	}
	if got, findErr := reader.FindSet(ctx, "café 한국어 🙂"); findErr != nil || !slices.Equal(got, []string{"café"}) {
		t.Fatal(got, findErr)
	}
	hook.failChunks.Store(false)
	waitV3(t, reader, r.Version)
	status = reader.Status()
	if status.FailedShard != -1 || status.LastError != "" || status.DownloadedShards != 2 || status.ReusedShards != 2 {
		t.Fatal("recovery did not clear failure or rebuild every changed shard", status)
	}
}

// WaitForVersion must not report success while one changed shard remains unavailable.
func TestVersionedShardedWaitForVersionRequiresEveryShard(t *testing.T) {
	ctx := context.Background()
	server := miniredis.RunT(t)
	writer := openShardedV3Test(t, server, "sharded-wait")
	reader := openShardedV3Test(t, server, "sharded-wait")
	initial := reader.Status().ServingVersion
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	hook := &v3FaultHook{chunkKey: shardedV3ChunkKey(reader, 3, "한국어"),
		chunkEntered: make(chan struct{}, 1), chunkRelease: release}
	reader.client.AddHook(hook)
	r, err := writer.Replace(ctx, initial, []string{"café", "🙂", "한국어"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-hook.chunkEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("refresh did not reach the blocked shard")
	}
	waitCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	if err = reader.WaitForVersion(waitCtx, r.Version); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("incomplete generation wait = %v", err)
	}
	status := reader.Status()
	if !status.Building || status.RefreshingShards != 3 || status.ServingVersion != initial || status.ActiveVersion != r.Version {
		t.Fatalf("pending shard observations: %+v", status)
	}
	if found, findErr := reader.Contains(ctx, "café"); findErr != nil || found {
		t.Fatal("candidate shard leaked into serving generation", found, findErr)
	}
	unblock()
	waitV3(t, reader, r.Version)
	if got, findErr := reader.FindSet(ctx, "café 🙂 한국어"); findErr != nil || !slices.Equal(got, []string{"café", "🙂", "한국어"}) {
		t.Fatal(got, findErr)
	}
	// A skipped target is satisfied only by a complete later committed generation.
	older := r.Version
	r, err = writer.Remove(ctx, r.Version, "🙂")
	if err != nil {
		t.Fatal(err)
	}
	waitV3(t, reader, r.Version)
	if err = reader.WaitForVersion(ctx, older); err != nil {
		t.Fatal(err)
	}
}

func shardedV3ChunkKey(v *VersionedCollection, shard uint16, word string) string {
	data, _ := json.Marshal([]string{word})
	return v.shardKey(shard, "chunk:"+v3Hash(data))
}

// Manifest failures occur before engine building, but must still identify the shard.
func TestVersionedShardedRefreshReportsMissingManifest(t *testing.T) {
	ctx := context.Background()
	server := miniredis.RunT(t)
	writer := openShardedV3Test(t, server, "missing-refresh-manifest")
	reader := openShardedV3Test(t, server, "missing-refresh-manifest")
	reader.refreshMu.Lock()
	locked := true
	defer func() {
		if locked {
			reader.refreshMu.Unlock()
		}
	}()
	initial := reader.current.Load()
	r, err := writer.Add(ctx, initial.version, "한국어")
	if err != nil {
		t.Fatal(err)
	}
	m, err := writer.globalManifest(ctx, r.Version)
	if err != nil {
		t.Fatal(err)
	}
	key := writer.shardKey(3, "manifest:"+m.Shards[3])
	data, err := writer.client.Get(ctx, key).Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if err = writer.client.Del(ctx, key).Err(); err != nil {
		t.Fatal(err)
	}
	reader.refreshMu.Unlock()
	locked = false
	status := awaitV3Status(t, reader, func(s VersionedStatus) bool { return s.RefreshFailures != 0 })
	if status.FailedShard != 3 || status.ActiveVersion != r.Version || reader.current.Load() != initial {
		t.Fatalf("missing manifest changed generation or lost shard identity: %+v", status)
	}
	if err = writer.client.Set(ctx, key, data, 0).Err(); err != nil {
		t.Fatal(err)
	}
	waitV3(t, reader, r.Version)
}

func TestVersionedShardedPublicationIsVersionHint(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	v := openShardedV3Test(t, miniredis.RunT(t), "publication-hint")
	sub := v.client.Subscribe(ctx, v.key("events"))
	defer sub.Close()
	if _, err := sub.Receive(ctx); err != nil {
		t.Fatal(err)
	}
	r, err := v.Replace(ctx, v.Status().ServingVersion, []string{"café", "한국어", "🙂"})
	if err != nil {
		t.Fatal(err)
	}
	message, err := sub.ReceiveMessage(ctx)
	if err != nil || message.Payload != string(r.Version) {
		t.Fatal("publication must contain only the committed version hint", message, err)
	}
}

func awaitV3Status(t *testing.T, v *VersionedCollection, ready func(VersionedStatus) bool) VersionedStatus {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if status := v.Status(); ready(status) {
			return status
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for status: %+v", v.Status())
	return VersionedStatus{}
}

// Missing or misidentified immutable references must never become a snapshot.
func TestVersionedShardedManifestRejectsMissingShard(t *testing.T) {
	for _, damage := range []string{"missing", "id", "shard", "bucket", "count"} {
		t.Run(damage, func(t *testing.T) {
			ctx := context.Background()
			server := miniredis.RunT(t)
			v := openShardedV3Test(t, server, "integrity-"+damage)
			version := v.Status().ServingVersion
			m, err := v.globalManifest(ctx, version)
			if err != nil {
				t.Fatal(err)
			}
			key := v.shardKey(0, "manifest:"+m.Shards[0])
			if damage == "missing" {
				v.client.Del(ctx, key)
			} else {
				data, readErr := v.client.Get(ctx, key).Bytes()
				if readErr != nil {
					t.Fatal(readErr)
				}
				var shard v3ShardManifest
				if err = json.Unmarshal(data, &shard); err != nil {
					t.Fatal(err)
				}
				switch damage {
				case "id":
					shard.ID = v3ID()
				case "shard":
					shard.Shard = 1
				case "bucket":
					shard.Buckets[1] = v3Bucket{Count: 1}
					shard.Count = 1
				case "count":
					shard.Count = 1
				}
				data, _ = json.Marshal(shard)
				v.client.Set(ctx, key, data, 0)
			}
			if _, err = v.globalManifest(ctx, version); !errors.Is(err, ErrVersionedCorrupt) {
				t.Fatalf("global manifest error = %v", err)
			}
			if _, err = v.Snapshot(ctx); !errors.Is(err, ErrVersionedCorrupt) {
				t.Fatalf("snapshot error = %v", err)
			}
		})
	}
}

// A damaged sharded header must never be interpreted as an empty legacy generation.
func TestVersionedShardedManifestRejectsRemovedLayout(t *testing.T) {
	for _, disguise := range []string{"shards", "shards-and-buckets", "no-storage-fields", "null-buckets"} {
		t.Run(disguise, func(t *testing.T) {
			ctx := context.Background()
			server := miniredis.RunT(t)
			v := openShardedV3Test(t, server, "removed-layout-"+disguise)
			version := v.Status().ServingVersion
			key := v.key("gen:" + string(version))
			data, err := v.client.Get(ctx, key).Bytes()
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err = json.Unmarshal(data, &fields); err != nil {
				t.Fatal(err)
			}
			delete(fields, "Layout")
			switch disguise {
			case "shards-and-buckets":
				fields["Buckets"] = json.RawMessage(`[]`)
			case "no-storage-fields":
				delete(fields, "Shards")
			case "null-buckets":
				delete(fields, "Shards")
				fields["Buckets"] = json.RawMessage(`null`)
			}
			data, _ = json.Marshal(fields)
			v.client.Set(ctx, key, data, 0)
			if _, err = v.globalManifest(ctx, version); !errors.Is(err, ErrVersionedCorrupt) {
				t.Fatalf("global manifest error = %v", err)
			}
			if _, err = v.Add(ctx, version, "한국어"); !errors.Is(err, ErrVersionedCorrupt) {
				t.Fatalf("malformed generation write error = %v", err)
			}
			if got := v.client.Get(ctx, v.key("active")).Val(); got != string(version) {
				t.Fatal("malformed generation changed active version", got)
			}
		})
	}
}

// Rewriting every shard breaks incremental CRUD and pinned snapshot reuse.
func TestVersionedShardedAddRewritesOnlyAffectedShard(t *testing.T) {
	ctx := context.Background()
	server := miniredis.RunT(t)
	v := openShardedV3Test(t, server, "incremental-shards")
	initial := v.Status().ServingVersion
	before, err := v.globalManifest(ctx, initial)
	if err != nil {
		t.Fatal(err)
	}
	if len(before.Shards) != 4 {
		t.Fatalf("new layout = %+v", before)
	}
	r, err := v.AddMany(ctx, initial, []string{"한국어", " CAFÉ ", "café"})
	if err != nil || r.Added != 2 || r.Removed != 0 {
		t.Fatal(r, err)
	}
	after, err := v.globalManifest(ctx, r.Version)
	if err != nil {
		t.Fatal(err)
	}
	// Independent vectors: café bucket 0x850 owns shard 0; 한국어 0xea3 owns shard 3.
	for shard := range before.Shards {
		changed := shard == 0 || shard == 3
		if (before.Shards[shard] != after.Shards[shard]) != changed {
			t.Fatalf("shard %d changed incorrectly", shard)
		}
	}
	if after.Count != 2 || after.Sequence != before.Sequence+1 {
		t.Fatalf("committed manifest = %+v", after)
	}
	noop, err := v.Add(ctx, r.Version, "café")
	if err != nil || noop.Version != r.Version || noop.Added != 0 {
		t.Fatal(noop, err)
	}
}

func TestVersionedShardedRemoveAndReplacePreservePinnedSnapshot(t *testing.T) {
	ctx := context.Background()
	server := miniredis.RunT(t)
	v := openShardedV3Test(t, server, "snapshot-shards")
	r, err := v.Replace(ctx, v.Status().ServingVersion, []string{"한국어", "café"})
	if err != nil {
		t.Fatal(err)
	}
	s, err := v.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close(ctx)
	removed, err := v.RemoveMany(ctx, r.Version, []string{"한국어", "absent"})
	if err != nil || removed.Removed != 1 {
		t.Fatal(removed, err)
	}
	words, err := s.all(ctx)
	slices.Sort(words)
	if err != nil || !slices.Equal(words, []string{"café", "한국어"}) {
		t.Fatal(words, err)
	}
	empty, err := v.Replace(ctx, removed.Version, nil)
	if err != nil || empty.Removed != 1 {
		t.Fatal(empty, err)
	}
	waitV3(t, v, empty.Version)
}

func TestVersionedShardedLostCommitReceiptResolvesOnce(t *testing.T) {
	ctx := context.Background()
	server := miniredis.RunT(t)
	v := openShardedV3Test(t, server, "shard-receipt")
	hook := &v3FaultHook{}
	v.client.AddHook(hook)
	hook.dropCommit.Store(true)
	r, err := v.AddMany(ctx, v.Status().ServingVersion, []string{"café", "한국어"})
	if !errors.Is(err, ErrCommitUnknown) || r == nil {
		t.Fatal(r, err)
	}
	receipt, err := v.ResolveOperation(ctx, r.OperationID)
	if err != nil || *receipt != *r {
		t.Fatal(receipt, err)
	}
	next, err := v.Add(ctx, receipt.Version, "🙂")
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := v.commit(ctx, &v3Lease{member: "expired"}, receipt, &v3Manifest{Sequence: 2})
	if err != nil || *replayed != *receipt {
		t.Fatal(replayed, err)
	}
	m, err := v.globalManifest(ctx, next.Version)
	if err != nil || m.Count != 3 || m.Sequence != 3 || v.client.Get(ctx, v.key("active")).Val() != string(next.Version) {
		t.Fatal(m, err)
	}
}

func TestVersionedShardedReopenRetainsPersistedLayout(t *testing.T) {
	ctx := context.Background()
	server := miniredis.RunT(t)
	v := openShardedV3Test(t, server, "persisted-shards")
	r, err := v.Replace(ctx, v.Status().ServingVersion, []string{"한국어", "café"})
	if err != nil {
		t.Fatal(err)
	}
	if err = v.Close(); err != nil {
		t.Fatal(err)
	}
	for _, count := range []uint16{0, 1, 2, 16} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			reopened, openErr := OpenVersioned(ctx, &VersionedOptions{
				Redis: AhoCorasickArgs{Addr: server.Addr(), Name: "persisted-shards"}, ShardCount: count,
			})
			if openErr != nil {
				t.Fatal(openErr)
			}
			defer reopened.Close()
			status := reopened.Status()
			if status.LayoutVersion != 2 || status.ShardCount != 4 || status.ServingVersion != r.Version {
				t.Fatal(status)
			}
			got, findErr := reopened.FindSet(ctx, "한국어 café")
			if findErr != nil || !slices.Equal(got, []string{"한국어", "café"}) {
				t.Fatal(got, findErr)
			}
		})
	}
}

func TestVersionedShardedConcurrentWritersCommitOneGeneration(t *testing.T) {
	ctx := context.Background()
	server := miniredis.RunT(t)
	v := openShardedV3Test(t, server, "concurrent-shards")
	initial := v.Status().ServingVersion
	var wg sync.WaitGroup
	errorsSeen := make(chan error, 2)
	for _, word := range []string{"한국어", "café"} {
		wg.Go(func() {
			_, err := v.Add(ctx, initial, word)
			errorsSeen <- err
		})
	}
	wg.Wait()
	close(errorsSeen)
	successes, conflicts := 0, 0
	for err := range errorsSeen {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrConcurrencyConflict):
			conflicts++
		default:
			t.Fatal(err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes %d, conflicts %d", successes, conflicts)
	}
	s, err := v.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close(ctx)
	if s.Count() != 1 || s.manifest.Sequence != 2 {
		t.Fatalf("mixed concurrent generation: count %d sequence %d", s.Count(), s.manifest.Sequence)
	}
}

// Catches accidental layout changes or dictionary loss when reopening legacy V3.
func TestVersionedShardOptionsPreserveSingleShardCompatibility(t *testing.T) {
	ctx := context.Background()
	server := miniredis.RunT(t)
	v := openV3Test(t, server, "legacy-shards")
	r, err := v.Replace(ctx, v.Status().ServingVersion, []string{"한국어", "legacy"})
	if err != nil {
		t.Fatal(err)
	}
	if err = v.Close(); err != nil {
		t.Fatal(err)
	}
	for _, count := range []uint16{0, 1, 4} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			reopened, err := OpenVersioned(ctx, &VersionedOptions{
				Redis: AhoCorasickArgs{Addr: server.Addr(), Name: "legacy-shards"}, ShardCount: count,
			})
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			status := reopened.Status()
			if status.ServingVersion != r.Version || status.LayoutVersion != 1 || status.ShardCount != 1 {
				t.Fatalf("legacy reopen status: %+v", status)
			}
			if got, err := reopened.FindSet(ctx, "legacy 한국어"); err != nil || !slices.Equal(got, []string{"legacy", "한국어"}) {
				t.Fatalf("reopened dictionary = %v, err = %v", got, err)
			}
		})
	}
}

// Catches accepting non-power-of-two or unbounded fan-out and negative workers.
func TestVersionedShardOptionsRejectInvalidCountsAndConcurrency(t *testing.T) {
	for _, count := range []uint16{3, 7, 255, 257, 512, 65535} {
		t.Run(fmt.Sprintf("count-%d", count), func(t *testing.T) {
			_, err := versionedOptions(&VersionedOptions{Redis: AhoCorasickArgs{Name: "options"}, ShardCount: count})
			if err == nil {
				t.Fatalf("accepted shard count %d", count)
			}
		})
	}
	if _, err := versionedOptions(&VersionedOptions{Redis: AhoCorasickArgs{Name: "options"}, ShardConcurrency: -1}); err == nil {
		t.Fatal("accepted negative shard concurrency")
	}
}

func TestVersionedShardOptionsNormalizeDefaults(t *testing.T) {
	for _, count := range []uint16{0, 1, 2, 4, 16, 256} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			o, err := versionedOptions(&VersionedOptions{Redis: AhoCorasickArgs{Name: "options"}, ShardCount: count})
			if err != nil {
				t.Fatal(err)
			}
			wantCount := max(count, 1)
			if o.ShardCount != wantCount || o.ShardConcurrency != max(1, min(runtime.GOMAXPROCS(0), int(wantCount))) {
				t.Fatalf("normalized options: %+v", o)
			}
		})
	}
	o, err := versionedOptions(&VersionedOptions{Redis: AhoCorasickArgs{Name: "options"}, ShardCount: 2, ShardConcurrency: 3})
	if err != nil || o.ShardConcurrency != 3 {
		t.Fatalf("explicit concurrency = %d, err = %v", o.ShardConcurrency, err)
	}
}

// Literal hash vectors catch changed encoding, byte order, or unstable hashing.
func TestV3ShardForIsStableForUnicode(t *testing.T) {
	for _, tc := range []struct {
		word  string
		count uint16
		want  uint16
	}{
		{"한국어", 0, 0}, {"한국어", 1, 0}, {"한국어", 16, 3},
		{"한국어", 256, 163}, {"café", 16, 0}, {"🙂", 16, 6},
	} {
		for range 20 {
			if got := v3ShardFor(tc.word, tc.count); got != tc.want {
				t.Fatalf("shard(%q, %d) = %d, want %d", tc.word, tc.count, got, tc.want)
			}
		}
	}
}

func TestVersionedShardLayoutValidation(t *testing.T) {
	for _, layout := range []v3Layout{{Version: 1, ShardCount: 1}, {Version: 2, ShardCount: 2}, {Version: 2, ShardCount: 256}} {
		if err := layout.validate(); err != nil {
			t.Fatalf("rejected valid layout %+v: %v", layout, err)
		}
	}
	for _, layout := range []v3Layout{
		{}, {Version: 3, ShardCount: 2}, {Version: 1, ShardCount: 2},
		{Version: 2, ShardCount: 1}, {Version: 2, ShardCount: 3}, {Version: 2, ShardCount: 512},
	} {
		if err := layout.validate(); !errors.Is(err, ErrVersionedCorrupt) {
			t.Fatalf("invalid layout %+v returned %v", layout, err)
		}
	}
}
