package acor

import (
	"fmt"
)

const invalidateChannelPrefix = "acor:invalidate:"

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
				if msg.Payload == ac.name {
					if ac.cache != nil {
						if skipSelfCheck(ac.cache) {
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
