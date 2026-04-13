package acor

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-redis/redis/v8"
)

// Compile-time check that v2Operations satisfies the operations interface.
var _ operations = (*v2Operations)(nil)

// v2Operations implements the operations interface for the V2 schema.
// It holds all dependencies needed for V2 Aho-Corasick operations without
// depending directly on the AhoCorasick struct.
type v2Operations struct {
	storage KVStorage             // for all standard Redis operations
	client  redis.UniversalClient // ONLY for Lua script execution (addV2Script/removeV2Script)
	name    string
	ctx     context.Context
	cache   *trieCache
	logger  Logger
}

// --- operations interface methods ---

func (o *v2Operations) find(ctx context.Context, text string) ([]string, error) {
	if text == "" {
		return []string{}, nil
	}

	prefixes, outputs, err := o.getOrLoadCache(ctx)
	if err != nil {
		return nil, err
	}

	return o.localFind(ctx, text, prefixes, outputs), nil
}

func (o *v2Operations) findIndex(ctx context.Context, text string) (map[string][]int, error) {
	if text == "" {
		return map[string][]int{}, nil
	}

	prefixes, outputs, err := o.getOrLoadCache(ctx)
	if err != nil {
		return nil, err
	}

	return o.localFindIndex(ctx, text, prefixes, outputs), nil
}

