// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"errors"
	"sort"
	"testing"

	miniredis "github.com/alicebob/miniredis/v2"
	redis "github.com/go-redis/redis/v8"

	kvstore "github.com/skyoo2003/acor/internal/storage"
)

// newInjectedStorage returns a KVStorage passed through the public Storage
// field (not the built-in wiring), backed by an isolated miniredis.
func newInjectedStorage(t *testing.T) kvstore.KVStorage {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return kvstore.NewRedisStorage(client)
}

func TestCustomStorageV1EndToEnd(t *testing.T) {
	ac, err := Create(&AhoCorasickArgs{
		Name:          "custom",
		SchemaVersion: SchemaV1,
		Storage:       newInjectedStorage(t),
	})
	if err != nil {
		t.Fatalf("Create with custom storage: %v", err)
	}
	defer func() { _ = ac.Close() }()

	// The injected backend must bypass the built-in Redis client entirely.
	if ac.redisClient != nil {
		t.Fatal("redisClient should be nil when a custom Storage is injected")
	}

	for _, kw := range []string{"he", "she", "his", "hers"} {
		if _, addErr := ac.Add(kw); addErr != nil {
			t.Fatalf("Add(%q): %v", kw, addErr)
		}
	}

	got, err := ac.Find("ushers")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	sort.Strings(got)
	want := []string{"he", "hers", "she"}
	if len(got) != len(want) {
		t.Fatalf("Find(ushers) = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Find(ushers) = %v, want %v", got, want)
		}
	}

	if _, remErr := ac.Remove("she"); remErr != nil {
		t.Fatalf("Remove: %v", remErr)
	}
	if got2, _ := ac.Find("ushers"); len(got2) != 2 {
		t.Fatalf("after Remove, Find(ushers) = %v, want 2 matches", got2)
	}
}

func TestCustomStorageMigrationUnavailable(t *testing.T) {
	ac, err := Create(&AhoCorasickArgs{
		Name:          "custom-mig",
		SchemaVersion: SchemaV1,
		Storage:       newInjectedStorage(t),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer func() { _ = ac.Close() }()

	if _, err := ac.MigrateV1ToV2(nil); !errors.Is(err, ErrMigrationRequiresRedis) {
		t.Fatalf("MigrateV1ToV2 = %v, want ErrMigrationRequiresRedis", err)
	}
	if err := ac.RollbackToV1(); !errors.Is(err, ErrMigrationRequiresRedis) {
		t.Fatalf("RollbackToV1 = %v, want ErrMigrationRequiresRedis", err)
	}
}

func TestCustomStorageRejectsUnsupportedConfig(t *testing.T) {
	tests := []struct {
		name string
		args *AhoCorasickArgs
		want error
	}{
		{
			name: "V2 schema",
			args: &AhoCorasickArgs{Name: "c", SchemaVersion: SchemaV2, Storage: newInjectedStorage(t)},
			want: ErrCustomStorageRequiresV1,
		},
		{
			name: "default schema (V2)",
			args: &AhoCorasickArgs{Name: "c", Storage: newInjectedStorage(t)},
			want: ErrCustomStorageRequiresV1,
		},
		{
			name: "preset set",
			args: &AhoCorasickArgs{Name: "c", SchemaVersion: SchemaV1, Preset: PresetSpeed, Storage: newInjectedStorage(t)},
			want: ErrCustomStorageRequiresV1,
		},
		{
			name: "cache enabled",
			args: &AhoCorasickArgs{Name: "c", SchemaVersion: SchemaV1, EnableCache: true, Storage: newInjectedStorage(t)},
			want: ErrCacheRequiresV2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ac, err := Create(tt.args)
			if !errors.Is(err, tt.want) {
				if ac != nil {
					_ = ac.Close()
				}
				t.Fatalf("Create = %v, want %v", err, tt.want)
			}
		})
	}
}
