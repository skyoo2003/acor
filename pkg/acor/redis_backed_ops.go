// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"errors"
	"strings"
	"time"

	matchengine "github.com/skyoo2003/acor/internal/engine"
)

const redisBackedMaxRetries = 3
const redisBackedRetryBackoff = 10 * time.Millisecond

// Add inserts a keyword into the automaton. The keyword is written atomically
// to Redis via a V2 Lua script (optimistic locking), then the local automaton
// is rebuilt and an invalidation is published.
func (ac *redisBackedAC) Add(ctx context.Context, keyword string) (int, error) {
	keyword = normalizeKeyword(keyword, ac.caseSensitive)
	if keyword == "" {
		return 0, nil
	}

	v2 := &redisBackedV2{client: ac.redisClient}

	for attempt := 0; attempt < redisBackedMaxRetries; attempt++ {
		added, err := ac.tryAdd(ctx, keyword, v2)
		if errors.Is(err, ErrConcurrencyConflict) {
			backoff := time.Duration(attempt+1) * redisBackedRetryBackoff
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

func (ac *redisBackedAC) tryAdd(ctx context.Context, keyword string, v2 *redisBackedV2) (int, error) { //nolint:gocyclo,funlen
	snap, err := readTrieSnapshot(ctx, ac.storage, ac.name)
	if err != nil {
		return 0, err
	}

	for _, kw := range snap.Keywords {
		if kw == keyword {
			return 0, nil
		}
	}

	keywordSet := make(map[string]struct{}, len(snap.Keywords)+1)
	for _, kw := range snap.Keywords {
		keywordSet[kw] = struct{}{}
	}
	prefixSet := make(map[string]struct{}, len(snap.Prefixes))
	for _, p := range snap.Prefixes {
		prefixSet[p] = struct{}{}
	}

	keywordRunes := []rune(keyword)
	var newPrefixes, newSuffixes []string
	for i := 0; i < len(keywordRunes); i++ {
		prefix := string(keywordRunes[:i+1])
		if _, exists := prefixSet[prefix]; !exists {
			newPrefixes = append(newPrefixes, prefix)
			newSuffixes = append(newSuffixes, reverse(prefix))
			prefixSet[prefix] = struct{}{}
		}
	}

	keywordSet[keyword] = struct{}{}
	newOutputs := make(map[string][]string)
	for _, prefix := range newPrefixes {
		outs := computeRBOutputs(prefix, prefixSet, keywordSet)
		if len(outs) > 0 {
			newOutputs[prefix] = outs
		}
	}
	for _, prefix := range snap.Prefixes {
		if prefix == "" {
			continue
		}
		outs := computeRBOutputs(prefix, prefixSet, keywordSet)
		if len(outs) > 0 {
			newOutputs[prefix] = outs
		}
	}

	snap.Keywords = append(snap.Keywords, keyword)
	snap.Prefixes = append(snap.Prefixes, newPrefixes...)
	snap.Suffixes = append(snap.Suffixes, newSuffixes...)

	newVersion, genErr := generateVersion()
	if genErr != nil {
		return 0, genErr
	}

	outputsToSet := make(map[string]string)
	for state, outs := range newOutputs {
		jsonOuts, marshalErr := toJSON(outs)
		if marshalErr != nil {
			return 0, marshalErr
		}
		outputsToSet[state] = jsonOuts
	}

	args, marshalErr := marshalTrieArgs(snap, outputsToSet, newVersion)
	if marshalErr != nil {
		return 0, marshalErr
	}
	args["trieKey"] = trieKey(ac.name)
	args["outputsKey"] = outputsKey(ac.name)

	val, err := v2.runAddScript(ctx, args)
	if err != nil {
		return 0, err
	}
	if val == 0 {
		return 0, ErrConcurrencyConflict
	}

	ac.mu.Lock()
	ac.keywordSet[keyword] = struct{}{}
	ac.engine.Build(ac.keywordSet)
	ac.localVersion = newVersion
	ac.stale = false
	ac.mu.Unlock()

	ac.publishInvalidate(ctx)
	return 1, nil
}

// Remove deletes a keyword from the automaton.
func (ac *redisBackedAC) Remove(ctx context.Context, keyword string) (int, error) {
	keyword = normalizeKeyword(keyword, ac.caseSensitive)
	if keyword == "" {
		return 0, nil
	}

	v2 := &redisBackedV2{client: ac.redisClient}

	for attempt := 0; attempt < redisBackedMaxRetries; attempt++ {
		removed, err := ac.tryRemove(ctx, keyword, v2)
		if errors.Is(err, ErrConcurrencyConflict) {
			backoff := time.Duration(attempt+1) * redisBackedRetryBackoff
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

func (ac *redisBackedAC) tryRemove(ctx context.Context, keyword string, v2 *redisBackedV2) (int, error) { //nolint:gocyclo,funlen
	snap, err := readTrieSnapshot(ctx, ac.storage, ac.name)
	if err != nil {
		return 0, err
	}

	keywordExists := false
	newKeywords := make([]string, 0, len(snap.Keywords))
	for _, kw := range snap.Keywords {
		if kw == keyword {
			keywordExists = true
		} else {
			newKeywords = append(newKeywords, kw)
		}
	}
	if !keywordExists {
		return 0, nil
	}

	keywordSet := make(map[string]struct{})
	for _, kw := range newKeywords {
		keywordSet[kw] = struct{}{}
	}

	newPrefixes := []string{""}
	for _, prefix := range snap.Prefixes {
		if prefix == "" {
			continue
		}
		for kw := range keywordSet {
			if strings.HasPrefix(kw, prefix) {
				newPrefixes = append(newPrefixes, prefix)
				break
			}
		}
	}

	prefixSet := make(map[string]struct{})
	for _, p := range newPrefixes {
		prefixSet[p] = struct{}{}
	}

	newSuffixes := make([]string, len(newPrefixes))
	for i, p := range newPrefixes {
		newSuffixes[i] = reverse(p)
	}

	newOutputs := make(map[string][]string)
	for _, prefix := range newPrefixes {
		if prefix == "" {
			continue
		}
		outs := computeRBOutputs(prefix, prefixSet, keywordSet)
		if len(outs) > 0 {
			newOutputs[prefix] = outs
		}
	}

	newVersion, genErr := generateVersion()
	if genErr != nil {
		return 0, genErr
	}

	outputsToSet := make(map[string]string)
	for state, outs := range newOutputs {
		jsonOuts, marshalErr := toJSON(outs)
		if marshalErr != nil {
			return 0, marshalErr
		}
		outputsToSet[state] = jsonOuts
	}

	updatedSnap := &trieSnapshot{
		Keywords: newKeywords,
		Prefixes: newPrefixes,
		Suffixes: newSuffixes,
		Version:  snap.Version,
	}

	args, marshalErr := marshalTrieArgs(updatedSnap, outputsToSet, newVersion)
	if marshalErr != nil {
		return 0, marshalErr
	}
	args["trieKey"] = trieKey(ac.name)
	args["outputsKey"] = outputsKey(ac.name)

	val, err := v2.runRemoveScript(ctx, args)
	if err != nil {
		return 0, err
	}
	if val == 0 {
		return 0, ErrConcurrencyConflict
	}

	ac.mu.Lock()
	delete(ac.keywordSet, keyword)
	ac.engine.Build(ac.keywordSet)
	ac.localVersion = newVersion
	ac.stale = false
	ac.mu.Unlock()

	ac.publishInvalidate(ctx)
	return 1, nil
}

// Find searches the text for all keywords using the local automaton.
func (ac *redisBackedAC) Find(ctx context.Context, text string) ([]string, error) {
	if text == "" {
		return []string{}, nil
	}
	text = normalizeText(text, ac.caseSensitive)

	if err := ac.ensureValid(ctx); err != nil {
		return nil, err
	}

	ac.mu.RLock()
	defer ac.mu.RUnlock()
	return ac.engine.Find(text), nil
}

// FindIndex searches the text for all keywords and returns their start indices.
func (ac *redisBackedAC) FindIndex(ctx context.Context, text string) (map[string][]int, error) {
	if text == "" {
		return map[string][]int{}, nil
	}
	text = normalizeText(text, ac.caseSensitive)

	if err := ac.ensureValid(ctx); err != nil {
		return nil, err
	}

	ac.mu.RLock()
	defer ac.mu.RUnlock()
	return ac.engine.FindIndex(text), nil
}

// Flush removes all keywords from the automaton.
func (ac *redisBackedAC) Flush(ctx context.Context) error {
	err := ac.storage.TxPipelined(ctx, func(pipe Pipeliner) error {
		tKey := trieKey(ac.name)
		oKey := outputsKey(ac.name)
		nKey := nodesKey(ac.name)
		if err := pipe.Del(ctx, oKey, nKey); err != nil {
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
		return newRedisError("TXPIPELINED", trieKey(ac.name), err)
	}

	ac.mu.Lock()
	ac.keywordSet = make(map[string]struct{})
	ac.engine.Build(ac.keywordSet)
	ac.stale = false
	ac.mu.Unlock()

	ac.publishInvalidate(ctx)
	return nil
}

// Info returns statistics about the local automaton state.
func (ac *redisBackedAC) Info(ctx context.Context) (*matchengine.InMemoryInfo, error) {
	ac.mu.RLock()
	defer ac.mu.RUnlock()
	return ac.engine.Info(), nil
}

// computeRBOutputs returns all keywords that are suffixes of the given state.
func computeRBOutputs(state string, prefixSet, keywordSet map[string]struct{}) []string {
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
