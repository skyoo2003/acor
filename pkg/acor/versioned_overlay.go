// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"iter"
	"time"

	matchengine "github.com/skyoo2003/acor/internal/engine"
)

func v3MapSequence(words map[string]struct{}) iter.Seq[string] {
	return func(yield func(string) bool) {
		for word := range words {
			if !yield(word) {
				return
			}
		}
	}
}

// Compare the target to the dictionary represented by the base automaton.
// Unchanged buckets share their backing slice and cost no per-word work.
func v3BucketDelta(previous *v3Engine, target *v3Manifest, next *[v3BucketCount][]string) (added, removed map[string]struct{}) {
	base := previous.baseBuckets
	if base == nil {
		base = previous.buckets
	}
	manifest := previous.baseManifest
	if manifest == nil {
		manifest = previous.manifest
	}
	added = make(map[string]struct{})
	removed = make(map[string]struct{})
	for i := range next {
		if sameBucket(manifest.Buckets[i], target.Buckets[i]) {
			continue
		}
		old := make(map[string]struct{}, len(base[i]))
		for _, word := range base[i] {
			old[word] = struct{}{}
		}
		for _, word := range next[i] {
			if _, ok := old[word]; ok {
				delete(old, word)
			} else {
				added[word] = struct{}{}
			}
		}
		for word := range old {
			removed[word] = struct{}{}
		}
	}
	return added, removed
}

func (v *VersionedCollection) tryOverlay(ctx context.Context, s *Snapshot, buckets *[v3BucketCount][]string,
	downloaded, reused int) (bool, error) {
	previous := v.current.Load()
	if previous == nil || !v.opts.DeltaSearch || v.opts.Preset != PresetMemoryEfficient {
		return false, nil
	}
	base := previous.base
	if base == nil {
		base = previous.engine
	}
	added, removed := v3BucketDelta(previous, s.manifest, buckets)
	if len(added)+len(removed) > v3OverlayLimit {
		return false, nil
	}
	addition := matchengine.New(enginePreset(v.opts.Preset))
	if err := addition.BuildSequenceContext(ctx, v3MapSequence(added), len(added)); err != nil {
		return true, err
	}
	if err := s.lease.check(ctx); err != nil {
		return true, err
	}
	if err := v.check(ctx); err != nil {
		return true, err
	}
	v.installOverlay(s, base, addition, added, removed, buckets, downloaded, reused)
	return true, nil
}

func (v *VersionedCollection) installOverlay(s *Snapshot, base, addition *matchengine.Engine,
	added, removed map[string]struct{}, buckets *[v3BucketCount][]string, downloaded, reused int) {
	previous := v.current.Load()
	baseBuckets := previous.baseBuckets
	if baseBuckets == nil {
		baseBuckets = previous.buckets
	}
	baseManifest := previous.baseManifest
	if baseManifest == nil {
		baseManifest = previous.manifest
	}
	v.current.Store(&v3Engine{engine: matchengine.Overlay(base, addition, removed), base: base, baseBuckets: baseBuckets,
		baseManifest: baseManifest,
		added:        added, removed: removed, version: s.Version(), sequence: s.manifest.Sequence,
		manifest: s.manifest, buckets: buckets, installed: time.Now()})
	v.mu.Lock()
	v.status.ServingVersion = s.Version()
	v.status.LastError = ""
	v.status.DownloadedBuckets = downloaded
	v.status.ReusedBuckets = reused
	v.status.CompletedBuilds++
	v.status.DeltaSearch = true
	v.status.DeltaKeywords = len(added) + len(removed)
	v.mu.Unlock()
}

func (v *VersionedCollection) compactLoop() {
	defer v.wg.Done()
	ticker := time.NewTicker(v3CompactPoll)
	defer ticker.Stop()
	for {
		select {
		case <-v.ctx.Done():
			return
		case <-ticker.C:
			v.compactIfIdle(v.ctx)
		}
	}
}

func (v *VersionedCollection) compactIfIdle(ctx context.Context) {
	current := v.current.Load()
	if current == nil || current.base == nil || time.Since(current.installed) < v3CompactDelay {
		return
	}
	if v.current.Load() != current || ctx.Err() != nil {
		return
	}
	start := time.Now()
	v.mu.Lock()
	v.status.Building = true
	v.status.BuildStarted = start
	v.mu.Unlock()
	defer func() {
		v.mu.Lock()
		if v.current.Load() == current {
			v.status.Building = false
			v.status.BuildDuration = time.Since(start)
		}
		v.mu.Unlock()
	}()
	e := matchengine.New(enginePreset(v.opts.Preset))
	if err := e.BuildSequenceContext(ctx, bucketSequence(current.buckets), current.manifest.Count); err != nil {
		if v.current.Load() == current {
			v.recordCompactError(err)
		}
		return
	}
	active, err := v.client.Get(ctx, v.key("active")).Result()
	if err != nil {
		if v.current.Load() == current {
			v.recordCompactError(err)
		}
		return
	}
	v.refreshMu.Lock()
	defer v.refreshMu.Unlock()
	if Version(active) != current.version || v.current.Load() != current || ctx.Err() != nil {
		return
	}
	v.current.Store(&v3Engine{engine: e, version: current.version, sequence: current.sequence,
		manifest: current.manifest, buckets: current.buckets, installed: time.Now()})
	v.mu.Lock()
	v.status.Building = false
	v.status.BuildDuration = time.Since(start)
	v.status.LastError = ""
	v.status.CompletedBuilds++
	v.status.DeltaSearch = false
	v.status.DeltaKeywords = 0
	v.mu.Unlock()
}

func (v *VersionedCollection) recordCompactError(err error) {
	if v.ctx.Err() != nil {
		return
	}
	v.mu.Lock()
	v.status.LastError = err.Error()
	v.mu.Unlock()
}
