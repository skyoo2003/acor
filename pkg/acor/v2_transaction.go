package acor

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/skyoo2003/acor/internal/pkg/utils"
)

// versionRandBytes is the number of random bytes used to extend version timestamps
// and prevent collisions under heavy concurrent writes.
const versionRandBytes = 4

// --- V2 transaction helpers (optimistic locking) ---

// generateVersion returns a unique version timestamp with a random suffix
// to prevent collisions under heavy concurrent writes where multiple instances
// may generate the same UnixNano timestamp.
func generateVersion() (int64, error) {
	b := make([]byte, versionRandBytes)
	if _, err := rand.Read(b); err != nil {
		return 0, fmt.Errorf("generateVersion: crypto/rand.Read failed: %w", err)
	}
	return time.Now().UnixNano() + int64(b[0])<<48 + int64(b[1])<<40 + int64(b[2])<<32 + int64(b[3])<<24, nil
}

// trieSnapshot holds the deserialized trie data read from Redis.
type trieSnapshot struct {
	Keywords []string
	Prefixes []string
	Suffixes []string
	Version  int64
}

// readTrieData loads and deserializes the trie hash from Redis.
func (o *v2Operations) readTrieData(ctx context.Context) (*trieSnapshot, error) {
	trieData, err := o.storage.HGetAll(ctx, trieKey(o.name))
	if err != nil {
		return nil, newRedisError("HGETALL", trieKey(o.name), err)
	}

	snap := &trieSnapshot{}

	if data, ok := trieData["keywords"]; ok {
		if err := json.Unmarshal([]byte(data), &snap.Keywords); err != nil {
			return nil, newOperationError("unmarshal", SchemaV2, err)
		}
	}
	if data, ok := trieData["prefixes"]; ok {
		if err := json.Unmarshal([]byte(data), &snap.Prefixes); err != nil {
			return nil, newOperationError("unmarshal", SchemaV2, err)
		}
	}
	if data, ok := trieData["suffixes"]; ok {
		if err := json.Unmarshal([]byte(data), &snap.Suffixes); err != nil {
			return nil, newOperationError("unmarshal", SchemaV2, err)
		}
	}
	if v, ok := trieData["version"]; ok {
		if err := json.Unmarshal([]byte(v), &snap.Version); err != nil {
			snap.Version = 0
		}
	}

	return snap, nil
}

// marshalTrieArgs serializes trie data into the args map for Lua scripts.
func marshalTrieArgs(snap *trieSnapshot, outputs map[string]string, newVersion int64) (map[string]interface{}, error) {
	args := map[string]interface{}{
		"trieKey":    "", // caller must set
		"outputsKey": "", // caller must set
		"newVersion": newVersion,
		"oldVersion": snap.Version,
	}
	var err error
	if args["keywords"], err = toJSON(snap.Keywords); err != nil {
		return nil, newOperationError("marshal", SchemaV2, err)
	}
	if args["prefixes"], err = toJSON(snap.Prefixes); err != nil {
		return nil, newOperationError("marshal", SchemaV2, err)
	}
	if args["suffixes"], err = toJSON(snap.Suffixes); err != nil {
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

func (o *v2Operations) tryAddV2(ctx context.Context, keyword string) (int, error) { //nolint:gocyclo,funlen
	snap, err := o.readTrieData(ctx)
	if err != nil {
		return 0, err
	}

	keywordSet := make(map[string]struct{}, len(snap.Keywords))
	for _, kw := range snap.Keywords {
		keywordSet[kw] = struct{}{}
	}
	if _, exists := keywordSet[keyword]; exists {
		return 0, nil
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

	for _, prefix := range snap.Prefixes {
		if prefix == "" {
			continue
		}
		updatedOutputs := o.computeOutputsV2(prefix, prefixSet, keywordSet)
		if len(updatedOutputs) > 0 {
			newOutputs[prefix] = updatedOutputs
		}
	}

	// Build updated snapshot
	snap.Keywords = append(snap.Keywords, keyword)
	snap.Prefixes = append(snap.Prefixes, newPrefixes...)
	snap.Suffixes = append(snap.Suffixes, newSuffixes...)

	newVersion, genErr := generateVersion()
	if genErr != nil {
		return 0, genErr
	}

	outputsToUpdate := make(map[string]string)
	for state, outs := range newOutputs {
		jsonOuts, marshalErr := toJSON(outs)
		if marshalErr != nil {
			return 0, newOperationError("marshal", SchemaV2, marshalErr)
		}
		outputsToUpdate[state] = jsonOuts
	}

	args, err := marshalTrieArgs(snap, outputsToUpdate, newVersion)
	if err != nil {
		return 0, err
	}
	args["trieKey"] = trieKey(o.name)
	args["outputsKey"] = outputsKey(o.name)

	result, err := o.addV2Script(ctx, o.client, args).Result()
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
	snap, err := o.readTrieData(ctx)
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
		return len(snap.Keywords), nil
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

	newVersion, genErr := generateVersion()
	if genErr != nil {
		return 0, genErr
	}

	outputsToSet := make(map[string]string)
	for state, outs := range newOutputs {
		jsonOuts, marshalErr := toJSON(outs)
		if marshalErr != nil {
			return 0, newOperationError("marshal", SchemaV2, marshalErr)
		}
		outputsToSet[state] = jsonOuts
	}

	updatedSnap := &trieSnapshot{
		Keywords: newKeywords,
		Prefixes: newPrefixes,
		Suffixes: newSuffixes,
		Version:  snap.Version,
	}

	args, err := marshalTrieArgs(updatedSnap, outputsToSet, newVersion)
	if err != nil {
		return 0, err
	}
	args["trieKey"] = trieKey(o.name)
	args["outputsKey"] = outputsKey(o.name)

	result, err := o.removeV2Script(ctx, o.client, args).Result()
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
