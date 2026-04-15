package acor

import (
	"context"
	"os"
	"testing"

	miniredis "github.com/alicebob/miniredis/v2"
	redis "github.com/go-redis/redis/v8"
)

type redisNoopLogger struct{}

func (redisNoopLogger) Printf(_ context.Context, _ string, _ ...interface{}) {}

func TestMain(m *testing.M) {
	redis.SetLogger(redisNoopLogger{})
	os.Exit(m.Run())
}

// testLogger is a no-op Logger implementation for tests.
type testLogger struct{}

func (l *testLogger) Printf(format string, args ...interface{}) {}
func (l *testLogger) Println(v ...interface{})                  {}

// Shared test constants used across multiple test files.
const (
	testKeywordHE         = "he"
	testKeywordHim        = "him"
	testKeywordTest       = "test"
	testKeywordHello      = "hello"
	testKeywordHelloUpper = "Hello"
)

func createTestRedisServer(t *testing.T) *miniredis.Miniredis {
	t.Helper()

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}

	return mr
}

func createAhoCorasick(t *testing.T) (*AhoCorasick, *miniredis.Miniredis) {
	t.Helper()

	mr := createTestRedisServer(t)
	ac, err := Create(&AhoCorasickArgs{
		Addr:     mr.Addr(),
		Password: "",
		DB:       0,
		Name:     "test",
		Debug:    false,
	})
	if err != nil {
		mr.Close()
		t.Fatal(err)
	}

	return ac, mr
}

func createAhoCorasickV1(t *testing.T) (*AhoCorasick, *miniredis.Miniredis) {
	t.Helper()

	mr := createTestRedisServer(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	if err := client.ZAdd(context.Background(), "{test}:prefix", &redis.Z{Score: 0, Member: ""}).Err(); err != nil {
		mr.Close()
		t.Fatalf("failed to seed V1 prefix data: %v", err)
	}
	if err := client.Close(); err != nil {
		mr.Close()
		t.Fatalf("failed to close V1 seed client: %v", err)
	}

	ac, err := Create(&AhoCorasickArgs{
		Addr:          mr.Addr(),
		Password:      "",
		DB:            0,
		Name:          "test",
		Debug:         false,
		SchemaVersion: SchemaV1,
	})
	if err != nil {
		mr.Close()
		t.Fatal(err)
	}

	return ac, mr
}

func createAhoCorasickWithSchema(t *testing.T, schemaVersion int) (*AhoCorasick, *miniredis.Miniredis) {
	t.Helper()

	if schemaVersion == SchemaV1 {
		return createAhoCorasickV1(t)
	}
	return createAhoCorasick(t)
}

func assertIndexResults(t *testing.T, actual, expected map[string][]int) {
	t.Helper()

	if len(actual) != len(expected) {
		t.Errorf("results' count is unexpected: got %d, want %d", len(actual), len(expected))
	}

	for keyword, expectedIndexes := range expected {
		actualIndexes, ok := actual[keyword]
		if !ok {
			t.Errorf("results are missing %s", keyword)
			continue
		}
		if len(actualIndexes) != len(expectedIndexes) {
			t.Errorf("results for %s have unexpected count: got %d, want %d", keyword, len(actualIndexes), len(expectedIndexes))
			continue
		}
		for idx, actualIndex := range actualIndexes {
			if actualIndex != expectedIndexes[idx] {
				t.Errorf("results for %s have invalid index: got %d, want %d", keyword, actualIndex, expectedIndexes[idx])
			}
		}
	}
}

func equalStringSets(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	aSet := make(map[string]int)
	for _, s := range a {
		aSet[s]++
	}
	for _, s := range b {
		aSet[s]--
		if aSet[s] < 0 {
			return false
		}
	}
	return true
}

func containsAll(slice []string, items ...string) bool {
	set := make(map[string]struct{})
	for _, s := range slice {
		set[s] = struct{}{}
	}
	for _, item := range items {
		if _, exists := set[item]; !exists {
			return false
		}
	}
	return true
}
