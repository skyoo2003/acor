// SPDX-License-Identifier: Apache-2.0

package acor

import kvstore "github.com/skyoo2003/acor/internal/storage"

// Storage backend contract, re-exported from the internal storage package so
// callers depend only on the public acor package. Implement KVStorage and pass
// it via AhoCorasickArgs.Storage to plug in a custom (non-Redis) backend; only
// the V1 schema is supported for custom backends. The full method set is
// documented at KVStorage in the internal/storage package and in the
// "Custom Storage" guide.
type (
	// KVStorage is the key-value storage backend contract used by ACOR.
	KVStorage = kvstore.KVStorage
	// Z is a sorted-set member (score + value) used by KVStorage.
	Z = kvstore.Z
	// Pipeliner batches storage commands; passed to KVStorage.TxPipelined.
	Pipeliner = kvstore.Pipeliner
	// Subscription is a pub/sub subscription returned by KVStorage.Subscribe.
	Subscription = kvstore.Subscription
	// StringMapResult is a deferred hash result from a pipelined HGetAll.
	StringMapResult = kvstore.StringMapResult
	// PubSubMessage is a message delivered on a Subscription channel.
	PubSubMessage = kvstore.PubSubMessage
)
