// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"strings"

	redis "github.com/redis/go-redis/v9"
)

const (
	v3LegacyLayoutVersion  uint16 = 1
	v3ShardedLayoutVersion uint16 = 2
	v3MaxShardCount        uint16 = 256
	v3HashLength                  = 64
	v3Maintenance                 = "maintenance"
)

// v3Layout describes storage for one immutable logical generation. The legacy
// format has one shard; only an explicit transition changes an existing layout.
type v3Layout struct {
	Version    uint16
	ShardCount uint16
}

func v3ValidShardCount(count uint16) bool {
	return count >= 1 && count <= v3MaxShardCount && count&(count-1) == 0
}

func v3NormalizeShardOptions(o *VersionedOptions) error {
	if o.ShardCount == 0 {
		o.ShardCount = 1
	}
	if !v3ValidShardCount(o.ShardCount) {
		return errors.New("acor: shard count must be a power of two between 1 and 256")
	}
	if o.ShardConcurrency < 0 {
		return errors.New("acor: invalid shard concurrency")
	}
	return nil
}

// Keep automatic concurrency unresolved in opts: creation options need not match
// a reopened generation, and a reshard target can differ from the serving layout.
func (v *VersionedCollection) shardConcurrency(count int) int {
	if v.opts.ShardConcurrency != 0 {
		return v.opts.ShardConcurrency
	}
	return max(1, min(runtime.GOMAXPROCS(0), count))
}

func (l v3Layout) validate() error {
	if !v3ValidShardCount(l.ShardCount) {
		return ErrVersionedCorrupt
	}
	if l.Version == v3LegacyLayoutVersion && l.ShardCount == 1 {
		return nil
	}
	if l.Version == v3ShardedLayoutVersion && l.ShardCount > 1 {
		return nil
	}
	return ErrVersionedCorrupt
}

// v3GlobalManifest orders shard manifest IDs by shard index. Version and
// Sequence retain the logical collection version independently of shard IDs.
type v3GlobalManifest struct {
	Version  Version
	Sequence uint64
	Layout   v3Layout
	Shards   []string
	Count    int
}

// v3ShardManifest contains only the existing global bucket IDs owned by Shard.
// The legacy v3Manifest remains unchanged and readable.
type v3ShardManifest struct {
	ID      string
	Shard   uint16
	Buckets map[uint16]v3Bucket
	Count   int
}

func (v *VersionedCollection) shardKey(shard uint16, suffix string) string {
	return strings.TrimSuffix(v.prefix, "}:") + "-shard-" + strconv.Itoa(int(shard)) + "}:" + suffix
}

func v3StoredLayout(version, count interface{}) (v3Layout, error) {
	if version == nil && count == nil {
		return v3Layout{Version: v3LegacyLayoutVersion, ShardCount: 1}, nil
	}
	ver, err := strconv.ParseUint(fmt.Sprint(version), 10, 16)
	if err != nil {
		return v3Layout{}, ErrVersionedCorrupt
	}
	n, err := strconv.ParseUint(fmt.Sprint(count), 10, 16)
	if err != nil {
		return v3Layout{}, ErrVersionedCorrupt
	}
	layout := v3Layout{Version: uint16(ver), ShardCount: uint16(n)}
	return layout, layout.validate()
}

func (v *VersionedCollection) initialManifest(ctx context.Context, token Version) ([]byte, v3Layout, error) {
	layout := v3Layout{Version: v3LegacyLayoutVersion, ShardCount: 1}
	exists, err := v.client.Exists(ctx, v.key("meta")).Result()
	if err != nil {
		return nil, layout, err
	}
	if exists != 0 || v.opts.ShardCount == 1 {
		data, _ := json.Marshal(v3Manifest{Version: token, Sequence: 1})
		return data, layout, nil
	}
	layout = v3Layout{Version: v3ShardedLayoutVersion, ShardCount: v.opts.ShardCount}
	m := v3GlobalManifest{Version: token, Sequence: 1, Layout: layout, Shards: make([]string, layout.ShardCount)}
	const initialize = `
 if redis.call('EXISTS',KEYS[3])==1 then return 0 end
 local t=redis.call('TIME'); redis.call('ZADD',KEYS[2],'NX',t[1],ARGV[1])
 redis.call('SET',KEYS[1],ARGV[2],'NX'); return 1`
	for shard := range layout.ShardCount {
		id := v3ID()
		data, _ := json.Marshal(v3ShardManifest{ID: id, Shard: shard, Buckets: make(map[uint16]v3Bucket)})
		keys := []string{v.shardKey(shard, "manifest:"+id), v.shardKey(shard, "manifests"), v.shardKey(shard, v3Maintenance)}
		ok, stageErr := v.client.Eval(ctx, initialize, keys, id, data).Int()
		if stageErr != nil {
			return nil, layout, stageErr
		}
		if ok != 1 {
			return nil, layout, ErrMaintenance
		}
		m.Shards[shard] = id
	}
	data, _ := json.Marshal(m)
	return data, layout, nil
}

