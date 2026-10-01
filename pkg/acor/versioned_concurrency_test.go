// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/alicebob/miniredis/v2"
	redis "github.com/redis/go-redis/v9"
)

type v3ShardReadHook struct {
	v3FaultHook
	mu       sync.Mutex
	maxBatch int
}

func (h *v3ShardReadHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		count := 0
		for _, cmd := range cmds {
			if cmd.Name() == "get" && strings.Contains(fmt.Sprint(cmd.Args()[1]), "}:manifest:") {
				count++
			}
		}
		h.mu.Lock()
		h.maxBatch = max(h.maxBatch, count)
		h.mu.Unlock()
		return next(ctx, cmds)
	}
}

// Reading the persisted generation must use its layout, even when creation
// options omit the shard count or request a different layout.
func TestVersionedReopenConcurrencyUsesStoredLayout(t *testing.T) {
	previous := runtime.GOMAXPROCS(4)
	t.Cleanup(func() { runtime.GOMAXPROCS(previous) })
	ctx := context.Background()
	server := miniredis.RunT(t)
	initial, err := OpenVersioned(ctx, &VersionedOptions{
		Redis: AhoCorasickArgs{Addr: server.Addr(), Name: "concurrency-reopen"}, ShardCount: 16,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = initial.Close(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name        string
		count       uint16
		concurrency int
		want        int
	}{
		{"omitted", 0, 0, 4},
		{"different-creation-layout", 2, 0, 4},
		{"explicit", 0, 3, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v, openErr := OpenVersioned(ctx, &VersionedOptions{
				Redis:      AhoCorasickArgs{Addr: server.Addr(), Name: "concurrency-reopen"},
				ShardCount: tc.count, ShardConcurrency: tc.concurrency,
			})
			if openErr != nil {
				t.Fatal(openErr)
			}
			defer v.Close()
			assertV3ManifestReadConcurrency(t, v, tc.want)
		})
	}
}

// Automatic concurrency must follow each target layout rather than remain
// pinned to the legacy collection's single shard; explicit limits remain fixed.
func TestVersionedReshardConcurrencyUsesTargetLayout(t *testing.T) {
	previous := runtime.GOMAXPROCS(4)
	t.Cleanup(func() { runtime.GOMAXPROCS(previous) })
	for _, concurrency := range []int{0, 3} {
		t.Run(fmt.Sprint(concurrency), func(t *testing.T) {
			ctx := context.Background()
			server := miniredis.RunT(t)
			v, err := OpenVersioned(ctx, &VersionedOptions{
				Redis:            AhoCorasickArgs{Addr: server.Addr(), Name: "concurrency-reshard"},
				ShardConcurrency: concurrency,
			})
			if err != nil {
				t.Fatal(err)
			}
			defer v.Close()
			for _, tc := range []struct {
				count uint16
				want  int
			}{{16, 4}, {2, 2}, {1, 0}, {16, 4}} {
				r, reshardErr := v.Reshard(ctx, v.Status().ServingVersion, tc.count)
				if reshardErr != nil {
					t.Fatal(reshardErr)
				}
				waitV3(t, v, r.Version)
				want := tc.want
				if concurrency != 0 && tc.count > 1 {
					want = min(concurrency, int(tc.count))
				}
				assertV3ManifestReadConcurrency(t, v, want)
			}
		})
	}
}

func assertV3ManifestReadConcurrency(t *testing.T, v *VersionedCollection, want int) {
	t.Helper()
	hook := &v3ShardReadHook{}
	v.client.AddHook(hook)
	if _, err := v.manifest(context.Background(), v.Status().ServingVersion); err != nil {
		t.Fatal(err)
	}
	hook.mu.Lock()
	got := hook.maxBatch
	hook.mu.Unlock()
	if got != want {
		t.Fatalf("shard manifest batch = %d, want %d", got, want)
	}
}
