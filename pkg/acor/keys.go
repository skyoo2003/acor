package acor

import "fmt"

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
