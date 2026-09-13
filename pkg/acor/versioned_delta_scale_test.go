// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestVersionedDeltaScale compares the opt-in path against the full rebuild in
// fresh processes. The companion script owns the server and repeats each run.
//
//nolint:gocyclo,funlen // Opt-in real-server measurement keeps operation ordering explicit.
func TestVersionedDeltaScale(t *testing.T) {
	addr := os.Getenv("ACOR_V3_SCALE_ADDR")
	if addr == "" {
		t.Skip("set ACOR_V3_SCALE_ADDR to a disposable real server")
	}
	n, err := strconv.Atoi(os.Getenv("ACOR_V3_SCALE_N"))
	if err != nil || n < 129 {
		t.Fatal("ACOR_V3_SCALE_N must be at least 129")
	}
	kind := os.Getenv("ACOR_V3_SCALE_KIND")
	words := v3ScaleWords(n, kind)
	delta := os.Getenv("ACOR_V3_SCALE_DELTA") == "1"
	ctx := context.Background()
	opts := &VersionedOptions{Redis: AhoCorasickArgs{Addr: addr, Name: "delta-scale-" + v3ID()},
		PollInterval: 50 * time.Millisecond, DeltaSearch: delta}
	v, err := OpenVersioned(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer v3DeleteTestKeys(ctx, t, v)
	defer func() { _ = v.Close() }()
	serverInfo, _ := v.client.Info(ctx, "server").Result()
	t.Logf("environment go=%s os=%s arch=%s cpus=%d n=%d kind=%s delta=%t seed=20260906 server=%s",
		runtime.Version(), runtime.GOOS, runtime.GOARCH, runtime.NumCPU(), n, kind, delta,
		strings.ReplaceAll(serverInfo, "\r\n", " "))
	r, err := v.Replace(ctx, v.Status().ServingVersion, words)
	if err != nil {
		t.Fatal(err)
	}
	if err = v.WaitForVersion(ctx, r.Version); err != nil {
		t.Fatal(err)
	}
	if v.Status().DeltaSearch {
		t.Fatal("initial load unexpectedly used delta search")
	}
	version := r.Version
	change := make([]string, 129)
	for i := range change {
		change[i] = fmt.Sprintf("delta-change-%03d", i)
	}
	measure := func(label string, expectedDelta bool, write func(Version) (*WriteResult, error)) {
		t.Helper()
		start := time.Now()
		r, e := write(version)
		commit := time.Since(start)
		if e != nil {
			t.Fatal(label, e)
		}
		if e = v.WaitForVersion(ctx, r.Version); e != nil {
			t.Fatal(label, e)
		}
		ready := time.Since(start)
		version = r.Version
		status := v.Status()
		if status.DeltaSearch != (delta && expectedDelta) {
			t.Fatalf("%s: unexpected search mode: %+v", label, status)
		}
		// Include an unchanged base word and a changed word. The same input is
		// used for both modes, including after removal.
		query := words[0] + " " + words[n/2] + " " + change[0] + " " + words[n-1]
		found, e := v.FindSet(ctx, query)
		if e != nil || !slices.Contains(found, words[0]) || slices.Contains(found, words[n/2]) == (label == "remove_base_1") ||
			!slices.Contains(found, words[n-1]) || slices.Contains(found, change[0]) != strings.HasPrefix(label, "add_") {
			t.Fatalf("%s: serving search mismatch: %v (%v)", label, found, e)
		}
		latencies := make([]int64, 0, 512)
		until := time.Now().Add(500 * time.Millisecond)
		for time.Now().Before(until) {
			searchStart := time.Now()
			if _, e := v.Find(ctx, query); e != nil {
				t.Fatal(label, e)
			}
			latencies = append(latencies, time.Since(searchStart).Nanoseconds())
			time.Sleep(time.Millisecond)
		}
		slices.Sort(latencies)
		apiStart := time.Now()
		buildingBeforeAPI := v.Status().Building
		apiSamples := v3DeltaAPISamples(t, v, ctx, query, label)
		buildingAfterAPI := v.Status().Building
		if current := v.Status(); current.ServingVersion != version ||
			current.DeltaSearch != (delta && expectedDelta) {
			t.Fatalf("%s: serving mode changed during API sample: %+v", label, current)
		}
		var heap runtime.MemStats
		runtime.ReadMemStats(&heap)
		record := map[string]any{
			"operation": label, "n": n, "kind": kind, "delta_enabled": delta,
			"delta_serving": status.DeltaSearch, "delta_keywords": status.DeltaKeywords,
			"commit_ms":                     float64(commit.Microseconds()) / 1000,
			"ready_ms":                      float64(ready.Microseconds()) / 1000,
			"search_p95_ns":                 latencies[(len(latencies)-1)*95/100],
			"search_samples":                len(latencies),
			"rss_highwater_at_sample_bytes": v3ProcessMaxRSS(),
			"heap_alloc_at_sample_bytes":    heap.Alloc,
			"downloaded_buckets":            status.DownloadedBuckets,
			"reused_buckets":                status.ReusedBuckets,
			"search_apis":                   apiSamples,
			"api_sample_ms":                 float64(time.Since(apiStart).Microseconds()) / 1000,
			"building_during_api":           buildingBeforeAPI || buildingAfterAPI,
		}
		data, _ := json.Marshal(record)
		t.Log(string(data))
	}
	measure("add_1", true, func(vn Version) (*WriteResult, error) { return v.Add(ctx, vn, change[0]) })
	measure("remove_1", true, func(vn Version) (*WriteResult, error) { return v.Remove(ctx, vn, change[0]) })
	measure("add_128", true, func(vn Version) (*WriteResult, error) { return v.AddMany(ctx, vn, change[:128]) })
	measure("remove_128", true, func(vn Version) (*WriteResult, error) { return v.RemoveMany(ctx, vn, change[:128]) })
	measure("remove_base_1", true, func(vn Version) (*WriteResult, error) { return v.Remove(ctx, vn, words[n/2]) })
	restored, err := v.Add(ctx, version, words[n/2])
	if err != nil {
		t.Fatal("restore base word", err)
	}
	if err = v.WaitForVersion(ctx, restored.Version); err != nil {
		t.Fatal("restore base word", err)
	}
	version = restored.Version
	measure("add_129", false, func(vn Version) (*WriteResult, error) { return v.AddMany(ctx, vn, change) })
	measure("remove_129", false, func(vn Version) (*WriteResult, error) { return v.RemoveMany(ctx, vn, change) })
	// A final small update is left idle to observe the full compaction's cost.
	measure("add_1_compact", true, func(vn Version) (*WriteResult, error) { return v.Add(ctx, vn, change[0]) })
	compactStart := time.Now()
	compactLatencies := make([]int64, 0, 512)
	if delta {
		deadline := time.Now().Add(2 * time.Minute)
		for v.Status().DeltaSearch && time.Now().Before(deadline) {
			searchStart := time.Now()
			if _, err := v.Find(ctx, words[0]+" "+change[0]); err != nil {
				t.Fatal("search during compaction", err)
			}
			compactLatencies = append(compactLatencies, time.Since(searchStart).Nanoseconds())
			time.Sleep(time.Millisecond)
		}
		if v.Status().DeltaSearch {
			t.Fatal("compaction did not complete")
		}
	}
	if got, err := v.FindSet(ctx, change[0]); err != nil || !slices.Equal(got, []string{change[0]}) {
		t.Fatal("post-compaction search", got, err)
	}
	slices.Sort(compactLatencies)
	var compactP95 int64
	if len(compactLatencies) > 0 {
		compactP95 = compactLatencies[(len(compactLatencies)-1)*95/100]
	}
	var heap runtime.MemStats
	runtime.ReadMemStats(&heap)
	data, _ := json.Marshal(map[string]any{"operation": "post_compaction", "n": n, "kind": kind,
		"delta_enabled": delta, "compaction_wait_ms": float64(time.Since(compactStart).Microseconds()) / 1000,
		"rss_highwater_at_sample_bytes": v3ProcessMaxRSS(), "heap_alloc_at_sample_bytes": heap.Alloc,
		"compaction_search_p95_ns": compactP95, "compaction_search_samples": len(compactLatencies),
		"delta_serving": v.Status().DeltaSearch})
	t.Log(string(data))
}

func v3ProcessMaxRSS() int64 {
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		return 0
	}
	rss := usage.Maxrss
	if runtime.GOOS != "darwin" {
		rss *= 1024
	}
	return rss
}

