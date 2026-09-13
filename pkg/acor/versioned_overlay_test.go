// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
)

//nolint:gocyclo // Lifecycle assertions cover publication, search, and compaction together.
func TestVersionedOverlayLifecycle(t *testing.T) {
	ctx := context.Background()
	server := miniredis.RunT(t)
	v, err := OpenVersioned(ctx, &VersionedOptions{
		Redis:       AhoCorasickArgs{Addr: server.Addr(), Name: "overlay"},
		DeltaSearch: true, PollInterval: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = v.Close() })
	base := make([]string, 129)
	for i := range base {
		base[i] = fmt.Sprintf("word-%03d", i)
	}
	r, err := v.Replace(ctx, v.Status().ServingVersion, base)
	if err != nil {
		t.Fatal(err)
	}
	waitV3(t, v, r.Version)
	if v.Status().DeltaSearch {
		t.Fatal("large replacement used overlay")
	}
	r, err = v.AddMany(ctx, r.Version, []string{"she", "한국어"})
	if err != nil {
		t.Fatal(err)
	}
	waitV3(t, v, r.Version)
	if s := v.Status(); !s.DeltaSearch || s.DeltaKeywords != 2 {
		t.Fatalf("status=%+v", s)
	}
	r, err = v.Remove(ctx, r.Version, "word-000")
	if err != nil {
		t.Fatal(err)
	}
	waitV3(t, v, r.Version)
	if s := v.Status(); !s.DeltaSearch || s.DeltaKeywords != 3 {
		t.Fatalf("status=%+v", s)
	}
	text := "SHE 한국어 word-000 word-001"
	got, err := v.Find(ctx, text)
	if err != nil || !reflect.DeepEqual(got, []string{"she", "한국어", "word-001"}) {
		t.Fatal(got, err)
	}
	var stream []Match
	if err = v.FindStream(ctx, strings.NewReader(strings.Repeat(text+" ", 300)), func(m Match) bool { stream = append(stream, m); return true }); err != nil {
		t.Fatal(err)
	}
	all, err := v.FindMatches(ctx, strings.Repeat(text+" ", 300), nil)
	if err != nil || !reflect.DeepEqual(stream, all) {
		t.Fatal("stream parity", err)
	}
	deadline := time.After(5 * time.Second)
	for v.Status().DeltaSearch {
		select {
		case <-deadline:
			t.Fatal("compaction did not run")
		case <-time.After(20 * time.Millisecond):
		}
	}
	if got, err := v.Find(ctx, text); err != nil || !reflect.DeepEqual(got, []string{"she", "한국어", "word-001"}) {
		t.Fatal(got, err)
	}
}

func TestVersionedOverlayCompactionRedisError(t *testing.T) {
	ctx := context.Background()
	server := miniredis.RunT(t)
	v, err := OpenVersioned(ctx, &VersionedOptions{
		Redis:       AhoCorasickArgs{Addr: server.Addr(), Name: "overlay-compact-error"},
		DeltaSearch: true, PollInterval: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = v.Close() })
	base := make([]string, 129)
	for i := range base {
		base[i] = fmt.Sprintf("word-%03d", i)
	}
	r, err := v.Replace(ctx, v.Status().ServingVersion, base)
	if err != nil {
		t.Fatal(err)
	}
	waitV3(t, v, r.Version)
	r, err = v.Add(ctx, r.Version, "new-word")
	if err != nil {
		t.Fatal(err)
	}
	waitV3(t, v, r.Version)
	current := v.current.Load()
	if current.base == nil {
		t.Fatal("expected an overlay")
	}
	ready := *current
	ready.installed = time.Now().Add(-v3CompactDelay)
	v.current.Store(&ready)
	server.Close()
	v.compactIfIdle(ctx)
	if s := v.Status(); s.LastError == "" || s.ServingVersion != r.Version || !s.DeltaSearch {
		t.Fatalf("failed compaction changed serving snapshot: %+v", s)
	}
}
