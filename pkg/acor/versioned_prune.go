// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"fmt"
	"strings"

	redis "github.com/redis/go-redis/v9"
)

// PruneResult counts deleted immutable records. Operation receipts are retained
// indefinitely so delayed commit reconciliation remains possible.
type PruneResult struct {
	// Generations counts global generation records; shard manifests are also collected.
	Generations int
	// Chunks counts deleted chunks across legacy and shard namespaces.
	Chunks int
}

const v3MaintenanceTTL = 30000
const v3PruneBatch = 64
const v3RetentionSeconds = 24 * 60 * 60

const v3FenceScript = `
 if redis.call('GET',KEYS[1])~=ARGV[1] then return 0 end
 redis.call('PEXPIRE',KEYS[1],ARGV[2]); return 1`

// Prune explicitly collects unreachable data, retaining the active generation,
// generations prepared in the last 24 hours, and all valid reader leases.
// Retained global generations protect every referenced shard manifest and chunk.
// It excludes writers and new snapshots while allowing local searches and lease
// renewals. Each bounded deletion checks a monotonic fencing token atomically.
func (v *VersionedCollection) Prune(ctx context.Context) (*PruneResult, error) {
	if err := v.check(ctx); err != nil {
		return nil, err
	}
	const acquire = v3Now + `
 redis.call('ZREMRANGEBYSCORE',KEYS[2],'-inf',now)
 redis.call('ZREMRANGEBYSCORE',KEYS[4],'-inf',now)
 if redis.call('EXISTS',KEYS[1])==1 or redis.call('ZCARD',KEYS[2])>0 then return {} end
 redis.call('INCR',KEYS[3]); local token=redis.call('GET',KEYS[3])
 redis.call('SET',KEYS[1],token,'PX',ARGV[1])
 return {token,tostring(math.floor(now/1000))}`
	r, err := v.client.Eval(ctx, acquire, []string{v.key("maintenance"), v.key("writers"), v.key("fence"), v.key("leases")}, v3MaintenanceTTL).StringSlice()
	if err != nil {
		return nil, err
	}
	if len(r) != v3PairLength {
		return nil, ErrMaintenance
	}
	token := r[0]
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), v3CleanupTimeout)
		defer cancel()
		const release = `if redis.call('GET',KEYS[1])==ARGV[1] then return redis.call('DEL',KEYS[1]) end return 0`
		_ = v.client.Eval(cleanup, release, []string{v.key("maintenance")}, token).Err()
	}()
	var now int64
	if _, err = fmt.Sscan(r[1], &now); err != nil {
		return nil, err
	}
	return v.pruneLocked(ctx, token, now)
}
func (v *VersionedCollection) fence(ctx context.Context, token string) error {
	n, err := v.client.Eval(ctx, v3FenceScript, []string{v.key("maintenance")}, token, v3MaintenanceTTL).Int()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrMaintenance
	}
	return nil
}
func (v *VersionedCollection) pruneLocked(ctx context.Context, token string, now int64) (*PruneResult, error) {
	keep, gens, err := v.pruneKeep(ctx, now)
	if err != nil {
		return nil, err
	}
	refs, err := v.pruneReferences(ctx, token, keep)
	if err != nil {
		return nil, err
	}
	deadChunks, err := v.pruneCandidates(ctx, v.key, "chunks", "chunk:", refs)
	if err != nil {
		return nil, err
	}
	deadGens := make([]string, 0)
	for _, g := range gens {
		name := fmt.Sprint(g.Member)
		if !keep[name] {
			deadGens = append(deadGens, name)
		}
	}
	result := &PruneResult{}
	result.Chunks, err = v.pruneDelete(ctx, token, "chunks", "chunk:", deadChunks)
	if err != nil {
		return result, err
	}
	result.Generations, err = v.pruneDelete(ctx, token, "generations", "gen:", deadGens)
	if err != nil {
		return result, err
	}
	// Sweep every supported namespace, including abandoned preparations for a
	// target layout that never acquired a global manifest or became active.
	err = v.pruneShards(ctx, token, refs, result)
	return result, err
}

