package acor

import (
	"context"
	"errors"
	"fmt"
	"strings"
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

func createAhoCorasickV1(t *testing.T) (*AhoCorasick, *miniredis.Miniredis) {
	t.Helper()

	mr := createTestRedisServer(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	_ = client.ZAdd(context.Background(), "{test}:prefix", &redis.Z{Score: 0, Member: ""}).Err()
	_ = client.Close()

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

func TestInitAndFlushAndClose(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	if err := ac.Flush(); err != nil {
		t.Fatal(err)
	}
}

//nolint:funlen
func TestAdd(t *testing.T) {
	tests := []struct {
		name      string
		keywords  []string
		wantCount int
		wantErr   bool
		setupHook func(ac *AhoCorasick)
		cleanup   func(ac *AhoCorasick)
	}{
		{
			name:      "single keyword",
			keywords:  []string{"hello"},
			wantCount: 1,
			wantErr:   false,
		},
		{
			name:      "multiple keywords",
			keywords:  []string{"her", "he", "his"},
			wantCount: 3,
			wantErr:   false,
		},
		{
			name:      "unicode keywords",
			keywords:  []string{"한글", "日本語", "中文"},
			wantCount: 3,
			wantErr:   false,
		},
		{
			name:      "emoji keywords",
			keywords:  []string{"😀", "🎉", "🚀"},
			wantCount: 3,
			wantErr:   false,
		},
		{
			name:      "special characters",
			keywords:  []string{"@user", "#tag", "$var", "a*b+c"},
			wantCount: 4,
			wantErr:   false,
		},
		{
			name:      "duplicate keywords returns idempotent count",
			keywords:  []string{"test", "test", "test"},
			wantCount: 1,
			wantErr:   false,
		},
		{
			name:      "very long keyword",
			keywords:  []string{strings.Repeat("a", 1000)},
			wantCount: 1,
			wantErr:   false,
		},
		{
			name:      "whitespace keyword is trimmed",
			keywords:  []string{"   ", "\t", "\n"},
			wantCount: 0,
			wantErr:   false,
		},
		{
			name:      "mixed case keywords treated case-insensitively",
			keywords:  []string{"Hello", "HELLO", "hello"},
			wantCount: 1,
			wantErr:   false,
		},
		{
			name:      "keywords with numbers",
			keywords:  []string{"test1", "test2", "123"},
			wantCount: 3,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ac, mr := createAhoCorasick(t)
			defer mr.Close()
			defer func() { _ = ac.Close() }()
			defer func() { _ = ac.Flush() }()

			if tt.setupHook != nil {
				tt.setupHook(ac)
			}
			if tt.cleanup != nil {
				defer tt.cleanup(ac)
			}

			addedCount := 0
			for _, keyword := range tt.keywords {
				count, err := ac.Add(keyword)
				if tt.wantErr {
					if err == nil {
						t.Errorf("Add(%q) expected error, got nil", keyword)
					}
					return
				}
				if err != nil {
					t.Errorf("Add(%q) unexpected error: %v", keyword, err)
					return
				}
				addedCount += count
			}

			if addedCount != tt.wantCount {
				t.Errorf("Add() total count = %d, want %d", addedCount, tt.wantCount)
			}
		})
	}
}

func TestRemove(t *testing.T) {
	tests := []struct {
		name        string
		addFirst    []string
		remove      []string
		wantCount   int
		wantErr     bool
		findAfter   string
		wantFindLen int
	}{
		{
			name:        "remove single keyword",
			addFirst:    []string{"hello", "world"},
			remove:      []string{"hello"},
			wantCount:   1,
			wantErr:     false,
			findAfter:   "hello",
			wantFindLen: 0,
		},
		{
			name:      "remove multiple keywords",
			addFirst:  []string{"her", "he", "his"},
			remove:    []string{"her", "he", "his"},
			wantCount: 3,
			wantErr:   false,
		},
		{
			name:      "remove non-existent keyword returns remaining count",
			addFirst:  []string{"hello"},
			remove:    []string{"world"},
			wantCount: 1,
			wantErr:   false,
		},
		{
			name:      "remove unicode keywords returns remaining count",
			addFirst:  []string{"한글", "日本語", "中文"},
			remove:    []string{"한글"},
			wantCount: 2,
			wantErr:   false,
		},
		{
			name:      "remove duplicate keywords",
			addFirst:  []string{"test", "test"},
			remove:    []string{"test"},
			wantCount: 0,
			wantErr:   false,
		},
		{
			name:      "remove partial match",
			addFirst:  []string{"he", "her", "here"},
			remove:    []string{"he"},
			wantCount: 2,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ac, mr := createAhoCorasick(t)
			defer mr.Close()
			defer func() { _ = ac.Close() }()
			defer func() { _ = ac.Flush() }()

			for _, kw := range tt.addFirst {
				if _, err := ac.Add(kw); err != nil {
					t.Fatalf("Add(%q) error: %v", kw, err)
				}
			}

			removedCount := 0
			for _, kw := range tt.remove {
				count, err := ac.Remove(kw)
				if tt.wantErr {
					if err == nil {
						t.Errorf("Remove(%q) expected error, got nil", kw)
					}
					return
				}
				if err != nil {
					t.Errorf("Remove(%q) unexpected error: %v", kw, err)
					return
				}
				removedCount += count
			}

			if removedCount != tt.wantCount {
				t.Errorf("Remove() total count = %d, want %d", removedCount, tt.wantCount)
			}

			if tt.findAfter != "" {
				results, err := ac.Find(tt.findAfter)
				if err != nil {
					t.Fatalf("Find(%q) error: %v", tt.findAfter, err)
				}
				if len(results) != tt.wantFindLen {
					t.Errorf("Find(%q) after remove = %d results, want %d", tt.findAfter, len(results), tt.wantFindLen)
				}
			}
		})
	}
}

//nolint:funlen
func TestFind(t *testing.T) {
	tests := []struct {
		name      string
		keywords  []string
		input     string
		wantLen   int
		wantMatch []string
	}{
		{
			name:      "single match",
			keywords:  []string{"her", "he", "his"},
			input:     "he",
			wantLen:   1,
			wantMatch: []string{"he"},
		},
		{
			name:      "multiple matches",
			keywords:  []string{"her", "he", "his"},
			input:     "her",
			wantLen:   2,
			wantMatch: []string{"he", "her"},
		},
		{
			name:      "no match",
			keywords:  []string{"her", "he", "his"},
			input:     "xyz",
			wantLen:   0,
			wantMatch: nil,
		},
		{
			name:      "repeated pattern finds all occurrences",
			keywords:  []string{"he"},
			input:     "hehe",
			wantLen:   2,
			wantMatch: []string{"he"},
		},
		{
			name:      "unicode input",
			keywords:  []string{"한글"},
			input:     "가한글나",
			wantLen:   1,
			wantMatch: []string{"한글"},
		},
		{
			name:      "emoji input",
			keywords:  []string{"😀"},
			input:     "hello😀world",
			wantLen:   1,
			wantMatch: []string{"😀"},
		},
		{
			name:      "special characters",
			keywords:  []string{"@user", "#tag"},
			input:     "hello @user and #tag",
			wantLen:   2,
			wantMatch: []string{"@user", "#tag"},
		},
		{
			name:      "empty input",
			keywords:  []string{"test"},
			input:     "",
			wantLen:   0,
			wantMatch: nil,
		},
		{
			name:      "long input",
			keywords:  []string{"needle"},
			input:     strings.Repeat("haystack ", 100) + "needle",
			wantLen:   1,
			wantMatch: []string{"needle"},
		},
		{
			name:      "overlapping keywords",
			keywords:  []string{"a", "ab", "abc"},
			input:     "abc",
			wantLen:   3,
			wantMatch: []string{"a", "ab", "abc"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ac, mr := createAhoCorasick(t)
			defer mr.Close()
			defer func() { _ = ac.Close() }()
			defer func() { _ = ac.Flush() }()

			for _, kw := range tt.keywords {
				if _, err := ac.Add(kw); err != nil {
					t.Fatalf("Add(%q) error: %v", kw, err)
				}
			}

			results, err := ac.Find(tt.input)
			if err != nil {
				t.Fatalf("Find(%q) error: %v", tt.input, err)
			}

			if len(results) != tt.wantLen {
				t.Errorf("Find(%q) = %d results, want %d", tt.input, len(results), tt.wantLen)
			}

			if tt.wantMatch != nil {
				for _, want := range tt.wantMatch {
					found := false
					for _, got := range results {
						if got == want {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("Find(%q) missing expected match %q", tt.input, want)
					}
				}
			}
		})
	}
}

func TestFindIndex(t *testing.T) {
	tests := []struct {
		name     string
		keywords []string
		input    string
		want     map[string][]int
	}{
		{
			name:     "single match at start",
			keywords: []string{"he"},
			input:    "he",
			want:     map[string][]int{"he": {0}},
		},
		{
			name:     "overlapping matches",
			keywords: []string{"her", "he", "his"},
			input:    "her",
			want:     map[string][]int{"he": {0}, "her": {0}},
		},
		{
			name:     "repeated pattern",
			keywords: []string{"he"},
			input:    "hehe",
			want:     map[string][]int{"he": {0, 2}},
		},
		{
			name:     "no match",
			keywords: []string{"test"},
			input:    "xyz",
			want:     map[string][]int{},
		},
		{
			name:     "unicode match",
			keywords: []string{"한글"},
			input:    "가한글",
			want:     map[string][]int{"한글": {1}},
		},
		{
			name:     "multiple occurrences",
			keywords: []string{"ab"},
			input:    "ababab",
			want:     map[string][]int{"ab": {0, 2, 4}},
		},
		{
			name:     "emoji match",
			keywords: []string{"😀"},
			input:    "a😀b😀c",
			want:     map[string][]int{"😀": {1, 3}},
		},
		{
			name:     "nested keywords",
			keywords: []string{"a", "aa", "aaa"},
			input:    "aaa",
			want:     map[string][]int{"a": {0, 1, 2}, "aa": {0, 1}, "aaa": {0}},
		},
		{
			name:     "empty input",
			keywords: []string{"test"},
			input:    "",
			want:     map[string][]int{},
		},
		{
			name:     "special characters",
			keywords: []string{"@@"},
			input:    "@@@@",
			want:     map[string][]int{"@@": {0, 1, 2}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ac, mr := createAhoCorasick(t)
			defer mr.Close()
			defer func() { _ = ac.Close() }()
			defer func() { _ = ac.Flush() }()

			for _, kw := range tt.keywords {
				if _, err := ac.Add(kw); err != nil {
					t.Fatalf("Add(%q) error: %v", kw, err)
				}
			}

			results, err := ac.FindIndex(tt.input)
			if err != nil {
				t.Fatalf("FindIndex(%q) error: %v", tt.input, err)
			}

			assertIndexResults(t, results, tt.want)
		})
	}
}

func TestSuggest(t *testing.T) {
	tests := []struct {
		name      string
		keywords  []string
		input     string
		wantLen   int
		wantMatch []string
	}{
		{
			name:      "single suggestion",
			keywords:  []string{"he"},
			input:     "h",
			wantLen:   1,
			wantMatch: []string{"he"},
		},
		{
			name:      "multiple suggestions",
			keywords:  []string{"her", "he", "his"},
			input:     "he",
			wantLen:   2,
			wantMatch: []string{"he", "her"},
		},
		{
			name:      "no suggestions",
			keywords:  []string{"test"},
			input:     "xyz",
			wantLen:   0,
			wantMatch: nil,
		},
		{
			name:      "exact match",
			keywords:  []string{"hello"},
			input:     "hello",
			wantLen:   1,
			wantMatch: []string{"hello"},
		},
		{
			name:      "unicode suggestions",
			keywords:  []string{"한글", "한국"},
			input:     "한",
			wantLen:   2,
			wantMatch: []string{"한글", "한국"},
		},
		{
			name:      "empty input",
			keywords:  []string{"test"},
			input:     "",
			wantLen:   0,
			wantMatch: nil,
		},
		{
			name:      "common prefix",
			keywords:  []string{"app", "apple", "application", "apply"},
			input:     "app",
			wantLen:   4,
			wantMatch: []string{"app", "apple", "application", "apply"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ac, mr := createAhoCorasick(t)
			defer mr.Close()
			defer func() { _ = ac.Close() }()
			defer func() { _ = ac.Flush() }()

			for _, kw := range tt.keywords {
				if _, err := ac.Add(kw); err != nil {
					t.Fatalf("Add(%q) error: %v", kw, err)
				}
			}

			results, err := ac.Suggest(tt.input)
			if err != nil {
				t.Fatalf("Suggest(%q) error: %v", tt.input, err)
			}

			if len(results) != tt.wantLen {
				t.Errorf("Suggest(%q) = %d results, want %d", tt.input, len(results), tt.wantLen)
			}

			if tt.wantMatch != nil {
				for _, want := range tt.wantMatch {
					found := false
					for _, got := range results {
						if got == want {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("Suggest(%q) missing expected match %q", tt.input, want)
					}
				}
			}
		})
	}
}

func TestSuggestIndex(t *testing.T) {
	tests := []struct {
		name     string
		keywords []string
		input    string
		want     map[string][]int
	}{
		{
			name:     "single suggestion",
			keywords: []string{"he"},
			input:    "h",
			want:     map[string][]int{"he": {0}},
		},
		{
			name:     "multiple suggestions",
			keywords: []string{"her", "he", "his"},
			input:    "he",
			want:     map[string][]int{"he": {0}, "her": {0}},
		},
		{
			name:     "no suggestions",
			keywords: []string{"test"},
			input:    "xyz",
			want:     map[string][]int{},
		},
		{
			name:     "unicode suggestions",
			keywords: []string{"한글"},
			input:    "한",
			want:     map[string][]int{"한글": {0}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ac, mr := createAhoCorasick(t)
			defer mr.Close()
			defer func() { _ = ac.Close() }()
			defer func() { _ = ac.Flush() }()

			for _, kw := range tt.keywords {
				if _, err := ac.Add(kw); err != nil {
					t.Fatalf("Add(%q) error: %v", kw, err)
				}
			}

			results, err := ac.SuggestIndex(tt.input)
			if err != nil {
				t.Fatalf("SuggestIndex(%q) error: %v", tt.input, err)
			}

			assertIndexResults(t, results, tt.want)
		})
	}
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
	ac, mr := createAhoCorasickV1(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	if _, err := ac.Add("he"); err != nil {
		t.Fatal(err)
	}

	keys := []string{
		keywordKey(ac.name),
		prefixKey(ac.name),
		suffixKey(ac.name),
		outputKey(ac.name, "he"),
		nodeKey(ac.name, "he"),
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

	ac, mr := createAhoCorasickV1(t)
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

	ac, mr := createAhoCorasickV1(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	if _, err := ac.Add(input); err != nil {
		t.Fatal(err)
	}

	pKey := prefixKey(ac.name)
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

	ac, mr := createAhoCorasickV1(t)
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

func TestV2KeyHelpers(t *testing.T) {
	ac := &AhoCorasick{name: "test"}

	tests := []struct {
		name     string
		got      string
		expected string
	}{
		{"trieKey", trieKey(ac.name), "{test}:trie"},
		{"outputsKey", outputsKey(ac.name), "{test}:outputs"},
		{"nodesKey", nodesKey(ac.name), "{test}:nodes"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("%s() = %s, want %s", tt.name, tt.got, tt.expected)
			}
		})
	}
}

func TestV1V2Compatibility(t *testing.T) {
	mr := createTestRedisServer(t)

	keywords := []string{"he", "she", "his", "hers", "hello"}
	testTexts := []string{
		"he",
		"she is here",
		"this is his",
		"hers is better",
		"hello world",
		"ushers",
	}

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	_ = client.ZAdd(context.Background(), "{v1test}:prefix", &redis.Z{Score: 0, Member: ""}).Err()
	_ = client.Close()

	args := &AhoCorasickArgs{Addr: mr.Addr(), Name: "v1test", SchemaVersion: SchemaV1}
	acV1, err := Create(args)
	if err != nil {
		t.Fatal(err)
	}

	for _, kw := range keywords {
		_, _ = acV1.Add(kw)
	}

	v1Results := make(map[string][]string)
	for _, text := range testTexts {
		v1Results[text], _ = acV1.Find(text)
	}
	_ = acV1.Close()

	args = &AhoCorasickArgs{Addr: mr.Addr(), Name: "v1test", SchemaVersion: SchemaV1}
	acMigrate, err := Create(args)
	if err != nil {
		t.Fatal(err)
	}

	_, err = acMigrate.MigrateV1ToV2(nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = acMigrate.Close()

	args = &AhoCorasickArgs{Addr: mr.Addr(), Name: "v1test"}
	acV2, err := Create(args)
	if err != nil {
		t.Fatal(err)
	}

	v2Results := make(map[string][]string)
	for _, text := range testTexts {
		v2Results[text], _ = acV2.Find(text)
	}
	_ = acV2.Close()

	for _, text := range testTexts {
		if !equalStringSets(v1Results[text], v2Results[text]) {
			t.Errorf("Results differ for %q:\n  V1: %v\n  V2: %v", text, v1Results[text], v2Results[text])
		}
	}
}

func TestEndToEndV2(t *testing.T) { //nolint:gocyclo // Integration test with multiple scenarios
	mr := createTestRedisServer(t)

	args := &AhoCorasickArgs{
		Addr: mr.Addr(),
		Name: "e2e",
	}

	ac, err := Create(args)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ac.Close() }()

	if ac.SchemaVersion() != SchemaV2 {
		t.Errorf("SchemaVersion() = %d, want %d", ac.SchemaVersion(), SchemaV2)
	}

	keywords := []string{"apple", "application", "apply", "banana"}
	for _, kw := range keywords {
		count, addErr := ac.Add(kw)
		if addErr != nil {
			t.Fatalf("Add(%s) error: %v", kw, addErr)
		}
		if count != 1 {
			t.Errorf("Add(%s) = %d, want 1", kw, count)
		}
	}

	matches, err := ac.Find("I have an apple application")
	if err != nil {
		t.Fatal(err)
	}
	if !containsAll(matches, "apple", "application") {
		t.Errorf("Find() = %v, should contain apple, application", matches)
	}

	suggestions, err := ac.Suggest("app")
	if err != nil {
		t.Fatal(err)
	}
	if !containsAll(suggestions, "apple", "application", "apply") {
		t.Errorf("Suggest(app) = %v, should contain apple, application, apply", suggestions)
	}

	info, err := ac.Info()
	if err != nil {
		t.Fatal(err)
	}
	if info.Keywords != 4 {
		t.Errorf("Info.Keywords = %d, want 4", info.Keywords)
	}

	count, err := ac.Remove("apple")
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Errorf("Remove(apple) = %d, want 3 (remaining)", count)
	}

	matches, _ = ac.Find("I have an apple")
	if containsAll(matches, "apple") {
		t.Error("Find should not match 'apple' after removal")
	}

	if err := ac.Flush(); err != nil {
		t.Fatal(err)
	}

	info, _ = ac.Info()
	if info.Keywords != 0 {
		t.Errorf("After Flush, Keywords = %d, want 0", info.Keywords)
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

func TestV1Info(t *testing.T) {
	ac, mr := createAhoCorasickV1(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	info, err := ac.Info()
	if err != nil {
		t.Fatalf("Info() error: %v", err)
	}
	if info.Keywords != 0 {
		t.Errorf("Info().Keywords = %d, want 0", info.Keywords)
	}

	keywords := []string{"he", "she", "his"}
	for _, kw := range keywords {
		if _, addErr := ac.Add(kw); addErr != nil {
			t.Fatal(addErr)
		}
	}

	info, err = ac.Info()
	if err != nil {
		t.Fatalf("Info() after add error: %v", err)
	}
	if info.Keywords != 3 {
		t.Errorf("Info().Keywords = %d, want 3", info.Keywords)
	}
	if info.Nodes == 0 {
		t.Error("Info().Nodes should be > 0")
	}
}

func TestV1Suggest(t *testing.T) {
	ac, mr := createAhoCorasickV1(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	keywords := []string{"he", "she", "hello", "help", "her"}
	for _, kw := range keywords {
		if _, addErr := ac.Add(kw); addErr != nil {
			t.Fatal(addErr)
		}
	}

	tests := []struct {
		name     string
		input    string
		wantSome []string
		wantNone bool
	}{
		{"prefix he", "he", []string{"he", "hello", "help", "her"}, false},
		{"prefix sh", "sh", []string{"she"}, false},
		{"no match", "xyz", nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := ac.Suggest(tt.input)
			if err != nil {
				t.Fatalf("Suggest(%q) error: %v", tt.input, err)
			}
			if tt.wantNone {
				if len(results) != 0 {
					t.Errorf("Suggest(%q) = %v, want empty", tt.input, results)
				}
				return
			}
			for _, want := range tt.wantSome {
				if !containsAll(results, want) {
					t.Errorf("Suggest(%q) = %v, missing %q", tt.input, results, want)
				}
			}
		})
	}
}

func TestV1SuggestIndex(t *testing.T) {
	ac, mr := createAhoCorasickV1(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	keywords := []string{"he", "she", "hello"}
	for _, kw := range keywords {
		if _, err := ac.Add(kw); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		name     string
		input    string
		wantSome []string
		wantNone bool
	}{
		{"prefix he", "he", []string{"he", "hello"}, false},
		{"no match", "xyz", nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := ac.SuggestIndex(tt.input)
			if err != nil {
				t.Fatalf("SuggestIndex(%q) error: %v", tt.input, err)
			}
			if tt.wantNone {
				if len(results) != 0 {
					t.Errorf("SuggestIndex(%q) = %v, want empty", tt.input, results)
				}
				return
			}
			for _, want := range tt.wantSome {
				if _, ok := results[want]; !ok {
					t.Errorf("SuggestIndex(%q) missing %q", tt.input, want)
				}
			}
		})
	}
}

func TestV1Remove(t *testing.T) {
	ac, mr := createAhoCorasickV1(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	keywords := []string{"he", "she", "hello"}
	for _, kw := range keywords {
		if _, err := ac.Add(kw); err != nil {
			t.Fatal(err)
		}
	}

	count, err := ac.Remove("he")
	if err != nil {
		t.Fatalf("Remove(he) error: %v", err)
	}
	if count != 2 {
		t.Errorf("Remove(he) = %d, want 2 (remaining)", count)
	}

	matches, err := ac.Find("he she")
	if err != nil {
		t.Fatal(err)
	}
	if containsAll(matches, "he") {
		t.Error("Find should not match 'he' after removal")
	}
	if !containsAll(matches, "she") {
		t.Error("Find should still match 'she'")
	}
}

func TestV2Info(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	info, err := ac.Info()
	if err != nil {
		t.Fatalf("Info() error: %v", err)
	}
	if info.Keywords != 0 {
		t.Errorf("Info().Keywords = %d, want 0", info.Keywords)
	}

	keywords := []string{"foo", "bar", "baz"}
	for _, kw := range keywords {
		if _, addErr := ac.Add(kw); addErr != nil {
			t.Fatal(addErr)
		}
	}

	info, err = ac.Info()
	if err != nil {
		t.Fatalf("Info() after add error: %v", err)
	}
	if info.Keywords != 3 {
		t.Errorf("Info().Keywords = %d, want 3", info.Keywords)
	}
}

func TestV2Suggest(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	keywords := []string{"apple", "application", "apply", "banana"}
	for _, kw := range keywords {
		if _, addErr := ac.Add(kw); addErr != nil {
			t.Fatal(addErr)
		}
	}

	tests := []struct {
		name     string
		input    string
		wantSome []string
	}{
		{"prefix app", "app", []string{"apple", "application", "apply"}},
		{"prefix ban", "ban", []string{"banana"}},
		{"no match", "xyz", nil},
		{"empty string", "", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := ac.Suggest(tt.input)
			if err != nil {
				t.Fatalf("Suggest(%q) error: %v", tt.input, err)
			}
			if tt.wantSome == nil {
				if len(results) != 0 {
					t.Errorf("Suggest(%q) = %v, want empty", tt.input, results)
				}
				return
			}
			for _, want := range tt.wantSome {
				if !containsAll(results, want) {
					t.Errorf("Suggest(%q) = %v, missing %q", tt.input, results, want)
				}
			}
		})
	}
}

func TestV2Remove(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	keywords := []string{"foo", "bar", "baz"}
	for _, kw := range keywords {
		if _, err := ac.Add(kw); err != nil {
			t.Fatal(err)
		}
	}

	count, err := ac.Remove("foo")
	if err != nil {
		t.Fatalf("Remove(foo) error: %v", err)
	}
	if count != 2 {
		t.Errorf("Remove(foo) = %d, want 2 (remaining)", count)
	}

	matches, err := ac.Find("foo bar")
	if err != nil {
		t.Fatal(err)
	}
	if containsAll(matches, "foo") {
		t.Error("Find should not match 'foo' after removal")
	}
	if !containsAll(matches, "bar") {
		t.Error("Find should still match 'bar'")
	}
}

func TestMigrationResultStats(t *testing.T) {
	result := &MigrationResult{
		Keywords:    10,
		Prefixes:    50,
		OutputsKeys: 10,
		NodesKeys:   50,
		KeysBefore:  120,
		KeysAfter:   2,
	}

	stats := result.Stats()
	if stats["keywords"] != 10 {
		t.Errorf("Stats[keywords] = %v, want 10", stats["keywords"])
	}
	if stats["prefixes"] != 50 {
		t.Errorf("Stats[prefixes] = %v, want 50", stats["prefixes"])
	}
	if stats["keys_before"] != 120 {
		t.Errorf("Stats[keys_before] = %v, want 120", stats["keys_before"])
	}
	if stats["keys_after"] != 2 {
		t.Errorf("Stats[keys_after] = %v, want 2", stats["keys_after"])
	}
}

func TestDebug(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	if _, err := ac.Add("test"); err != nil {
		t.Fatal(err)
	}

	ac.Debug()
}

func TestDefaultParallelOptions(t *testing.T) {
	opts := DefaultParallelOptions()
	if opts == nil {
		t.Fatal("DefaultParallelOptions() returned nil")
	}
	if opts.Workers <= 0 {
		t.Errorf("Workers = %d, want > 0", opts.Workers)
	}
	if opts.ChunkSize != DefaultChunkSize {
		t.Errorf("ChunkSize = %d, want %d", opts.ChunkSize, DefaultChunkSize)
	}
	if opts.Boundary != ChunkBoundaryWord {
		t.Errorf("Boundary = %d, want %d", opts.Boundary, ChunkBoundaryWord)
	}
	if opts.Overlap != DefaultOverlap {
		t.Errorf("Overlap = %d, want %d", opts.Overlap, DefaultOverlap)
	}
}

//nolint:gocyclo,funlen
func TestStorageAdapterMethods(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	storage := newRedisStorage(client)
	ctx := context.Background()

	t.Run("SRem", func(t *testing.T) {
		_ = storage.SAdd(ctx, "set", "a", "b")
		if err := storage.SRem(ctx, "set", "a"); err != nil {
			t.Errorf("SRem() error: %v", err)
		}
	})

	t.Run("SCard", func(t *testing.T) {
		count, err := storage.SCard(ctx, "set")
		if err != nil {
			t.Errorf("SCard() error: %v", err)
		}
		if count != 1 {
			t.Errorf("SCard() = %d, want 1", count)
		}
	})

	t.Run("SIsMember", func(t *testing.T) {
		isMember, err := storage.SIsMember(ctx, "set", "b")
		if err != nil {
			t.Errorf("SIsMember() error: %v", err)
		}
		if !isMember {
			t.Error("SIsMember() = false, want true")
		}
	})

	t.Run("ZAdd", func(t *testing.T) {
		if err := storage.ZAdd(ctx, "zset2", &Z{Score: 1.0, Member: "a"}); err != nil {
			t.Errorf("ZAdd() error: %v", err)
		}
	})

	t.Run("ZRank", func(t *testing.T) {
		rank, err := storage.ZRank(ctx, "zset2", "a")
		if err != nil {
			t.Errorf("ZRank() error: %v", err)
		}
		if rank != 0 {
			t.Errorf("ZRank() = %d, want 0", rank)
		}
	})

	t.Run("ZScore", func(t *testing.T) {
		score, err := storage.ZScore(ctx, "zset2", "a")
		if err != nil {
			t.Errorf("ZScore() error: %v", err)
		}
		if score != 1.0 {
			t.Errorf("ZScore() = %f, want 1.0", score)
		}
	})

	t.Run("ZCard", func(t *testing.T) {
		count, err := storage.ZCard(ctx, "zset2")
		if err != nil {
			t.Errorf("ZCard() error: %v", err)
		}
		if count != 1 {
			t.Errorf("ZCard() = %d, want 1", count)
		}
	})

	t.Run("ZRem", func(t *testing.T) {
		if err := storage.ZRem(ctx, "zset2", "a"); err != nil {
			t.Errorf("ZRem() error: %v", err)
		}
	})

	t.Run("Exists", func(t *testing.T) {
		_ = storage.Set(ctx, "existskey", "value")
		count, err := storage.Exists(ctx, "existskey")
		if err != nil {
			t.Errorf("Exists() error: %v", err)
		}
		if count != 1 {
			t.Errorf("Exists() = %d, want 1", count)
		}
	})

	t.Run("TxPipelined", func(t *testing.T) {
		err := storage.TxPipelined(ctx, func(pipe Pipeliner) error {
			_ = pipe.SAdd(ctx, "txset", "member")
			_ = pipe.HSet(ctx, "txhash", "field", "value")
			_ = pipe.ZAdd(ctx, "txzset", &Z{Score: 1.0, Member: "a"})
			return nil
		})
		if err != nil {
			t.Errorf("TxPipelined() error: %v", err)
		}
	})

	t.Run("Close", func(t *testing.T) {
		if err := storage.Close(); err != nil {
			t.Errorf("Close() error: %v", err)
		}
	})
}

func TestMigrationErrorPaths(t *testing.T) {
	ac, mr := createAhoCorasickV1(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	keywords := []string{"test1", "test2"}
	for _, kw := range keywords {
		if _, err := ac.Add(kw); err != nil {
			t.Fatal(err)
		}
	}

	result, err := ac.MigrateV1ToV2(nil)
	if err != nil {
		t.Fatalf("MigrateV1ToV2() error: %v", err)
	}
	if result == nil {
		t.Fatal("MigrateV1ToV2() returned nil result")
	}
	if result.Keywords != 2 {
		t.Errorf("MigrateV1ToV2().Keywords = %d, want 2", result.Keywords)
	}
}

func TestV2RemoveNonExistent(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	if _, err := ac.Add("keep"); err != nil {
		t.Fatal(err)
	}

	count, err := ac.Remove("nonexistent")
	if err != nil {
		t.Fatalf("Remove(nonexistent) error: %v", err)
	}
	if count != 1 {
		t.Errorf("Remove(nonexistent) = %d, want 1 (remaining)", count)
	}
}

func TestV2RemoveConcurrencyRetry(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	for i := 0; i < 10; i++ {
		if _, err := ac.Add(fmt.Sprintf("keyword%d", i)); err != nil {
			t.Fatal(err)
		}
	}

	count, err := ac.Remove("keyword5")
	if err != nil {
		t.Fatalf("Remove() error: %v", err)
	}
	if count != 9 {
		t.Errorf("Remove() = %d, want 9 (remaining)", count)
	}
}

func TestStorageDel(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	storage := newRedisStorage(client)
	ctx := context.Background()

	_ = storage.Set(ctx, "delkey", "value")

	if delErr := storage.Del(ctx, "delkey"); delErr != nil {
		t.Errorf("Del() error: %v", delErr)
	}

	_, err = storage.Get(ctx, "delkey")
	if err == nil {
		t.Error("Get() should fail for deleted key")
	}
}

func TestIsBoundaryEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		idx      int
		boundary ChunkBoundary
		want     bool
	}{
		{"word boundary at space", "hello world", 5, ChunkBoundaryWord, true},
		{"word boundary at world", "hello world", 6, ChunkBoundaryWord, false},
		{"not word boundary", "hello world", 3, ChunkBoundaryWord, false},
		{"line boundary", "hello\nworld", 6, ChunkBoundaryLine, true},
		{"not line boundary", "hello world", 5, ChunkBoundaryLine, false},
		{"sentence boundary at space", "hello. world", 6, ChunkBoundarySentence, true},
		{"not sentence boundary", "hello world", 5, ChunkBoundarySentence, false},
		{"index 0", "hello", 0, ChunkBoundaryWord, false},
		{"index at end", "hello", 5, ChunkBoundaryWord, false},
		{"invalid boundary type", "hello", 2, ChunkBoundary(99), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runes := []rune(tt.text)
			got := isBoundary(runes, tt.idx, tt.boundary)
			if got != tt.want {
				t.Errorf("isBoundary(%q, %d, %v) = %v, want %v", tt.text, tt.idx, tt.boundary, got, tt.want)
			}
		})
	}
}

func TestCloseWithNilClient(t *testing.T) {
	ac := &AhoCorasick{redisClient: nil}
	if err := ac.Close(); err != ErrRedisAlreadyClosed {
		t.Errorf("Close() with nil client error = %v, want ErrRedisAlreadyClosed", err)
	}
}

func TestFindParallelEdgeCases(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	keywords := []string{"test", "hello", "world"}
	for _, kw := range keywords {
		if _, err := ac.Add(kw); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		name string
		text string
		opts *ParallelOptions
	}{
		{"empty text", "", nil},
		{"short text", "test", nil},
		{"line boundaries", "test\nhello\nworld", &ParallelOptions{Workers: 2, ChunkSize: 5, Boundary: ChunkBoundaryLine}},
		{"sentence boundaries", "Test. Hello world.", &ParallelOptions{Workers: 2, ChunkSize: 10, Boundary: ChunkBoundarySentence}},
		{"nil options uses defaults", "test hello world", nil},
		{"single worker", "test hello world", &ParallelOptions{Workers: 1, ChunkSize: 100}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ac.FindParallel(tt.text, tt.opts)
			if err != nil {
				t.Errorf("FindParallel() error: %v", err)
			}
		})
	}
}

func TestFindIndexParallelEdgeCases(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	keywords := []string{"test", "hello"}
	for _, kw := range keywords {
		if _, err := ac.Add(kw); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		name string
		text string
	}{
		{"empty text", ""},
		{"short text", "test"},
		{"long text", strings.Repeat("test hello ", 100)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ac.FindIndexParallel(tt.text, nil)
			if err != nil {
				t.Errorf("FindIndexParallel() error: %v", err)
			}
		})
	}
}
