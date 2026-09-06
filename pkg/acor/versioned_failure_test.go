// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	redis "github.com/redis/go-redis/v9"
)

type v3FaultHook struct {
	dropCommit      atomic.Bool
	failChunks      atomic.Bool
	suppressPublish bool
	chunksWritten   atomic.Int64
}

func (h *v3FaultHook) DialHook(next redis.DialHook) redis.DialHook {
	return func(ctx context.Context, network, addr string) (net.Conn, error) { return next(ctx, network, addr) }
}
func (h *v3FaultHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}
func (h *v3FaultHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		args := cmd.Args()
		if cmd.Name() == "get" && strings.Contains(args[1].(string), ":chunk:") && h.failChunks.Load() {
			return errors.New("injected chunk read failure")
		}
		commit := cmd.Name() == "eval" && args[1] == v3CommitScript
		if cmd.Name() == "eval" && args[1] == v3StageScript && strings.Contains(args[5].(string), ":chunk:") {
			h.chunksWritten.Add(1)
		}
		if commit && h.suppressPublish {
			args[1] = strings.ReplaceAll(v3CommitScript, "redis.call('PUBLISH',KEYS[6],ARGV[3])", "-- deliberately dropped publication")
		}
		err := next(ctx, cmd)
		if commit && err == nil && h.dropCommit.Swap(false) {
			return errors.New("injected lost commit response")
		}
		return err
	}
}
func TestVersionedLostCommitReceiptAndReuse(t *testing.T) {
	ctx := context.Background()
	server := miniredis.RunT(t)
	v := openV3Test(t, server, "receipt")
	hook := &v3FaultHook{}
	v.client.AddHook(hook)
	hook.dropCommit.Store(true)
	r, err := v.Add(ctx, v.Status().ServingVersion, "first")
	if !errors.Is(err, ErrCommitUnknown) || r == nil {
		t.Fatal(r, err)
	}
	receipt, err := v.ResolveOperation(ctx, r.OperationID)
	if err != nil || receipt.Version != r.Version {
		t.Fatal(receipt, err)
	}
	waitV3(t, v, receipt.Version)
	next, err := v.Add(ctx, receipt.Version, "second")
	if err != nil {
		t.Fatal(err)
	}
	waitV3(t, v, next.Version)
	// A delayed transport retry must return the old receipt, not reapply it.
	recovered, err := v.commit(ctx, &v3Lease{member: "expired"}, receipt, 2)
	if err != nil || recovered.Version != receipt.Version {
		t.Fatal(recovered, err)
	}
	if active := v.client.Get(ctx, v.key("active")).Val(); active != string(next.Version) {
		t.Fatal(active)
	}
	before := hook.chunksWritten.Load()
	if _, err = v.Replace(ctx, next.Version, []string{"first", "second"}); err != nil {
		t.Fatal(err)
	}
	if hook.chunksWritten.Load() != before {
		t.Fatal("identical dictionary rewrote chunks")
	}
	before = hook.chunksWritten.Load()
	if _, err = v.Add(ctx, next.Version, "third"); err != nil {
		t.Fatal(err)
	}
	if hook.chunksWritten.Load() != before+1 {
		t.Fatal("single addition rewrote unaffected buckets")
	}
}
func TestVersionedPollingAndBuildFailure(t *testing.T) {
	ctx := context.Background()
	server := miniredis.RunT(t)
	writer := openV3Test(t, server, "poll")
	reader := openV3Test(t, server, "poll")
	writer.client.AddHook(&v3FaultHook{suppressPublish: true})
	readHook := &v3FaultHook{}
	reader.client.AddHook(readHook)
	initial := reader.Status().ServingVersion
	readHook.failChunks.Store(true)
	r, err := writer.Add(ctx, initial, "hello")
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for reader.Status().LastError == "" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	status := reader.Status()
	if status.LastError == "" || status.ServingVersion != initial {
		t.Fatal(status)
	}
	if found, e := reader.Find(ctx, "hello"); e != nil || len(found) != 0 {
		t.Fatal(found, e)
	}
	readHook.failChunks.Store(false)
	waitV3(t, reader, r.Version)
	if found, e := reader.Contains(ctx, "hello"); e != nil || !found {
		t.Fatal(found, e)
	}
	if _, e := reader.FindSet(ctx, "hello hello"); e != nil {
		t.Fatal(e)
	}
	if _, e := reader.FindBatch(ctx, []string{"hello", "missing"}); e != nil {
		t.Fatal(e)
	}
	if _, e := reader.FindParallel(ctx, "hello hello", &ParallelOptions{ChunkSize: 3, AutoOverlap: true}); e != nil {
		t.Fatal(e)
	}
	if _, e := reader.Remove(ctx, r.Version, "hello"); e != nil {
		t.Fatal(e)
	}
}

