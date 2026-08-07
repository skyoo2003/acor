// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
)

// rollbackRemoved undoes the removes a transactional RemoveMany already committed
// before a later keyword aborted the batch. Both tests below call it directly, and
// that is deliberate rather than lazy:
//
// the public route cannot reach it with a non-empty list. Only a non-batchPlanner
// mode falls through to the per-keyword transactional loop, V1 is the only one, and
// V1 writes return ErrV1ReadOnly (v1_ops.go:55,62) — so `removed` is still empty by
// the time the batch aborts. The undo is live code guarding the transactional
// contract, but nothing in the current mode mix can make it do work. Recorded here
// so the next reader does not mistake these direct calls for a shortcut.

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

// TestRollbackRemovedWithLogger covers the failure path: the undo is already the
// error path, so a re-add that itself fails is logged and swallowed rather than
// returned. Mirrors TestRollbackAddedWithLogger.
func TestRollbackRemovedWithLogger(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := newTestRedisClient(mr.Addr())
	defer func() { _ = client.Close() }()

	log := &testLogger{}
	ac := &AhoCorasick{
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

	mr.Close()

	ac.rollbackRemoved(context.Background(), []string{"keyword1"})
}

func TestRollbackAddedWithLogger(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := newTestRedisClient(mr.Addr())
	defer func() { _ = client.Close() }()

	log := &testLogger{}
	ac := &AhoCorasick{
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

	mr.Close()

	ac.rollbackAdded(context.Background(), []string{"keyword1"})
}

func TestRollbackAddedEmpty(t *testing.T) {
	ac, _ := createAhoCorasick(t)
	defer func() { _ = ac.Close() }()

	ac.rollbackAdded(context.Background(), []string{})
}
