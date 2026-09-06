// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	redis "github.com/redis/go-redis/v9"
)

// TestVersionedScale is opt-in and must use a disposable real Redis/Valkey
// server. The deterministic seed and workload are recorded with each result.
// Run each size/workload in a separate process for independent maximum RSS.
//
//nolint:gocyclo,funlen // Opt-in end-to-end measurement keeps operation ordering explicit.
func TestVersionedScale(t *testing.T) {
	addr := os.Getenv("ACOR_V3_SCALE_ADDR")
	if addr == "" {
		t.Skip("set ACOR_V3_SCALE_ADDR to a disposable real server")
	}
	n, err := strconv.Atoi(os.Getenv("ACOR_V3_SCALE_N"))
	if err != nil || n < 1 {
		t.Fatal("ACOR_V3_SCALE_N must be positive")
	}
	kind := os.Getenv("ACOR_V3_SCALE_KIND")
	if kind == "" {
		kind = "shared"
	}
	ctx := context.Background()
	name := "scale-" + v3ID()
	opts := &VersionedOptions{Redis: AhoCorasickArgs{Addr: addr, Name: name}, PollInterval: 50 * time.Millisecond}
	v, err := OpenVersioned(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = v.Close() }()
	// Delete only this test's keys, never FLUSHDB on a supplied endpoint.
	defer func() {
		c, _ := newRedisClient(&opts.Redis)
		defer c.Close()
		var cursor uint64
		for {
			keys, next, e := c.Scan(ctx, cursor, v.prefix+"*", 256).Result()
			if e != nil {
				return
			}
			if len(keys) > 0 {
				c.Del(ctx, keys...)
			}
			cursor = next
			if cursor == 0 {
				break
			}
		}
	}()
	words := v3ScaleWords(n, kind)
	serverInfo, _ := v.client.Info(ctx, "server").Result()
	t.Logf("environment go=%s os=%s arch=%s cpus=%d n=%d kind=%s seed=20260906 server=%s",
		runtime.Version(), runtime.GOOS, runtime.GOARCH, runtime.NumCPU(), n, kind, strings.ReplaceAll(serverInfo, "\r\n", " "))
	measure := func(label string, write func() (*WriteResult, error)) *WriteResult {
		t.Helper()
		before := v3ServerMetrics(ctx, v.client)
		done := make(chan struct{})
		var wg sync.WaitGroup
		var latencies []int64
		wg.Go(func() {
			ticker := time.NewTicker(time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-done:
					return
				case <-ticker.C:
					start := time.Now()
					_, e := v.Find(ctx, words[0]+" "+words[n/2]+" "+words[n-1])
					if e != nil {
						return
					}
					latencies = append(latencies, time.Since(start).Nanoseconds())
				}
			}
		})
		start := time.Now()
		r, e := write()
		commit := time.Since(start)
		if e == nil {
			e = v.WaitForVersion(ctx, r.Version)
		}
		ready := time.Since(start)
		close(done)
		wg.Wait()
		if e != nil {
			t.Fatal(label, e)
		}
		after := v3ServerMetrics(ctx, v.client)
		slices.Sort(latencies)
		percentile := func(p int) int64 {
			if len(latencies) == 0 {
				return 0
			}
			return latencies[(len(latencies)-1)*p/100]
		}
		var usage syscall.Rusage
		_ = syscall.Getrusage(syscall.RUSAGE_SELF, &usage)
		rss := usage.Maxrss
		if runtime.GOOS != "darwin" {
			rss *= 1024
		}
		record := map[string]interface{}{"operation": label,
			"n":              n,
			"kind":           kind,
			"commit_ms":      float64(commit.Microseconds()) / 1000,
			"ready_ms":       float64(ready.Microseconds()) / 1000,
			"max_rss_bytes":  rss,
			"redis_bytes":    after["used_memory"],
			"sent_bytes":     after["total_net_input_bytes"] - before["total_net_input_bytes"],
			"received_bytes": after["total_net_output_bytes"] - before["total_net_output_bytes"],
			"search_samples": len(latencies),
			"search_p50_ns":  percentile(50),
			"search_p95_ns":  percentile(95),
			"search_p99_ns":  percentile(99)}
		data, _ := json.Marshal(record)
		t.Log(string(data))
		return r
	}
	r := measure("initial_load", func() (*WriteResult, error) { return v.Replace(ctx, v.Status().ServingVersion, words) })
	// Reopening builds from stored keywords; search equivalence is checked below.
	_ = v.Close()
	runtime.GC()
	started := time.Now()
	v, err = OpenVersioned(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("startup_ms=%.3f", float64(time.Since(started).Microseconds())/1000)
	snapshot, err := v.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	cursor := ""
	for {
		page, e := snapshot.List(ctx, cursor, 4096)
		if e != nil {
			t.Fatal(e)
		}
		count += len(page.Keywords)
		cursor = page.NextCursor
		if cursor == "" {
			break
		}
	}
	_ = snapshot.Close(ctx)
	if count != n {
		t.Fatalf("count=%d want=%d", count, n)
	}
	text := words[0] + " " + words[n/2] + " " + words[n-1]
	got, err := v.FindSet(ctx, text)
	if err != nil {
		t.Fatal(err)
	}
	expected := make([]string, 0)
	for _, w := range words {
		if strings.Contains(text, w) {
			expected = append(expected, w)
		}
	}
	slices.Sort(got)
	slices.Sort(expected)
	if !slices.Equal(got, expected) {
		t.Fatal("search differs from naive reference")
	}
	measure("identical_replace", func() (*WriteResult, error) { return v.Replace(ctx, r.Version, words) })
	for _, changes := range []int{1, 1000, max(1, n/100)} {
		added := make([]string, changes)
		for i := range added {
			added[i] = fmt.Sprintf("change-%d-%08d", changes, i)
		}
		r = measure(fmt.Sprintf("add_%d", changes), func() (*WriteResult, error) { return v.AddMany(ctx, r.Version, added) })
		r = measure(fmt.Sprintf("remove_%d", changes), func() (*WriteResult, error) { return v.RemoveMany(ctx, r.Version, added) })
	}
	target := make([]string, len(words))
	for i, w := range words {
		target[i] = w + "-x"
	}
	r = measure("full_replace", func() (*WriteResult, error) { return v.Replace(ctx, r.Version, target) })
	// Simulate passage of the retention horizon, keeping the active generation.
	gens, _ := v.client.ZRange(ctx, v.key("generations"), 0, -1).Result()
	for _, g := range gens {
		if g != string(r.Version) {
			v.client.ZAdd(ctx, v.key("generations"), redis.Z{Score: 1, Member: g})
		}
	}
	start := time.Now()
	pruned, err := v.Prune(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("prune_ms=%.3f result=%+v redis_after=%d", float64(time.Since(start).Microseconds())/1000, pruned, v3ServerMetrics(ctx, v.client)["used_memory"])
}
func v3ScaleWords(n int, kind string) []string {
	rng := rand.New(rand.NewSource(20260906))
	words := make([]string, n)
	for i := range words {
		switch kind {
		case "shared":
			words[i] = fmt.Sprintf("shared-prefix-%08d", i)
		case "diverse":
			words[i] = fmt.Sprintf("%04x-%08d", rng.Uint32()&0xfff, i)
		case "korean":
			words[i] = fmt.Sprintf("한국어-%04x-키워드-%08d", rng.Uint32()&0xffff, i)
		default:
			panic("unknown scale workload")
		}
	}
	return words
}
func v3ServerMetrics(ctx context.Context, client redis.UniversalClient) map[string]int64 {
	info, _ := client.Info(ctx, "memory", "stats").Result()
	out := make(map[string]int64)
	for _, line := range strings.Split(info, "\n") {
		k, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if ok {
			out[k], _ = strconv.ParseInt(value, 10, 64)
		}
	}
	return out
}
