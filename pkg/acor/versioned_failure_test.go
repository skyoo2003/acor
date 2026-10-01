// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	stagePipelines  atomic.Int64
	checkSlots      bool
	chunkKey        string
	chunkEntered    chan struct{}
	chunkRelease    chan struct{}
}

func (h *v3FaultHook) DialHook(next redis.DialHook) redis.DialHook {
	return func(ctx context.Context, network, addr string) (net.Conn, error) { return next(ctx, network, addr) }
}
func (h *v3FaultHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		staged := false
		for _, cmd := range cmds {
			if h.checkSlots {
				if err := v3CheckScriptSlot(cmd); err != nil {
					return err
				}
			}
			args := cmd.Args()
			if cmd.Name() == "eval" && args[1] == v3StageScript {
				staged = true
				if strings.Contains(args[5].(string), ":chunk:") {
					h.chunksWritten.Add(1)
				}
			}
		}
		if staged {
			h.stagePipelines.Add(1)
		}
		return next(ctx, cmds)
	}
}
func (h *v3FaultHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if h.checkSlots {
			if err := v3CheckScriptSlot(cmd); err != nil {
				return err
			}
		}
		args := cmd.Args()
		if err := h.beforeChunkRead(ctx, cmd); err != nil {
			return err
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

func (h *v3FaultHook) beforeChunkRead(ctx context.Context, cmd redis.Cmder) error {
	args := cmd.Args()
	if cmd.Name() != "get" || !strings.Contains(args[1].(string), ":chunk:") ||
		(h.chunkKey != "" && args[1] != h.chunkKey) {
		return nil
	}
	if h.chunkRelease != nil {
		select {
		case h.chunkEntered <- struct{}{}:
		default:
		}
		select {
		case <-h.chunkRelease:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if h.failChunks.Load() {
		return errors.New("injected chunk read failure")
	}
	return nil
}

func v3CheckScriptSlot(cmd redis.Cmder) error {
	if cmd.Name() != "eval" {
		return nil
	}
	args := cmd.Args()
	count := args[2].(int)
	tag := ""
	for _, arg := range args[3 : 3+count] {
		_, rest, found := strings.Cut(arg.(string), "{")
		current, _, closed := strings.Cut(rest, "}")
		if !found || !closed || current == "" || (tag != "" && current != tag) {
			return fmt.Errorf("script spans Redis Cluster hash tags: %v", args[3:3+count])
		}
		tag = current
	}
	return nil
}

func TestVersionedShardedScriptsStayWithinOneSlot(t *testing.T) {
	ctx := context.Background()
	server := miniredis.RunT(t)
	v := openShardedV3Test(t, server, "slot-{unsafe}-name")
	v.client.AddHook(&v3FaultHook{checkSlots: true})
	r, err := v.AddMany(ctx, v.Status().ServingVersion, []string{"café", "한국어", "🙂"})
	if err != nil {
		t.Fatal(err)
	}
	waitV3(t, v, r.Version)
	for shard, word := range map[uint16]string{0: "café", 3: "한국어", 2: "🙂"} {
		data, _ := json.Marshal([]string{word})
		if v.client.Exists(ctx, v.shardKey(shard, "chunk:"+v3Hash(data))).Val() != 1 {
			t.Fatalf("shard %d missing distributed chunk", shard)
		}
		if v.client.Exists(ctx, v.key("chunk:"+v3Hash(data))).Val() != 0 {
			t.Fatal("sharded chunk incorrectly staged in global slot")
		}
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

// Reusing a corrupted content-addressed key must fail before global publication.
func TestVersionedShardedPreparationRejectsCorruptExistingChunk(t *testing.T) {
	ctx := context.Background()
	server := miniredis.RunT(t)
	v := openShardedV3Test(t, server, "corrupt-preparation")
	initial := v.Status().ServingVersion
	data := []byte(`["한국어"]`)
	v.client.Set(ctx, v.shardKey(3, "chunk:"+v3Hash(data)), `["bad"]`, 0)
	if _, err := v.Add(ctx, initial, "한국어"); !errors.Is(err, ErrVersionedCorrupt) {
		t.Fatalf("corrupt preparation error = %v", err)
	}
	if got := v.client.Get(ctx, v.key("active")).Val(); got != string(initial) {
		t.Fatal("failed preparation changed active version", got)
	}
}

func TestVersionedShardedWriterMirrorsRenewAndClose(t *testing.T) {
	ctx := context.Background()
	server := miniredis.RunT(t)
	v := openShardedV3Test(t, server, "shard-writer-lease")
	l, _, err := v.acquire(ctx, v.Status().ServingVersion, true)
	if err != nil {
		t.Fatal(err)
	}
	defer l.close(ctx)
	if err = v.mirrorWriter(ctx, l, 3); err != nil {
		t.Fatal(err)
	}
	v.client.ZAdd(ctx, v.shardKey(3, "writers"), redis.Z{Member: l.member, Score: 1})
	l.renew(ctx)
	global := v.client.ZScore(ctx, v.key("writers"), l.member).Val()
	local := v.client.ZScore(ctx, v.shardKey(3, "writers"), l.member).Val()
	if global <= 1 || local != global {
		t.Fatalf("mirror deadline %v, global %v", local, global)
	}
	if err = l.close(ctx); err != nil {
		t.Fatal(err)
	}
	if err = v.client.ZScore(ctx, v.shardKey(3, "writers"), l.member).Err(); !errors.Is(err, redis.Nil) {
		t.Fatal("closed writer retained shard lease", err)
	}
}

func TestVersionedShardedPreparationFailureKeepsActive(t *testing.T) {
	ctx := context.Background()
	server := miniredis.RunT(t)
	v := openShardedV3Test(t, server, "shard-preparation-failure")
	initial := v.Status().ServingVersion
	v.client.Set(ctx, v.shardKey(3, "chunks"), "wrong-type", 0)
	if _, err := v.AddMany(ctx, initial, []string{"café", "한국어"}); err == nil {
		t.Fatal("expected shard preparation failure")
	}
	if got := v.client.Get(ctx, v.key("active")).Val(); got != string(initial) {
		t.Fatal("failed shard preparation changed active version", got)
	}
}

func TestVersionedShardedExpiredAndFencedWritersCannotStage(t *testing.T) {
	for _, cause := range []string{"expired", "fenced", "maintenance"} {
		t.Run(cause, func(t *testing.T) {
			ctx := context.Background()
			server := miniredis.RunT(t)
			v := openShardedV3Test(t, server, "shard-lease-"+cause)
			initial := v.Status().ServingVersion
			l, _, err := v.acquire(ctx, initial, true)
			if err != nil {
				t.Fatal(err)
			}
			defer l.close(ctx)
			switch cause {
			case "expired":
				v.client.ZAdd(ctx, v.key("writers"), redis.Z{Score: 1, Member: l.member})
			case "fenced":
				v.client.Set(ctx, v.shardKey(3, "fence"), "1", 0)
			case "maintenance":
				v.client.Set(ctx, v.shardKey(3, "maintenance"), "1", time.Minute)
			}
			b, stages := v3Stages([]string{"한국어"})
			for i := range stages {
				stages[i].sharded, stages[i].shard = true, 3
			}
			if err = v.stageAll(ctx, l, stages); !errors.Is(err, ErrLeaseExpired) {
				t.Fatalf("writer %s stage error = %v", cause, err)
			}
			if v.client.Exists(ctx, v.shardKey(3, "chunk:"+b.Chunks[0])).Val() != 0 {
				t.Fatal("rejected writer staged a chunk")
			}
		})
	}
}

func TestVersionedBatchStagesChangedBucketsInOnePipeline(t *testing.T) {
	ctx := context.Background()
	server := miniredis.RunT(t)
	v := openV3Test(t, server, "batch-stage")
	hook := &v3FaultHook{}
	v.client.AddHook(hook)
	words := []string{"first"}
	for i := 0; ; i++ {
		word := fmt.Sprintf("word-%d", i)
		if v3BucketNumber(words[0]) != v3BucketNumber(word) {
			words = append(words, word)
			break
		}
	}
	if _, err := v.AddMany(ctx, v.Status().ServingVersion, words); err != nil {
		t.Fatal(err)
	}
	if got := hook.stagePipelines.Load(); got != 1 {
		t.Fatalf("stage pipelines = %d, want 1", got)
	}
}

//nolint:gocyclo // Covers the refresh failure, polling recovery, and search parity paths.
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
	if status.RefreshFailures == 0 || status.LastRefreshFailure.IsZero() {
		t.Fatalf("refresh failure was not recorded: %+v", status)
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
