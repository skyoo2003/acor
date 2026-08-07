// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	redis "github.com/redis/go-redis/v9"
)

// rollbackRemoved undoes the removes a transactional RemoveMany already committed
// before a later keyword aborted the batch. Both tests below call it directly, and
// that is deliberate rather than lazy:
//
// the public route cannot reach it with a non-empty list. Only a non-batchPlanner
// mode falls through to the per-keyword transactional loop, V1 is the only one, and
// v1Operations.add/remove return ErrV1ReadOnly — so `removed` is still empty by the
// time the batch aborts. The undo is live code guarding the transactional contract,
// but nothing in the current mode mix can make it do work. Recorded here so the
// next reader does not mistake these direct calls for a shortcut.

// recordingLogger captures what rollbackBatch logs. testLogger discards everything,
// so a test wired to it cannot tell a logged failure from a silent one.
type recordingLogger struct {
	mu    sync.Mutex
	lines []string
}

func (l *recordingLogger) Printf(format string, args ...interface{}) {
	l.mu.Lock()
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
	l.mu.Unlock()
}

func (l *recordingLogger) Println(...interface{}) {}

func (l *recordingLogger) recorded() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.lines...)
}

// TestRollbackRemovedRestoresKeywords asserts the undo's effect, not merely that it
// was entered: a keyword the batch removed is back in the dictionary afterwards.
func TestRollbackRemovedRestoresKeywords(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	if _, err := ac.Add("alpha"); err != nil {
		t.Fatalf("Add() error: %v", err)
	}
	if _, err := ac.Remove("alpha"); err != nil {
		t.Fatalf("Remove() error: %v", err)
	}
	if matches, err := ac.Find("alpha"); err != nil || len(matches) != 0 {
		t.Fatalf("Find() after Remove = %v (err %v), want no matches", matches, err)
	}

	ac.rollbackRemoved(context.Background(), []string{"alpha"})

	matches, err := ac.Find("alpha")
	if err != nil {
		t.Fatalf("Find() error: %v", err)
	}
	if len(matches) != 1 || matches[0] != "alpha" {
		t.Fatalf("Find() after rollbackRemoved = %v, want [alpha]; the undo did not re-add", matches)
	}
}

// TestRollbackRemovedRestoresEveryKeyword covers the multi-keyword undo. Every undo
// CAS-writes the same trie key, so this is the only shape that can make the undos
// interfere with one another; a one-keyword rollback never gets there.
func TestRollbackRemovedRestoresEveryKeyword(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	keywords := []string{"alpha", "bravo", "charlie", "delta", "echo"}
	for _, keyword := range keywords {
		if _, err := ac.Add(keyword); err != nil {
			t.Fatalf("Add(%q) error: %v", keyword, err)
		}
		if _, err := ac.Remove(keyword); err != nil {
			t.Fatalf("Remove(%q) error: %v", keyword, err)
		}
	}

	ac.rollbackRemoved(context.Background(), keywords)

	for _, keyword := range keywords {
		matches, err := ac.Find(keyword)
		if err != nil {
			t.Fatalf("Find(%q) error: %v", keyword, err)
		}
		if len(matches) != 1 || matches[0] != keyword {
			t.Errorf("Find(%q) after rollbackRemoved = %v, want [%q]; the undo dropped it",
				keyword, matches, keyword)
		}
	}
}

// TestRollbackBatchUndoesOneAtATime pins the mechanism the multi-keyword undo
// relies on. Every undo optimistically writes the same trie key, so undoing
// concurrently makes the undos race each other, and a loser that exhausts its
// retries comes back ErrConcurrencyConflict — which rollbackBatch only logs. That
// drop is probabilistic, and no end-to-end test catches it reliably, so the guard
// is on the invariant instead: never two undos in flight.
func TestRollbackBatchUndoesOneAtATime(t *testing.T) {
	keywords := []string{"alpha", "bravo", "charlie", "delta", "echo"}

	var mu sync.Mutex
	inFlight := 0
	overlapped := false
	var undone []string

	ac := &AhoCorasick{}
	ac.rollbackBatch(context.Background(), keywords, "remove",
		func(_ context.Context, keyword string) (int, error) {
			mu.Lock()
			inFlight++
			if inFlight > 1 {
				overlapped = true
			}
			undone = append(undone, keyword)
			mu.Unlock()

			// Long enough that a concurrent rollback would show an overlap; a
			// serial one never can, however slow the undo is.
			time.Sleep(time.Millisecond)

			mu.Lock()
			inFlight--
			mu.Unlock()
			return 1, nil
		})

	if overlapped {
		t.Error("rollbackBatch ran undos at the same time; they contend on one trie key " +
			"and a loser is dropped with only a log line")
	}
	if len(undone) != len(keywords) {
		t.Errorf("rollbackBatch undid %d of %d keywords: %v", len(undone), len(keywords), undone)
	}
}

// TestRollbackRemovedWithLogger covers the failure path: the undo is already the
// error path, so a re-add that itself fails is logged and swallowed rather than
// returned. Mirrors TestRollbackAddedWithLogger.
func TestRollbackRemovedWithLogger(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := newTestRedisClient(mr.Addr())
	defer func() { _ = client.Close() }()

	log := &recordingLogger{}
	ac := newRollbackTestAC(client, log)

	mr.Close()

	ac.rollbackRemoved(context.Background(), []string{"keyword1"})

	assertRollbackLogged(t, log, "re-add")
}

func TestRollbackAddedWithLogger(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := newTestRedisClient(mr.Addr())
	defer func() { _ = client.Close() }()

	log := &recordingLogger{}
	ac := newRollbackTestAC(client, log)

	mr.Close()

	ac.rollbackAdded(context.Background(), []string{"keyword1"})

	assertRollbackLogged(t, log, "remove")
}

// newRollbackTestAC builds a V2 instance around client, wired so that a rollback
// hitting a dead server reaches the logger.
func newRollbackTestAC(client redis.UniversalClient, log Logger) *AhoCorasick {
	return &AhoCorasick{
		redisClient:   client,
		storage:       newRedisStorage(client),
		ctx:           context.Background(),
		name:          "test",
		logger:        log,
		schemaVersion: SchemaV2,
		ops: &v2Operations{
			storage: newRedisStorage(client),
			client:  client,
			name:    "test",
			cache:   &trieCache{},
			logger:  log,
		},
	}
}

func assertRollbackLogged(t *testing.T, log *recordingLogger, op string) {
	t.Helper()
	want := fmt.Sprintf("rollback: failed to %s %q", op, "keyword1")
	for _, line := range log.recorded() {
		if strings.HasPrefix(line, want) {
			return
		}
	}
	t.Errorf("rollback logged %v, want a line starting %q; the undo failed silently",
		log.recorded(), want)
}

func TestRollbackAddedEmpty(t *testing.T) {
	ac, _ := createAhoCorasick(t)
	defer func() { _ = ac.Close() }()

	ac.rollbackAdded(context.Background(), []string{})
}
