// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"crypto/rand"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Cross-instance cache invalidation, shared by the two modes that keep local
// state: the V2 trie cache and the preset engine. Both publish a message on the
// collection's channel after a write and both must ignore the echo of their own
// publish, so the bookkeeping lives here once.

// selfSkipTTL bounds how long a self-published ID is remembered. A publish whose
// message never comes back — dropped delivery, a listener restart — would
// otherwise leak an entry forever. 30s is orders of magnitude beyond normal
// Redis pub/sub delivery latency.
const selfSkipTTL = 30 * time.Second

// invalidateIDBytes is the length of the random suffix in an invalidation ID.
const invalidateIDBytes = 8

// defaultSelfInvalidationCleanupInterval controls how often selfSkipSet.sweep
// runs relative to publishInvalidate calls. Every N publishes triggers one O(n) sweep.
const defaultSelfInvalidationCleanupInterval = 128

// selfSkipSet remembers the invalidation IDs this process published so the
// listener can skip the invalidation it already applied locally.
//
// sync.Map keeps the publisher and listener goroutines off a shared lock. Stale
// entries are swept periodically rather than on a timer: every cleanupEvery
// publishes triggers one O(n) pass.
type selfSkipSet struct {
	ids sync.Map // id -> time.Time of the publish
	// publishCount is atomic.Uint64 rather than a bare uint64 so it stays
	// 8-byte aligned: this struct is embedded by value, and on 386/arm (both
	// released by goreleaser) a misaligned atomic.AddUint64 panics.
	publishCount atomic.Uint64
	// cleanupEvery is the number of publishes between sweeps. Zero means
	// defaultSelfInvalidationCleanupInterval. Immutable: set it before the set
	// is shared with the listener goroutine, since add reads it unsynchronized.
	cleanupEvery uint64
}

// add records id as self-published and periodically sweeps expired entries.
func (s *selfSkipSet) add(id string) {
	s.ids.Store(id, time.Now())

	every := s.cleanupEvery
	if every == 0 {
		every = defaultSelfInvalidationCleanupInterval
	}
	if s.publishCount.Add(1)%every == 0 {
		s.sweep()
	}
}

// forget drops an id, for a publish that never made it to Redis.
func (s *selfSkipSet) forget(id string) {
	s.ids.Delete(id)
}

// claim atomically consumes id, reporting whether it was a live self-publish.
// An expired or unknown id returns false, so the caller invalidates.
func (s *selfSkipSet) claim(id string) bool {
	val, loaded := s.ids.LoadAndDelete(id)
	if !loaded {
		return false
	}
	t, ok := val.(time.Time)
	if !ok {
		return false
	}
	age := time.Since(t)
	// A negative age means the clock moved backwards; treat it as untrustworthy
	// and invalidate rather than skip.
	if age < 0 {
		return false
	}
	return age < selfSkipTTL
}

// sweep removes expired entries so lost messages cannot grow the map without
// bound. Safe for concurrent use.
func (s *selfSkipSet) sweep() {
	cutoff := time.Now().Add(-selfSkipTTL)
	s.ids.Range(func(key, value interface{}) bool {
		t, ok := value.(time.Time)
		if !ok || t.Before(cutoff) {
			s.ids.Delete(key)
		}
		return true
	})
}

// newInvalidationID returns an id unique to this publish. The timestamp keeps
// ids ordered for debugging; the random suffix keeps two instances publishing in
// the same nanosecond from generating the same id — a collision is the dangerous
// direction, since the loser would mistake the winner's message for its own echo
// and skip a real invalidation.
//
// The id itself contains a ':', so it must always be recovered with the
// invalidatePayloadSplitMax-limited split isSelfEcho uses, never a full split.
func newInvalidationID() string {
	b := make([]byte, invalidateIDBytes)
	// Since Go 1.24 crypto/rand.Read never reports an error; it crashes the
	// process instead, so there is no degraded path to handle here.
	_, _ = rand.Read(b)
	return fmt.Sprintf("%d:%x", time.Now().UnixNano(), b)
}

// invalidationPayload formats the message published on a collection's channel.
// isSelfEcho is the matching parser; the two must stay in sync.
func invalidationPayload(name, id string) string {
	return name + ":" + id
}

// isSelfEcho reports whether payload is this instance's own invalidation, which
// has already been applied locally. A payload that does not parse, or names a
// different collection, counts as foreign: a corrupt message then still
// invalidates instead of silently leaving stale data behind.
func isSelfEcho(payload, name string, skip *selfSkipSet) bool {
	parts := strings.SplitN(payload, ":", invalidatePayloadSplitMax)
	if len(parts) != invalidatePayloadSplitMax || parts[0] != name {
		return false
	}
	return skip.claim(parts[1])
}

// invalidationLag reports how long ago the invalidation in payload was published,
// for CacheStats.LastInvalidationLag. It reads the timestamp newInvalidationID
// already puts in front of every ID, so measuring lag needs no extra wire format.
//
// It reports false rather than a zero duration for a payload it cannot parse, so a
// corrupt message is left out of the statistics instead of recorded as instant
// delivery. The two timestamps come from two machines, so the result carries their
// clock skew; recordInvalidationLag discards a negative value for that reason.
func invalidationLag(payload string) (time.Duration, bool) {
	parts := strings.SplitN(payload, ":", invalidatePayloadSplitMax)
	if len(parts) != invalidatePayloadSplitMax {
		return 0, false
	}
	// parts[1] is the ID, itself "<unixnano>:<random>".
	nanos, _, found := strings.Cut(parts[1], ":")
	if !found {
		return 0, false
	}
	published, err := strconv.ParseInt(nanos, 10, 64)
	if err != nil {
		return 0, false
	}
	return time.Since(time.Unix(0, published)), true
}

// subscribeInvalidations subscribes to the collection's invalidation channel and
// calls onMessage for every payload received, until stopCh is closed, ctx is
// canceled, or the subscription channel closes. The caller closes the returned
// Subscription to release the connection.
func subscribeInvalidations(ctx context.Context, storage KVStorage, name string,
	stopCh <-chan struct{}, onMessage func(payload string)) (Subscription, error) {
	pubsub := storage.Subscribe(ctx, invalidateChannelPrefix+name)
	if err := pubsub.Receive(ctx); err != nil {
		_ = pubsub.Close()
		return nil, err
	}

	go func() {
		msgCh := pubsub.Channel()
		for {
			select {
			case msg, ok := <-msgCh:
				if !ok {
					return
				}
				onMessage(msg.Payload)
			case <-stopCh:
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	return pubsub, nil
}
