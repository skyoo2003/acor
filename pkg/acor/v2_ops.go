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

	"github.com/go-redis/redis/v8"
)

// defaultSelfInvalidationCleanupInterval controls how often cleanupExpiredSelfInvalidations
// runs relative to publishInvalidate calls. Every N publishes triggers one O(n) sweep.
const defaultSelfInvalidationCleanupInterval = 128

const maxRetries = 3

const retryBackoffBase = 10 * time.Millisecond

const luaKeys = 2

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

	_, prefixSet, outputs, err := o.getOrLoadCache(ctx)
	if err != nil {
		return nil, err
	}

	matched, err := o.localFind(ctx, text, prefixSet, outputs)
	return matched, err
}

func (o *v2Operations) findIndex(ctx context.Context, text string) (map[string][]int, error) {
	if text == "" {
		return map[string][]int{}, nil
	}

	if !o.caseSensitive {
		text = strings.ToLower(text)
	}

	_, prefixSet, outputs, err := o.getOrLoadCache(ctx)
	if err != nil {
		return nil, err
	}

	matched, err := o.localFindIndex(ctx, text, prefixSet, outputs)
	return matched, err
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

		if err := pipe.Del(ctx, tKey, oKey, nKey); err != nil {
			return err
		}
		if err := pipe.HSet(ctx, tKey, map[string]interface{}{
			"keywords": "[]",
			"prefixes": "[\"\"]",
			"suffixes": "[\"\"]",
			"version":  time.Now().UnixNano(),
		}); err != nil {
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
	if data, ok := result["keywords"]; ok {
		if err := json.Unmarshal([]byte(data), &keywords); err != nil {
			return nil, newOperationError("unmarshal", SchemaV2, err)
		}
	}

	var prefixes []string
	if data, ok := result["prefixes"]; ok {
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
	if data, ok := result["keywords"]; ok {
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

	indexed := make(map[string][]int)
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
	if data, ok := trieData["prefixes"]; ok {
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
	prefixes, outputs, err := o.fetchTrieData(ctx)
	if err != nil {
		return err
	}
	o.cache.set(prefixes, outputs)
	return nil
}

// getOrLoadCache returns cached trie data if valid, otherwise loads from storage.
func (o *v2Operations) getOrLoadCache(ctx context.Context) (prefixes []string, prefixSet map[string]struct{}, outputs map[string][]string, err error) {
	if o.cache == nil {
		prefixes, outputs, err = o.fetchTrieData(ctx)
		if err != nil {
			return nil, nil, nil, err
		}
		prefixSet = make(map[string]struct{}, len(prefixes))
		for _, p := range prefixes {
			prefixSet[p] = struct{}{}
		}
		return prefixes, prefixSet, outputs, nil
	}

	var valid bool
	prefixes, outputs, valid = o.cache.get()
	if valid {
		return prefixes, o.cache.getPrefixSet(), outputs, nil
	}

	o.cache.loadMu.Lock()
	defer o.cache.loadMu.Unlock()

	// Double-check after acquiring lock
	prefixes, outputs, valid = o.cache.get()
	if valid {
		return prefixes, o.cache.getPrefixSet(), outputs, nil
	}

	if err := o.loadCache(ctx); err != nil {
		return nil, nil, nil, err
	}

	prefixes, outputs, _ = o.cache.get()
	return prefixes, o.cache.getPrefixSet(), outputs, nil
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

// --- local computation helpers (pure functions on v2Operations receiver) ---
//
// Context cancellation is checked every contextCheckInterval characters to balance
// responsiveness with overhead. For texts shorter than this interval, cancellation
// is only observed after the scan completes.
const contextCheckInterval = 1000

func (o *v2Operations) localFind(ctx context.Context, text string, prefixSet map[string]struct{}, outputs map[string][]string) ([]string, error) {
	matched := make([]string, 0)
	state := ""

	for i, char := range text {
		if i%contextCheckInterval == 0 {
			select {
			case <-ctx.Done():
				return matched, ctx.Err()
			default:
			}
		}
		nextState := state + string(char)

		if _, exists := prefixSet[nextState]; !exists {
			nextState = o.findFailState(nextState, prefixSet)
		}

		state = nextState
		if outs, exists := outputs[state]; exists {
			matched = append(matched, outs...)
		}
	}

	return matched, nil
}

func (o *v2Operations) localFindIndex(ctx context.Context, text string, prefixSet map[string]struct{}, outputs map[string][]string) (map[string][]int, error) {
	matched := make(map[string][]int)
	state := ""
	runeIndex := 0

	for i, char := range text {
		if i%contextCheckInterval == 0 {
			select {
			case <-ctx.Done():
				return matched, ctx.Err()
			default:
			}
		}
		nextState := state + string(char)

		if _, exists := prefixSet[nextState]; !exists {
			nextState = o.findFailState(nextState, prefixSet)
		}

		state = nextState
		runeIndex++
		if outs, exists := outputs[state]; exists {
			for _, out := range outs {
				startIdx := runeIndex - len([]rune(out))
				matched[out] = append(matched[out], startIdx)
			}
		}
	}

	return matched, nil
}

// findFailState finds the longest proper suffix of state that exists in prefixSet.
// This recomputes the failure function on-the-fly rather than precomputing failure links,
// which trades O(N*M) time per character (where N = text length, M = max prefix length)
// for zero setup cost. This is optimal for V2 where the trie is cached locally and
// scans are infrequent relative to writes.
func (o *v2Operations) findFailState(state string, prefixSet map[string]struct{}) string {
	runes := []rune(state)
	for i := 1; i < len(runes); i++ {
		suffix := string(runes[i:])
		if _, exists := prefixSet[suffix]; exists {
			return suffix
		}
	}
	return ""
}
