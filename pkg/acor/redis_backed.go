// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	redis "github.com/redis/go-redis/v9"
	"golang.org/x/sync/singleflight"

	matchengine "github.com/skyoo2003/acor/internal/engine"
)

// redisBackedAC is a Redis-backed Aho-Corasick automaton that combines the
// persistence of Redis (V2 schema) with the speed of a local preset-optimized
// automaton. Writes go to Redis atomically (Lua scripts with optimistic
// locking); reads hit the local automaton (no Redis I/O on the hot path).
//
// Cross-instance invalidation uses Redis Pub/Sub so that every instance
// rebuilds its local automaton when another instance mutates the data.
type redisBackedAC struct {
	mu            sync.RWMutex
	engine        *matchengine.Engine
	preset        Preset
	caseSensitive bool
	name          string

	storage     kvStorage
	redisClient redis.UniversalClient

	keywordSet   map[string]struct{}
	localVersion int64
	stale        bool
	pollInterval time.Duration

	stats *cacheStats

	selfSkip    selfSkipSet
	reloadGroup singleflight.Group
	pubsub      subscription
	stopCh      chan struct{}
	ctx         context.Context
	cancel      context.CancelFunc
	closeOnce   sync.Once
	closed      int32
}

// newRedisBacked creates a Redis-backed Aho-Corasick engine. It loads the
// current keywords from Redis, builds the local automaton, and starts a
// Pub/Sub listener for cross-instance invalidation.
func newRedisBacked(ctx context.Context, args *AhoCorasickArgs) (*redisBackedAC, error) {
	if args == nil {
		return nil, ErrNilArgs
	}
	if strings.Contains(args.Name, ":") {
		return nil, ErrInvalidName
	}

	preset := args.Preset
	if preset == PresetNone || preset == presetDefault {
		preset = PresetBalanced
	}

	redisClient, err := newRedisClient(args)
	if err != nil {
		return nil, err
	}

	storage := newRedisStorage(redisClient)
	// Background, not ctx: ctx is the construction context (see CreateContext) and
	// may be request-scoped, while acCtx drives the subscribe, listener, and poller
	// until Close. Deriving it from ctx would stop them the moment the caller's
	// request ended. initTrie and reloadFromRedis below still take ctx, so
	// construction honors its deadline for everything except the subscribe.
	acCtx, acCancel := context.WithCancel(context.Background())

	ac := &redisBackedAC{
		engine:        matchengine.New(enginePreset(preset)),
		preset:        preset,
		caseSensitive: args.CaseSensitive,
		name:          args.Name,
		storage:       storage,
		redisClient:   redisClient,
		keywordSet:    make(map[string]struct{}),
		stats:         &cacheStats{},
		pollInterval:  args.InvalidationPollInterval,
		ctx:           acCtx,
		cancel:        acCancel,
	}
	// Set before startListener shares the set with the listener goroutine;
	// selfSkipSet reads it without synchronization. Zero means the default.
	ac.selfSkip.cleanupEvery = args.SelfInvalidationCleanupInterval

	if err := ac.initTrie(ctx); err != nil {
		acCancel()
		_ = storage.Close()
		return nil, err
	}

	if err := ac.reloadFromRedis(ctx); err != nil {
		acCancel()
		_ = storage.Close()
		return nil, err
	}

	if err := ac.startListener(); err != nil {
		acCancel()
		_ = storage.Close()
		return nil, fmt.Errorf("pub/sub setup failed: %w", err)
	}

	if ac.pollInterval > 0 {
		ac.startPoller()
	}

	return ac, nil
}

// Close stops the Pub/Sub listener and closes the Redis connection.
func (ac *redisBackedAC) Close() error {
	var closeErr error
	ac.closeOnce.Do(func() {
		atomic.StoreInt32(&ac.closed, 1)
		ac.cancel()
		if ac.stopCh != nil {
			close(ac.stopCh)
		}
		if ac.pubsub != nil {
			_ = ac.pubsub.Close()
		}
		closeErr = ac.storage.Close()
	})
	return closeErr
}

// --- internal helpers ---

func (ac *redisBackedAC) initTrie(ctx context.Context) error {
	exists, err := ac.storage.Exists(ctx, trieKey(ac.name))
	if err != nil {
		return fmt.Errorf("check trie key: %w", err)
	}
	if exists == 0 {
		err := ac.storage.HSet(ctx, trieKey(ac.name), emptyTrieFields())
		if err != nil {
			return fmt.Errorf("initialize trie: %w", err)
		}
	}
	return nil
}

// buildEngine returns a freshly built engine for the given keyword set. The
// engine is replaced (not mutated in place) on every rebuild so that a pointer
// obtained under RLock stays immutable after the lock is released — this is what
// makes lock-free scanning (loadEngine) and long-running streaming safe.
func buildEngine(preset Preset, keywordSet map[string]struct{}) *matchengine.Engine {
	e := matchengine.New(enginePreset(preset))
	e.Build(keywordSet)
	return e
}

// rebuildEngine replaces the engine from the current keyword set and records the
// build. Every build in this mode routes through here, not just the reload path: a
// single-keyword write and a flush rebuild directly, and a build the counters miss
// inflates the mean rebuild cost that a write is supposed to explain.
//
// The timing covers the build alone. The Redis round trip that fetched the keywords
// happened in the caller, and folding it in would report the network as build time.
//
// Caller holds ac.mu.
func (ac *redisBackedAC) rebuildEngine() {
	start := time.Now()
	engine := buildEngine(ac.preset, ac.keywordSet)
	ac.stats.recordRebuild(time.Since(start))
	ac.engine = engine
}

