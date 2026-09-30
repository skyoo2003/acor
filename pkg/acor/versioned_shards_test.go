// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
)

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
	replayed, err := v.commit(ctx, &v3Lease{member: "expired"}, receipt, 2)
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
