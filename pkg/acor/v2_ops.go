package acor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-redis/redis/v8"

	"github.com/skyoo2003/acor/internal/pkg/utils"
)

// ErrConcurrencyConflict is returned when an optimistic lock conflict occurs
// during V2 schema write operations after exhausting all retry attempts.
// The caller should retry the operation or investigate concurrent modifications.
var ErrConcurrencyConflict = errors.New("concurrency conflict - please retry")

const maxRetries = 3

const luaKeys = 2

// mustJSON marshals v to JSON string. Panics on marshal failure.
// Note: json.Marshal rarely fails for the types used in this package
// (strings, slices, maps). Consider using toJSON with error handling
// for production use if type safety is a concern.
func mustJSON(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("json.Marshal failed: %v", err))
	}
	return string(b)
}

func (ac *AhoCorasick) findV2(text string) ([]string, error) {
	if text == "" {
		return []string{}, nil
	}

	prefixes, outputs, err := ac.getOrLoadCache()
	if err != nil {
		return nil, err
	}

	return ac.localFind(text, prefixes, outputs), nil
}

func (ac *AhoCorasick) localFind(text string, prefixes []string, outputs map[string][]string) []string {
	prefixSet := make(map[string]struct{})
	for _, p := range prefixes {
		prefixSet[p] = struct{}{}
	}

	matched := make([]string, 0)
	state := ""

	for _, char := range text {
		nextState := string(append([]rune(state), char))

		if _, exists := prefixSet[nextState]; !exists {
			nextState = ac.findFailState(nextState, prefixSet)
		}

		state = nextState
		if outs, exists := outputs[state]; exists {
			matched = append(matched, outs...)
		}
	}

	return matched
}

func (ac *AhoCorasick) findFailState(state string, prefixSet map[string]struct{}) string {
	runes := []rune(state)
	for i := 1; i < len(runes); i++ {
		suffix := string(runes[i:])
		if _, exists := prefixSet[suffix]; exists {
			return suffix
		}
	}
	return ""
}

func (ac *AhoCorasick) findIndexV2(text string) (map[string][]int, error) {
	if text == "" {
		return map[string][]int{}, nil
	}

	prefixes, outputs, err := ac.getOrLoadCache()
	if err != nil {
		return nil, err
	}

	return ac.localFindIndex(text, prefixes, outputs), nil
}

