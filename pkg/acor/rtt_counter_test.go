// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"sync/atomic"
	"testing"
)

// Round-trip counting for the performance claims published in README.md and
// docs/content/reference/benchmarks.md.
//
// This counts *round trips*, not commands. A pipeline carrying N commands is
// one round trip, so TxPipelined and pipeliner.Exec each add 1 regardless of
// how many commands were queued. Counting commands instead would inflate every
// published number in the direction that flatters us.
//
// Counting is backend-independent: it wraps the kvStorage seam, not the wire,
// so miniredis and a real server must report identical counts. That is what
// makes these claims structural rather than incidental to a benchmark host.

// rttCounter accumulates round trips across every storage handle wrapped for a
// single AhoCorasick instance.
type rttCounter struct {
	n atomic.Int64
}

func (c *rttCounter) add()       { c.n.Add(1) }
func (c *rttCounter) count() int { return int(c.n.Load()) }
func (c *rttCounter) reset()     { c.n.Store(0) }

// countRTT wraps every storage handle reachable from ac so subsequent calls are
// counted. Call it after Create and before the operation under test.
//
// ac.storage is not sufficient on its own: newV2Ops/newV1Ops copy the reference
// at construction (acor.go:561), and preset mode leaves ac.storage nil entirely,
// holding storage on redisBackedAC instead. All live handles must be wrapped or
// the operation under test is measured through an unwrapped path and silently
// reports zero.
func countRTT(tb testing.TB, ac *AhoCorasick) *rttCounter {
	tb.Helper()
	c := &rttCounter{}
	wrap := func(s kvStorage) kvStorage {
		if s == nil {
			return nil
		}
		return &countingStorage{inner: s, c: c}
	}

	ac.storage = wrap(ac.storage)

	switch o := ac.ops.(type) {
	case *v2Operations:
		o.storage = wrap(o.storage)
	case *v1Operations:
		o.storage = wrap(o.storage)
	case *v1WritableOps:
		// The fixture-writable V1 wrapper shares the embedded v1Operations, so
		// wrapping its storage counts the same round trips.
		o.storage = wrap(o.storage)
	case *redisBackedAC:
		o.mu.Lock()
		o.storage = wrap(o.storage)
		o.mu.Unlock()
	default:
		// A new backend strategy must be wired in here. Failing loudly beats
		// reporting 0 round trips for a path nobody wrapped.
		tb.Fatalf("countRTT: unhandled operations type %T; add it to the switch", ac.ops)
	}

	return c
}

// countingStorage counts one round trip per storage call.
//
// Every method is implemented explicitly rather than embedding kvStorage: if
// the interface gains a method, this fails to compile instead of quietly
// passing the new call through uncounted.
type countingStorage struct {
	inner kvStorage
	c     *rttCounter
}

func (s *countingStorage) HGetAll(ctx context.Context, key string) (map[string]string, error) {
	s.c.add()
	return s.inner.HGetAll(ctx, key)
}

func (s *countingStorage) HSet(ctx context.Context, key string, values ...interface{}) error {
	s.c.add()
	return s.inner.HSet(ctx, key, values...)
}

func (s *countingStorage) SAdd(ctx context.Context, key string, members ...interface{}) error {
	s.c.add()
	return s.inner.SAdd(ctx, key, members...)
}

func (s *countingStorage) SMembers(ctx context.Context, key string) ([]string, error) {
	s.c.add()
	return s.inner.SMembers(ctx, key)
}

func (s *countingStorage) SRem(ctx context.Context, key string, members ...interface{}) error {
	s.c.add()
	return s.inner.SRem(ctx, key, members...)
}

func (s *countingStorage) SCard(ctx context.Context, key string) (int64, error) {
	s.c.add()
	return s.inner.SCard(ctx, key)
}

func (s *countingStorage) SIsMember(ctx context.Context, key, member string) (bool, error) {
	s.c.add()
	return s.inner.SIsMember(ctx, key, member)
}

func (s *countingStorage) ZAdd(ctx context.Context, key string, members ...*zMember) error {
	s.c.add()
	return s.inner.ZAdd(ctx, key, members...)
}

func (s *countingStorage) ZRange(ctx context.Context, key string, start, stop int64) ([]string, error) {
	s.c.add()
	return s.inner.ZRange(ctx, key, start, stop)
}

func (s *countingStorage) ZRank(ctx context.Context, key, member string) (int64, error) {
	s.c.add()
	return s.inner.ZRank(ctx, key, member)
}

func (s *countingStorage) ZScore(ctx context.Context, key, member string) (float64, error) {
	s.c.add()
	return s.inner.ZScore(ctx, key, member)
}

func (s *countingStorage) ZCard(ctx context.Context, key string) (int64, error) {
	s.c.add()
	return s.inner.ZCard(ctx, key)
}

func (s *countingStorage) ZRem(ctx context.Context, key string, members ...interface{}) error {
	s.c.add()
	return s.inner.ZRem(ctx, key, members...)
}

func (s *countingStorage) Del(ctx context.Context, keys ...string) error {
	s.c.add()
	return s.inner.Del(ctx, keys...)
}

func (s *countingStorage) Exists(ctx context.Context, keys ...string) (int64, error) {
	s.c.add()
	return s.inner.Exists(ctx, keys...)
}

// TxPipelined is one round trip for the whole transaction, however many
// commands the callback queues.
func (s *countingStorage) TxPipelined(ctx context.Context, fn func(pipeliner) error) error {
	s.c.add()
	return s.inner.TxPipelined(ctx, fn)
}

// Pipeline buffers locally and costs nothing until Exec, which is where the
// round trip is counted.
func (s *countingStorage) Pipeline() pipeliner {
	return &countingPipeliner{inner: s.inner.Pipeline(), c: s.c}
}

func (s *countingStorage) Publish(ctx context.Context, channel string, message interface{}) error {
	s.c.add()
	return s.inner.Publish(ctx, channel, message)
}

func (s *countingStorage) Subscribe(ctx context.Context, channels ...string) subscription {
	s.c.add()
	return s.inner.Subscribe(ctx, channels...)
}

// Close is connection teardown, not part of any measured operation.
func (s *countingStorage) Close() error { return s.inner.Close() }

// countingPipeliner counts the single round trip that Exec performs. Queuing
// commands costs nothing.
type countingPipeliner struct {
	inner pipeliner
	c     *rttCounter
}

func (p *countingPipeliner) SAdd(ctx context.Context, key string, members ...interface{}) error {
	return p.inner.SAdd(ctx, key, members...)
}

func (p *countingPipeliner) HSet(ctx context.Context, key string, values ...interface{}) error {
	return p.inner.HSet(ctx, key, values...)
}

func (p *countingPipeliner) HGetAll(ctx context.Context, key string) stringMapResult {
	return p.inner.HGetAll(ctx, key)
}

func (p *countingPipeliner) ZAdd(ctx context.Context, key string, members ...*zMember) error {
	return p.inner.ZAdd(ctx, key, members...)
}

func (p *countingPipeliner) Del(ctx context.Context, keys ...string) error {
	return p.inner.Del(ctx, keys...)
}

func (p *countingPipeliner) Exec(ctx context.Context) error {
	p.c.add()
	return p.inner.Exec(ctx)
}

// Compile-time proof the wrappers still satisfy the interfaces they stand in for.
var (
	_ kvStorage = (*countingStorage)(nil)
	_ pipeliner = (*countingPipeliner)(nil)
)

func (s *countingStorage) HGet(ctx context.Context, key, field string) (string, error) {
	s.c.add()
	return s.inner.HGet(ctx, key, field)
}
