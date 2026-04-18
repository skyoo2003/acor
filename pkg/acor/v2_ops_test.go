// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"io"
	"log"
	"testing"

	redis "github.com/go-redis/redis/v8"
)

func TestV2Find(t *testing.T) {
	mr := createTestRedisServer(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	storage := newRedisStorage(client)
	ac := &AhoCorasick{
		redisClient:   client,
		storage:       storage,
		name:          "test",
		schemaVersion: SchemaV2,
	}
	ac.ops = &v2Operations{
		storage: storage,
		client:  client,
		name:    "test",
		logger:  log.New(io.Discard, "", 0),
	}

	client.HSet(context.Background(), "{test}:trie", map[string]interface{}{
		"keywords": `["he","she","his","hers"]`,
		"prefixes": `["","h","he","s","sh","she","hi","his","her","hers"]`,
		"suffixes": `["","e","eh","s","hs","ehs","i","ih","si","sih","r","reh","s","sreh"]`,
		"version":  "1234567890",
	})

	client.HSet(context.Background(), "{test}:outputs", map[string]interface{}{
		"he":   `["he"]`,
		"she":  `["he","she"]`,
		"his":  `["his"]`,
		"hers": `["hers"]`,
	})

	tests := []struct {
		input    string
		expected []string
	}{
		{"he", []string{"he"}},
		{"she", []string{"he", "she"}},
		{"hers", []string{"he", "hers"}},
		{"ushers", []string{"she", "he", "hers"}},
		{"xyz", []string{}},
		{"", []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ac.ops.find(context.Background(), tt.input)
			if err != nil {
				t.Fatalf("ops.find() error: %v", err)
			}
			if !equalStringSets(got, tt.expected) {
				t.Errorf("ops.find(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}