// Global maintenance excludes new writers. Discover occupied namespaces in
// bounded pipelines without creating shard state for a legacy-only collection.
// Expired preparations cannot register fresh data after this discovery because
// their mirrored writer deadlines cannot outlive their expired global leases.
func (v *VersionedCollection) pruneShards(ctx context.Context, token string, refs map[string]bool, result *PruneResult) error {
	for start := uint16(0); start < v3MaxShardCount; start += v3PruneBatch {
		if err := v.fence(ctx, token); err != nil {
			return err
		}
		end := min(start+v3PruneBatch, v3MaxShardCount)
		cmds := make([]*redis.IntCmd, 0, end-start)
		_, err := v.client.Pipelined(ctx, func(pipe redis.Pipeliner) error {
			for shard := start; shard < end; shard++ {
				cmds = append(cmds, pipe.Exists(ctx, v.shardKey(shard, "chunks"), v.shardKey(shard, "manifests"), v.shardKey(shard, "writers")))
			}
			return nil
		})
		if err != nil {
			return err
		}
		for i, cmd := range cmds {
			if cmd.Val() == 0 {
				continue
			}
			deleted, err := v.pruneShard(ctx, token, start+uint16(i), refs) //nolint:gosec // Batch index is at most 63.
			result.Chunks += deleted
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (v *VersionedCollection) pruneReferences(ctx context.Context, token string, keep map[string]bool) (map[string]bool, error) {
	refs := make(map[string]bool)
	for generation := range keep {
		if fenceErr := v.fence(ctx, token); fenceErr != nil {
			return nil, fenceErr
		}
		m, readErr := v.manifest(ctx, Version(generation))
		if readErr != nil {
			return nil, readErr
		}
		if m.global != nil {
			for shard, id := range m.global.Shards {
				refs[v.shardKey(uint16(shard), "manifest:"+id)] = true //nolint:gosec // Validated shard count is at most 256.
			}
		}
		for _, b := range &m.Buckets {
			for _, h := range b.Chunks {
				refs[v.bucketChunkKey(b, h)] = true
			}
		}
	}
	return refs, nil
}

func (v *VersionedCollection) pruneCandidates(ctx context.Context, key func(string) string, registry, prefix string,
	refs map[string]bool) ([]string, error) {
	ids, err := v.client.ZRange(ctx, key(registry), 0, -1).Result()
	if err != nil {
		return nil, err
	}
	dead := make([]string, 0)
	for _, id := range ids {
		if !refs[key(prefix+id)] {
			dead = append(dead, id)
		}
	}
	return dead, nil
}

// Renew the global token before granting shard maintenance with the same fence
// and deadline. A local lock can never outlive the global owner's authority.
func (v *VersionedCollection) pruneShardFence(ctx context.Context, token string, shard uint16) error {
	const global = v3Now + `
 if redis.call('GET',KEYS[1])~=ARGV[1] then return 0 end
 redis.call('PEXPIRE',KEYS[1],ARGV[2]); return now+ARGV[2]`
	deadline, err := v.client.Eval(ctx, global, []string{v.key(v3Maintenance)}, token, v3MaintenanceTTL).Int64()
	if err != nil {
		return err
	}
	if deadline == 0 {
		return ErrMaintenance
	}
	const local = v3Now + `
 if tonumber(ARGV[2])<=now then return 0 end
 local fence=redis.call('GET',KEYS[3]) or '0'
 if tonumber(fence)>tonumber(ARGV[1]) then return 0 end
 local owner=redis.call('GET',KEYS[1])
 if owner and owner~=ARGV[1] then return 0 end
 redis.call('ZREMRANGEBYSCORE',KEYS[2],'-inf',now)
 if redis.call('ZCARD',KEYS[2])>0 then return 0 end
 redis.call('SET',KEYS[3],ARGV[1])
 redis.call('SET',KEYS[1],ARGV[1],'PX',ARGV[2]-now); return 1`
	keys := []string{v.shardKey(shard, v3Maintenance), v.shardKey(shard, "writers"), v.shardKey(shard, "fence")}
	ok, err := v.client.Eval(ctx, local, keys, token, deadline).Int()
	if err != nil {
		return err
	}
	if ok != 1 {
		return ErrMaintenance
	}
	return nil
}

func (v *VersionedCollection) pruneShard(ctx context.Context, token string, shard uint16, refs map[string]bool) (int, error) {
	renew := func(ctx context.Context) error { return v.pruneShardFence(ctx, token, shard) }
	if err := renew(ctx); err != nil {
		return 0, err
	}
	key := func(suffix string) string { return v.shardKey(shard, suffix) }
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), v3CleanupTimeout)
		defer cancel()
		const release = `if redis.call('GET',KEYS[1])==ARGV[1] then return redis.call('DEL',KEYS[1]) end return 0`
		_ = v.client.Eval(cleanup, release, []string{key(v3Maintenance)}, token).Err()
	}()
	chunks, err := v.pruneCandidates(ctx, key, "chunks", "chunk:", refs)
	if err != nil {
		return 0, err
	}
	manifests, err := v.pruneCandidates(ctx, key, "manifests", "manifest:", refs)
	if err != nil {
		return 0, err
	}
	count, err := v.pruneDeleteAt(ctx, token, "chunks", "chunk:", chunks, key, renew)
	if err != nil {
		return count, err
	}
	_, err = v.pruneDeleteAt(ctx, token, "manifests", "manifest:", manifests, key, renew)
	return count, err
}
func (v *VersionedCollection) pruneDelete(ctx context.Context, token, registry, prefix string, ids []string) (int, error) {
	return v.pruneDeleteAt(ctx, token, registry, prefix, ids, v.key, func(ctx context.Context) error { return v.fence(ctx, token) })
}

