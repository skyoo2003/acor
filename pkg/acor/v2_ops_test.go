package acor

import (
	"context"
	"testing"

	redis "github.com/go-redis/redis/v8"
)

func TestV2Find(t *testing.T) {
	mr := createTestRedisServer(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	ac := &AhoCorasick{
		redisClient:   client,
		ctx:           context.Background(),
		name:          "test",
		schemaVersion: SchemaV2,
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
			got, err := ac.findV2(tt.input)
			if err != nil {
				t.Fatalf("findV2() error: %v", err)
			}
			if !equalStringSlices(got, tt.expected) {
				t.Errorf("findV2(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func equalStringSlices(a, b []string) bool {
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