func (ac *redisBackedAC) applyReload(snap *trieSnapshot) {
	keywordSet := make(map[string]struct{}, len(snap.Keywords))
	for _, kw := range snap.Keywords {
		keywordSet[kw] = struct{}{}
	}
	ac.keywordSet = keywordSet
	ac.rebuildEngine()
	ac.localVersion = snap.Version
	ac.stale = false
}

// loadEngine returns an immutable engine snapshot for the current keyword set,
// refreshing from Redis first if the local copy is stale. The returned engine is
// never mutated after this point (rebuilds swap in a new one), so the caller may
// scan it without holding ac.mu.
func (ac *redisBackedAC) loadEngine(ctx context.Context) (*matchengine.Engine, error) {
	if err := ac.ensureValid(ctx); err != nil {
		return nil, err
	}
	ac.mu.RLock()
	e := ac.engine
	ac.mu.RUnlock()
	return e, nil
}

func (ac *redisBackedAC) reloadFromRedis(ctx context.Context) error {
	snap, err := readTrieSnapshot(ctx, ac.storage, ac.name)
	if err != nil {
		return err
	}

	ac.mu.Lock()
	defer ac.mu.Unlock()
	ac.applyReload(snap)
	return nil
}

func (ac *redisBackedAC) markStale() {
	ac.mu.Lock()
	ac.stale = true
	ac.mu.Unlock()
}

func (ac *redisBackedAC) ensureValid(ctx context.Context) error {
	ac.mu.RLock()
	if !ac.stale {
		ac.mu.RUnlock()
		ac.stats.hit()
		return nil
	}
	ac.mu.RUnlock()

	// Counted before the singleflight, not inside it: this read found stale state and
	// waits for a rebuild whether or not it performs one. Readers coalesced onto
	// another goroutine's reload therefore still count as misses, which is what makes
	// Misses-Rebuilds the work the coalescing saved.
	ac.stats.miss()

	_, err, _ := ac.reloadGroup.Do("reload", func() (interface{}, error) {
		ac.mu.Lock()
		defer ac.mu.Unlock()

		if !ac.stale {
			return nil, nil
		}

		snap, err := readTrieSnapshot(ctx, ac.storage, ac.name)
		if err != nil {
			return nil, err
		}
		ac.applyReload(snap)
		return nil, nil
	})
	return err
}

// --- Pub/Sub ---

// publishRetryAttempts / publishRetryBackoff give the fire-and-forget invalidation
// PUBLISH a few tries before giving up, so a single transient blip does not drop a
// whole batch's cross-node notification. The version poller (InvalidationPollInterval)
// is the durable safety net; this just narrows the transient-failure window.
const (
	publishRetryAttempts = 3
	publishRetryBackoff  = 10 * time.Millisecond
)

func (ac *redisBackedAC) startListener() error {
	ac.stopCh = make(chan struct{})

	// Bind the counters once, as AhoCorasick.startCacheListener does: the callback
	// runs on its own goroutine, so reading the field live would race anything that
	// reassigns it (the stats benchmark switches recording off that way).
	stats := ac.stats
	pubsub, err := subscribeInvalidations(ac.ctx, ac.storage, ac.name, ac.stopCh, func(payload string) {
		ac.handleInvalidation(stats, payload)
	})
	if err != nil {
		return err
	}

	ac.pubsub = pubsub
	return nil
}

func (ac *redisBackedAC) handleInvalidation(stats *cacheStats, payload string) {
	if !foreignInvalidation(payload, ac.name, &ac.selfSkip, stats) {
		return
	}
	ac.markStale()
}

// startPoller runs a background safety net for missed Pub/Sub invalidations:
// every pollInterval it compares the stored collection version against the local
// one and marks the engine stale on any difference, so a dropped invalidation
// self-heals within one interval instead of persisting until the next local write.
func (ac *redisBackedAC) startPoller() {
	go func() {
		ticker := time.NewTicker(ac.pollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				ac.pollVersion()
			case <-ac.stopCh:
				return
			case <-ac.ctx.Done():
				return
			}
		}
	}()
}

// pollVersion marks the engine stale if Redis holds a version other than the one
// last loaded locally. A transient read error is ignored; the next tick retries.
func (ac *redisBackedAC) pollVersion() {
	snap, err := readTrieSnapshot(ac.ctx, ac.storage, ac.name)
	if err != nil {
		return
	}
	ac.mu.RLock()
	changed := snap.Version != ac.localVersion
	ac.mu.RUnlock()
	if changed {
		ac.markStale()
	}
}

func (ac *redisBackedAC) publishInvalidate(ctx context.Context) {
	channel := invalidateChannelPrefix + ac.name
	msgID := newInvalidationID()
	payload := invalidationPayload(ac.name, msgID)

	ac.selfSkip.add(msgID)

	var err error
	for attempt := 0; attempt < publishRetryAttempts; attempt++ {
		if err = ac.storage.Publish(ctx, channel, payload); err == nil {
			break
		}
		if attempt == publishRetryAttempts-1 {
			break
		}
		select {
		case <-ctx.Done():
			// Undelivered: drop the self-skip entry so it does not linger, then stop.
			ac.selfSkip.forget(msgID)
			return
		case <-time.After(publishRetryBackoff):
		}
	}
	if err != nil {
		ac.selfSkip.forget(msgID)
	}
}