func (v *VersionedCollection) pruneDeleteAt(ctx context.Context, token, registry, prefix string, ids []string,
	key func(string) string, renew func(context.Context) error) (int, error) {
	const script = `
 if redis.call('GET',KEYS[1])~=ARGV[1] then return -1 end
 for i=3,#KEYS do redis.call('DEL',KEYS[i]); redis.call('ZREM',KEYS[2],ARGV[i-1]) end
 return #KEYS-2`
	count := 0
	for len(ids) > 0 {
		if err := renew(ctx); err != nil {
			return count, err
		}
		n := min(v3PruneBatch, len(ids))
		keys := []string{key(v3Maintenance), key(registry)}
		args := []interface{}{token}
		for _, id := range ids[:n] {
			keys = append(keys, key(prefix+id))
			args = append(args, id)
		}
		deleted, err := v.client.Eval(ctx, script, keys, args...).Int()
		if err != nil {
			return count, err
		}
		if deleted < 0 {
			return count, ErrMaintenance
		}
		count += deleted
		ids = ids[n:]
	}
	return count, nil
}

func (v *VersionedCollection) pruneKeep(ctx context.Context, now int64) (map[string]bool, []redis.Z, error) {
	active, err := v.client.Get(ctx, v.key("active")).Result()
	if err != nil {
		return nil, nil, err
	}
	keep := map[string]bool{active: true}
	leases, err := v.client.ZRangeWithScores(ctx, v.key("leases"), 0, -1).Result()
	if err != nil {
		return nil, nil, err
	}
	for _, l := range leases {
		if l.Score > float64(now*v3MillisPerSecond) {
			generation, _, _ := strings.Cut(fmt.Sprint(l.Member), "/")
			keep[generation] = true
		}
	}
	gens, err := v.client.ZRangeWithScores(ctx, v.key("generations"), 0, -1).Result()
	if err != nil {
		return nil, nil, err
	}
	for _, g := range gens {
		if g.Score >= float64(now-v3RetentionSeconds) {
			keep[fmt.Sprint(g.Member)] = true
		}
	}
	return keep, gens, nil
}
