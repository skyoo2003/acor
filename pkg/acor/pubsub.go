package acor

import (
	"fmt"
)

const invalidateChannelPrefix = "acor:invalidate:"

func (ac *AhoCorasick) startCacheListener() error {
	channel := invalidateChannelPrefix + ac.name
	pubsub := ac.redisClient.Subscribe(ac.ctx, channel)

	_, err := pubsub.Receive(ac.ctx)
	if err != nil {
		return fmt.Errorf("pub/sub connection failed: %w", err)
	}

	ac.pubsub = pubsub
	ac.stopCh = make(chan struct{})

	go func() {
		for {
			select {
			case msg, ok := <-pubsub.Channel():
				if !ok {
					return
				}
				if msg.Payload == ac.name {
					ac.cache.invalidate()
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

func (ac *AhoCorasick) publishInvalidate() {
	if ac.cache == nil {
		return
	}
	ac.cache.invalidate()
	channel := invalidateChannelPrefix + ac.name
	ac.redisClient.Publish(ac.ctx, channel, ac.name)
}