// globalManifest validates every referenced shard before returning its metadata.
// Legacy generations are adapted without changing their stored representation.
func (v *VersionedCollection) globalManifest(ctx context.Context, version Version) (*v3GlobalManifest, error) {
	m, err := v.loadManifest(ctx, version)
	if err != nil {
		return nil, err
	}
	if m.global != nil {
		return m.global, nil
	}
	return &v3GlobalManifest{Version: m.Version, Sequence: m.Sequence, Count: m.Count,
		Layout: v3Layout{Version: v3LegacyLayoutVersion, ShardCount: 1}}, nil
}

type v3ManifestHeader struct {
	Layout  json.RawMessage
	Buckets json.RawMessage
	Shards  json.RawMessage
}

func (h v3ManifestHeader) legacy() bool {
	return len(h.Buckets) != 0 && string(h.Buckets) != "null" && len(h.Shards) == 0
}

func (v *VersionedCollection) loadManifest(ctx context.Context, version Version) (*v3Manifest, error) {
	if !v.valid(version) {
		return nil, ErrInvalidVersion
	}
	data, err := v.client.Get(ctx, v.key("gen:"+string(version))).Bytes()
	if err != nil {
		return nil, err
	}
	var header v3ManifestHeader
	if json.Unmarshal(data, &header) != nil {
		return nil, ErrVersionedCorrupt
	}
	if len(header.Layout) == 0 {
		if !header.legacy() {
			return nil, ErrVersionedCorrupt
		}
		return v.legacyManifest(data, version)
	}
	var global v3GlobalManifest
	if json.Unmarshal(data, &global) != nil || global.Version != version || global.Sequence == 0 || global.Count < 0 {
		return nil, ErrVersionedCorrupt
	}
	if global.Layout.validate() != nil || global.Layout.Version != v3ShardedLayoutVersion || len(global.Shards) != int(global.Layout.ShardCount) {
		return nil, ErrVersionedCorrupt
	}
	m := &v3Manifest{Version: version, Sequence: global.Sequence, Count: global.Count, global: &global}
	if err := v.loadShards(ctx, m); err != nil {
		return nil, err
	}
	return m, nil
}

func (v *VersionedCollection) loadShards(ctx context.Context, m *v3Manifest) error {
	count := 0
	concurrency := v.shardConcurrency(int(m.global.Layout.ShardCount))
	for start := 0; start < len(m.global.Shards); {
		end := min(start+concurrency, len(m.global.Shards))
		cmds := make([]*redis.StringCmd, 0, end-start)
		_, err := v.client.Pipelined(ctx, func(pipe redis.Pipeliner) error {
			for shard := start; shard < end; shard++ {
				id := m.global.Shards[shard]
				if len(id) != v3IDLength {
					return &v3ShardError{shard: shard, err: ErrVersionedCorrupt}
				}
				cmds = append(cmds, pipe.Get(ctx, v.shardKey(uint16(shard), "manifest:"+id))) //nolint:gosec // Validated shard count is at most 256.
			}
			return nil
		})
		if err != nil {
			for i, cmd := range cmds {
				if cause := cmd.Err(); cause != nil {
					if errors.Is(cause, redis.Nil) {
						cause = ErrVersionedCorrupt
					}
					return &v3ShardError{shard: start + i, err: cause}
				}
			}
			return err
		}
		for i, cmd := range cmds {
			data, readErr := cmd.Bytes()
			if readErr != nil {
				return &v3ShardError{shard: start + i, err: readErr}
			}
			shard := uint16(start + i) //nolint:gosec // Validated shard count is at most 256.
			n, readErr := v3DecodeShard(data, m, shard)
			if readErr != nil || n > m.Count-count {
				return &v3ShardError{shard: int(shard), err: ErrVersionedCorrupt}
			}
			count += n
		}
		start = end
	}
	if count != m.Count {
		return ErrVersionedCorrupt
	}
	return nil
}

