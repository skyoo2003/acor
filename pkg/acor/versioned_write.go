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
)

const v3ChunkBytes = 1 << 20
const v3Add = "add"

// Every preparation write is fenced, including writes by an expired process
// resuming after Prune. Registration and content creation are one atomic step.
const v3StageScript = v3Now + `
 if redis.call('EXISTS',KEYS[1])==1 then return 0 end
 local e=redis.call('ZSCORE',KEYS[2],ARGV[1])
 if not e or tonumber(e)<=now then return 0 end
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
	result := &WriteResult{PreviousVersion: expected, Version: expected, OperationID: v3ID()}
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
		b, stageErr := v.stageBucket(ctx, l, after)
		if stageErr != nil {
			return nil, stageErr
		}
		next.Buckets[i] = b
	}
	return v.prepareCommit(ctx, l, &next, result)
}
func (v *VersionedCollection) prepareCommit(ctx context.Context, l *v3Lease, next *v3Manifest, result *WriteResult) (*WriteResult, error) {
	if result.Added != 0 || result.Removed != 0 {
		next.Version = Version(v.id + "." + v3ID())
		next.Sequence++
		next.Count += result.Added - result.Removed
		result.Version = next.Version
		data, _ := json.Marshal(next)
		if err := v.stage(ctx, l, "gen:"+string(next.Version), "generations", string(next.Version), data); err != nil {
			return nil, err
		}
	}
	return v.commit(ctx, l, result, next.Sequence)
}

func (v *VersionedCollection) commit(ctx context.Context, l *v3Lease, result *WriteResult, sequence uint64) (*WriteResult, error) {
	data, _ := json.Marshal(result)
	keys := []string{v.key("maintenance"), v.key("writers"), v.key("active"), v.key("gen:" + string(result.Version)),
		v.key("op:" + result.OperationID), v.key("events"), v.key("committed:" + string(result.Version)), v.key("generations")}
	raw, err := v.client.Eval(ctx, v3CommitScript, keys, l.member, string(result.PreviousVersion), string(result.Version), data, sequence).Text()
	if err != nil {
		return result, fmt.Errorf("%w: %w", ErrCommitUnknown, err)
	}
	switch raw {
	case "conflict":
		return nil, ErrConcurrencyConflict
	case "maintenance":
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
	ok, err := v.client.Eval(ctx, v3StageScript, []string{v.key("maintenance"), v.key("writers"), v.key(key), v.key(registry)}, l.member, data, id).Int()
	if err != nil {
		return err
	}
	if ok != 1 {
		return ErrLeaseExpired
	}
	return nil
}
func (v *VersionedCollection) stageBucket(ctx context.Context, l *v3Lease, words []string) (v3Bucket, error) {
	b := v3Bucket{Count: len(words)}
	if len(words) == 0 {
		return b, nil
	}
	data, _ := json.Marshal(words)
	b.Checksum = v3Hash(data)
	// Size includes JSON quotes, escaping, commas and brackets. Oversize single
	// keywords form independent chunks and cannot make neighboring chunks exceed 1 MiB.
	start, size := 0, 2
	flush := func(end int) error {
		part, _ := json.Marshal(words[start:end])
		h := v3Hash(part)
		if err := v.stage(ctx, l, "chunk:"+h, "chunks", h, part); err != nil {
			return err
		}
		b.Chunks = append(b.Chunks, h)
		start = end
		size = 2
		return nil
	}
	for i, w := range words {
		encoded, _ := json.Marshal(w)
		n := len(encoded)
		if i > start {
			n++
		}
		if size+n > v3ChunkBytes && i > start {
			if err := flush(i); err != nil {
				return b, err
			}
			n = len(encoded)
		}
		size += n
	}
	if err := flush(len(words)); err != nil {
		return b, err
	}
	return b, nil
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