func (ac *AhoCorasick) localFindIndex(text string, prefixes []string, outputs map[string][]string) map[string][]int {
	prefixSet := make(map[string]struct{})
	for _, p := range prefixes {
		prefixSet[p] = struct{}{}
	}

	matched := make(map[string][]int)
	state := ""
	runeIndex := 0

	for _, char := range text {
		nextState := string(append([]rune(state), char))

		if _, exists := prefixSet[nextState]; !exists {
			nextState = ac.findFailState(nextState, prefixSet)
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

func (ac *AhoCorasick) infoV2() (*AhoCorasickInfo, error) {
	result, err := ac.redisClient.HGetAll(ac.ctx, trieKey(ac.name)).Result()
	if err != nil {
		return nil, err
	}

	var keywords []string
	if data, ok := result["keywords"]; ok {
		if err := json.Unmarshal([]byte(data), &keywords); err != nil {
			return nil, err
		}
	}

	var prefixes []string
	if data, ok := result["prefixes"]; ok {
		if err := json.Unmarshal([]byte(data), &prefixes); err != nil {
			return nil, err
		}
	}

	return &AhoCorasickInfo{
		Keywords: len(keywords),
		Nodes:    len(prefixes),
	}, nil
}

func (ac *AhoCorasick) suggestV2(input string) ([]string, error) {
	input = strings.ToLower(strings.TrimSpace(input))
	if input == "" {
		return []string{}, nil
	}

	result, err := ac.redisClient.HGetAll(ac.ctx, trieKey(ac.name)).Result()
	if err != nil {
		return nil, err
	}

	var keywords []string
	if data, ok := result["keywords"]; ok {
		if err := json.Unmarshal([]byte(data), &keywords); err != nil {
			return nil, err
		}
	}

	results := []string{}
	for _, kw := range keywords {
		if strings.HasPrefix(kw, input) {
			results = append(results, kw)
		}
	}

	return results, nil
}

func (ac *AhoCorasick) suggestIndexV2(input string) (map[string][]int, error) {
	results, err := ac.suggestV2(input)
	if err != nil {
		return nil, err
	}

	indexed := make(map[string][]int)
	for _, kw := range results {
		indexed[kw] = []int{0}
	}
	return indexed, nil
}

func (ac *AhoCorasick) addV2(keyword string) (int, error) {
	keyword = strings.ToLower(strings.TrimSpace(keyword))
	if keyword == "" {
		return 0, nil
	}

	for attempt := 0; attempt < maxRetries; attempt++ {
		added, err := ac.tryAddV2(keyword)
		if err == ErrConcurrencyConflict {
			time.Sleep(time.Duration(attempt+1) * 10 * time.Millisecond)
			continue
		}
		return added, err
	}
	return 0, ErrConcurrencyConflict
}

func (ac *AhoCorasick) tryAddV2(keyword string) (int, error) { //nolint:gocyclo,funlen // Complex optimistic locking with retry logic
	trieData, err := ac.redisClient.HGetAll(ac.ctx, trieKey(ac.name)).Result()
	if err != nil {
		return 0, err
	}

	var keywords []string
	if data, ok := trieData["keywords"]; ok {
		if unmarshalErr := json.Unmarshal([]byte(data), &keywords); unmarshalErr != nil {
			return 0, unmarshalErr
		}
	}
	keywordSet := make(map[string]struct{})
	for _, kw := range keywords {
		keywordSet[kw] = struct{}{}
	}
	if _, exists := keywordSet[keyword]; exists {
		return 0, nil
	}

	var prefixes []string
	if data, ok := trieData["prefixes"]; ok {
		if unmarshalErr := json.Unmarshal([]byte(data), &prefixes); unmarshalErr != nil {
			return 0, unmarshalErr
		}
	}
	prefixSet := make(map[string]struct{})
	for _, p := range prefixes {
		prefixSet[p] = struct{}{}
	}

	var oldVersion int64
	if v, ok := trieData["version"]; ok {
		if unmarshalErr := json.Unmarshal([]byte(v), &oldVersion); unmarshalErr != nil {
			oldVersion = 0
		}
	}

	outputsRaw, err := ac.redisClient.HGetAll(ac.ctx, outputsKey(ac.name)).Result()
	if err != nil {
		return 0, err
	}
	outputs := make(map[string][]string)
	for state, jsonArr := range outputsRaw {
		var arr []string
		if unmarshalErr := json.Unmarshal([]byte(jsonArr), &arr); unmarshalErr != nil {
			return 0, unmarshalErr
		}
		outputs[state] = arr
	}

	keywordRunes := []rune(keyword)
	newPrefixes := []string{}
	newSuffixes := []string{}

	for i := 0; i < len(keywordRunes); i++ {
		prefix := string(keywordRunes[:i+1])
		if _, exists := prefixSet[prefix]; !exists {
			newPrefixes = append(newPrefixes, prefix)
			newSuffixes = append(newSuffixes, utils.Reverse(prefix))
			prefixSet[prefix] = struct{}{}
		}
	}

	newOutputs := make(map[string][]string)
	keywordSet[keyword] = struct{}{}
	for _, prefix := range newPrefixes {
		prefixOutputs := ac.computeOutputsV2(prefix, prefixSet, keywordSet)
		if len(prefixOutputs) > 0 {
			newOutputs[prefix] = prefixOutputs
		}
	}

	for _, prefix := range prefixes {
		if prefix == "" {
			continue
		}
		updatedOutputs := ac.computeOutputsV2(prefix, prefixSet, keywordSet)
		if len(updatedOutputs) > 0 {
			newOutputs[prefix] = updatedOutputs
		}
	}

	keywords = append(keywords, keyword)
	prefixes = append(prefixes, newPrefixes...)

	var suffixes []string
	if data, ok := trieData["suffixes"]; ok {
		if unmarshalErr := json.Unmarshal([]byte(data), &suffixes); unmarshalErr != nil {
			return 0, unmarshalErr
		}
	}
	suffixes = append(suffixes, newSuffixes...)

	newVersion := time.Now().UnixNano()

	outputsToUpdate := make(map[string]string)
	for state, outs := range newOutputs {
		outputsToUpdate[state] = mustJSON(outs)
	}

	result, err := ac.addV2Script(ac.ctx, ac.redisClient, map[string]interface{}{
		"trieKey":    trieKey(ac.name),
		"outputsKey": outputsKey(ac.name),
		"keywords":   mustJSON(keywords),
		"prefixes":   mustJSON(prefixes),
		"suffixes":   mustJSON(suffixes),
		"newVersion": newVersion,
		"oldVersion": oldVersion,
		"outputs":    mustJSON(outputsToUpdate),
	}).Result()

	if err != nil {
		return 0, err
	}

	if result == 0 {
		return 0, ErrConcurrencyConflict
	}

	if ac.cache != nil {
		ac.cache.invalidate()
	}

	return 1, nil
}

func (ac *AhoCorasick) addV2Script(ctx context.Context, client redis.UniversalClient, args map[string]interface{}) *redis.IntCmd {
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

func (ac *AhoCorasick) computeOutputsV2(state string, prefixSet, keywordSet map[string]struct{}) []string {
	outputs := []string{}

	if _, isKeyword := keywordSet[state]; isKeyword {
		outputs = append(outputs, state)
	}

	stateRunes := []rune(state)
	for i := 1; i < len(stateRunes); i++ {
		failState := string(stateRunes[i:])
		if _, isPrefix := prefixSet[failState]; isPrefix {
			if _, isKeyword := keywordSet[failState]; isKeyword {
				outputs = append(outputs, failState)
			}
		}
	}

	return outputs
}

func (ac *AhoCorasick) removeV2(keyword string) (int, error) {
	keyword = strings.ToLower(strings.TrimSpace(keyword))
	if keyword == "" {
		return 0, nil
	}

	for attempt := 0; attempt < maxRetries; attempt++ {
		removed, err := ac.tryRemoveV2(keyword)
		if err == ErrConcurrencyConflict {
			time.Sleep(time.Duration(attempt+1) * 10 * time.Millisecond)
			continue
		}
		return removed, err
	}
	return 0, ErrConcurrencyConflict
}

func (ac *AhoCorasick) tryRemoveV2(keyword string) (int, error) { //nolint:gocyclo,funlen // Complex optimistic locking with retry logic
	trieData, err := ac.redisClient.HGetAll(ac.ctx, trieKey(ac.name)).Result()
	if err != nil {
		return 0, err
	}

	var keywords []string
	if data, ok := trieData["keywords"]; ok {
		if unmarshalErr := json.Unmarshal([]byte(data), &keywords); unmarshalErr != nil {
			return 0, unmarshalErr
		}
	}

	keywordExists := false
	newKeywords := make([]string, 0)
	if len(keywords) > 0 {
		newKeywords = make([]string, 0, len(keywords))
	}
	for _, kw := range keywords {
		if kw == keyword {
			keywordExists = true
		} else {
			newKeywords = append(newKeywords, kw)
		}
	}

	if !keywordExists {
		return len(keywords), nil
	}

	var prefixes []string
	if data, ok := trieData["prefixes"]; ok {
		if unmarshalErr := json.Unmarshal([]byte(data), &prefixes); unmarshalErr != nil {
			return 0, unmarshalErr
		}
	}

	var oldVersion int64
	if v, ok := trieData["version"]; ok {
		if unmarshalErr := json.Unmarshal([]byte(v), &oldVersion); unmarshalErr != nil {
			oldVersion = 0
		}
	}

	keywordSet := make(map[string]struct{})
	for _, kw := range newKeywords {
		keywordSet[kw] = struct{}{}
	}

	newPrefixes := []string{""}
	for _, prefix := range prefixes {
		if prefix == "" {
			continue
		}
		keep := false
		for kw := range keywordSet {
			if strings.HasPrefix(kw, prefix) {
				keep = true
				break
			}
		}
		if keep {
			newPrefixes = append(newPrefixes, prefix)
		}
	}

	prefixSet := make(map[string]struct{})
	for _, p := range newPrefixes {
		prefixSet[p] = struct{}{}
	}

	newSuffixes := make([]string, len(newPrefixes))
	for i, p := range newPrefixes {
		newSuffixes[i] = utils.Reverse(p)
	}

	newOutputs := make(map[string][]string)
	for _, prefix := range newPrefixes {
		if prefix == "" {
			continue
		}
		outs := ac.computeOutputsV2(prefix, prefixSet, keywordSet)
		if len(outs) > 0 {
			newOutputs[prefix] = outs
		}
	}

	newVersion := time.Now().UnixNano()

	outputsToSet := make(map[string]string)
	for state, outs := range newOutputs {
		outputsToSet[state] = mustJSON(outs)
	}

	result, err := ac.removeV2Script(ac.ctx, ac.redisClient, map[string]interface{}{
		"trieKey":    trieKey(ac.name),
		"outputsKey": outputsKey(ac.name),
		"keywords":   mustJSON(newKeywords),
		"prefixes":   mustJSON(newPrefixes),
		"suffixes":   mustJSON(newSuffixes),
		"newVersion": newVersion,
		"oldVersion": oldVersion,
		"outputs":    mustJSON(outputsToSet),
	}).Result()

	if err != nil {
		return 0, err
	}

	if result == 0 {
		return 0, ErrConcurrencyConflict
	}

	if ac.cache != nil {
		ac.cache.invalidate()
	}

	return len(newKeywords), nil
}

func (ac *AhoCorasick) removeV2Script(ctx context.Context, client redis.UniversalClient, args map[string]interface{}) *redis.IntCmd {
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

func (ac *AhoCorasick) flushV2() error {
	_, err := ac.redisClient.Del(ac.ctx, trieKey(ac.name), outputsKey(ac.name), nodesKey(ac.name)).Result()
	if err != nil {
		return err
	}

	_, err = ac.redisClient.HSet(ac.ctx, trieKey(ac.name), map[string]interface{}{
		"keywords": "[]",
		"prefixes": "[\"\"]",
		"suffixes": "[\"\"]",
		"version":  time.Now().UnixNano(),
	}).Result()
	if err != nil {
		return err
	}

	if ac.cache != nil {
		ac.cache.invalidate()
	}

	return nil
}