//nolint:gocyclo // A copied generation supplies the context and cursor rejection cases.
func TestVersionedCopyV2AndCancellation(t *testing.T) {
	ctx := context.Background()
	server := miniredis.RunT(t)
	v := openV3Test(t, server, "target")
	old, err := Create(&AhoCorasickArgs{Addr: server.Addr(), Name: "source"})
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	if _, err = old.AddMany([]string{"한국", "HELLO"}, nil); err != nil {
		t.Fatal(err)
	}
	copied, err := v.CopyV2(ctx, "source", v.Status().ServingVersion, nil)
	if err != nil || copied.Count != 2 || copied.Checksum == "" || copied.SourceVersion == "" {
		t.Fatal(copied, err)
	}
	waitV3(t, v, copied.Write.Version)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = v.Snapshot(canceled); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err = OpenVersioned(canceled, &VersionedOptions{Redis: AhoCorasickArgs{Addr: server.Addr(), Name: "cancel"}}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	s, err := v.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close(ctx)
	if _, err = s.List(ctx, "invalid", 1); !errors.Is(err, ErrInvalidVersion) {
		t.Fatal(err)
	}
	if _, err = s.List(ctx, "", 0); err == nil {
		t.Fatal("invalid limit accepted")
	}
	p, err := s.List(ctx, "", 1)
	if err != nil {
		t.Fatal(err)
	}
	r, err := v.Add(ctx, s.Version(), "another")
	if err != nil {
		t.Fatal(err)
	}
	waitV3(t, v, r.Version)
	newer, err := v.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer newer.Close(ctx)
	if _, err = newer.List(ctx, p.NextCursor, 1); !errors.Is(err, ErrInvalidVersion) {
		t.Fatal(err)
	}
}
func TestVersionedCorruptionAndOrphanRecovery(t *testing.T) {
	ctx := context.Background()
	server := miniredis.RunT(t)
	v := openV3Test(t, server, "orphan")
	initial := v.Status().ServingVersion
	l, _, err := v.acquire(ctx, initial, true)
	if err != nil {
		t.Fatal(err)
	}
	b, err := v.stageBucket(ctx, l, []string{"orphan"})
	if err != nil {
		t.Fatal(err)
	}
	// Simulate process death after all immutable data was prepared, before commit.
	orphan := v3Manifest{Version: Version(v.id + "." + v3ID()), Sequence: 2, Count: 1}
	orphan.Buckets[v3BucketNumber("orphan")] = b
	data, _ := json.Marshal(orphan)
	if err = v.stage(ctx, l, "gen:"+string(orphan.Version), "generations", string(orphan.Version), data); err != nil {
		t.Fatal(err)
	}
	_ = l.close(ctx)
	v.client.ZAdd(ctx, v.key("generations"), redis.Z{Member: string(orphan.Version), Score: 1})
	if _, err = v.Prune(ctx); err != nil {
		t.Fatal(err)
	}
	if v.client.Exists(ctx, v.key("chunk:"+b.Chunks[0])).Val() != 0 {
		t.Fatal("orphan chunk survived")
	}
	if v.client.Get(ctx, v.key("active")).Val() != string(initial) {
		t.Fatal("orphan became active")
	}
	r, err := v.Add(ctx, initial, "good")
	if err != nil {
		t.Fatal(err)
	}
	waitV3(t, v, r.Version)
	s, err := v.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close(ctx)
	h := s.manifest.Buckets[v3BucketNumber("good")].Chunks[0]
	v.client.Set(ctx, v.key("chunk:"+h), `["bad"]`, 0)
	if _, err = s.List(ctx, "", 10); !errors.Is(err, ErrVersionedCorrupt) {
		t.Fatal(err)
	}
}

func FuzzVersionedNormalization(f *testing.F) {
	f.Add(" 한글 ", "ABC")
	f.Add("", "x")
	f.Fuzz(func(t *testing.T, a, b string) {
		normalized, err := v3Normalize([]string{a, b, a}, false)
		if err != nil {
			return
		}
		again, e := v3Normalize(normalized, false)
		if e != nil || strings.Join(normalized, "|") != strings.Join(again, "|") {
			t.Fatal("normalization is not idempotent")
		}
		for _, w := range normalized {
			if bucket := v3BucketNumber(w); bucket < 0 || bucket >= v3BucketCount {
				t.Fatal(bucket)
			}
		}
	})
}

func TestVersionedReconnect(t *testing.T) {
	ctx := context.Background()
	server := miniredis.RunT(t)
	v := openV3Test(t, server, "reconnect")
	r, err := v.Add(ctx, v.Status().ServingVersion, "before")
	if err != nil {
		t.Fatal(err)
	}
	waitV3(t, v, r.Version)
	server.Close()
	if found, e := v.Contains(ctx, "before"); e != nil || !found {
		t.Fatal(found, e)
	}
	if err = server.Restart(); err != nil {
		t.Fatal(err)
	}
	next, err := v.Add(ctx, r.Version, "after")
	if err != nil {
		t.Fatal(err)
	}
	waitV3(t, v, next.Version)
	if found, e := v.Contains(ctx, "after"); e != nil || !found {
		t.Fatal(found, e)
	}
}

func TestVersionedSearchGenerationConsistency(t *testing.T) {
	ctx := context.Background()
	server := miniredis.RunT(t)
	v := openV3Test(t, server, "consistent")
	old := []string{"old-a", "old-b"}
	newer := []string{"new-a", "new-b"}
	r, err := v.Replace(ctx, v.Status().ServingVersion, old)
	if err != nil {
		t.Fatal(err)
	}
	waitV3(t, v, r.Version)
	done := make(chan struct{})
	outcome := make(chan error, 1)
	go func() {
		defer close(outcome)
		for {
			select {
			case <-done:
				return
			default:
			}
			batch, e := v.FindBatch(ctx, []string{"old-a old-b new-a new-b", "old-a old-b new-a new-b"})
			if e != nil {
				outcome <- e
				return
			}
			if len(batch) != 2 || len(batch[0]) != 2 || strings.Join(batch[0], "|") != strings.Join(batch[1], "|") {
				outcome <- errors.New("search batch mixed generations")
				return
			}
		}
	}()
	for i := range 20 {
		target := old
		if i%2 == 0 {
			target = newer
		}
		r, err = v.Replace(ctx, r.Version, target)
		if err != nil {
			close(done)
			<-outcome
			t.Fatal(err)
		}
	}
	close(done)
	if e := <-outcome; e != nil {
		t.Fatal(e)
	}
	waitV3(t, v, r.Version)
}
func TestVersionedSensitiveAndEmptyCopy(t *testing.T) {
	ctx := context.Background()
	server := miniredis.RunT(t)
	v, err := OpenVersioned(ctx, &VersionedOptions{Redis: AhoCorasickArgs{Addr: server.Addr(), Name: "sensitive"}, CaseSensitive: true})
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	r, err := v.Replace(ctx, v.Status().ServingVersion, []string{"Hello", "hello"})
	if err != nil || r.Added != 2 {
		t.Fatal(r, err)
	}
	waitV3(t, v, r.Version)
	found, err := v.Find(ctx, "HELLO Hello")
	if err != nil || len(found) != 1 || found[0] != "Hello" {
		t.Fatal(found, err)
	}
	old, err := Create(&AhoCorasickArgs{Addr: server.Addr(), Name: "empty-v2"})
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	if _, err = v.CopyV2(ctx, "empty-v2", r.Version, &V2CopyOptions{RejectEmpty: true}); err == nil {
		t.Fatal("empty copy accepted")
	}
	copied, err := v.CopyV2(ctx, "empty-v2", r.Version, nil)
	if err != nil || copied.Count != 0 {
		t.Fatal(copied, err)
	}
}
