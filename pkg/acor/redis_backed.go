// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"crypto/rand"
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

	storage     KVStorage
	redisClient redis.UniversalClient

	keywordSet   map[string]struct{}
	localVersion int64
	stale        bool
	pollInterval time.Duration

	selfSkip      sync.Map
	selfSkipCount uint64
	reloadGroup   singleflight.Group
	pubsub        Subscription
	stopCh        chan struct{}
	ctx           context.Context
	cancel        context.CancelFunc
	closeOnce     sync.Once
	closed        int32
}

// newRedisBacked creates a Redis-backed Aho-Corasick engine. It loads the
// current keywords from Redis, builds the local automaton, and starts a
// Pub/Sub listener for cross-instance invalidation.
func newRedisBacked(ctx context.Context, args *AhoCorasickArgs) (*redisBackedAC, error) {
	if args == nil || strings.Contains(args.Name, ":") {
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
	acCtx, acCancel := context.WithCancel(ctx)

	ac := &redisBackedAC{
		engine:        matchengine.New(preset),
		preset:        preset,
		caseSensitive: args.CaseSensitive,
		name:          args.Name,
		storage:       storage,
		redisClient:   redisClient,
		keywordSet:    make(map[string]struct{}),
		pollInterval:  args.InvalidationPollInterval,
		ctx:           acCtx,
		cancel:        acCancel,
	}

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
	e := matchengine.New(preset)
	e.Build(keywordSet)
	return e
}

func (ac *redisBackedAC) applyReload(snap *trieSnapshot) {
	keywordSet := make(map[string]struct{}, len(snap.Keywords))
	for _, kw := range snap.Keywords {
		keywordSet[kw] = struct{}{}
	}
	ac.keywordSet = keywordSet
	ac.engine = buildEngine(ac.preset, keywordSet)
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
		return nil
	}
	ac.mu.RUnlock()

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

const selfInvalTTL = 30 * time.Second

// publishRetryAttempts / publishRetryBackoff give the fire-and-forget invalidation
// PUBLISH a few tries before giving up, so a single transient blip does not drop a
// whole batch's cross-node notification. The version poller (InvalidationPollInterval)
// is the durable safety net; this just narrows the transient-failure window.
const (
	publishRetryAttempts = 3
	publishRetryBackoff  = 10 * time.Millisecond
)

func (ac *redisBackedAC) startListener() error {
	channel := invalidateChannelPrefix + ac.name
	pubsub := ac.storage.Subscribe(ac.ctx, channel)
	if err := pubsub.Receive(ac.ctx); err != nil {
		_ = pubsub.Close()
		return err
	}

	ac.pubsub = pubsub
	ac.stopCh = make(chan struct{})

	go func() {
		msgCh := pubsub.Channel()
		for {
			select {
			case msg, ok := <-msgCh:
				if !ok {
					return
				}
				ac.handleInvalidation(msg.Payload)
			case <-ac.stopCh:
				return
			case <-ac.ctx.Done():
				return
			}
		}
	}()

	return nil
}

func (ac *redisBackedAC) handleInvalidation(payload string) {
	if parts := strings.SplitN(payload, ":", invalidatePayloadSplitMax); len(parts) == invalidatePayloadSplitMax && parts[0] == ac.name {
		if ac.skipSelfCheck(parts[1]) {
			return
		}
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

func (ac *redisBackedAC) skipSelfSet(id string) {
	ac.selfSkip.Store(id, time.Now())
}

func (ac *redisBackedAC) skipSelfCheck(id string) bool {
	val, loaded := ac.selfSkip.LoadAndDelete(id)
	if !loaded {
		return false
	}
	t, ok := val.(time.Time)
	if !ok {
		return false
	}
	return time.Since(t) < selfInvalTTL
}

func (ac *redisBackedAC) publishInvalidate(ctx context.Context) {
	channel := invalidateChannelPrefix + ac.name
	b := make([]byte, invalidateIDBytes)
	if _, err := rand.Read(b); err != nil {
		b = b[:0]
	}
	msgID := fmt.Sprintf("%d:%x", time.Now().UnixNano(), b)
	payload := ac.name + ":" + msgID

	ac.skipSelfSet(msgID)
	if atomic.AddUint64(&ac.selfSkipCount, 1)%defaultSelfInvalidationCleanupInterval == 0 {
		ac.cleanupExpiredSelfSkips()
	}

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
			ac.selfSkip.Delete(msgID)
			return
		case <-time.After(publishRetryBackoff):
		}
	}
	if err != nil {
		ac.selfSkip.Delete(msgID)
	}
}

func (ac *redisBackedAC) cleanupExpiredSelfSkips() {
	cutoff := time.Now().Add(-selfInvalTTL)
	ac.selfSkip.Range(func(key, value interface{}) bool {
		t, ok := value.(time.Time)
		if !ok || t.Before(cutoff) {
			ac.selfSkip.Delete(key)
		}
		return true
	})
}

// --- V2 Lua script adapter ---

type redisBackedV2 struct {
	client redis.UniversalClient
}

func (rb *redisBackedV2) runAddScript(ctx context.Context, args map[string]interface{}) (int64, error) {
	if err := validateScriptArgs(args); err != nil {
		return 0, err
	}
	cmd := addV2Script.Run(ctx, rb.client,
		[]string{args[argTrieKey].(string), args[argOutputsKey].(string)},
		args["oldVersion"], args["newVersion"], args[fieldKeywords],
		args[fieldPrefixes], args[fieldSuffixes], args["outputs"])
	return cmd.Int64()
}

func (rb *redisBackedV2) runRemoveScript(ctx context.Context, args map[string]interface{}) (int64, error) {
	if err := validateScriptArgs(args); err != nil {
		return 0, err
	}
	cmd := removeV2Script.Run(ctx, rb.client,
		[]string{args[argTrieKey].(string), args[argOutputsKey].(string)},
		args["oldVersion"], args["newVersion"], args[fieldKeywords],
		args[fieldPrefixes], args[fieldSuffixes], args["outputs"])
	return cmd.Int64()
}
