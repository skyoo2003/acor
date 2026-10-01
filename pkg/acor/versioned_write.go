// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	redis "github.com/redis/go-redis/v9"
	"golang.org/x/sync/errgroup"
)

const v3ChunkBytes = 1 << 20
const v3Add = "add"

type v3Stage struct {
	key, registry, id string
	data              []byte
	shard             uint16
	sharded           bool
}

// Every preparation write is fenced, including writes by an expired process
// resuming after Prune. Registration and content creation are one atomic step.
const v3StageScript = v3Now + `
 if redis.call('EXISTS',KEYS[1])==1 then return 0 end
 local e=redis.call('ZSCORE',KEYS[2],ARGV[1])
 if not e or tonumber(e)<=now then return 0 end
 local content=redis.call('GET',KEYS[3])
 if content and content~=ARGV[2] then return -1 end
 redis.call('ZADD',KEYS[4],'NX',math.floor(now/1000),ARGV[3])
 redis.call('SET',KEYS[3],ARGV[2],'NX')
 return 1`

// The commit does not decode manifests or traverse dictionary data. An operation
// receipt makes transport-level retries idempotent, even after another commit.
const v3CommitScript = v3Now + `
 local receipt=redis.call('GET',KEYS[5]); if receipt then return receipt end
 if redis.call('EXISTS',KEYS[1])==1 then return 'maintenance' end
 local e=redis.call('ZSCORE',KEYS[2],ARGV[1])
 if not e or tonumber(e)<=now then return 'expired' end
 if redis.call('GET',KEYS[3])~=ARGV[2] then return 'conflict' end
 if redis.call('EXISTS',KEYS[4])==0 then return 'missing' end
 redis.call('SET',KEYS[5],ARGV[4])
 if ARGV[2]~=ARGV[3] then
 redis.call('SET',KEYS[3],ARGV[3])
 redis.call('SET',KEYS[7],ARGV[5])
 redis.call('ZADD',KEYS[8],math.floor(now/1000),ARGV[3])
 redis.call('HSET',KEYS[9],'layout',ARGV[6],'shards',ARGV[7])
 redis.call('PUBLISH',KEYS[6],ARGV[3])
 end
 return ARGV[4]`

// Replace atomically replaces the dictionary against a mandatory expected
// version. An empty target deletes all keywords; an identical target is a no-op.
func (v *VersionedCollection) Replace(ctx context.Context, expected Version, words []string) (*WriteResult, error) {
	return v.change(ctx, expected, words, "replace")
}

// Add atomically adds a normalized keyword against expected.
func (v *VersionedCollection) Add(ctx context.Context, expected Version, word string) (*WriteResult, error) {
	return v.AddMany(ctx, expected, []string{word})
}

// Remove atomically removes a normalized keyword against expected.
func (v *VersionedCollection) Remove(ctx context.Context, expected Version, word string) (*WriteResult, error) {
	return v.RemoveMany(ctx, expected, []string{word})
}

// AddMany adds all normalized keywords or none. Only affected buckets are read
// and rewritten. Duplicate and already-present keywords are no-ops.
func (v *VersionedCollection) AddMany(ctx context.Context, expected Version, words []string) (*WriteResult, error) {
	return v.change(ctx, expected, words, v3Add)
}

// RemoveMany removes all normalized keywords or none; absent entries are no-ops.
func (v *VersionedCollection) RemoveMany(ctx context.Context, expected Version, words []string) (*WriteResult, error) {
	return v.change(ctx, expected, words, "remove")
}

// Reshard explicitly changes storage layout while preserving the dictionary.
// shardCount must be a power of two between 1 (legacy layout) and 256. A changed
// layout commits a new generation, even for an empty dictionary; the same layout
// is a no-op. Existing snapshots remain pinned to their original layout.
func (v *VersionedCollection) Reshard(ctx context.Context, expected Version, shardCount uint16) (*WriteResult, error) {
	if !v.valid(expected) {
		return nil, ErrInvalidVersion
	}
	if !v3ValidShardCount(shardCount) {
		return nil, errors.New("acor: shard count must be a power of two between 1 and 256")
	}
	l, _, err := v.acquire(ctx, expected, true)
	if err != nil {
		return nil, err
	}
	defer func() { _ = l.close(ctx) }()
	old, err := v.manifest(ctx, expected)
	if err != nil {
		return nil, err
	}
	result := &WriteResult{PreviousVersion: expected, Version: expected, OperationID: v3ID()}
	if v3ManifestLayout(old).ShardCount == shardCount {
		return v.prepareCommit(ctx, l, old, result, nil, nil, false)
	}
	next := &v3Manifest{Version: expected, Sequence: old.Sequence, Count: old.Count}
	changedShards := make(map[uint16]bool)
	if shardCount > 1 {
		next.global = &v3GlobalManifest{Layout: v3Layout{Version: v3ShardedLayoutVersion, ShardCount: shardCount},
			Shards: make([]string, shardCount)}
		for shard := range shardCount {
			changedShards[shard] = true
		}
	}
	stages, err := v.reshardBuckets(ctx, old, next)
	if err != nil {
		return nil, err
	}
	return v.prepareCommit(ctx, l, next, result, stages, changedShards, true)
}

