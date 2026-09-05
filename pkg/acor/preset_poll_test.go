// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"errors"
	"testing"

	"github.com/redis/go-redis/v9"
)

type versionProbeStorage struct {
	kvStorage
	gets      int
	snapshots int
	failure   error
}

func (s *versionProbeStorage) HGet(ctx context.Context, key, field string) (string, error) {
	s.gets++
	if field != fieldVersion {
		return "", errors.New("unexpected field")
	}
	if s.failure != nil {
		return "", s.failure
	}
	return s.kvStorage.HGet(ctx, key, field)
}
func (s *versionProbeStorage) HGetAll(ctx context.Context, key string) (map[string]string, error) {
	s.snapshots++
	return s.kvStorage.HGetAll(ctx, key)
}
func TestPresetPollVersionOnlyAndRecovery(t *testing.T) {
	ac := newTestPresetRedis(t, PresetBalanced)
	testPresetPollRecovery(t, ac)
}
func TestIntegrationPresetPollVersionOnlyAndRecovery(t *testing.T) {
	ac := newIntegrationAC(t, "acor-it-preset-poll", &AhoCorasickArgs{Preset: PresetBalanced})
	testPresetPollRecovery(t, ac)
}
func testPresetPollRecovery(t *testing.T, ac *AhoCorasick) {
	t.Helper()
	rb := ac.ops.(*redisBackedAC)
	probe := &versionProbeStorage{kvStorage: rb.storage}
	rb.storage = probe
	rb.pollVersion()
	if probe.gets != 1 || probe.snapshots != 0 {
		t.Fatalf("poll commands HGET=%d HGETALL=%d", probe.gets, probe.snapshots)
	}
	// Redis mutation with no PUBLISH deliberately drops the notification.
	if err := rb.redisClient.HSet(context.Background(), trieKey(rb.name), fieldKeywords, `["new"]`, fieldVersion, "123").Err(); err != nil {
		t.Fatal(err)
	}
	probe.failure = errors.New("Redis unavailable")
	rb.pollVersion()
	if ac.CacheStats().PresetPollFailures != 1 {
		t.Fatal(ac.CacheStats())
	}
	probe.failure = nil
	rb.pollVersion()
	found, err := ac.Find("new")
	if err != nil || len(found) != 1 {
		t.Fatalf("recovery: %v %v", found, err)
	}
	if probe.snapshots != 1 {
		t.Fatalf("reloads %d", probe.snapshots)
	}
	rb.pollVersion()
	if probe.snapshots != 1 {
		t.Fatal("unchanged poll read full dictionary")
	}
	if _, err := ac.Find("new"); err != nil {
		t.Fatal(err)
	}
	if probe.snapshots != 1 {
		t.Fatal("warm read touched Redis")
	}
}

func TestTrieVersionCompatibility(t *testing.T) {
	ac := newTestPresetRedis(t, PresetBalanced)
	rb := ac.ops.(*redisBackedAC)
	for _, value := range []string{"0", "123", "9223372036854775807", "-23", "null", "bad", "1.5", "9223372036854775808"} {
		if err := rb.redisClient.HSet(context.Background(), trieKey(rb.name), fieldVersion, value).Err(); err != nil {
			t.Fatal(err)
		}
		snap, err := readTrieSnapshot(context.Background(), rb.storage, rb.name)
		if err != nil {
			t.Fatal(err)
		}
		version, err := readTrieVersion(context.Background(), rb.storage, rb.name)
		if err != nil || version != snap.Version {
			t.Fatalf("%s: %d %v", value, version, err)
		}
	}
	if err := rb.redisClient.HDel(context.Background(), trieKey(rb.name), fieldVersion).Err(); err != nil {
		t.Fatal(err)
	}
	if version, err := readTrieVersion(context.Background(), rb.storage, rb.name); err != nil || version != 0 {
		t.Fatalf("missing: %d %v", version, err)
	}
	if version, err := readTrieVersion(context.Background(), rb.storage, "absent"); err != nil || version != 0 {
		t.Fatalf("absent: %d %v", version, err)
	}
	probe := &versionProbeStorage{kvStorage: rb.storage, failure: redis.Nil}
	if version, err := readTrieVersion(context.Background(), probe, rb.name); err != nil || version != 0 {
		t.Fatalf("nil: %d %v", version, err)
	}
}

// Backend timeouts count as failures when the job/request itself is not canceled.
func TestPresetRefreshTimeoutFailures(t *testing.T) {
	ac := newTestPresetRedis(t, PresetBalanced)
	rb, held := holdPreset(t, ac)
	held.failure = context.DeadlineExceeded
	read := startPresetRead(ac, context.Background())
	<-held.started
	close(held.release)
	if err := awaitPresetError(t, read); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	probe := &versionProbeStorage{kvStorage: rb.storage, failure: context.DeadlineExceeded}
	rb.storage = probe
	rb.pollVersion()
	stats := ac.CacheStats()
	if stats.PresetReloadFailures != 1 || stats.PresetPollFailures != 1 {
		t.Fatal(stats)
	}
}
