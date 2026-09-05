// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sync/atomic"
	"testing"
	"time"
)

//nolint:gocyclo // Cross-product parity matrix covers all modes and boundary options.
func TestAutoOverlapSerialParity(t *testing.T) {
	for _, preset := range []Preset{PresetNone, PresetSpeed, PresetBalanced, PresetMemoryEfficient} {
		t.Run(preset.String(), func(t *testing.T) {
			var ac *AhoCorasick
			if preset == PresetNone {
				var err error
				mr := createTestRedisServer(t)
				t.Cleanup(mr.Close)
				ac, err = Create(&AhoCorasickArgs{Addr: mr.Addr(), Name: t.Name()})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = ac.Close() })
			} else {
				ac = newTestPresetRedis(t, preset)
			}
			keywords := []string{"abcdefghijk", "hijk", "ijk", "한글😀키워드", "😀키워드", "키워드", "a. b\nc", "b\nc"}
			if _, err := ac.AddMany(keywords, nil); err != nil {
				t.Fatal(err)
			}
			text := "xxabcdefghijk 한글😀키워드. a. b\nc! abcdefghijk 한글😀키워드"
			want, err := ac.FindIndex(text)
			if err != nil {
				t.Fatal(err)
			}
			wantSet := make([]string, 0, len(want))
			for k := range want {
				wantSet = append(wantSet, k)
			}
			slices.Sort(wantSet)
			counter := countRTT(t, ac)
			for _, boundary := range []ChunkBoundary{ChunkBoundaryWord, ChunkBoundaryLine, ChunkBoundarySentence, ChunkBoundary(99)} {
				for _, size := range []int{1, 2, 5, 11, 1000} {
					for _, overlap := range []int{-1, 0, 3, 100} {
						opts := &ParallelOptions{ChunkSize: size, Boundary: boundary, Overlap: overlap, AutoOverlap: true}
						before := *opts
						counter.reset()
						got, err := ac.FindIndexParallel(text, opts)
						if err != nil {
							t.Fatal(err)
						}
						if !reflect.DeepEqual(got, want) {
							t.Fatalf("boundary %v size %d overlap %d: %v != %v", boundary, size, overlap, got, want)
						}
						expectedRTT := 0
						if preset == PresetNone {
							expectedRTT = 1
						}
						if counter.count() != expectedRTT {
							t.Fatalf("RTT = %d", counter.count())
						}
						found, err := ac.FindParallel(text, opts)
						if err != nil {
							t.Fatal(err)
						}
						slices.Sort(found)
						if !reflect.DeepEqual(found, wantSet) {
							t.Fatalf("set = %v", found)
						}
						if *opts != before {
							t.Fatal("caller options mutated")
						}
					}
				}
			}
			counter.reset()
			if _, err := ac.FindParallel("", &ParallelOptions{ChunkSize: 1, AutoOverlap: true}); err != nil {
				t.Fatal(err)
			}
			if _, err := ac.FindIndexParallel("", &ParallelOptions{AutoOverlap: true}); !errors.Is(err, ErrInvalidChunkSize) {
				t.Fatal(err)
			}
			if counter.count() != 0 {
				t.Fatal("empty input read Redis")
			}
		})
	}
}

func TestAutoOverlapOrder(t *testing.T) {
	ac := newTestPresetRedis(t, PresetBalanced)
	if _, err := ac.AddMany([]string{"abcdef", "bc", "de", "f"}, nil); err != nil {
		t.Fatal(err)
	}
	got, err := ac.FindParallel("abcdefabcdef", &ParallelOptions{ChunkSize: 2, AutoOverlap: true})
	if err != nil {
		t.Fatal(err)
	}
	// Chunk order, then engine scan order within each owned chunk.
	if !reflect.DeepEqual(got, []string{"bc", "abcdef", "de", "f"}) {
		t.Fatal(got)
	}
}

// Hold the first snapshot after reading it, so writes can commit while the
// reloader has an old snapshot. Later calls (including writes) pass through.
type heldPresetStorage struct {
	kvStorage
	calls   atomic.Int64
	started chan context.Context
	release chan struct{}
	failure error
}