func (o *v2Operations) add(ctx context.Context, keyword string) (int, error) {
	keyword = strings.ToLower(strings.TrimSpace(keyword))
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
	keyword = strings.ToLower(strings.TrimSpace(keyword))
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
	err := o.storage.Del(ctx, trieKey(o.name), outputsKey(o.name), nodesKey(o.name))
	if err != nil {
		return newRedisError("DEL", trieKey(o.name), err)
	}

	err = o.storage.HSet(ctx, trieKey(o.name), map[string]interface{}{
		"keywords": "[]",
		"prefixes": "[\"\"]",
		"suffixes": "[\"\"]",
		"version":  time.Now().UnixNano(),
	})
	if err != nil {
		return newRedisError("HSET", trieKey(o.name), err)
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
	input = strings.ToLower(strings.TrimSpace(input))
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

	var results []string
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
func (o *v2Operations) getOrLoadCache(ctx context.Context) (prefixes []string, outputs map[string][]string, err error) {
	if o.cache == nil {
		return o.fetchTrieData(ctx)
	}

	var valid bool
	prefixes, outputs, valid = o.cache.get()
	if valid {
		return
	}

	o.cache.loadMu.Lock()
	defer o.cache.loadMu.Unlock()

	// Double-check after acquiring lock
	prefixes, outputs, valid = o.cache.get()
	if valid {
		return
	}

	if err = o.loadCache(ctx); err != nil {
		return
	}

	prefixes, outputs, _ = o.cache.get()
	return
}

// --- publishInvalidate ---

// publishInvalidate invalidates the local cache and publishes an invalidation
// message so other instances refresh their caches. Each publish includes a
// unique ID to avoid a leakable counter when skipping self-messages.
func (o *v2Operations) publishInvalidate(ctx context.Context) {
	channel := invalidateChannelPrefix + o.name
	b := make([]byte, invalidateIDBytes)
	_, _ = rand.Read(b)
	msgID := fmt.Sprintf("%d:%x", time.Now().UnixNano(), b)
	payload := o.name + ":" + msgID

	if o.cache != nil {
		skipSelfSet(o.cache, msgID)
		cleanupExpiredSelfInvalidations(o.cache)
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

func (o *v2Operations) localFind(ctx context.Context, text string, prefixes []string, outputs map[string][]string) []string {
	prefixSet := make(map[string]struct{})
	for _, p := range prefixes {
		prefixSet[p] = struct{}{}
	}

	matched := make([]string, 0)
	state := ""

	for i, char := range text {
		if i%1000 == 0 {
			select {
			case <-ctx.Done():
				return matched
			default:
			}
		}
		nextState := string(append([]rune(state), char))

		if _, exists := prefixSet[nextState]; !exists {
			nextState = o.findFailState(nextState, prefixSet)
		}

		state = nextState
		if outs, exists := outputs[state]; exists {
			matched = append(matched, outs...)
		}
	}

	return matched
}

func (o *v2Operations) localFindIndex(ctx context.Context, text string, prefixes []string, outputs map[string][]string) map[string][]int {
	prefixSet := make(map[string]struct{})
	for _, p := range prefixes {
		prefixSet[p] = struct{}{}
	}

	matched := make(map[string][]int)
	state := ""
	runeIndex := 0

	for i, char := range text {
		if i%1000 == 0 {
			select {
			case <-ctx.Done():
				return matched
			default:
			}
		}
		nextState := string(append([]rune(state), char))

		if _, exists := prefixSet[nextState]; !exists {
			nextState = o.findFailState(nextState, prefixSet)
		}

		state = nextState
		if outs, exists := outputs[state]; exists {
			for _, out := range outs {
				startIdx := runeIndex - len([]rune(out)) + 1
				matched[out] = append(matched[out], startIdx)
			}
		}
		runeIndex++
	}

	return matched
}

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

// --- Lua script helpers ---

func (o *v2Operations) addV2Script(ctx context.Context, client redis.UniversalClient, args map[string]interface{}) *redis.IntCmd {
	cmd := redis.NewIntCmd(ctx, "eval", `
		local trieKey = KEYS[1]
		local outputsKey = KEYS[2]
		local oldVersion = tonumber(ARGV[1])
		local newVersion = tonumber(ARGV[2])
		local keywords = ARGV[3]
		local prefixes = ARGV[4]
		local suffixes = ARGV[5]
		local outputsJson = ARGV[6]

		local currentVersion = redis.call('HGET', trieKey, 'version')
		if currentVersion and tonumber(currentVersion) ~= oldVersion then
			return 0
		end

		redis.call('HSET', trieKey, 'keywords', keywords, 'prefixes', prefixes, 'suffixes', suffixes, 'version', newVersion)

		local outputs = cjson.decode(outputsJson)
		for state, jsonOuts in pairs(outputs) do
			redis.call('HSET', outputsKey, state, jsonOuts)
		end

		return 1
	`, luaKeys, args["trieKey"], args["outputsKey"],
		args["oldVersion"], args["newVersion"], args["keywords"],
		args["prefixes"], args["suffixes"], args["outputs"])
	_ = client.Process(ctx, cmd)
	return cmd
}

func (o *v2Operations) removeV2Script(ctx context.Context, client redis.UniversalClient, args map[string]interface{}) *redis.IntCmd {
	cmd := redis.NewIntCmd(ctx, "eval", `
		local trieKey = KEYS[1]
		local outputsKey = KEYS[2]
		local oldVersion = tonumber(ARGV[1])
		local newVersion = tonumber(ARGV[2])
		local keywords = ARGV[3]
		local prefixes = ARGV[4]
		local suffixes = ARGV[5]
		local outputsJson = ARGV[6]

		local currentVersion = redis.call('HGET', trieKey, 'version')
		if currentVersion and tonumber(currentVersion) ~= oldVersion then
			return 0
		end

		redis.call('HSET', trieKey, 'keywords', keywords, 'prefixes', prefixes, 'suffixes', suffixes, 'version', newVersion)

		redis.call('DEL', outputsKey)

		local outputs = cjson.decode(outputsJson)
		for state, jsonOuts in pairs(outputs) do
			redis.call('HSET', outputsKey, state, jsonOuts)
		end

		return 1
	`, luaKeys, args["trieKey"], args["outputsKey"],
		args["oldVersion"], args["newVersion"], args["keywords"],
		args["prefixes"], args["suffixes"], args["outputs"])
	_ = client.Process(ctx, cmd)
	return cmd
}
