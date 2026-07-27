// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	mrand "math/rand/v2"
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
//
// The V2 trie hash also carried a "suffixes" field until v0.11: every prefix
// reversed, written on each update and read back, but never consulted by any
// matcher (only the V1 schema's suffix ZSET is used, for its own rebuild walk).
// It is no longer written; a leftover field on an existing collection is
// ignored and disappears on the next Flush.
type trieSnapshot struct {
	Keywords []string
	Prefixes []string
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
	if v, ok := trieData[fieldVersion]; ok {
		if err := json.Unmarshal([]byte(v), &snap.Version); err != nil {
			snap.Version = 0
		}
	}

	return snap, nil
}

// marshalTrieArgs serializes a snapshot and its output states into the complete
// script arguments for collection name: nothing is left for the caller to patch
// in afterwards.
func marshalTrieArgs(name string, snap *trieSnapshot, outputs map[string]string,
	newVersion int64, clearOutputs bool) (*v2ScriptArgs, error) {
	args := &v2ScriptArgs{
		TrieKey:      trieKey(name),
		OutputsKey:   outputsKey(name),
		OldVersion:   snap.Version,
		NewVersion:   newVersion,
		ClearOutputs: clearOutputs,
	}
	var err error
	if args.Keywords, err = toJSON(snap.Keywords); err != nil {
		return nil, newOperationError("marshal", SchemaV2, err)
	}
	if args.Prefixes, err = toJSON(snap.Prefixes); err != nil {
		return nil, newOperationError("marshal", SchemaV2, err)
	}
	if args.Outputs, err = toJSON(outputs); err != nil {
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
func commitV2Write(ctx context.Context, client redis.UniversalClient, name string,
	snap *trieSnapshot, outputs map[string][]string, clearOutputs bool) (int64, error) {
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

	args, err := marshalTrieArgs(name, snap, encoded, newVersion, clearOutputs)
	if err != nil {
		return 0, err
	}

	result, err := runV2Script(ctx, client, args)
	if err != nil {
		return 0, newRedisError("EVAL", trieKey(name), err)
	}
	if result == 0 {
		return 0, ErrConcurrencyConflict
	}
	return newVersion, nil
}

// flushV2Keys resets a collection's V2 keys to empty: the outputs and nodes
// hashes are dropped and the trie hash is replaced with emptyTrieFields.
//
// The trie key is deleted rather than only overwritten, so fields no longer
// written by this version (the pre-v0.11 "suffixes") don't survive a flush. The
// one thing that costs is the key's TTL, which acor never sets itself; a caller
// that expires collections externally has to re-apply it after Flush.
//
// Shared by both V2 write paths, which differ only in the local state they reset
// afterwards.
func flushV2Keys(ctx context.Context, storage KVStorage, name string) error {
	tKey := trieKey(name)
	err := storage.TxPipelined(ctx, func(pipe Pipeliner) error {
		// nodesKey is only written during migration; including it here ensures a clean state.
		if err := pipe.Del(ctx, outputsKey(name), nodesKey(name), tKey); err != nil {
			return err
		}
		return pipe.HSet(ctx, tKey, emptyTrieFields())
	})
	if err != nil {
		return newRedisError("TXPIPELINED", tKey, err)
	}
	return nil
}

// retryOnConflict runs attempt until it stops reporting a lost optimistic-lock
// race, backing off in between. Shared by both V2 write paths.
func retryOnConflict(ctx context.Context, attempt func() (int, error)) (int, error) {
	for i := 0; i < maxRetries; i++ {
		n, err := attempt()
		if !errors.Is(err, ErrConcurrencyConflict) {
			return n, err
		}
		// Nothing left to wait for after the last attempt.
		if i == maxRetries-1 {
			break
		}
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(conflictBackoff(i)):
		}
	}
	return 0, ErrConcurrencyConflict
}

// conflictBackoff returns how long to wait before retry attempt+1: a linear
// ramp plus jitter in [0, retryBackoffBase).
//
// The jitter is what keeps contention from persisting. Two writers that lose
// the same CAS race lose it at the same instant, so a purely deterministic
// schedule wakes them together and they collide again on every retry. Spreading
// the wakeups over one base interval breaks that lockstep.
//
// math/rand is the right tool here: this only spreads load, and no security
// property depends on the value. Version stamps and invalidation IDs, which do
// need unpredictability, keep using crypto/rand.
func conflictBackoff(attempt int) time.Duration {
	jitter := mrand.N(retryBackoffBase) //nolint:gosec // G404: load spreading, not a security decision.
	return time.Duration(attempt+1)*retryBackoffBase + jitter
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

	if _, err := commitV2Write(ctx, o.client, o.name, snap, outputs, false); err != nil {
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

	if _, err := commitV2Write(ctx, o.client, o.name, snap, outputs, true); err != nil {
		return 0, err
	}

	o.publishInvalidate(ctx)
	return 1, nil
}
