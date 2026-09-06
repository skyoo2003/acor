// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	redis "github.com/redis/go-redis/v9"
)

// TestVersionedMillionSafety verifies pinned paging, conflicting commits and
// lease-safe pruning at one million entries on the supplied real endpoint.
//
//nolint:gocyclo,funlen // The acceptance scenario retains one million-entry snapshot throughout.
func TestVersionedMillionSafety(t *testing.T) {
	addr := os.Getenv("ACOR_V3_SCALE_ADDR")
	if addr == "" {
		t.Skip("requires real disposable server")
	}
	ctx := context.Background()
	v, err := OpenVersioned(ctx, &VersionedOptions{Redis: AhoCorasickArgs{Addr: addr, Name: "million-safety-" + v3ID()}})
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	defer func() {
		var cursor uint64
		for {
			keys, next, e := v.client.Scan(ctx, cursor, v.prefix+"*", 256).Result()
			if e != nil {
				return
			}
			if len(keys) > 0 {
				v.client.Del(ctx, keys...)
			}
			cursor = next
			if cursor == 0 {
				break
			}
		}
	}()
	words := v3ScaleWords(1000000, "shared")
	r, err := v.Replace(ctx, v.Status().ServingVersion, words)
	if err != nil {
		t.Fatal(err)
	}
	waitV3(t, v, r.Version)
	s, err := v.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close(ctx)
	var wg sync.WaitGroup
	outcomes := make(chan error, 2)
	for _, w := range []string{"competitor-a", "competitor-b"} {
		wg.Go(func() { _, e := v.Add(ctx, r.Version, w); outcomes <- e })
	}
	wg.Wait()
	close(outcomes)
	success, conflict := 0, 0
	for e := range outcomes {
		if e == nil {
			success++
		} else if errors.Is(e, ErrConcurrencyConflict) {
			conflict++
		} else {
			t.Fatal(e)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal(success, conflict)
	}
	active, err := v.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	waitV3(t, v, active.Version())
	_ = active.Close(ctx)
	gens, _ := v.client.ZRange(ctx, v.key("generations"), 0, -1).Result()
	for _, g := range gens {
		v.client.ZAdd(ctx, v.key("generations"), redis.Z{Score: 1, Member: g})
	}
	if _, err = v.Prune(ctx); err != nil {
		t.Fatal(err)
	}
	count := 0
	cursor := ""
	for {
		p, e := s.List(ctx, cursor, 4096)
		if e != nil {
			t.Fatal(e)
		}
		for _, word := range p.Keywords {
			if !strings.HasPrefix(word, "shared-prefix-") {
				t.Fatal("mixed snapshot", word)
			}
		}
		count += len(p.Keywords)
		cursor = p.NextCursor
		if cursor == "" {
			break
		}
	}
	if count != len(words) {
		t.Fatal(count)
	}
	_ = s.Close(ctx)
	if _, err = v.Prune(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = v.manifest(ctx, r.Version); !errors.Is(err, redis.Nil) {
		t.Fatal("unleased old generation retained", err)
	}
	if found, e := v.Contains(ctx, words[0]); e != nil || !found {
		t.Fatal(found, e)
	}
}
