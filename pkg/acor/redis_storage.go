// SPDX-License-Identifier: Apache-2.0

package acor

import (
	redis "github.com/go-redis/redis/v8"

	kvstore "github.com/skyoo2003/acor/internal/storage"
)

// newRedisStorage wraps a Redis client in the internal storage adapter.
// It is a package-private helper so call sites (and tests) depend on this
// unexported name rather than importing internal/storage directly.
func newRedisStorage(client redis.UniversalClient) KVStorage {
	return kvstore.NewRedisStorage(client)
}
