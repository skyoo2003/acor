// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
)

func TestVersionedReusesVerifiedBuckets(t *testing.T) {
	ctx := context.Background()
	server := miniredis.RunT(t)
	v := openV3Test(t, server, "reuse-r2")
	words := v3ScaleWords(10000, "shared")
	r, err := v.Replace(ctx, v.Status().ServingVersion, words)
	if err != nil {
		t.Fatal(err)
	}
	waitV3(t, v, r.Version)
	old := v.current.Load()
	next, err := v.Add(ctx, r.Version, "one-new-keyword")
	if err != nil {
		t.Fatal(err)
	}
	waitV3(t, v, next.Version)
	status := v.Status()
	if status.DownloadedBuckets != 1 || status.ReusedBuckets != v3BucketCount-1 {
		t.Fatal(status)
	}
	current := v.current.Load()
	for i, words := range old.buckets {
		if i == v3BucketNumber("one-new-keyword") || len(words) == 0 {
			continue
		}
		if &words[0] != &current.buckets[i][0] {
			t.Fatal("unchanged bucket copied", i)
		}
	}
	// A failed download must not replace the last successful cache or engine.
	writer := openV3Test(t, server, "reuse-r2")
	fault := &v3FaultHook{}
	v.client.AddHook(fault)
	fault.failChunks.Store(true)
	changed, err := writer.Add(ctx, next.Version, "next-new-keyword")
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for v.Status().LastError == "" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if v.current.Load() != current {
		t.Fatal("failed refresh discarded serving cache")
	}
	fault.failChunks.Store(false)
	waitV3(t, v, changed.Version)
}
func TestVersionedDebounceCoalescesWithoutStarvation(t *testing.T) {
	ctx := context.Background()
	server := miniredis.RunT(t)
	opts := &VersionedOptions{Redis: AhoCorasickArgs{Addr: server.Addr(), Name: "coalesce-r2"}, RefreshDebounce: 200 * time.Millisecond, PollInterval: time.Hour}
	reader, err := OpenVersioned(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	writer := openV3Test(t, server, "coalesce-r2")
	expected := reader.Status().ServingVersion
	for i := range 10 {
		r, e := writer.Add(ctx, expected, fmt.Sprint(i))
		if e != nil {
			t.Fatal(e)
		}
		expected = r.Version
	}
	waitV3(t, reader, expected)
	if builds := reader.Status().CompletedBuilds; builds >= 11 {
		t.Fatal("every intermediate generation was built", builds)
	}
	// Waiting on a skipped version is valid once a later generation is serving.
	if err = reader.WaitForVersion(ctx, writer.Status().ServingVersion); err != nil {
		t.Fatal(err)
	}
}
