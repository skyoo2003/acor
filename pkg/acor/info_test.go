// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"testing"

	redis "github.com/redis/go-redis/v9"
)

// TestInfoWithSchemaVersion merges TestV1Info and TestV2Info into
// a single table-driven test parameterized by schema version.
func TestInfoWithSchemaVersion(t *testing.T) {
	tests := []struct {
		name       string
		schema     int
		keywords   []string
		wantKw     int
		checkNodes bool
	}{
		{name: "v1", schema: SchemaV1, keywords: []string{"he", "she", "his"}, wantKw: 3, checkNodes: true},
		{name: "v2", schema: SchemaV2, keywords: []string{"foo", "bar", "baz"}, wantKw: 3, checkNodes: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ac, mr := createAhoCorasickWithSchema(t, tt.schema)
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

			for _, kw := range tt.keywords {
				if _, addErr := ac.Add(kw); addErr != nil {
					t.Fatal(addErr)
				}
			}

			info, err = ac.Info()
			if err != nil {
				t.Fatalf("Info() after add error: %v", err)
			}
			if info.Keywords != tt.wantKw {
				t.Errorf("Info().Keywords = %d, want %d", info.Keywords, tt.wantKw)
			}
			if tt.checkNodes && info.Nodes == 0 {
				t.Error("Info().Nodes should be > 0")
			}
		})
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

func TestInfoSuggestAndSuggestIndexReturnErrorsWhenRedisUnavailable(t *testing.T) {
	ac, mr := createAhoCorasickV1(t)
	defer func() { _ = ac.Close() }()

	if _, err := ac.Add(testKeywordHE); err != nil {
		t.Fatal(err)
	}

	mr.Close()

	if _, err := ac.Info(); err == nil {
		t.Fatal("expected info to return an error")
	}
	if _, err := ac.Suggest(testKeywordHE); err == nil {
		t.Fatal("expected suggest to return an error")
	}
	if _, err := ac.SuggestIndex(testKeywordHE); err == nil {
		t.Fatal("expected suggest index to return an error")
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
	if err := client.ZAdd(context.Background(), "{v1test}:prefix", redis.Z{Score: 0, Member: ""}).Err(); err != nil {
		t.Fatalf("failed to seed V1 prefix: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("failed to close seed client: %v", err)
	}

	args := &AhoCorasickArgs{Addr: mr.Addr(), Name: "v1test", SchemaVersion: SchemaV1}
	acV1, err := Create(args)
	if err != nil {
		t.Fatal(err)
	}

	for _, kw := range keywords {
		if _, addErr := acV1.Add(kw); addErr != nil {
			t.Fatalf("V1 Add(%q) error: %v", kw, addErr)
		}
	}

	v1Results := make(map[string][]string)
	for _, text := range testTexts {
		results, findErr := acV1.Find(text)
		if findErr != nil {
			t.Fatalf("V1 Find(%q) error: %v", text, findErr)
		}
		v1Results[text] = results
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
		results, err := acV2.Find(text)
		if err != nil {
			t.Fatalf("V2 Find(%q) error: %v", text, err)
		}
		v2Results[text] = results
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
	if count != 1 {
		t.Errorf("Remove(apple) = %d, want 1", count)
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

func TestMigrationSuccess(t *testing.T) {
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
