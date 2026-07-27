// SPDX-License-Identifier: Apache-2.0

package acor

import "time"

// Key format constants define the Redis key patterns used by the V1 schema.
// These constants use %s as a placeholder for the collection name.
// For V2 schema, fewer keys are used (see trieKey, outputsKey, nodesKey).
const (
	// KeywordKey is the format for the keywords set key: "{name}:keyword"
	KeywordKey = "%s:keyword"
	// PrefixKey is the format for the prefixes sorted set key: "{name}:prefix"
	PrefixKey = "%s:prefix"
	// SuffixKey is the format for the suffixes sorted set key: "{name}:suffix"
	SuffixKey = "%s:suffix"
	// OutputKey is the format for output set keys: "{name}:output:{state}"
	OutputKey = "%s:output"
	// NodeKey is the format for node set keys: "{name}:node:{keyword}"
	NodeKey = "%s:node"
)

// V2 trie-hash field names and the internal arg-map keys passed to the Lua
// transaction helpers. Kept as constants so a typo can't silently break a
// Redis read or write.
const (
	fieldKeywords = "keywords"
	fieldPrefixes = "prefixes"
	fieldVersion  = "version"

	// emptyKeywordsJSON and emptyStringArrayJSON are the default JSON values
	// stored in an empty V2 trie hash: no keywords, and the root prefix only.
	emptyKeywordsJSON    = "[]"
	emptyStringArrayJSON = `[""]`
)

// keyPrefix wraps the collection name in a Redis hash tag so every key of a
// collection lands on the same cluster slot.
func keyPrefix(name string) string {
	return "{" + name + "}"
}

func keywordKey(name string) string {
	return keyPrefix(name) + ":keyword"
}

func prefixKey(name string) string {
	return keyPrefix(name) + ":prefix"
}

func suffixKey(name string) string {
	return keyPrefix(name) + ":suffix"
}

func outputKey(name, state string) string {
	return keyPrefix(name) + ":output:" + state
}

func nodeKey(name, keyword string) string {
	return keyPrefix(name) + ":node:" + keyword
}

func trieKey(name string) string {
	return keyPrefix(name) + ":trie"
}

func outputsKey(name string) string {
	return keyPrefix(name) + ":outputs"
}

func nodesKey(name string) string {
	return keyPrefix(name) + ":nodes"
}

// emptyTrieFields returns the hash fields written to initialize an empty V2
// trie. The version is stamped fresh on each call.
func emptyTrieFields() map[string]interface{} {
	return map[string]interface{}{
		fieldKeywords: emptyKeywordsJSON,
		fieldPrefixes: emptyStringArrayJSON,
		fieldVersion:  time.Now().UnixNano(),
	}
}
