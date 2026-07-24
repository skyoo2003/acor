// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"fmt"
	"time"
)

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
	fieldSuffixes = "suffixes"
	fieldVersion  = "version"

	argTrieKey    = "trieKey"
	argOutputsKey = "outputsKey"

	// emptyKeywordsJSON and emptyStringArrayJSON are the default JSON values
	// stored in an empty V2 trie hash.
	emptyKeywordsJSON    = "[]"
	emptyStringArrayJSON = `[""]`
)

func keyPrefix(name string) string {
	return fmt.Sprintf("{%s}", name)
}

func keywordKey(name string) string {
	return fmt.Sprintf("%s:keyword", keyPrefix(name))
}

func prefixKey(name string) string {
	return fmt.Sprintf("%s:prefix", keyPrefix(name))
}

func suffixKey(name string) string {
	return fmt.Sprintf("%s:suffix", keyPrefix(name))
}

func outputKey(name, state string) string {
	return fmt.Sprintf("%s:output:%s", keyPrefix(name), state)
}

func nodeKey(name, keyword string) string {
	return fmt.Sprintf("%s:node:%s", keyPrefix(name), keyword)
}

func trieKey(name string) string {
	return fmt.Sprintf("%s:trie", keyPrefix(name))
}

func outputsKey(name string) string {
	return fmt.Sprintf("%s:outputs", keyPrefix(name))
}

func nodesKey(name string) string {
	return fmt.Sprintf("%s:nodes", keyPrefix(name))
}

// emptyTrieFields returns the hash fields written to initialize an empty V2
// trie. The version is stamped fresh on each call.
func emptyTrieFields() map[string]interface{} {
	return map[string]interface{}{
		fieldKeywords: emptyKeywordsJSON,
		fieldPrefixes: emptyStringArrayJSON,
		fieldSuffixes: emptyStringArrayJSON,
		fieldVersion:  time.Now().UnixNano(),
	}
}
