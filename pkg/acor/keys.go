package acor

import "fmt"

const (
	KeywordKey = "%s:keyword"
	PrefixKey  = "%s:prefix"
	SuffixKey  = "%s:suffix"
	OutputKey  = "%s:output"
	NodeKey    = "%s:node"
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
