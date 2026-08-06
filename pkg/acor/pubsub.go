// SPDX-License-Identifier: Apache-2.0

package acor

import "fmt"

const (
	invalidateChannelPrefix   = "acor:invalidate:"
	invalidatePayloadSplitMax = 2
)

func (ac *AhoCorasick) startCacheListener() error {
	ac.stopCh = make(chan struct{})

	// Bind the cache once per message: RollbackToV1 clears ac.cache, so re-reading
	// the field mid-callback can hand us a nil pointer between the guard and the use.
	cache := ac.cache
	// Bound alongside cache for the same reason: the callback runs on its own
	// goroutine, so it reads neither field live.
	stats := ac.stats
	pubsub, err := subscribeInvalidations(ac.ctx, ac.storage, ac.name, ac.stopCh, func(payload string) {
		if cache == nil {
			return
		}
		if isSelfEcho(payload, ac.name, &cache.selfSkip) {
			return
		}
		// Foreign invalidations only: a self-echo returned above, and timing this
		// process against its own clock would say nothing about how fast peers reach us.
		if lag, ok := invalidationLag(payload); ok {
			stats.recordInvalidationLag(lag)
		}
		cache.invalidate()
	})
	if err != nil {
		return fmt.Errorf("pub/sub connection failed: %w", err)
	}

	ac.pubsub = pubsub
	return nil
}

// stopCacheListener is idempotent: RollbackToV1 stops the listener mid-life and
// Close stops it again, and closing an already-closed stopCh would panic.
func (ac *AhoCorasick) stopCacheListener() {
	if ac.stopCh != nil {
		close(ac.stopCh)
		ac.stopCh = nil
	}
	if ac.pubsub != nil {
		_ = ac.pubsub.Close()
		ac.pubsub = nil
	}
}