// Read each source bucket through its pinned storage coordinates and checksum
// validation, then prepare equivalent chunks in the target layout.
func (v *VersionedCollection) reshardBuckets(ctx context.Context, old, next *v3Manifest) ([]v3Stage, error) {
	var stages []v3Stage
	for i, before := range &old.Buckets {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		words, err := v.bucket(ctx, before)
		if err != nil {
			return nil, err
		}
		bucket, work := v3Stages(words)
		if next.global != nil && bucket.Count > 0 {
			bucket.sharded, bucket.shard = true, uint16(i)%next.global.Layout.ShardCount //nolint:gosec // Bucket index is at most 4095.
			for j := range work {
				work[j].sharded, work[j].shard = true, bucket.shard
			}
		}
		next.Buckets[i] = bucket
		stages = append(stages, work...)
	}
	return stages, nil
}

func v3ManifestLayout(m *v3Manifest) v3Layout {
	if m.global != nil {
		return m.global.Layout
	}
	return v3Layout{Version: v3LegacyLayoutVersion, ShardCount: 1}
}

func (v *VersionedCollection) change(ctx context.Context, expected Version, words []string, mode string) (*WriteResult, error) {
	if !v.valid(expected) {
		return nil, ErrInvalidVersion
	}
	normalized, err := v3Normalize(words, v.opts.CaseSensitive)
	if err != nil {
		return nil, err
	}
	l, _, err := v.acquire(ctx, expected, true)
	if err != nil {
		return nil, err
	}
	defer func() { _ = l.close(ctx) }()
	old, err := v.manifest(ctx, expected)
	if err != nil {
		return nil, err
	}
	next := *old
	changedShards := make(map[uint16]bool)
	result := &WriteResult{PreviousVersion: expected, Version: expected, OperationID: v3ID()}
	stages := make([]v3Stage, 0)
	var buckets [v3BucketCount][]string
	for _, w := range normalized {
		b := v3BucketNumber(w)
		buckets[b] = append(buckets[b], w)
	}
	for i, input := range &buckets {
		if mode != "replace" && len(input) == 0 {
			continue
		}
		before, readErr := v.bucket(ctx, old.Buckets[i])
		if readErr != nil {
			return nil, readErr
		}
		after, added, removed := v3Apply(before, input, mode)
		if added == 0 && removed == 0 {
			continue
		}
		result.Added += added
		result.Removed += removed
		b, bucketStages := v3Stages(after)
		if old.global != nil {
			shard := uint16(i) % old.global.Layout.ShardCount //nolint:gosec // Bucket index is at most 4095.
			b.sharded, b.shard = true, shard
			changedShards[shard] = true
			for j := range bucketStages {
				bucketStages[j].sharded, bucketStages[j].shard = true, shard
			}
		}
		stages = append(stages, bucketStages...)
		next.Buckets[i] = b
	}
	return v.prepareCommit(ctx, l, &next, result, stages, changedShards, result.Added != 0 || result.Removed != 0)
}
func (v *VersionedCollection) prepareCommit(ctx context.Context, l *v3Lease, next *v3Manifest,
	result *WriteResult, stages []v3Stage, changedShards map[uint16]bool, changed bool) (*WriteResult, error) {
	if changed {
		next.Version = Version(v.id + "." + v3ID())
		next.Sequence++
		next.Count += result.Added - result.Removed
		result.Version = next.Version
		data := v3NextManifest(next, changedShards, &stages)
		generation := v3Stage{key: "gen:" + string(next.Version), registry: "generations", id: string(next.Version), data: data}
		if next.global == nil {
			stages = append(stages, generation)
		}
		if err := v.stageAll(ctx, l, stages); err != nil {
			return nil, err
		}
		if next.global != nil {
			if err := v.stageAll(ctx, l, []v3Stage{generation}); err != nil {
				return nil, err
			}
			if _, err := v.manifest(ctx, next.Version); err != nil {
				return nil, err
			}
		}
	}
	return v.commit(ctx, l, result, next)
}

