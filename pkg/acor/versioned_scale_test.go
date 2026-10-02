// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"encoding/json"
	"errors"
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

func TestVersionedScaleRequiresServerIdentity(t *testing.T) {
	valid := "redis_version:8.10.1\r\nredis_build_id:fixture\r\nredis_mode:standalone\r\nos:Linux\r\narch_bits:64\r\n"
	for _, tc := range []struct {
		name string
		info string
		err  error
	}{
		{"permission denied", valid, errors.New("NOPERM INFO denied")},
		{"missing identity", "", nil},
		{"missing version", strings.ReplaceAll(valid, "redis_version:8.10.1\r\n", ""), nil},
		{"missing build", strings.ReplaceAll(valid, "redis_build_id:fixture\r\n", ""), nil},
		{"missing mode", strings.ReplaceAll(valid, "redis_mode:standalone\r\n", ""), nil},
		{"missing OS", strings.ReplaceAll(valid, "os:Linux\r\n", ""), nil},
		{"missing architecture", strings.ReplaceAll(valid, "arch_bits:64\r\n", ""), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := v3ScaleServerIdentity(tc.info, tc.err); err == nil {
				t.Fatal("unidentified server accepted for scale evidence")
			}
		})
	}
	if got, err := v3ScaleServerIdentity(valid, nil); err != nil || got["redis_version"] != "8.10.1" {
		t.Fatal("valid server identity rejected", got, err)
	}
	valkey := strings.ReplaceAll(strings.ReplaceAll(valid, "redis_version", "valkey_version"), "redis_build_id", "valkey_build_id")
	if got, err := v3ScaleServerIdentity(valkey, nil); err != nil || got["valkey_version"] != "8.10.1" {
		t.Fatal("valid Valkey identity rejected", got, err)
	}
}

func TestVersionedScaleChangeSizesHaveUniqueOperations(t *testing.T) {
	for _, tc := range []struct {
		n    int
		want []int
	}{{1, []int{1, 1000}}, {100, []int{1, 1000}}, {100000, []int{1, 1000}},
		{1000, []int{1, 1000, 10}}, {1000000, []int{1, 1000, 10000}}} {
		if got := v3ScaleChangeSizes(tc.n); !slices.Equal(got, tc.want) {
			t.Errorf("n=%d change sizes=%v want=%v", tc.n, got, tc.want)
		}
	}
}

func v3ScaleChangeSizes(n int) []int {
	sizes := []int{1, 1000}
	if percent := max(1, n/100); percent != 1 && percent != 1000 {
		sizes = append(sizes, percent)
	}
	return sizes
}

func v3ScaleServerIdentity(info string, infoErr error) (map[string]string, error) {
	if infoErr != nil {
		return nil, fmt.Errorf("scale server identification: %w", infoErr)
	}
	server := make(map[string]string)
	for _, line := range strings.Split(info, "\n") {
		key, value, _ := strings.Cut(strings.TrimSpace(line), ":")
		switch key {
		case "redis_version", "valkey_version", "redis_build_id", "valkey_build_id", "redis_mode", "os", "arch_bits":
			server[key] = strings.TrimSpace(value)
		}
	}
	if server["redis_version"] == "" && server["valkey_version"] == "" {
		return nil, errors.New("scale server identification: missing version")
	}
	if server["redis_build_id"] == "" && server["valkey_build_id"] == "" {
		return nil, errors.New("scale server identification: missing build ID")
	}
	for _, field := range []string{"redis_mode", "os", "arch_bits"} {
		if server[field] == "" {
			return nil, fmt.Errorf("scale server identification: missing %s", field)
		}
	}
	return server, nil
}

// Missing shard evidence must fail independently of the opt-in real-server run.
func TestVersionedScaleReportsShardMetrics(t *testing.T) {
	before := &v3Manifest{Version: "before", global: &v3GlobalManifest{
		Layout: v3Layout{Version: 2, ShardCount: 4}, Shards: []string{"a", "b", "c", "d"}}}
	after := &v3Manifest{Version: "after", Count: 4, global: &v3GlobalManifest{
		Layout: v3Layout{Version: 2, ShardCount: 4}, Shards: []string{"e", "b", "c", "d"}}}
	after.Buckets[0].Count, after.Buckets[1].Count, after.Buckets[4].Count = 2, 1, 1
	metrics := v3ScaleShardMetrics(before, after)
	for field, want := range map[string]float64{"shard_count": 4, "changed_shards": 1,
		"shard_min_keywords": 0, "shard_max_keywords": 3, "shard_skew_ratio": 3} {
		if got, ok := metrics[field]; !ok || got != want {
			t.Errorf("%s=%v present=%v want=%v", field, got, ok, want)
		}
	}
	if got := v3ScaleShardMetrics(after, after)["changed_shards"]; got != 0 {
		t.Errorf("identical replacement changed %v shards", got)
	}
	legacy := &v3Manifest{Version: "legacy", Count: 4}
	legacy.Buckets[0].Count = 4
	if got := v3ScaleShardMetrics(after, legacy); got["shard_count"] != 1 || got["changed_shards"] != 1 || got["shard_skew_ratio"] != 1 {
		t.Errorf("legacy metrics=%v", got)
	}
	if got := v3ScaleShardMetrics(legacy, after)["changed_shards"]; got != 4 {
		t.Errorf("layout transition changed %v target shards want=4", got)
	}
	if got := v3ScaleShardMetrics(before, &v3Manifest{global: before.global}); got["shard_skew_ratio"] != 0 {
		t.Errorf("empty dictionary skew=%v", got)
	}
}

