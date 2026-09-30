// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"errors"
	"runtime"
)

const (
	v3LegacyLayoutVersion  uint16 = 1
	v3ShardedLayoutVersion uint16 = 2
	v3MaxShardCount        uint16 = 256
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
	if o.ShardConcurrency == 0 {
		o.ShardConcurrency = max(1, min(runtime.GOMAXPROCS(0), int(o.ShardCount)))
	}
	return nil
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
//
//nolint:unused // Defines the metadata contract consumed by the next persistence task.
type v3GlobalManifest struct {
	Version  Version
	Sequence uint64
	Layout   v3Layout
	Shards   []string
	Count    int
}

// v3ShardManifest contains only the existing global bucket IDs owned by Shard.
// The legacy v3Manifest remains unchanged and readable.
//
//nolint:unused // Defines the metadata contract consumed by the next persistence task.
type v3ShardManifest struct {
	ID      string
	Shard   uint16
	Buckets map[uint16]v3Bucket
	Count   int
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
