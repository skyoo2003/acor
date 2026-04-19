// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	redis "github.com/go-redis/redis/v8"
	"golang.org/x/sync/singleflight"
)

// RedisBackedArgs configures a Redis-backed Aho-Corasick engine with a
// selectable architecture preset. Redis is the source of truth; a local
// preset-optimized automaton is cached for fast reads.
type RedisBackedArgs struct {
	AhoCorasickArgs
	Preset        Preset
	CaseSensitive bool
}

// RedisBackedAC is a Redis-backed Aho-Corasick automaton that combines the
// persistence of Redis (V2 schema) with the speed of a local preset-optimized
// automaton. Writes go to Redis atomically (Lua scripts with optimistic
// locking); reads hit the local automaton (no Redis I/O on the hot path).
//
// Cross-instance invalidation uses Redis Pub/Sub so that every instance
// rebuilds its local automaton when another instance mutates the data.
type RedisBackedAC struct {
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

// NewRedisBacked creates a Redis-backed Aho-Corasick engine. It loads the
// current keywords from Redis, builds the local automaton, and starts a
// Pub/Sub listener for cross-instance invalidation.
func NewRedisBacked(ctx context.Context, args *RedisBackedArgs) (*RedisBackedAC, error) {
	if args == nil || strings.Contains(args.Name, ":") {
		return nil, ErrInvalidName
	}

	preset := args.Preset
	if preset < PresetSpeed || preset > PresetUltimate {
		preset = PresetBalanced
	}

	redisClient, err := newRedisClient(&args.AhoCorasickArgs)
	if err != nil {
		return nil, err
	}

	storage := newRedisStorage(redisClient)
	acCtx, acCancel := context.WithCancel(ctx)

	ac := &RedisBackedAC{
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
func (ac *RedisBackedAC) Close() error {
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

// Preset returns the architecture preset used by this engine.
func (ac *RedisBackedAC) Preset() Preset {
	return ac.preset
}

// --- internal helpers ---

func (ac *RedisBackedAC) initTrie(ctx context.Context) error {
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

func (ac *RedisBackedAC) reloadFromRedis(ctx context.Context) error {
	trieData, err := ac.storage.HGetAll(ctx, trieKey(ac.name))
	if err != nil {
		return fmt.Errorf("HGETALL %s: %w", trieKey(ac.name), err)
	}

	var keywords []string
	if data, ok := trieData["keywords"]; ok {
		if err := json.Unmarshal([]byte(data), &keywords); err != nil {
			return fmt.Errorf("unmarshal keywords: %w", err)
		}
	}

	var version int64
	if v, ok := trieData["version"]; ok {
		if err := json.Unmarshal([]byte(v), &version); err != nil {
			return fmt.Errorf("unmarshal version: %w", err)
		}
	}

	keywordSet := make(map[string]struct{}, len(keywords))
	for _, kw := range keywords {
		keywordSet[kw] = struct{}{}
	}

	ac.mu.Lock()
	defer ac.mu.Unlock()

	ac.keywordSet = keywordSet
	ac.engine.buildFromKeywords(keywordSet)
	ac.localVersion = version
	ac.stale = false

	return nil
}

func (ac *RedisBackedAC) markStale() {
	ac.mu.Lock()
	ac.stale = true
	ac.mu.Unlock()
}

func (ac *RedisBackedAC) ensureValid(ctx context.Context) error {
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

		if err := ac.reloadFromRedisLocked(ctx); err != nil {
			// Degraded mode: keep last-good engine, stay stale.
			return nil, nil
		}
		return nil, nil
	})
	return err
}

// reloadFromRedisLocked reloads from Redis. Caller must hold ac.mu (write).
func (ac *RedisBackedAC) reloadFromRedisLocked(ctx context.Context) error {
	trieData, err := ac.storage.HGetAll(ctx, trieKey(ac.name))
	if err != nil {
		return err
	}

	var keywords []string
	if data, ok := trieData["keywords"]; ok {
		if err := json.Unmarshal([]byte(data), &keywords); err != nil {
			return err
		}
	}

	var version int64
	if v, ok := trieData["version"]; ok {
		if err := json.Unmarshal([]byte(v), &version); err != nil {
			return fmt.Errorf("unmarshal version: %w", err)
		}
	}

	keywordSet := make(map[string]struct{}, len(keywords))
	for _, kw := range keywords {
		keywordSet[kw] = struct{}{}
	}

	ac.keywordSet = keywordSet
	ac.engine.buildFromKeywords(keywordSet)
	ac.localVersion = version
	ac.stale = false
	return nil
}

// --- Pub/Sub ---

const selfInvalTTL = 30 * time.Second

func (ac *RedisBackedAC) startListener() error {
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

func (ac *RedisBackedAC) handleInvalidation(payload string) {
	if parts := strings.SplitN(payload, ":", invalidatePayloadSplitMax); len(parts) == invalidatePayloadSplitMax && parts[0] == ac.name {
		if ac.skipSelfCheck(parts[1]) {
			return
		}
	}
	ac.markStale()
}

func (ac *RedisBackedAC) skipSelfSet(id string) {
	ac.selfSkip.Store(id, time.Now())
}

func (ac *RedisBackedAC) skipSelfCheck(id string) bool {
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

func (ac *RedisBackedAC) publishInvalidate(ctx context.Context) {
	channel := invalidateChannelPrefix + ac.name
	b := make([]byte, invalidateIDBytes)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failure is extremely rare; fall back to timestamp-only ID.
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

func (ac *RedisBackedAC) cleanupExpiredSelfSkips() {
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
