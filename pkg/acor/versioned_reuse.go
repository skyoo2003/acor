// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"iter"
	"slices"
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
