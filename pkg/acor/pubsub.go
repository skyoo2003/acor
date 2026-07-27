// SPDX-License-Identifier: Apache-2.0

package acor

import "fmt"

const (
	invalidateChannelPrefix   = "acor:invalidate:"
	invalidatePayloadSplitMax = 2
)

func (ac *AhoCorasick) startCacheListener() error {
	ac.stopCh = make(chan struct{})

	pubsub, err := subscribeInvalidations(ac.ctx, ac.storage, ac.name, ac.stopCh, func(payload string) {
		if ac.cache == nil {
			return
		}
		if isSelfEcho(payload, ac.name, &ac.cache.selfSkip) {
			return
		}
		ac.cache.invalidate()
	})
	if err != nil {
		return fmt.Errorf("pub/sub connection failed: %w", err)
	}

	ac.pubsub = pubsub
	return nil
}

func (ac *AhoCorasick) stopCacheListener() {
	if ac.stopCh != nil {
		close(ac.stopCh)
	}
	if ac.pubsub != nil {
		_ = ac.pubsub.Close()
	}
}
