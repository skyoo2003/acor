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

	selfSkip   selfSkipSet
	reload     *presetReload
	generation uint64
	pubsub     subscription
	stopCh     chan struct{}
	ctx        context.Context
	cancel     context.CancelFunc
	closeOnce  sync.Once
	closed     int32
}

// presetReload is protected by mu; closing done publishes err to its waiters.
type presetReload struct {
	done    chan struct{}
	cancel  context.CancelFunc
	waiters int
	err     error
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

// prepareSnapshot builds an immutable engine without holding the state lock.
func (ac *redisBackedAC) prepareSnapshot(snap *trieSnapshot) (map[string]struct{}, *matchengine.Engine) {
	keywords := make(map[string]struct{}, len(snap.Keywords))
	for _, kw := range snap.Keywords {
		keywords[kw] = struct{}{}
	}
	start := time.Now()
	engine := buildEngine(ac.preset, keywords)
	ac.stats.recordRebuild(time.Since(start))
	return keywords, engine
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

// reloadFromRedis fetches and builds outside the state lock. A local write or
// invalidation during either phase makes the snapshot ineligible for installation.
func (ac *redisBackedAC) reloadFromRedis(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		ac.mu.RLock()
		generation := ac.generation
		ac.mu.RUnlock()
		snap, err := readTrieSnapshot(ctx, ac.storage, ac.name)
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		keywords, engine := ac.prepareSnapshot(snap)
		ac.mu.Lock()
		if err := ctx.Err(); err != nil {
			ac.mu.Unlock()
			return err
		}
		if ac.generation != generation {
			ac.mu.Unlock()
			continue
		}
		ac.engine = engine
		ac.keywordSet = keywords
		ac.localVersion = snap.Version
		ac.stale = false
		ac.generation++
		ac.mu.Unlock()
		return nil
	}
}

func (ac *redisBackedAC) markStale() {
	ac.mu.Lock()
	ac.stale = true
	ac.generation++
	ac.mu.Unlock()
}

func (ac *redisBackedAC) ensureValid(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	ac.mu.RLock()
	valid := !ac.stale
	ac.mu.RUnlock()
	if valid {
		ac.stats.hit()
		return nil
	}
	ac.mu.Lock()
	if !ac.stale {
		ac.mu.Unlock()
		ac.stats.hit()
		return nil
	}
	ac.stats.miss()
	work := ac.reload
	if work == nil {
		workCtx, cancel := context.WithCancel(ac.ctx) //nolint:gosec // Ownership transfers to work; completion or the final waiter calls cancel.
		work = &presetReload{done: make(chan struct{}), cancel: cancel}
		ac.reload = work
		go ac.runReload(workCtx, work)
	}
	work.waiters++
	ac.mu.Unlock()

	select {
	case <-ctx.Done():
	case <-ac.ctx.Done():
	case <-work.done:
	}
	ac.mu.Lock()
	work.waiters--
	if work.waiters == 0 {
		work.cancel()
		if ac.reload == work {
			ac.reload = nil
		}
	}
	ac.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := ac.ctx.Err(); err != nil {
		return err
	}
	return work.err
}

func (ac *redisBackedAC) runReload(ctx context.Context, work *presetReload) {
	err := ac.reloadFromRedis(ctx)
	ac.mu.Lock()
	defer ac.mu.Unlock()
	if err != nil && ctx.Err() == nil {
		ac.stats.reloadFailure()
	}
	work.err = err
	if ac.reload == work {
		ac.reload = nil
	}
	close(work.done)
	work.cancel()
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
// can recover on a subsequent successful poll and reload. The interval is not
// a freshness bound during failures.
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
	version, err := readTrieVersion(ac.ctx, ac.storage, ac.name)
	if err != nil {
		if ac.ctx.Err() == nil {
			ac.stats.pollFailure()
		}
		return
	}
	ac.mu.Lock()
	defer ac.mu.Unlock()
	// Repeated polls must not invalidate a reload already in progress. Otherwise
	// a build slower than the poll interval could be retried forever.
	if version != ac.localVersion && !ac.stale {
		ac.stale = true
		ac.generation++
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
