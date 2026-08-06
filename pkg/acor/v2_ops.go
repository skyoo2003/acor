// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	matchengine "github.com/skyoo2003/acor/internal/engine"
)

const maxRetries = 3

const retryBackoffBase = 10 * time.Millisecond

// Compile-time check that v2Operations satisfies the operations interface.
var _ operations = (*v2Operations)(nil)

// v2Operations implements the operations interface for the V2 schema.
// It holds all dependencies needed for V2 Aho-Corasick operations without
// depending directly on the AhoCorasick struct.
type v2Operations struct {
	storage       KVStorage
	client        redis.UniversalClient
	name          string
	cache         *trieCache
	logger        Logger
	caseSensitive bool
	engines       engineMemo
	stats         *cacheStats
}

// --- operations interface methods ---

func (o *v2Operations) find(ctx context.Context, text string) ([]string, error) {
	if text == "" {
		return []string{}, nil
	}

	text = normalizeText(text, o.caseSensitive)

	engine, err := o.loadEngine(ctx)
	if err != nil {
		return nil, err
	}

	// Honor a canceled ctx at the match boundary; the in-memory scan itself isn't ctx-threaded.
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return engine.Find(text), nil
}

func (o *v2Operations) findIndex(ctx context.Context, text string) (map[string][]int, error) {
	if text == "" {
		return map[string][]int{}, nil
	}

	text = normalizeText(text, o.caseSensitive)

	engine, err := o.loadEngine(ctx)
	if err != nil {
		return nil, err
	}

	// See find: honor an already-canceled/expired ctx before the in-memory match.
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return engine.FindIndex(text), nil
}

func (o *v2Operations) add(ctx context.Context, keyword string) (int, error) {
	keyword = normalizeKeyword(keyword, o.caseSensitive)
	if keyword == "" {
		return 0, nil
	}
	return retryOnConflict(ctx, func() (int, error) { return o.tryAddV2(ctx, keyword) })
}

func (o *v2Operations) remove(ctx context.Context, keyword string) (int, error) {
	keyword = normalizeKeyword(keyword, o.caseSensitive)
	if keyword == "" {
		return 0, nil
	}
	return retryOnConflict(ctx, func() (int, error) { return o.tryRemoveV2(ctx, keyword) })
}

func (o *v2Operations) flush(ctx context.Context) error {
	if err := flushV2Keys(ctx, o.storage, o.name); err != nil {
		return err
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

	parsed, parseErr := parseOutputs(outputsResult.Val())
	if parseErr != nil {
		return nil, nil, parseErr
	}
	outputs = parsed

	return prefixes, outputs, nil
}

// parseOutputs unmarshals the per-state JSON arrays of the V2 outputs hash.
func parseOutputs(raw map[string]string) (map[string][]string, error) {
	outputs := make(map[string][]string, len(raw))
	for state, jsonArr := range raw {
		var arr []string
		if err := json.Unmarshal([]byte(jsonArr), &arr); err != nil {
			return nil, newOperationError("unmarshal", SchemaV2, err)
		}
		outputs[state] = arr
	}
	return outputs, nil
}

// fetchRawOutputs reads just the outputs hash, unparsed.
//
// The engine is built from the union of the outputs values alone, so the trie
// hash that fetchTrieData also pipelines is dead weight on the read path. This
// stays one round trip while transferring and parsing less.
func (o *v2Operations) fetchRawOutputs(ctx context.Context) (map[string]string, error) {
	raw, err := o.storage.HGetAll(ctx, outputsKey(o.name))
	if err != nil {
		return nil, newRedisError("HGETALL", outputsKey(o.name), err)
	}
	return raw, nil
}

// loadCache fetches trie data and populates the cache.
func (o *v2Operations) loadCache(ctx context.Context) error {
	_, outputs, err := o.fetchTrieData(ctx)
	if err != nil {
		return err
	}
	// Timed around set alone, which is where the automaton is built. fetchTrieData
	// above is Redis I/O, and folding it in would report the network as build time.
	start := time.Now()
	o.cache.set(outputs)
	o.stats.recordRebuild(time.Since(start))
	return nil
}

// loadEngine returns the locally cached Aho-Corasick match engine, loading it
// from storage on a cache miss. The engine is built once per cache load (see
// trieCache.set) and reused across Find calls with no Redis I/O.
//
// When caching is disabled (cache == nil) it still reads Redis on every call,
// because nothing else would notice a peer's write, but memoizes the parse and
// build behind that read. The assumption that Redis I/O dominated so heavily
// that the rebuild did not matter turned out to be wrong: rebuilding per call
// made uncached Find slower than V1, which had memoized its engine all along.
// EnableCache is still the way to avoid the read itself.
func (o *v2Operations) loadEngine(ctx context.Context) (*matchengine.Engine, error) {
	if o.cache == nil {
		// Without EnableCache there is no invalidation listener, so the read
		// still happens on every call to stay fresh — a peer's write must not
		// go unnoticed. Everything after the read is memoized on the raw
		// payload: repeating the unmarshal and automaton build over identical
		// bytes is what made uncached V2 Find slower than V1, which memoizes
		// its own engine the same way.
		raw, err := o.fetchRawOutputs(ctx)
		if err != nil {
			return nil, err
		}
		return o.engines.engineFor(digestRawOutputs(raw), func() (*matchengine.Engine, error) {
			outputs, parseErr := parseOutputs(raw)
			if parseErr != nil {
				return nil, parseErr
			}
			return buildEngineFromOutputs(outputs), nil
		})
	}

	if engine, valid := o.cache.getEngine(); valid {
		o.stats.hit()
		return engine, nil
	}

	// Counted before the lock for the same reason redisBackedAC.ensureValid counts
	// before its singleflight: this read found the cache invalid and waits for a
	// rebuild whether or not it performs one. Coalesced readers therefore stay misses
	// here too, which is what makes Misses-Rebuilds the work the coalescing saved.
	o.stats.miss()

	o.cache.loadMu.Lock()
	defer o.cache.loadMu.Unlock()

	// Double-check after acquiring lock: another goroutine loaded while this one
	// waited, so no fetch of its own is needed — but it still waited, so it was
	// counted a miss above.
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
	msgID := newInvalidationID()

	if o.cache != nil {
		o.cache.selfSkip.add(msgID)
	}

	err := o.storage.Publish(ctx, channel, invalidationPayload(o.name, msgID))
	if err != nil {
		if o.cache != nil {
			o.cache.selfSkip.forget(msgID)
		}
		if o.logger != nil {
			o.logger.Printf("failed to publish cache invalidation: channel=%s error=%v", channel, err)
		}
	}
	if o.cache != nil {
		o.cache.invalidate()
	}
}
