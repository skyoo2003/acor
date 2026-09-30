// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"iter"
	"slices"

	"golang.org/x/sync/errgroup"

	matchengine "github.com/skyoo2003/acor/internal/engine"
)

// Cached words have already passed chunk and bucket checksums. They are ordinary
// local immutable strings, so reusing them never reads a pruned Redis generation.
// Only the newly installed generation is kept; failed candidates are discarded.
func (v *VersionedCollection) engineBuckets(ctx context.Context, s *Snapshot) (next *[v3BucketCount][]string, downloaded, reused int, err error) {
	next = new([v3BucketCount][]string)
	previous := v.current.Load()

	for i, b := range &s.manifest.Buckets {
		if err := ctx.Err(); err != nil {
			return nil, 0, 0, err
		}
		if previous != nil && previous.buckets != nil && sameBucket(previous.manifest.Buckets[i], b) {
			next[i] = previous.buckets[i]
			reused++
			continue
		}
		words, err := v.bucket(ctx, b)
		if err != nil {
			return nil, 0, 0, err
		}
		next[i] = words
		if b.Count > 0 {
			downloaded++
		}
	}
	return next, downloaded, reused, nil
}

// Build a complete candidate without mutating any installed engine or cache.
// Manifest IDs identify immutable shard dictionaries, including empty shards.
func (v *VersionedCollection) buildGeneration(ctx context.Context, s *Snapshot,
	buckets *[v3BucketCount][]string) (engine *matchengine.Engine, shards []*matchengine.Engine, err error) {
	if s.manifest.global == nil {
		e := matchengine.New(enginePreset(v.opts.Preset))
		if err := e.BuildSequenceContext(ctx, bucketSequence(buckets), s.Count()); err != nil {
			return nil, nil, err
		}
		return e, nil, nil
	}
	global := s.manifest.global
	shards = make([]*matchengine.Engine, global.Layout.ShardCount)
	previous := v.current.Load()
	g, buildCtx := errgroup.WithContext(ctx)
	g.SetLimit(v.opts.ShardConcurrency)
	for shard := range shards {
		if reusableShard(previous, global, shard) {
			shards[shard] = previous.shards[shard]
			continue
		}
		g.Go(func() error {
			e := matchengine.New(enginePreset(v.opts.Preset))
			count := 0
			for bucket := shard; bucket < v3BucketCount; bucket += len(shards) {
				count += len(buckets[bucket])
			}
			if err := e.BuildSequenceContext(buildCtx, shardBucketSequence(buckets, shard, len(shards)), count); err != nil {
				return err
			}
			shards[shard] = e
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, nil, err
	}
	return matchengine.NewComposite(shards, v.opts.ShardConcurrency), shards, nil
}

func reusableShard(previous *v3Engine, global *v3GlobalManifest, shard int) bool {
	return previous != nil && previous.manifest.global != nil &&
		previous.manifest.global.Layout == global.Layout && len(previous.shards) == len(global.Shards) &&
		previous.manifest.global.Shards[shard] == global.Shards[shard]
}

func shardBucketSequence(buckets *[v3BucketCount][]string, shard, count int) iter.Seq[string] {
	return func(yield func(string) bool) {
		for bucket := shard; bucket < v3BucketCount; bucket += count {
			for _, word := range buckets[bucket] {
				if !yield(word) {
					return
				}
			}
		}
	}
}
func sameBucket(a, b v3Bucket) bool {
	return a.Count == b.Count && a.Checksum == b.Checksum && slices.Equal(a.Chunks, b.Chunks)
}
func bucketSequence(buckets *[v3BucketCount][]string) iter.Seq[string] {
	return func(yield func(string) bool) {
		for _, words := range buckets {
			for _, word := range words {
				if !yield(word) {
					return
				}
			}
		}
	}
}
