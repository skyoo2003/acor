package acor

import (
	"testing"
)

func TestMatcherInterfaceExists(t *testing.T) {
	var _ Matcher = (*AhoCorasick)(nil)
}

func TestIndexerInterfaceExists(t *testing.T) {
	var _ Indexer = (*AhoCorasick)(nil)
}