func v3DecodeShard(data []byte, m *v3Manifest, shard uint16) (int, error) {
	var sm v3ShardManifest
	if json.Unmarshal(data, &sm) != nil || sm.ID != m.global.Shards[shard] || sm.Shard != shard || sm.Count < 0 || sm.Buckets == nil {
		return 0, ErrVersionedCorrupt
	}
	count := 0
	for number, bucket := range sm.Buckets {
		if number >= v3BucketCount || number%m.global.Layout.ShardCount != shard || bucket.Count <= 0 || bucket.Count > sm.Count-count {
			return 0, ErrVersionedCorrupt
		}
		if !v3ValidBucketReferences(bucket) {
			return 0, ErrVersionedCorrupt
		}
		bucket.sharded, bucket.shard = true, shard
		m.Buckets[number] = bucket
		count += bucket.Count
	}
	if count != sm.Count {
		return 0, ErrVersionedCorrupt
	}
	return count, nil
}

func v3ValidBucketReferences(bucket v3Bucket) bool {
	if len(bucket.Chunks) == 0 || len(bucket.Checksum) != v3HashLength {
		return false
	}
	for _, chunk := range bucket.Chunks {
		if len(chunk) != v3HashLength {
			return false
		}
	}
	return true
}

// A shard lease never outlives its global writer lease. Shard maintenance uses
// the same monotonic global fence, rejecting a delayed pre-maintenance mirror.
func (v *VersionedCollection) mirrorWriter(ctx context.Context, l *v3Lease, shard uint16) error {
	if l.closed.Load() || l.expired.Load() {
		return ErrLeaseExpired
	}
	const deadline = v3Now + `
 if redis.call('EXISTS',KEYS[1])==1 then return {} end
 local e=redis.call('ZSCORE',KEYS[2],ARGV[1])
 if not e or tonumber(e)<=now then return {} end
 return {e,redis.call('GET',KEYS[3]) or '0'}`
	r, err := v.client.Eval(ctx, deadline, []string{v.key(v3Maintenance), v.key("writers"), v.key("fence")}, l.member).StringSlice()
	if err != nil {
		return err
	}
	if len(r) != v3PairLength {
		return ErrLeaseExpired
	}
	const mirror = v3Now + `
 if redis.call('EXISTS',KEYS[1])==1 or tonumber(ARGV[2])<=now then return 0 end
 local fence=redis.call('GET',KEYS[3]) or '0'
 if tonumber(fence)>tonumber(ARGV[3]) then return 0 end
 redis.call('SET',KEYS[3],ARGV[3])
 redis.call('ZADD',KEYS[2],ARGV[2],ARGV[1]); return 1`
	keys := []string{v.shardKey(shard, v3Maintenance), v.shardKey(shard, "writers"), v.shardKey(shard, "fence")}
	ok, err := v.client.Eval(ctx, mirror, keys, l.member, r[0], r[1]).Int()
	if err != nil {
		return err
	}
	if ok != 1 {
		return ErrLeaseExpired
	}
	v.mu.Lock()
	if l.closed.Load() {
		v.mu.Unlock()
		_ = v.client.ZRem(ctx, v.shardKey(shard, "writers"), l.member).Err()
		return ErrLeaseExpired
	}
	l.shards[shard] = struct{}{}
	v.mu.Unlock()
	return nil
}

// v3ShardFor routes normalized UTF-8 keyword bytes using the existing SHA-256
// bucket number, keeping every bucket on one shard so chunks can be reused.
// Counts must be validated before routing; zero is the legacy default.
func v3ShardFor(word string, shardCount uint16) uint16 {
	if shardCount <= 1 {
		return 0
	}
	return uint16(v3BucketNumber(word)) % shardCount //nolint:gosec // Bucket number is a 12-bit SHA-256 prefix, at most 4095.
}
