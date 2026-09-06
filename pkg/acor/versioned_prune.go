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
	Generations int
	Chunks      int
}

const v3MaintenanceTTL = 30000
const v3PruneBatch = 64
const v3RetentionSeconds = 24 * 60 * 60

const v3FenceScript = `
 if redis.call('GET',KEYS[1])~=ARGV[1] then return 0 end
 redis.call('PEXPIRE',KEYS[1],ARGV[2]); return 1`

// Prune explicitly collects unreachable data, retaining the active generation,
// generations prepared in the last 24 hours, and all valid reader leases.
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
	refs := make(map[string]bool)
	for generation := range keep {
		if fenceErr := v.fence(ctx, token); fenceErr != nil {
			return nil, fenceErr
		}
		m, readErr := v.manifest(ctx, Version(generation))
		if readErr != nil {
			return nil, readErr
		}
		for _, b := range &m.Buckets {
			for _, h := range b.Chunks {
				refs[h] = true
			}
		}
	}
	chunks, err := v.client.ZRange(ctx, v.key("chunks"), 0, -1).Result()
	if err != nil {
		return nil, err
	}
	deadChunks := make([]string, 0)
	for _, h := range chunks {
		if !refs[h] {
			deadChunks = append(deadChunks, h)
		}
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
	return result, err
}
func (v *VersionedCollection) pruneDelete(ctx context.Context, token, registry, prefix string, ids []string) (int, error) {
	const script = `
 if redis.call('GET',KEYS[1])~=ARGV[1] then return -1 end
 redis.call('PEXPIRE',KEYS[1],ARGV[2])
 for i=3,#KEYS do redis.call('DEL',KEYS[i]); redis.call('ZREM',KEYS[2],ARGV[i]) end
 return #KEYS-2`
	count := 0
	for len(ids) > 0 {
		n := min(v3PruneBatch, len(ids))
		keys := []string{v.key("maintenance"), v.key(registry)}
		args := []interface{}{token, v3MaintenanceTTL}
		for _, id := range ids[:n] {
			keys = append(keys, v.key(prefix+id))
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
