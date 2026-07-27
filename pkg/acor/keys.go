// SPDX-License-Identifier: Apache-2.0

package acor

import "time"

// Key format constants for the V1 schema, using %s as a placeholder for the
// collection name.
//
// These do not produce the keys ACOR actually writes. A collection name is
// wrapped in a Redis hash tag so all of a collection's keys hash to one cluster
// slot, making the real key "{mycoll}:keyword" while
// fmt.Sprintf(KeywordKey, "mycoll") yields "mycoll:keyword". Nothing in ACOR
// reads these constants; the key builders hold the correct format. Treat them
// as informational only — reading Redis directly with a key built from them
// will not find anything.
const (
	// KeywordKey is the suffix pattern for the keywords set. Real key: "{name}:keyword".
	KeywordKey = "%s:keyword"
	// PrefixKey is the suffix pattern for the prefixes sorted set. Real key: "{name}:prefix".
	PrefixKey = "%s:prefix"
	// SuffixKey is the suffix pattern for the suffixes sorted set. Real key: "{name}:suffix".
	SuffixKey = "%s:suffix"
	// OutputKey is the suffix pattern for output sets. Real key: "{name}:output:{state}".
	OutputKey = "%s:output"
	// NodeKey is the suffix pattern for node sets. Real key: "{name}:node:{keyword}".
	NodeKey = "%s:node"
)

// V2 trie-hash field names. Kept as constants so a typo can't silently break a
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