func v3ScaleShardMetrics(before, after *v3Manifest) map[string]float64 {
	count := int(v3ManifestLayout(after).ShardCount)
	keywords := make([]int, count)
	for bucket, contents := range &after.Buckets {
		keywords[bucket%count] += contents.Count
	}
	changed := 0
	if before.Version != after.Version {
		if after.global == nil || before.global == nil || before.global.Layout != after.global.Layout {
			changed = count
		} else {
			for shard, id := range after.global.Shards {
				if before.global.Shards[shard] != id {
					changed++
				}
			}
		}
	}
	skew := 0.0
	if after.Count > 0 {
		skew = float64(slices.Max(keywords)) * float64(count) / float64(after.Count)
	}
	return map[string]float64{"shard_count": float64(count), "changed_shards": float64(changed),
		"shard_min_keywords": float64(slices.Min(keywords)), "shard_max_keywords": float64(slices.Max(keywords)),
		"shard_skew_ratio": skew}
}

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
	shards := 1
	if value := os.Getenv("ACOR_V3_SCALE_SHARDS"); value != "" {
		shards, err = strconv.Atoi(value)
		if err != nil || shards < 1 || shards > int(v3MaxShardCount) || shards&(shards-1) != 0 {
			t.Fatal("ACOR_V3_SCALE_SHARDS must be a power of two from 1 through 256")
		}
	}
	opts := &VersionedOptions{Redis: AhoCorasickArgs{Addr: addr, Name: name}, PollInterval: 50 * time.Millisecond,
		ShardCount: uint16(shards)} //nolint:gosec // Validated against the supported uint16 bound above.
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
			// Both global and shard namespaces contain the collection digest.
			keys, next, e := c.Scan(ctx, cursor, strings.TrimSuffix(v.prefix, "}:")+"*", 256).Result()
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
	server, err := v3ScaleServerIdentity(v.client.Info(ctx, "server").Result())
	if err != nil {
		t.Fatal(err)
	}
	host, err := os.Hostname()
	if err != nil || strings.TrimSpace(host) == "" {
		t.Fatal("scale environment requires host identification", err)
	}
	environment, _ := json.Marshal(map[string]interface{}{"go": runtime.Version(), "os": runtime.GOOS,
		"arch": runtime.GOARCH, "cpus": runtime.NumCPU(), "gomaxprocs": runtime.GOMAXPROCS(0), "host": host,
		"server": server, "endpoint": addr, "seed": 20260906, "preset": v.opts.Preset,
		"poll_interval_ns": v.opts.PollInterval.Nanoseconds(), "refresh_debounce_ns": v.opts.RefreshDebounce.Nanoseconds(),
		"shard_concurrency": v.shardConcurrency(int(v.Status().ShardCount)), "gogc": os.Getenv("GOGC"), "gomemlimit": os.Getenv("GOMEMLIMIT"),
		"qualification_environment": os.Getenv("ACOR_V3_SCALE_ENVIRONMENT")})
	t.Logf("environment %s", environment)
	measure := func(label string, write func() (*WriteResult, error)) *WriteResult {
		t.Helper()
		before := v3ServerMetrics(ctx, v.client)
		previous := v.current.Load().manifest
		var gcBefore, gcAfter runtime.MemStats
		runtime.ReadMemStats(&gcBefore)
		done := make(chan struct{})
		var wg sync.WaitGroup
		var latencies []int64
		wg.Go(func() {
			ticker := time.NewTicker(time.Millisecond)
			defer ticker.Stop()
			for {
				start := time.Now()
				_, e := v.Find(ctx, words[0]+" "+words[n/2]+" "+words[n-1])
				if e != nil {
					return
				}
				latencies = append(latencies, time.Since(start).Nanoseconds())
				select {
				case <-done:
					return
				case <-ticker.C:
				}
			}
		})
		start := time.Now()
		r, e := write()
		commit := time.Since(start)
		committedAt := time.Now()
		if e == nil {
			e = v.WaitForVersion(ctx, r.Version)
		}
		ready := time.Since(start)
		commitReady := time.Since(committedAt)
		close(done)
		wg.Wait()
		if e != nil {
			t.Fatal(label, e)
		}
		runtime.ReadMemStats(&gcAfter)
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
			"n":                  n,
			"kind":               kind,
			"commit_ms":          float64(commit.Microseconds()) / 1000,
			"ready_ms":           float64(ready.Microseconds()) / 1000,
			"commit_to_ready_ms": float64(commitReady.Microseconds()) / 1000,
			"refresh_lag_ms":     float64(commitReady.Nanoseconds()) / 1e6,
			"gc_pause_ns":        gcAfter.PauseTotalNs - gcBefore.PauseTotalNs,
			"gc_cycles":          gcAfter.NumGC - gcBefore.NumGC,
			"delta_search":       opts.DeltaSearch,
			"max_rss_bytes":      rss,
			"redis_bytes":        after["used_memory"],
			"sent_bytes":         after["total_net_input_bytes"] - before["total_net_input_bytes"],
			"received_bytes":     after["total_net_output_bytes"] - before["total_net_output_bytes"],
			"search_samples":     len(latencies),
			"search_p50_ns":      percentile(50),
			"search_p95_ns":      percentile(95),
			"search_p99_ns":      percentile(99)}
		for field, value := range v3ScaleShardMetrics(previous, v.current.Load().manifest) {
			record[field] = value
		}
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
	for i := range 3 {
		word := fmt.Sprintf("single-change-%d", i)
		r = measure(fmt.Sprintf("add_1_repeat_%d", i), func() (*WriteResult, error) { return v.Add(ctx, r.Version, word) })
		r = measure(fmt.Sprintf("remove_1_repeat_%d", i), func() (*WriteResult, error) { return v.Remove(ctx, r.Version, word) })
	}
	for _, changes := range v3ScaleChangeSizes(n) {
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
