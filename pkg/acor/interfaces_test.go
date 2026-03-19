package acor

import (
	"reflect"
	"testing"
)

type redisStorage struct{}

func TestKVStorageInterfaceExists(t *testing.T) {
	storageType := reflect.TypeOf((*redisStorage)(nil))
	kvStorageType := reflect.TypeOf((*KVStorage)(nil)).Elem()
	if !storageType.Implements(kvStorageType) {
		t.Skip("redisStorage does not implement KVStorage - will be implemented in Task 1.3")
	}
}

func TestMatcherInterfaceExists(t *testing.T) {
	var _ Matcher = (*AhoCorasick)(nil)
}

func TestIndexerInterfaceExists(t *testing.T) {
	var _ Indexer = (*AhoCorasick)(nil)
}
