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

	redis "github.com/go-redis/redis/v8"
	"golang.org/x/sync/singleflight"
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
	engine        matchEngine
	preset        Preset
	caseSensitive bool
	name          string

	storage     KVStorage
	redisClient redis.UniversalClient

	keywordSet   map[string]struct{}
	localVersion int64
	stale        bool

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
	if preset == PresetNone || preset == PresetDefault {
		preset = PresetBalanced
	}

	redisClient, err := newRedisClient(args)
	if err != nil {
		return nil, err
	}

	storage := newRedisStorage(redisClient)
	acCtx, acCancel := context.WithCancel(ctx)

	ac := &redisBackedAC{
		engine:        newMatchEngine(preset),
		preset:        preset,
		caseSensitive: args.CaseSensitive,
		name:          args.Name,
		storage:       storage,
		redisClient:   redisClient,
		keywordSet:    make(map[string]struct{}),
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
		err := ac.storage.HSet(ctx, trieKey(ac.name), map[string]interface{}{
			"keywords": "[]",
			"prefixes": "[\"\"]",
			"suffixes": "[\"\"]",
			"version":  time.Now().UnixNano(),
		})
		if err != nil {
			return fmt.Errorf("initialize trie: %w", err)
		}
	}
	return nil
}

func (ac *redisBackedAC) applyReload(snap *trieSnapshot) {
	keywordSet := make(map[string]struct{}, len(snap.Keywords))
	for _, kw := range snap.Keywords {
		keywordSet[kw] = struct{}{}
	}
	ac.keywordSet = keywordSet
	ac.engine.buildFromKeywords(keywordSet)
	ac.localVersion = snap.Version
	ac.stale = false
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

	err := ac.storage.Publish(ctx, channel, payload)
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
		[]string{args["trieKey"].(string), args["outputsKey"].(string)},
		args["oldVersion"], args["newVersion"], args["keywords"],
		args["prefixes"], args["suffixes"], args["outputs"])
	return cmd.Int64()
}

func (rb *redisBackedV2) runRemoveScript(ctx context.Context, args map[string]interface{}) (int64, error) {
	if err := validateScriptArgs(args); err != nil {
		return 0, err
	}
	cmd := removeV2Script.Run(ctx, rb.client,
		[]string{args["trieKey"].(string), args["outputsKey"].(string)},
		args["oldVersion"], args["newVersion"], args["keywords"],
		args["prefixes"], args["suffixes"], args["outputs"])
	return cmd.Int64()
}
