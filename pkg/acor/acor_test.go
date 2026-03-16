package acor

import (
	"errors"
	"testing"

	miniredis "github.com/alicebob/miniredis/v2"
	redis "github.com/go-redis/redis/v8"
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

func TestInitAndFlushAndClose(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	if err := ac.Flush(); err != nil {
		t.Fatal(err)
	}
}

func TestAddAndRemove(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	addedCount, removedCount := 0, 0
	keywords := []string{"her", "he", "his"}

	for _, keyword := range keywords {
		count, err := ac.Add(keyword)
		if err != nil {
			t.Fatal(err)
		}
		addedCount += count
	}
	if addedCount != 3 {
		t.Errorf("The added count is not fit")
	}

	for _, keyword := range keywords {
		count, err := ac.Remove(keyword)
		if err != nil {
			t.Fatal(err)
		}
		removedCount += count
	}
	if removedCount != 3 {
		t.Errorf("The removed count is not fit")
	}
}

func TestSuggest(t *testing.T) {
	var results []string
	const input = "he"

	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	keywords := []string{"her", "he", "his"}
	for _, keyword := range keywords {
		if _, err := ac.Add(keyword); err != nil {
			t.Fatal(err)
		}
	}

	var err error
	results, err = ac.Suggest(input)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Suggest(%s) : Results(%s)", input, results)

	if len(results) != 2 {
		t.Error("results' count is unexpected")
	}
	for _, result := range results {
		switch result {
		case "her", input:
			continue
		}
		t.Error("results have invalid data")
	}
}

func TestSuggestIndex(t *testing.T) {
	const input = "he"

	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	keywords := []string{"her", "he", "his"}
	for _, keyword := range keywords {
		if _, err := ac.Add(keyword); err != nil {
			t.Fatal(err)
		}
	}

	results, err := ac.SuggestIndex(input)
	if err != nil {
		t.Fatal(err)
	}

	if len(results) != 2 {
		t.Error("results' count is unexpected")
	}

	expected := map[string][]int{
		"he":  {0},
		"her": {0},
	}
	for keyword, indexes := range expected {
		actualIndexes, ok := results[keyword]
		if !ok {
			t.Errorf("results are missing %s", keyword)
			continue
		}
		if len(actualIndexes) != len(indexes) {
			t.Errorf("results for %s have unexpected count", keyword)
			continue
		}
		for idx, actualIndex := range actualIndexes {
			if actualIndex != indexes[idx] {
				t.Errorf("results for %s have invalid index", keyword)
			}
		}
	}

	emptyResults, err := ac.SuggestIndex("x")
	if err != nil {
		t.Fatal(err)
	}
	if len(emptyResults) != 0 {
		t.Error("results should be empty")
	}
}

func TestFind(t *testing.T) {
	var results []string
	const input = "he"

	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	keywords := []string{"her", "he", "his"}
	for _, keyword := range keywords {
		if _, err := ac.Add(keyword); err != nil {
			t.Fatal(err)
		}
	}
	ac.Debug()

	var err error
	results, err = ac.Find(input)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Find(%s) : Results(%s)", input, results)

	if len(results) != 1 {
		t.Error("results' count is unexpected")
	}
	for _, result := range results {
		if result == input {
			continue
		}
		t.Error("results have invalid data")
	}
}

func TestFindIndex(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	keywords := []string{"her", "he", "his"}
	for _, keyword := range keywords {
		if _, err := ac.Add(keyword); err != nil {
			t.Fatal(err)
		}
	}

	overlapResults, err := ac.FindIndex("her")
	if err != nil {
		t.Fatal(err)
	}
	overlapExpected := map[string][]int{
		"he":  {0},
		"her": {0},
	}
	assertIndexResults(t, overlapResults, overlapExpected)

	repeatedResults, err := ac.FindIndex("hehe")
	if err != nil {
		t.Fatal(err)
	}
	repeatedExpected := map[string][]int{
		"he": {0, 2},
	}
	assertIndexResults(t, repeatedResults, repeatedExpected)

	err = ac.Flush()
	if err != nil {
		t.Fatal(err)
	}
	_, err = ac.Add("한글")
	if err != nil {
		t.Fatal(err)
	}
	unicodeResults, err := ac.FindIndex("가한글")
	if err != nil {
		t.Fatal(err)
	}
	unicodeExpected := map[string][]int{
		"한글": {1},
	}
	assertIndexResults(t, unicodeResults, unicodeExpected)
}

func TestCreateReturnsErrorWhenRedisUnavailable(t *testing.T) {
	mr := createTestRedisServer(t)
	addr := mr.Addr()
	mr.Close()

	ac, err := Create(&AhoCorasickArgs{
		Addr:     addr,
		Password: "",
		DB:       0,
		Name:     "test",
		Debug:    false,
	})
	if err == nil {
		t.Fatal("expected create to return an error")
	}
	if ac != nil {
		t.Fatal("expected create to return nil aho-corasick")
	}
}

func TestNewRedisClientSelectsStandaloneTopology(t *testing.T) {
	mr := createTestRedisServer(t)
	defer mr.Close()

	client, err := newRedisClient(&AhoCorasickArgs{
		Addr: mr.Addr(),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()

	if _, ok := client.(*redis.Client); !ok {
		t.Fatalf("expected standalone redis client, got %T", client)
	}
}

func TestNewRedisClientSelectsSentinelTopology(t *testing.T) {
	client, err := newRedisClient(&AhoCorasickArgs{
		Addrs:      []string{"127.0.0.1:26379"},
		MasterName: "mymaster",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()

	standaloneClient, ok := client.(*redis.Client)
	if !ok {
		t.Fatalf("expected failover client to use redis.Client, got %T", client)
	}
	if standaloneClient.Options().Addr != "FailoverClient" {
		t.Fatalf("expected sentinel failover client, got addr %q", standaloneClient.Options().Addr)
	}
}

func TestNewRedisClientSelectsClusterTopology(t *testing.T) {
	client, err := newRedisClient(&AhoCorasickArgs{
		Addrs: []string{"127.0.0.1:7000"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()

	clusterClient, ok := client.(*redis.ClusterClient)
	if !ok {
		t.Fatalf("expected cluster redis client, got %T", client)
	}
	if len(clusterClient.Options().Addrs) != 1 {
		t.Fatalf("expected cluster addresses to be preserved, got %v", clusterClient.Options().Addrs)
	}
}

func TestAddUsesCollectionScopedKeys(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	if _, err := ac.Add("he"); err != nil {
		t.Fatal(err)
	}

	keys := []string{
		ac.keywordKey(),
		ac.prefixKey(),
		ac.suffixKey(),
		ac.outputKey("he"),
		ac.nodeKey("he"),
	}
	for _, key := range keys {
		if !mr.Exists(key) {
			t.Fatalf("expected redis key %q to exist", key)
		}
	}

	if mr.Exists("he:output") {
		t.Fatal("expected output key to be collection-scoped")
	}
	if mr.Exists("he:node") {
		t.Fatal("expected node key to be collection-scoped")
	}
}

func TestNewRedisClientSelectsRingTopology(t *testing.T) {
	client, err := newRedisClient(&AhoCorasickArgs{
		RingAddrs: map[string]string{
			"shard-1": "127.0.0.1:7000",
			"shard-2": "127.0.0.1:7001",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()

	ringClient, ok := client.(*redis.Ring)
	if !ok {
		t.Fatalf("expected ring redis client, got %T", client)
	}
	if len(ringClient.Options().Addrs) != 2 {
		t.Fatalf("expected ring shard addresses to be preserved, got %v", ringClient.Options().Addrs)
	}
}

func TestNewRedisClientRejectsInvalidTopologyConfigurations(t *testing.T) {
	tests := []struct {
		name string
		args *AhoCorasickArgs
		err  error
	}{
		{
			name: "conflicting standalone and cluster",
			args: &AhoCorasickArgs{
				Addr:  "127.0.0.1:6379",
				Addrs: []string{"127.0.0.1:7000"},
			},
			err: ErrRedisConflictingTopology,
		},
		{
			name: "conflicting cluster and ring",
			args: &AhoCorasickArgs{
				Addrs: []string{"127.0.0.1:7000", "127.0.0.1:7001"},
				RingAddrs: map[string]string{
					"shard-1": "127.0.0.1:7100",
				},
			},
			err: ErrRedisConflictingTopology,
		},
		{
			name: "sentinel requires address",
			args: &AhoCorasickArgs{
				MasterName: "mymaster",
			},
			err: ErrRedisSentinelAddrs,
		},
		{
			name: "cluster does not support db selection",
			args: &AhoCorasickArgs{
				Addrs: []string{"127.0.0.1:7000", "127.0.0.1:7001"},
				DB:    1,
			},
			err: ErrRedisClusterDB,
		},
		{
			name: "ring requires non-empty shard address",
			args: &AhoCorasickArgs{
				RingAddrs: map[string]string{
					"shard-1": "   ",
				},
			},
			err: ErrRedisRingAddrs,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := newRedisClient(tt.args)
			if !errors.Is(err, tt.err) {
				t.Fatalf("expected %v, got %v", tt.err, err)
			}
			if client != nil {
				_ = client.Close()
				t.Fatalf("expected client to be nil, got %T", client)
			}
		})
	}
}

func TestFindReturnsErrorWhenRedisUnavailable(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer func() { _ = ac.Close() }()

	if _, err := ac.Add("he"); err != nil {
		t.Fatal(err)
	}

	mr.Close()

	if _, err := ac.Find("he"); err == nil {
		t.Fatal("expected find to return an error")
	}
}

func TestAddRollsBackPartialTrieWrites(t *testing.T) {
	const input = "he"

	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	hookErr := errors.New("forced build trie failure")
	ac.buildTrieHook = func(prefix string) error {
		if prefix == input {
			return hookErr
		}
		return nil
	}

	addedCount, err := ac.Add(input)
	if !errors.Is(err, hookErr) {
		t.Fatalf("expected add to fail with hook error, got %v", err)
	}
	if addedCount != 0 {
		t.Fatalf("expected add count to be zero on rollback, got %d", addedCount)
	}

	ac.buildTrieHook = nil

	results, err := ac.Find(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("expected no matches after rollback, got %v", results)
	}

	indexResults, err := ac.FindIndex(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(indexResults) != 0 {
		t.Fatalf("expected no indexed matches after rollback, got %v", indexResults)
	}
}

func TestAddFailedReAddKeepsExistingKeywordState(t *testing.T) {
	const input = "he"

	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	if _, err := ac.Add(input); err != nil {
		t.Fatal(err)
	}

	pKey := ac.prefixKey()
	if _, err := ac.redisClient.ZRem(ac.ctx, pKey, input).Result(); err != nil {
		t.Fatal(err)
	}

	hookErr := errors.New("forced duplicate rebuild failure")
	ac.buildTrieHook = func(prefix string) error {
		if prefix == input {
			return hookErr
		}
		return nil
	}

	addedCount, err := ac.Add(input)
	if !errors.Is(err, hookErr) {
		t.Fatalf("expected duplicate add to fail with hook error, got %v", err)
	}
	if addedCount != 0 {
		t.Fatalf("expected duplicate add count to be zero, got %d", addedCount)
	}

	ac.buildTrieHook = nil

	results, err := ac.Find(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0] != input {
		t.Fatalf("expected existing keyword state to remain after failed re-add, got %v", results)
	}

	indexResults, err := ac.FindIndex(input)
	if err != nil {
		t.Fatal(err)
	}
	assertIndexResults(t, indexResults, map[string][]int{input: {0}})
}

func TestInfoSuggestAndSuggestIndexReturnErrorsWhenRedisUnavailable(t *testing.T) {
	const input = "he"

	ac, mr := createAhoCorasick(t)
	defer func() { _ = ac.Close() }()

	if _, err := ac.Add(input); err != nil {
		t.Fatal(err)
	}

	mr.Close()

	if _, err := ac.Info(); err == nil {
		t.Fatal("expected info to return an error")
	}
	if _, err := ac.Suggest(input); err == nil {
		t.Fatal("expected suggest to return an error")
	}
	if _, err := ac.SuggestIndex(input); err == nil {
		t.Fatal("expected suggest index to return an error")
	}
}

func assertIndexResults(t *testing.T, actual, expected map[string][]int) {
	t.Helper()

	if len(actual) != len(expected) {
		t.Errorf("results' count is unexpected")
	}

	for keyword, expectedIndexes := range expected {
		actualIndexes, ok := actual[keyword]
		if !ok {
			t.Errorf("results are missing %s", keyword)
			continue
		}
		if len(actualIndexes) != len(expectedIndexes) {
			t.Errorf("results for %s have unexpected count", keyword)
			continue
		}
		for idx, actualIndex := range actualIndexes {
			if actualIndex != expectedIndexes[idx] {
				t.Errorf("results for %s have invalid index", keyword)
			}
		}
	}
}

func TestV2KeyHelpers(t *testing.T) {
	ac := &AhoCorasick{name: "test"}

	tests := []struct {
		name     string
		got      string
		expected string
	}{
		{"trieKey", ac.trieKey(), "{test}:trie"},
		{"outputsKey", ac.outputsKey(), "{test}:outputs"},
		{"nodesKey", ac.nodesKey(), "{test}:nodes"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("%s() = %s, want %s", tt.name, tt.got, tt.expected)
			}
		})
	}
}