// v3DeltaAPISamples uses identical text and call shapes in the paired modes.
// The parallel text exceeds ChunkSize many times; short texts exercise the
// ordinary local search path without making result construction dominate it.
//
//nolint:funlen // Each closure is a distinct public API under measurement.
func v3DeltaAPISamples(t *testing.T, v *VersionedCollection, ctx context.Context, query, label string) map[string]map[string]int64 {
	t.Helper()
	inputs := map[string]string{
		"change":     query,
		"delta_miss": "absent-delta-token-999999",
	}
	if label == "remove_base_1" {
		inputs["removed_base"] = query
	} else if strings.HasPrefix(label, "remove_") {
		inputs["removed_change"] = query
	} else {
		inputs["added_hit"] = query
	}
	result := make(map[string]map[string]int64)
	for _, inputKind := range []string{"change", "added_hit", "removed_change", "removed_base", "delta_miss"} {
		text, exists := inputs[inputKind]
		if !exists {
			continue
		}
		parallelText := strings.Repeat(text+" ", 48)
		parallelOpts := &ParallelOptions{Workers: 2, ChunkSize: 128, AutoOverlap: true}
		batch := []string{text, text, "absent-delta-token-999999"}
		calls := map[string]func() error{
			"Find":        func() error { _, err := v.Find(ctx, text); return err },
			"FindSet":     func() error { _, err := v.FindSet(ctx, text); return err },
			"FindIndex":   func() error { _, err := v.FindIndex(ctx, text); return err },
			"FindMatches": func() error { _, err := v.FindMatches(ctx, text, nil); return err },
			"Contains":    func() error { _, err := v.Contains(ctx, text); return err },
			"FindStream": func() error {
				return v.FindStream(ctx, strings.NewReader(text), func(Match) bool { return true })
			},
			"FindBatch":         func() error { _, err := v.FindBatch(ctx, batch); return err },
			"FindParallel":      func() error { _, err := v.FindParallel(ctx, parallelText, parallelOpts); return err },
			"FindIndexParallel": func() error { _, err := v.FindIndexParallel(ctx, parallelText, parallelOpts); return err },
			"Scan":              func() error { _, err := v.Scan(ctx, text, nil); return err },
			"MaskText":          func() error { _, err := v.MaskText(ctx, text, '*', nil); return err },
			"ReplaceText":       func() error { _, err := v.ReplaceText(ctx, text, "[x]", nil); return err },
		}
		for _, api := range []string{"Find", "FindSet", "FindIndex", "FindMatches", "Contains", "FindStream",
			"FindBatch", "FindParallel", "FindIndexParallel", "Scan", "MaskText", "ReplaceText"} {
			call := calls[api]
			latencies := make([]int64, 128)
			for i := range latencies {
				start := time.Now()
				if err := call(); err != nil {
					t.Fatalf("%s/%s/%s: %v", label, inputKind, api, err)
				}
				latencies[i] = time.Since(start).Nanoseconds()
			}
			slices.Sort(latencies)
			if result[api] == nil {
				result[api] = make(map[string]int64)
			}
			result[api][inputKind] = latencies[(len(latencies)-1)*95/100]
		}
	}
	return result
}

func v3DeleteTestKeys(ctx context.Context, t *testing.T, v *VersionedCollection) {
	t.Helper()
	c, err := newRedisClient(&v.opts.Redis)
	if err != nil {
		t.Log("cleanup connection:", err)
		return
	}
	defer c.Close()
	var cursor uint64
	var allKeys []string
	for {
		keys, next, err := c.Scan(ctx, cursor, v.prefix+"*", 256).Result()
		if err != nil {
			t.Log("cleanup scan:", err)
			return
		}
		allKeys = append(allKeys, keys...)
		cursor = next
		if cursor == 0 {
			break
		}
	}
	for len(allKeys) > 0 {
		batch := allKeys[:min(len(allKeys), 256)]
		if err := c.Del(ctx, batch...).Err(); err != nil {
			t.Log("cleanup delete:", err)
			return
		}
		allKeys = allKeys[len(batch):]
	}
}
