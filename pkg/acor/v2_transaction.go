// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// versionRandBytes is the number of random bytes used to extend version timestamps
// and prevent collisions under heavy concurrent writes.
const versionRandBytes = 2

// versionTimestampMask masks the lower 48 bits of a nanosecond timestamp,
// covering ~89 years of precision. Used by generateVersion to pack the
// timestamp into the lower portion of the version int64.
const versionTimestampMask int64 = 0xFFFFFFFFFFFF

// --- V2 transaction helpers (optimistic locking) ---

// generateVersion returns a unique version by packing a nanosecond timestamp into
// the lower 48 bits and a random suffix into the upper 16 bits. This avoids int64
// overflow from additive mixing and makes the encoding easy to reason about.
// The 48-bit timestamp covers ~89 years of nanosecond precision, far exceeding
// any practical use case. The 16-bit random suffix (65536 values) prevents
// collisions when multiple instances generate versions within the same nanosecond.
func generateVersion() (int64, error) {
	b := make([]byte, versionRandBytes)
	if _, err := rand.Read(b); err != nil {
		return 0, fmt.Errorf("generateVersion: crypto/rand.Read failed: %w", err)
	}
	ts := time.Now().UnixNano()
	return (int64(b[0])<<56 | int64(b[1])<<48) | (ts & versionTimestampMask), nil
}

// trieSnapshot holds the deserialized trie data read from Redis.
type trieSnapshot struct {
	Keywords []string
	Prefixes []string
	Suffixes []string
	Version  int64
}

// readTrieSnapshot loads and deserializes the trie hash from Redis.
func readTrieSnapshot(ctx context.Context, storage KVStorage, name string) (*trieSnapshot, error) {
	trieData, err := storage.HGetAll(ctx, trieKey(name))
	if err != nil {
		return nil, newRedisError("HGETALL", trieKey(name), err)
	}

	snap := &trieSnapshot{}

	if data, ok := trieData[fieldKeywords]; ok {
		if err := json.Unmarshal([]byte(data), &snap.Keywords); err != nil {
			return nil, newOperationError("unmarshal", SchemaV2, err)
		}
	}
	if data, ok := trieData[fieldPrefixes]; ok {
		if err := json.Unmarshal([]byte(data), &snap.Prefixes); err != nil {
			return nil, newOperationError("unmarshal", SchemaV2, err)
		}
	}
	if data, ok := trieData[fieldSuffixes]; ok {
		if err := json.Unmarshal([]byte(data), &snap.Suffixes); err != nil {
			return nil, newOperationError("unmarshal", SchemaV2, err)
		}
	}
	if v, ok := trieData[fieldVersion]; ok {
		if err := json.Unmarshal([]byte(v), &snap.Version); err != nil {
			snap.Version = 0
		}
	}

	return snap, nil
}

// marshalTrieArgs serializes trie data into the args map for Lua scripts.
func marshalTrieArgs(snap *trieSnapshot, outputs map[string]string, newVersion int64) (map[string]interface{}, error) {
	args := map[string]interface{}{
		argTrieKey:    "", // caller must set
		argOutputsKey: "", // caller must set
		"newVersion":  newVersion,
		"oldVersion":  snap.Version,
	}
	var err error
	if args[fieldKeywords], err = toJSON(snap.Keywords); err != nil {
		return nil, newOperationError("marshal", SchemaV2, err)
	}
	if args[fieldPrefixes], err = toJSON(snap.Prefixes); err != nil {
		return nil, newOperationError("marshal", SchemaV2, err)
	}
	if args[fieldSuffixes], err = toJSON(snap.Suffixes); err != nil {
		return nil, newOperationError("marshal", SchemaV2, err)
	}
	if args["outputs"], err = toJSON(outputs); err != nil {
		return nil, newOperationError("marshal", SchemaV2, err)
	}
	return args, nil
}

func toJSON(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("json.Marshal failed: %w", err)
	}
	return string(b), nil
}

// commitV2Write stamps a fresh version onto a planned mutation and commits it
// through script under optimistic locking. It returns the version it wrote, or
// ErrConcurrencyConflict when another writer won the race and the caller should
// re-read the snapshot and retry.
func commitV2Write(ctx context.Context, client redis.UniversalClient, script *redis.Script,
	name string, snap *trieSnapshot, outputs map[string][]string) (int64, error) {
	newVersion, err := generateVersion()
	if err != nil {
		return 0, err
	}

	encoded := make(map[string]string, len(outputs))
	for state, outs := range outputs {
		jsonOuts, marshalErr := toJSON(outs)
		if marshalErr != nil {
			return 0, newOperationError("marshal", SchemaV2, marshalErr)
		}
		encoded[state] = jsonOuts
	}

	args, err := marshalTrieArgs(snap, encoded, newVersion)
	if err != nil {
		return 0, err
	}
	args[argTrieKey] = trieKey(name)
	args[argOutputsKey] = outputsKey(name)

	result, err := runV2Script(ctx, client, script, args)
	if err != nil {
		return 0, newRedisError("EVAL", trieKey(name), err)
	}
	if result == 0 {
		return 0, ErrConcurrencyConflict
	}
	return newVersion, nil
}

// retryOnConflict runs attempt until it stops reporting a lost optimistic-lock
// race, backing off linearly in between. Shared by both V2 write paths.
func retryOnConflict(ctx context.Context, attempt func() (int, error)) (int, error) {
	for i := 0; i < maxRetries; i++ {
		n, err := attempt()
		if !errors.Is(err, ErrConcurrencyConflict) {
			return n, err
		}
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(time.Duration(i+1) * retryBackoffBase):
		}
	}
	return 0, ErrConcurrencyConflict
}

func (o *v2Operations) tryAddV2(ctx context.Context, keyword string) (int, error) {
	snap, err := readTrieSnapshot(ctx, o.storage, o.name)
	if err != nil {
		return 0, err
	}

	outputs, changed := planAdd(snap, keyword)
	if !changed {
		return 0, nil
	}

	if _, err := commitV2Write(ctx, o.client, addV2Script, o.name, snap, outputs); err != nil {
		return 0, err
	}

	o.publishInvalidate(ctx)
	return 1, nil
}

func (o *v2Operations) tryRemoveV2(ctx context.Context, keyword string) (int, error) {
	snap, err := readTrieSnapshot(ctx, o.storage, o.name)
	if err != nil {
		return 0, err
	}

	outputs, changed := planRemove(snap, keyword)
	if !changed {
		return 0, nil
	}

	if _, err := commitV2Write(ctx, o.client, removeV2Script, o.name, snap, outputs); err != nil {
		return 0, err
	}

	o.publishInvalidate(ctx)
	return 1, nil
}
