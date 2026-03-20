package acor

import "errors"

var errPubSubNotImplemented = errors.New("pub/sub listener not implemented")

func (ac *AhoCorasick) startCacheListener() error {
	return errPubSubNotImplemented
}
