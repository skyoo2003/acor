// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"

	matchengine "github.com/skyoo2003/acor/internal/engine"
)

// defaultSelfInvalidationCleanupInterval controls how often cleanupExpiredSelfInvalidations
// runs relative to publishInvalidate calls. Every N publishes triggers one O(n) sweep.
const defaultSelfInvalidationCleanupInterval = 128

const maxRetries = 3

const retryBackoffBase = 10 * time.Millisecond

const invalidateIDBytes = 8

// Compile-time check that v2Operations satisfies the operations interface.
var _ operations = (*v2Operations)(nil)

// v2Operations implements the operations interface for the V2 schema.
// It holds all dependencies needed for V2 Aho-Corasick operations without
// depending directly on the AhoCorasick struct.
type v2Operations struct {
	storage                         KVStorage
	client                          redis.UniversalClient
	name                            string
	cache                           *trieCache
	logger                          Logger
	selfInvalidationPublishCount    uint64
	selfInvalidationCleanupInterval uint64
	caseSensitive                   bool
}

// --- operations interface methods ---

func (o *v2Operations) find(ctx context.Context, text string) ([]string, error) {
	if text == "" {
		return []string{}, nil
	}

	if !o.caseSensitive {
		text = strings.ToLower(text)
	}

	engine, err := o.loadEngine(ctx)
	if err != nil {
		return nil, err
	}

	// Honor a canceled ctx at the match boundary; the in-memory scan itself isn't ctx-threaded.
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	matched := engine.Find(text)
	if matched == nil {
		matched = []string{}
	}
	return matched, nil
}

func (o *v2Operations) findIndex(ctx context.Context, text string) (map[string][]int, error) {
	if text == "" {
		return map[string][]int{}, nil
	}

	if !o.caseSensitive {
		text = strings.ToLower(text)
	}

	engine, err := o.loadEngine(ctx)
	if err != nil {
		return nil, err
	}

	// See find: honor an already-canceled/expired ctx before the in-memory match.
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	matched := engine.FindIndex(text)
	if matched == nil {
		matched = map[string][]int{}
	}
	return matched, nil
}

func (o *v2Operations) add(ctx context.Context, keyword string) (int, error) {
	keyword = strings.TrimSpace(keyword)
	if !o.caseSensitive {
		keyword = strings.ToLower(keyword)
	}
	if keyword == "" {
		return 0, nil
	}

	for attempt := 0; attempt < maxRetries; attempt++ {
		added, err := o.tryAddV2(ctx, keyword)
		if errors.Is(err, ErrConcurrencyConflict) {
			backoff := time.Duration(attempt+1) * retryBackoffBase
			select {
			case <-ctx.Done():
				return 0, ctx.Err()
			case <-time.After(backoff):
			}
			continue
		}
		return added, err
	}
	return 0, ErrConcurrencyConflict
}

func (o *v2Operations) remove(ctx context.Context, keyword string) (int, error) {
	keyword = strings.TrimSpace(keyword)
	if !o.caseSensitive {
		keyword = strings.ToLower(keyword)
	}
	if keyword == "" {
		return 0, nil
	}

	for attempt := 0; attempt < maxRetries; attempt++ {
		removed, err := o.tryRemoveV2(ctx, keyword)
		if errors.Is(err, ErrConcurrencyConflict) {
			backoff := time.Duration(attempt+1) * retryBackoffBase
			select {
			case <-ctx.Done():
				return 0, ctx.Err()
			case <-time.After(backoff):
			}
			continue
		}
		return removed, err
	}
	return 0, ErrConcurrencyConflict
}