func v3NextManifest(next *v3Manifest, changed map[uint16]bool, stages *[]v3Stage) []byte {
	if next.global == nil {
		data, _ := json.Marshal(next)
		return data
	}
	global := *next.global
	global.Version, global.Sequence, global.Count = next.Version, next.Sequence, next.Count
	global.Shards = slices.Clone(global.Shards)
	for shard := range changed {
		sm := v3ShardManifest{ID: v3ID(), Shard: shard, Buckets: make(map[uint16]v3Bucket)}
		for i, bucket := range &next.Buckets {
			if bucket.Count > 0 && uint16(i)%global.Layout.ShardCount == shard { //nolint:gosec // Bucket index is at most 4095.
				sm.Buckets[uint16(i)] = bucket //nolint:gosec // Bucket index is at most 4095.
				sm.Count += bucket.Count
			}
		}
		data, _ := json.Marshal(sm)
		*stages = append(*stages, v3Stage{key: "manifest:" + sm.ID, registry: "manifests", id: sm.ID,
			data: data, shard: shard, sharded: true})
		global.Shards[shard] = sm.ID
	}
	next.global = &global
	data, _ := json.Marshal(global)
	return data
}

func (v *VersionedCollection) commit(ctx context.Context, l *v3Lease, result *WriteResult, next *v3Manifest) (*WriteResult, error) {
	data, _ := json.Marshal(result)
	layout := v3ManifestLayout(next)
	keys := []string{v.key(v3Maintenance), v.key("writers"), v.key("active"), v.key("gen:" + string(result.Version)),
		v.key("op:" + result.OperationID), v.key("events"), v.key("committed:" + string(result.Version)), v.key("generations"), v.key("meta")}
	raw, err := v.client.Eval(ctx, v3CommitScript, keys, l.member, string(result.PreviousVersion), string(result.Version), data,
		next.Sequence, layout.Version, layout.ShardCount).Text()
	if err != nil {
		return result, fmt.Errorf("%w: %w", ErrCommitUnknown, err)
	}
	switch raw {
	case "conflict":
		return nil, ErrConcurrencyConflict
	case v3Maintenance:
		return nil, ErrMaintenance
	case "expired":
		return nil, ErrLeaseExpired
	case "missing":
		return nil, ErrVersionedCorrupt
	}
	var committed WriteResult
	if json.Unmarshal([]byte(raw), &committed) != nil {
		return result, ErrCommitUnknown
	}
	v.signal()
	return &committed, nil
}
func v3Apply(before, input []string, mode string) (after []string, added, removed int) {
	set := make(map[string]struct{}, len(before)+len(input))
	for _, w := range before {
		set[w] = struct{}{}
	}
	if mode == "replace" {
		for _, w := range input {
			if _, ok := set[w]; !ok {
				added++
			} else {
				delete(set, w)
			}
		}
		return input, added, len(set)
	}
	for _, w := range input {
		_, ok := set[w]
		if mode == v3Add && !ok {
			set[w] = struct{}{}
			added++
		}
		if mode == "remove" && ok {
			delete(set, w)
			removed++
		}
	}
	after = make([]string, 0, len(set))
	for w := range set {
		after = append(after, w)
	}
	slices.Sort(after)
	return after, added, removed
}
func (v *VersionedCollection) stage(ctx context.Context, l *v3Lease, key, registry, id string, data []byte) error {
	ok, err := v.client.Eval(ctx, v3StageScript, []string{v.key(v3Maintenance), v.key("writers"), v.key(key), v.key(registry)}, l.member, data, id).Int()
	if err != nil {
		return err
	}
	if ok == -1 {
		return ErrVersionedCorrupt
	}
	if ok != 1 {
		return ErrLeaseExpired
	}
	return nil
}
func (v *VersionedCollection) stageAll(ctx context.Context, l *v3Lease, stages []v3Stage) error {
	if len(stages) == 0 {
		return nil
	}
	groups := make(map[uint16][]v3Stage)
	var global []v3Stage
	for _, stage := range stages {
		if stage.sharded {
			groups[stage.shard] = append(groups[stage.shard], stage)
		} else {
			global = append(global, stage)
		}
	}
	if len(groups) == 0 {
		return v.stageBatch(ctx, l, global)
	}
	group, workCtx := errgroup.WithContext(ctx)
	group.SetLimit(v.opts.ShardConcurrency)
	for shard, work := range groups {
		group.Go(func() error {
			if err := v.mirrorWriter(workCtx, l, shard); err != nil {
				return err
			}
			for len(work) > 0 {
				n := min(len(work), v3StageBatch)
				if err := v.stageBatch(workCtx, l, work[:n]); err != nil {
					return err
				}
				work = work[n:]
			}
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return err
	}
	return v.stageBatch(ctx, l, global)
}

const v3StageBatch = 64

func (v *VersionedCollection) stageBatch(ctx context.Context, l *v3Lease, stages []v3Stage) error {
	if len(stages) == 0 {
		return nil
	}
	cmds := make([]*redis.Cmd, 0, len(stages))
	_, err := v.client.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		for _, stage := range stages {
			key := v.key
			if stage.sharded {
				key = func(suffix string) string { return v.shardKey(stage.shard, suffix) }
			}
			cmds = append(cmds, pipe.Eval(ctx, v3StageScript,
				[]string{key(v3Maintenance), key("writers"), key(stage.key), key(stage.registry)},
				l.member, stage.data, stage.id))
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, cmd := range cmds {
		ok, err := cmd.Int()
		if err != nil {
			return err
		}
		if ok == -1 {
			return ErrVersionedCorrupt
		}
		if ok != 1 {
			return ErrLeaseExpired
		}
	}
	return nil
}
func (v *VersionedCollection) stageBucket(ctx context.Context, l *v3Lease, words []string) (v3Bucket, error) {
	b, stages := v3Stages(words)
	return b, v.stageAll(ctx, l, stages)
}
func v3Stages(words []string) (v3Bucket, []v3Stage) {
	b := v3Bucket{Count: len(words)}
	if len(words) == 0 {
		return b, nil
	}
	data, _ := json.Marshal(words)
	b.Checksum = v3Hash(data)
	// Size includes JSON quotes, escaping, commas and brackets. Oversize single
	// keywords form independent chunks and cannot make neighboring chunks exceed 1 MiB.
	start, size := 0, 2
	stages := make([]v3Stage, 0, 1)
	flush := func(end int) {
		part, _ := json.Marshal(words[start:end])
		h := v3Hash(part)
		stages = append(stages, v3Stage{key: "chunk:" + h, registry: "chunks", id: h, data: part})
		b.Chunks = append(b.Chunks, h)
		start = end
		size = 2
	}
	for i, w := range words {
		encoded, _ := json.Marshal(w)
		n := len(encoded)
		if i > start {
			n++
		}
		if size+n > v3ChunkBytes && i > start {
			flush(i)
			n = len(encoded)
		}
		size += n
	}
	flush(len(words))
	return b, stages
}

// ResolveOperation retrieves a durable successful commit receipt. redis.Nil
// means no receipt was observed, not proof that an in-flight commit cannot still
// succeed. The caller must not automatically reapply an ambiguous operation.
func (v *VersionedCollection) ResolveOperation(ctx context.Context, id string) (*WriteResult, error) {
	if err := v.check(ctx); err != nil {
		return nil, err
	}
	if len(id) != v3IDLength {
		return nil, errors.New("acor: invalid operation ID")
	}
	raw, err := v.client.Get(ctx, v.key("op:"+id)).Bytes()
	if err != nil {
		return nil, err
	}
	var r WriteResult
	if json.Unmarshal(raw, &r) != nil || !v.valid(r.Version) {
		return nil, ErrVersionedCorrupt
	}
	return &r, nil
}

// V2CopyResult records the exact V2 version read and normalized target checksum.
// Freeze V2 writers before the final copy and application cutover.
type V2CopyResult struct {
	SourceVersion string
	Count         int
	Checksum      string
	Write         *WriteResult
}

// V2CopyOptions controls the optional empty-source guard. A nil option allows an empty source.
type V2CopyOptions struct {
	// RejectEmpty fails before writing when the normalized source contains no keywords.
	RejectEmpty bool
}

// CopyV2 atomically reads V2 keywords and version from a differently named source
// on the same Redis connection and replaces this collection against expected.
// V1 sources must first use the existing V1-to-V2 migration.
func (v *VersionedCollection) CopyV2(ctx context.Context, source string, expected Version, opts *V2CopyOptions) (*V2CopyResult, error) {
	if source == v.opts.Redis.Name || strings.TrimSpace(source) == "" {
		return nil, errors.New("acor: copy requires a distinct nonempty V2 name")
	}
	raw, err := v.client.HMGet(ctx, trieKey(source), fieldVersion, fieldKeywords).Result()
	if err != nil {
		return nil, err
	}
	if raw[0] == nil || raw[1] == nil {
		return nil, redis.Nil
	}
	var words []string
	if json.Unmarshal([]byte(fmt.Sprint(raw[1])), &words) != nil {
		return nil, ErrVersionedCorrupt
	}
	normalized, err := v3Normalize(words, v.opts.CaseSensitive)
	if err != nil {
		return nil, err
	}
	if opts != nil && opts.RejectEmpty && len(normalized) == 0 {
		return nil, errors.New("acor: empty V2 source rejected")
	}
	data, _ := json.Marshal(normalized)
	result := &V2CopyResult{SourceVersion: fmt.Sprint(raw[0]), Count: len(normalized), Checksum: v3Hash(data)}
	result.Write, err = v.Replace(ctx, expected, normalized)
	return result, err
}