func (s *heldPresetStorage) HGetAll(ctx context.Context, key string) (map[string]string, error) {
	data, err := s.kvStorage.HGetAll(ctx, key)
	if s.calls.Add(1) == 1 {
		s.started <- ctx
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-s.release:
		}
		if s.failure != nil {
			return nil, s.failure
		}
	}
	return data, err
}
func holdPreset(t *testing.T, ac *AhoCorasick) (*redisBackedAC, *heldPresetStorage) {
	t.Helper()
	rb := ac.ops.(*redisBackedAC)
	s := &heldPresetStorage{kvStorage: rb.storage, started: make(chan context.Context, 1), release: make(chan struct{})}
	rb.storage = s
	rb.markStale()
	return rb, s
}
func awaitPresetError(t *testing.T, ch <-chan error) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(3 * time.Second):
		t.Fatal("request did not complete")
		return nil
	}
}
func startPresetRead(ac *AhoCorasick, ctx context.Context) <-chan error {
	ch := make(chan error, 1)
	go func() { _, err := ac.FindContext(ctx, "old new"); ch <- err }()
	return ch
}
func awaitPresetWaiters(t *testing.T, rb *redisBackedAC, n int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		rb.mu.RLock()
		ok := rb.reload != nil && rb.reload.waiters == n
		rb.mu.RUnlock()
		if ok {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("waiters did not join")
}
func TestPresetReloadIndependentCancellation(t *testing.T) {
	ac := newTestPresetRedis(t, PresetBalanced)
	rb, s := holdPreset(t, ac)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first := startPresetRead(ac, ctx)
	workCtx := <-s.started
	second := startPresetRead(ac, context.Background())
	awaitPresetWaiters(t, rb, 2)
	cancel()
	if err := awaitPresetError(t, first); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if workCtx.Err() != nil {
		t.Fatal("one cancellation canceled shared work")
	}
	close(s.release)
	if err := awaitPresetError(t, second); err != nil {
		t.Fatal(err)
	}
	if s.calls.Load() != 1 {
		t.Fatal("reload was not shared")
	}
	if ac.CacheStats().PresetReloadFailures != 0 {
		t.Fatal("cancellation counted as failure")
	}
}
func TestPresetReloadAllCancelAndClose(t *testing.T) {
	for _, closeInstance := range []bool{false, true} {
		t.Run(fmt.Sprint(closeInstance), func(t *testing.T) {
			ac := newTestPresetRedis(t, PresetBalanced)
			rb, s := holdPreset(t, ac)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			first := startPresetRead(ac, ctx)
			workCtx := <-s.started
			second := startPresetRead(ac, ctx)
			awaitPresetWaiters(t, rb, 2)
			if closeInstance {
				if err := ac.Close(); err != nil {
					t.Fatal(err)
				}
			} else {
				cancel()
			}
			for _, ch := range []<-chan error{first, second} {
				if err := awaitPresetError(t, ch); !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			}
			select {
			case <-workCtx.Done():
			case <-time.After(time.Second):
				t.Fatal("shared work not canceled")
			}
			if !closeInstance {
				if _, err := ac.Find("new"); err != nil {
					t.Fatal(err)
				}
			}
			if ac.CacheStats().PresetReloadFailures != 0 {
				t.Fatal("cancellation counted")
			}
		})
	}
}
func TestPresetReloadFailureSharedAndRetry(t *testing.T) {
	ac := newTestPresetRedis(t, PresetBalanced)
	if _, err := ac.Add("old"); err != nil {
		t.Fatal(err)
	}
	rb, s := holdPreset(t, ac)
	s.failure = errors.New("snapshot unavailable")
	first := startPresetRead(ac, context.Background())
	<-s.started
	second := startPresetRead(ac, context.Background())
	awaitPresetWaiters(t, rb, 2)
	close(s.release)
	for _, ch := range []<-chan error{first, second} {
		if err := awaitPresetError(t, ch); !errors.Is(err, s.failure) {
			t.Fatal(err)
		}
	}
	if ac.CacheStats().PresetReloadFailures != 1 {
		t.Fatal(ac.CacheStats())
	}
	rb.mu.RLock()
	old := rb.engine.Find("old")
	stale := rb.stale
	rb.mu.RUnlock()
	if len(old) != 1 || !stale {
		t.Fatal("failed reload discarded previous state")
	}
	if got, err := ac.Find("old"); err != nil || len(got) != 1 {
		t.Fatalf("retry %v %v", got, err)
	}
}
func TestPresetReloadRejectsObsoleteSnapshot(t *testing.T) {
	for _, action := range []string{"add", "remove", "flush", "invalidate"} {
		t.Run(action, func(t *testing.T) {
			ac := newTestPresetRedis(t, PresetBalanced)
			if _, err := ac.Add("old"); err != nil {
				t.Fatal(err)
			}
			rb, s := holdPreset(t, ac)
			read := startPresetRead(ac, context.Background())
			<-s.started
			done := make(chan error, 1)
			go func() {
				var err error
				switch action {
				case "add":
					_, err = ac.Add("new")
				case "remove":
					_, err = ac.Remove("old")
				case "flush":
					err = ac.Flush()
				case "invalidate":
					rb.markStale()
				}
				done <- err
			}()
			if err := awaitPresetError(t, done); err != nil {
				t.Fatal(err)
			}
			close(s.release)
			if err := awaitPresetError(t, read); err != nil {
				t.Fatal(err)
			}
			got, err := ac.Find("old new")
			if err != nil {
				t.Fatal(err)
			}
			want := []string{}
			switch action {
			case "add":
				want = []string{"old", "new"}
			case "invalidate":
				want = []string{"old"}
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("got %v want %v", got, want)
			}
			if s.calls.Load() < 2 {
				t.Fatal("obsolete reload was not retried")
			}
		})
	}
}

func TestPresetPollDoesNotRestartSlowReload(t *testing.T) {
	ac := newTestPresetRedis(t, PresetBalanced)
	rb := ac.ops.(*redisBackedAC)
	if err := rb.redisClient.HSet(context.Background(), trieKey(rb.name), fieldKeywords, `["new"]`, fieldVersion, "123").Err(); err != nil {
		t.Fatal(err)
	}
	_, held := holdPreset(t, ac)
	read := startPresetRead(ac, context.Background())
	<-held.started
	for range 3 {
		rb.pollVersion()
	}
	close(held.release)
	if err := awaitPresetError(t, read); err != nil {
		t.Fatal(err)
	}
	if held.calls.Load() != 1 {
		t.Fatal("polling restarted an already stale reload")
	}
	if got, err := ac.Find("new"); err != nil || len(got) != 1 {
		t.Fatalf("got %v, err %v", got, err)
	}
}