func (o *v2Operations) flush(ctx context.Context) error {
	err := o.storage.TxPipelined(ctx, func(pipe Pipeliner) error {
		tKey := trieKey(o.name)
		oKey := outputsKey(o.name)
		// nodesKey is only written during migration; including it here ensures a clean state.
		nKey := nodesKey(o.name)

		// Delete outputs and nodes keys; overwrite trie key with HSET below
		// instead of DEL+HSET to avoid an unnecessary round-trip in the pipeline.
		if err := pipe.Del(ctx, oKey, nKey); err != nil {
			return err
		}
		if err := pipe.HSet(ctx, tKey, emptyTrieFields()); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return newRedisError("TXPIPELINED", trieKey(o.name), err)
	}

	o.publishInvalidate(ctx)

	return nil
}

func (o *v2Operations) info(ctx context.Context) (*AhoCorasickInfo, error) {
	result, err := o.storage.HGetAll(ctx, trieKey(o.name))
	if err != nil {
		return nil, newRedisError("HGETALL", trieKey(o.name), err)
	}

	var keywords []string
	if data, ok := result[fieldKeywords]; ok {
		if err := json.Unmarshal([]byte(data), &keywords); err != nil {
			return nil, newOperationError("unmarshal", SchemaV2, err)
		}
	}

	var prefixes []string
	if data, ok := result[fieldPrefixes]; ok {
		if err := json.Unmarshal([]byte(data), &prefixes); err != nil {
			return nil, newOperationError("unmarshal", SchemaV2, err)
		}
	}

	return &AhoCorasickInfo{
		Keywords: len(keywords),
		Nodes:    len(prefixes),
	}, nil
}

func (o *v2Operations) suggest(ctx context.Context, input string) ([]string, error) {
	input = strings.TrimSpace(input)
	if !o.caseSensitive {
		input = strings.ToLower(input)
	}
	if input == "" {
		return []string{}, nil
	}

	result, err := o.storage.HGetAll(ctx, trieKey(o.name))
	if err != nil {
		return nil, newRedisError("HGETALL", trieKey(o.name), err)
	}

	var keywords []string
	if data, ok := result[fieldKeywords]; ok {
		if err := json.Unmarshal([]byte(data), &keywords); err != nil {
			return nil, newOperationError("unmarshal", SchemaV2, err)
		}
	}

	results := make([]string, 0, len(keywords))
	for _, kw := range keywords {
		if strings.HasPrefix(kw, input) {
			results = append(results, kw)
		}
	}

	return results, nil
}

func (o *v2Operations) suggestIndex(ctx context.Context, input string) (map[string][]int, error) {
	results, err := o.suggest(ctx, input)
	if err != nil {
		return nil, err
	}

	indexed := make(map[string][]int, len(results))
	for _, kw := range results {
		indexed[kw] = []int{0}
	}
	return indexed, nil
}

// --- cache helpers ---

// fetchTrieData loads trie prefixes and outputs from storage using a pipeline.
func (o *v2Operations) fetchTrieData(ctx context.Context) (prefixes []string, outputs map[string][]string, err error) {
	pipe := o.storage.Pipeline()
	trieResult := pipe.HGetAll(ctx, trieKey(o.name))
	outputsResult := pipe.HGetAll(ctx, outputsKey(o.name))
	if err := pipe.Exec(ctx); err != nil {
		return nil, nil, newRedisError("PIPELINE", trieKey(o.name), err)
	}

	trieData := trieResult.Val()
	if data, ok := trieData[fieldPrefixes]; ok {
		if unmarshalErr := json.Unmarshal([]byte(data), &prefixes); unmarshalErr != nil {
			return nil, nil, newOperationError("unmarshal", SchemaV2, unmarshalErr)
		}
	}

	outputsRaw := outputsResult.Val()
	outputs = make(map[string][]string)
	for state, jsonArr := range outputsRaw {
		var arr []string
		if unmarshalErr := json.Unmarshal([]byte(jsonArr), &arr); unmarshalErr != nil {
			return nil, nil, newOperationError("unmarshal", SchemaV2, unmarshalErr)
		}
		outputs[state] = arr
	}

	return prefixes, outputs, nil
}

// loadCache fetches trie data and populates the cache.
func (o *v2Operations) loadCache(ctx context.Context) error {
	_, outputs, err := o.fetchTrieData(ctx)
	if err != nil {
		return err
	}
	o.cache.set(outputs)
	return nil
}

// loadEngine returns the locally cached Aho-Corasick match engine, loading it
// from storage on a cache miss. The engine is built once per cache load (see
// trieCache.set) and reused across Find calls with no Redis I/O.
//
// When caching is disabled (cache == nil) it fetches the trie from Redis and
// builds a throwaway engine per call. Redis I/O dominates that path, so the
// extra automaton build is negligible; enabling the cache is the way to avoid
// both, not per-call engine caching.
func (o *v2Operations) loadEngine(ctx context.Context) (*matchengine.Engine, error) {
	if o.cache == nil {
		_, outputs, err := o.fetchTrieData(ctx)
		if err != nil {
			return nil, err
		}
		return buildEngineFromOutputs(outputs), nil
	}

	if engine, valid := o.cache.getEngine(); valid {
		return engine, nil
	}

	o.cache.loadMu.Lock()
	defer o.cache.loadMu.Unlock()

	// Double-check after acquiring lock.
	if engine, valid := o.cache.getEngine(); valid {
		return engine, nil
	}

	if err := o.loadCache(ctx); err != nil {
		return nil, err
	}

	engine, _ := o.cache.getEngine()
	return engine, nil
}

// --- publishInvalidate ---

// publishInvalidate invalidates the local cache and publishes an invalidation
// message so other instances refresh their caches. Each publish includes a
// unique ID to avoid a leakable counter when skipping self-messages.
func (o *v2Operations) publishInvalidate(ctx context.Context) {
	channel := invalidateChannelPrefix + o.name
	b := make([]byte, invalidateIDBytes)
	if _, err := rand.Read(b); err != nil && o.logger != nil {
		o.logger.Printf("failed to generate random invalidation ID, using zero bytes: %v", err)
	}
	msgID := fmt.Sprintf("%d:%x", time.Now().UnixNano(), b)
	payload := o.name + ":" + msgID

	if o.cache != nil {
		skipSelfSet(o.cache, msgID)
		interval := o.selfInvalidationCleanupInterval
		if interval == 0 {
			interval = defaultSelfInvalidationCleanupInterval
		}
		if atomic.AddUint64(&o.selfInvalidationPublishCount, 1)%interval == 0 {
			cleanupExpiredSelfInvalidations(o.cache)
		}
	}
	err := o.storage.Publish(ctx, channel, payload)
	if err != nil && o.cache != nil {
		skipSelfClear(o.cache, msgID)
	}
	if err != nil && o.logger != nil {
		o.logger.Printf("failed to publish cache invalidation: channel=%s error=%v", channel, err)
	}
	if o.cache != nil {
		o.cache.invalidate()
	}
}
