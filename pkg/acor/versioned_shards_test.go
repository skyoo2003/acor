// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"testing"

	"github.com/alicebob/miniredis/v2"
)

// Catches accidental layout changes or dictionary loss when reopening legacy V3.
func TestVersionedShardOptionsPreserveSingleShardCompatibility(t *testing.T) {
	ctx := context.Background()
	server := miniredis.RunT(t)
	v := openV3Test(t, server, "legacy-shards")
	r, err := v.Replace(ctx, v.Status().ServingVersion, []string{"한국어", "legacy"})
	if err != nil {
		t.Fatal(err)
	}
	if err = v.Close(); err != nil {
		t.Fatal(err)
	}
	for _, count := range []uint16{0, 1} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			reopened, err := OpenVersioned(ctx, &VersionedOptions{
				Redis: AhoCorasickArgs{Addr: server.Addr(), Name: "legacy-shards"}, ShardCount: count,
			})
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			status := reopened.Status()
			if status.ServingVersion != r.Version || status.LayoutVersion != 1 || status.ShardCount != 1 {
				t.Fatalf("legacy reopen status: %+v", status)
			}
			if got, err := reopened.FindSet(ctx, "legacy 한국어"); err != nil || !slices.Equal(got, []string{"legacy", "한국어"}) {
				t.Fatalf("reopened dictionary = %v, err = %v", got, err)
			}
		})
	}
}

// Catches accepting non-power-of-two or unbounded fan-out and negative workers.
func TestVersionedShardOptionsRejectInvalidCountsAndConcurrency(t *testing.T) {
	for _, count := range []uint16{3, 7, 255, 257, 512, 65535} {
		t.Run(fmt.Sprintf("count-%d", count), func(t *testing.T) {
			_, err := versionedOptions(&VersionedOptions{Redis: AhoCorasickArgs{Name: "options"}, ShardCount: count})
			if err == nil {
				t.Fatalf("accepted shard count %d", count)
			}
		})
	}
	if _, err := versionedOptions(&VersionedOptions{Redis: AhoCorasickArgs{Name: "options"}, ShardConcurrency: -1}); err == nil {
		t.Fatal("accepted negative shard concurrency")
	}
}

func TestVersionedShardOptionsNormalizeDefaults(t *testing.T) {
	for _, count := range []uint16{0, 1, 2, 4, 16, 256} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			o, err := versionedOptions(&VersionedOptions{Redis: AhoCorasickArgs{Name: "options"}, ShardCount: count})
			if err != nil {
				t.Fatal(err)
			}
			wantCount := max(count, 1)
			if o.ShardCount != wantCount || o.ShardConcurrency != max(1, min(runtime.GOMAXPROCS(0), int(wantCount))) {
				t.Fatalf("normalized options: %+v", o)
			}
		})
	}
	o, err := versionedOptions(&VersionedOptions{Redis: AhoCorasickArgs{Name: "options"}, ShardCount: 2, ShardConcurrency: 3})
	if err != nil || o.ShardConcurrency != 3 {
		t.Fatalf("explicit concurrency = %d, err = %v", o.ShardConcurrency, err)
	}
}

// Literal hash vectors catch changed encoding, byte order, or unstable hashing.
func TestV3ShardForIsStableForUnicode(t *testing.T) {
	for _, tc := range []struct {
		word  string
		count uint16
		want  uint16
	}{
		{"한국어", 0, 0}, {"한국어", 1, 0}, {"한국어", 16, 3},
		{"한국어", 256, 163}, {"café", 16, 0}, {"🙂", 16, 6},
	} {
		for range 20 {
			if got := v3ShardFor(tc.word, tc.count); got != tc.want {
				t.Fatalf("shard(%q, %d) = %d, want %d", tc.word, tc.count, got, tc.want)
			}
		}
	}
}

func TestVersionedShardLayoutValidation(t *testing.T) {
	for _, layout := range []v3Layout{{Version: 1, ShardCount: 1}, {Version: 2, ShardCount: 2}, {Version: 2, ShardCount: 256}} {
		if err := layout.validate(); err != nil {
			t.Fatalf("rejected valid layout %+v: %v", layout, err)
		}
	}
	for _, layout := range []v3Layout{
		{}, {Version: 3, ShardCount: 2}, {Version: 1, ShardCount: 2},
		{Version: 2, ShardCount: 1}, {Version: 2, ShardCount: 3}, {Version: 2, ShardCount: 512},
	} {
		if err := layout.validate(); !errors.Is(err, ErrVersionedCorrupt) {
			t.Fatalf("invalid layout %+v returned %v", layout, err)
		}
	}
}
