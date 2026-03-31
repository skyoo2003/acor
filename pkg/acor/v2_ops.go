package acor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/skyoo2003/acor/internal/pkg/utils"
)

var ErrConcurrencyConflict = errors.New("concurrency conflict - please retry")

const maxRetries = 3

const retryBackoffBase = 10 * time.Millisecond

const luaKeys = 2

func mustJSON(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("json.Marshal failed: %v", err))
	}
	return string(b)
}

func (o *v2Operations) tryAddV2(ctx context.Context, keyword string) (int, error) { //nolint:gocyclo,funlen
	trieData, err := o.storage.HGetAll(ctx, trieKey(o.name))
	if err != nil {
		return 0, newRedisError("HGETALL", trieKey(o.name), err)
	}

	var keywords []string
	if data, ok := trieData["keywords"]; ok {
		if unmarshalErr := json.Unmarshal([]byte(data), &keywords); unmarshalErr != nil {
			return 0, newOperationError("unmarshal", SchemaV2, unmarshalErr)
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
			return 0, newOperationError("unmarshal", SchemaV2, unmarshalErr)
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

	keywordRunes := []rune(keyword)
	var newPrefixes, newSuffixes []string

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
		prefixOutputs := o.computeOutputsV2(prefix, prefixSet, keywordSet)
		if len(prefixOutputs) > 0 {
			newOutputs[prefix] = prefixOutputs
		}
	}

	for _, prefix := range prefixes {
		if prefix == "" {
			continue
		}
		updatedOutputs := o.computeOutputsV2(prefix, prefixSet, keywordSet)
		if len(updatedOutputs) > 0 {
			newOutputs[prefix] = updatedOutputs
		}
	}

	keywords = append(keywords, keyword)
	prefixes = append(prefixes, newPrefixes...)

	var suffixes []string
	if data, ok := trieData["suffixes"]; ok {
		if unmarshalErr := json.Unmarshal([]byte(data), &suffixes); unmarshalErr != nil {
			return 0, newOperationError("unmarshal", SchemaV2, unmarshalErr)
		}
	}
	suffixes = append(suffixes, newSuffixes...)

	newVersion := time.Now().UnixNano()

	outputsToUpdate := make(map[string]string)
	for state, outs := range newOutputs {
		outputsToUpdate[state] = mustJSON(outs)
	}

	result, err := o.addV2Script(ctx, o.client, map[string]interface{}{
		"trieKey":    trieKey(o.name),
		"outputsKey": outputsKey(o.name),
		"keywords":   mustJSON(keywords),
		"prefixes":   mustJSON(prefixes),
		"suffixes":   mustJSON(suffixes),
		"newVersion": newVersion,
		"oldVersion": oldVersion,
		"outputs":    mustJSON(outputsToUpdate),
	}).Result()

	if err != nil {
		return 0, newRedisError("EVAL", trieKey(o.name), err)
	}

	if result == 0 {
		return 0, ErrConcurrencyConflict
	}

	o.publishInvalidate(ctx)

	return 1, nil
}

func (o *v2Operations) tryRemoveV2(ctx context.Context, keyword string) (int, error) { //nolint:gocyclo,funlen
	trieData, err := o.storage.HGetAll(ctx, trieKey(o.name))
	if err != nil {
		return 0, newRedisError("HGETALL", trieKey(o.name), err)
	}

	var keywords []string
	if data, ok := trieData["keywords"]; ok {
		if unmarshalErr := json.Unmarshal([]byte(data), &keywords); unmarshalErr != nil {
			return 0, newOperationError("unmarshal", SchemaV2, unmarshalErr)
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
			return 0, newOperationError("unmarshal", SchemaV2, unmarshalErr)
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
		outs := o.computeOutputsV2(prefix, prefixSet, keywordSet)
		if len(outs) > 0 {
			newOutputs[prefix] = outs
		}
	}

	newVersion := time.Now().UnixNano()

	outputsToSet := make(map[string]string)
	for state, outs := range newOutputs {
		outputsToSet[state] = mustJSON(outs)
	}

	result, err := o.removeV2Script(ctx, o.client, map[string]interface{}{
		"trieKey":    trieKey(o.name),
		"outputsKey": outputsKey(o.name),
		"keywords":   mustJSON(newKeywords),
		"prefixes":   mustJSON(newPrefixes),
		"suffixes":   mustJSON(newSuffixes),
		"newVersion": newVersion,
		"oldVersion": oldVersion,
		"outputs":    mustJSON(outputsToSet),
	}).Result()

	if err != nil {
		return 0, newRedisError("EVAL", trieKey(o.name), err)
	}

	if result == 0 {
		return 0, ErrConcurrencyConflict
	}

	o.publishInvalidate(ctx)

	return len(newKeywords), nil
}

func (o *v2Operations) computeOutputsV2(state string, prefixSet, keywordSet map[string]struct{}) []string {
	var outputs []string

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

func (ac *AhoCorasick) tryAddV2(ctx context.Context, keyword string) (int, error) {
	v2Ops, ok := ac.ops.(*v2Operations)
	if !ok {
		return 0, fmt.Errorf("internal error: tryAddV2 called on non-v2 operations strategy")
	}
	return v2Ops.tryAddV2(ctx, keyword)
}

func (ac *AhoCorasick) tryRemoveV2(ctx context.Context, keyword string) (int, error) {
	v2Ops, ok := ac.ops.(*v2Operations)
	if !ok {
		return 0, fmt.Errorf("internal error: tryRemoveV2 called on non-v2 operations strategy")
	}
	return v2Ops.tryRemoveV2(ctx, keyword)
}
