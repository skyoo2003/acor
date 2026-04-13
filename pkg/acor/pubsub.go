package acor

import (
	"fmt"
	"strings"
)

const (
	invalidateChannelPrefix   = "acor:invalidate:"
	invalidatePayloadSplitMax = 2
)

func (ac *AhoCorasick) startCacheListener() error {
	channel := invalidateChannelPrefix + ac.name
	pubsub := ac.storage.Subscribe(ac.ctx, channel)

	if err := pubsub.Receive(ac.ctx); err != nil {
		_ = pubsub.Close()
		return fmt.Errorf("pub/sub connection failed: %w", err)
	}

	ac.pubsub = pubsub
	ac.stopCh = make(chan struct{})

	go func() {
		msgCh := pubsub.Channel()
		for {
			select {
			case msg, ok := <-msgCh:
				if !ok {
					return
				}
				if ac.cache != nil {
					if parts := strings.SplitN(msg.Payload, ":", invalidatePayloadSplitMax); len(parts) == invalidatePayloadSplitMax && parts[0] == ac.name {
						if skipSelfCheck(ac.cache, parts[1]) {
							continue
						}
						ac.cache.invalidate()
					}
				}
			case <-ac.stopCh:
				return
			case <-ac.ctx.Done():
				return
			}
		}
	}()

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
