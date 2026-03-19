// Package acor means Aho-Corasick automation working On Redis, Written in Go
package acor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	redis "github.com/go-redis/redis/v8"
)

const (
	initScore   = 0.0
	memberScore = 1.0
)

var (
	// ErrRedisAlreadyClosed is returned when attempting to close an already closed Redis client.
	ErrRedisAlreadyClosed = errors.New("redis client was already closed")
	// ErrRedisConflictingTopology is returned when conflicting Redis topology settings are provided.
	ErrRedisConflictingTopology = errors.New("redis topology settings are conflicting")
	// ErrRedisSentinelAddrs is returned when sentinel mode is specified without addresses.
	ErrRedisSentinelAddrs = errors.New("redis sentinel requires at least one address")
	// ErrRedisClusterDB is returned when DB selection is used with cluster mode.
	ErrRedisClusterDB = errors.New("redis cluster does not support DB selection")
	// ErrRedisRingAddrs is returned when ring mode is specified without shard addresses.
	ErrRedisRingAddrs = errors.New("redis ring requires at least one shard address")
)

// Logger defines the interface for logging operations used by AhoCorasick.
type Logger interface {
	Printf(format string, v ...interface{})
	Println(v ...interface{})
}

// AhoCorasickArgs contains configuration options for creating an AhoCorasick instance.
type AhoCorasickArgs struct {
	Addr          string // redis server address (ex) localhost:6379
	Addrs         []string
	MasterName    string
	RingAddrs     map[string]string
	Password      string // redis password
	DB            int    // redis db number
	Name          string // pattern's collection name
	Debug         bool   // debug flag
	Logger        Logger // Optional custom logger
	SchemaVersion int    // 1: V1 (Legacy), 2 or 0: V2 (default)
}

// AhoCorasick represents an Aho-Corasick automaton backed by Redis.
type AhoCorasick struct {
	ctx           context.Context
	redisClient   redis.UniversalClient // redis client
	name          string                // Pattern's collection name
	logger        Logger                // logger
	buildTrieHook func(string) error
	schemaVersion int // detected schema version
}

// AhoCorasickInfo contains statistics about the Aho-Corasick automaton.
type AhoCorasickInfo struct {
	Keywords int // Aho-Corasick keywords count
	Nodes    int // Aho-Corasick nodes count
}

// Create initializes and returns a new AhoCorasick instance connected to Redis.
func Create(args *AhoCorasickArgs) (*AhoCorasick, error) {
	stdLogger := log.New(io.Discard, "ACOR: ", log.LstdFlags|log.Lshortfile)
	if args.Debug {
		stdLogger.SetOutput(os.Stdout)
	}

	var logger Logger = stdLogger
	if args.Logger != nil {
		logger = args.Logger
	}

	redisClient, err := newRedisClient(args)
	if err != nil {
		return nil, err
	}

	ac := &AhoCorasick{
		redisClient:   redisClient,
		ctx:           context.Background(),
		name:          args.Name,
		logger:        logger,
		schemaVersion: args.SchemaVersion,
	}

	if ac.schemaVersion == 0 {
		ac.schemaVersion = SchemaV2
	}

	if err := ac.init(); err != nil {
		_ = ac.redisClient.Close()
		return nil, err
	}
	return ac, nil
}

// SchemaVersion returns the current schema version used by the AhoCorasick instance.
func (ac *AhoCorasick) SchemaVersion() int {
	return ac.schemaVersion
}

func (ac *AhoCorasick) init() error {
	if ac.schemaVersion == SchemaV2 {
		exists, err := ac.redisClient.Exists(ac.ctx, trieKey(ac.name)).Result()
		if err != nil {
			return fmt.Errorf("failed to check trie key: %w", err)
		}
		if exists == 0 {
			_, err := ac.redisClient.HSet(ac.ctx, trieKey(ac.name), map[string]interface{}{
				"keywords": "[]",
				"prefixes": "[\"\"]",
				"suffixes": "[\"\"]",
				"version":  time.Now().UnixNano(),
			}).Result()
			if err != nil {
				return err
			}
		}
		return nil
	}

	prefixKey := prefixKey(ac.name)
	member := &redis.Z{
		Score:  initScore,
		Member: "",
	}
	if err := ac.redisClient.ZAdd(ac.ctx, prefixKey, member).Err(); err != nil {
		return err
	}
	return nil
}

// Close closes the Redis client connection.
func (ac *AhoCorasick) Close() error {
	if ac.redisClient != nil {
		return ac.redisClient.Close()
	}
	return ErrRedisAlreadyClosed
}

// Add inserts a keyword into the Aho-Corasick automaton.
func (ac *AhoCorasick) Add(keyword string) (int, error) {
	if ac.schemaVersion == SchemaV2 {
		return ac.addV2(keyword)
	}
	return ac.addV1(keyword)
}

// Remove deletes a keyword from the Aho-Corasick automaton.
func (ac *AhoCorasick) Remove(keyword string) (int, error) {
	if ac.schemaVersion == SchemaV2 {
		return ac.removeV2(keyword)
	}
	return ac.removeV1(keyword)
}

// Find searches for all keywords in the given text and returns matched keywords.
func (ac *AhoCorasick) Find(text string) ([]string, error) {
	if ac.schemaVersion == SchemaV2 {
		return ac.findV2(text)
	}
	return ac.findV1(text)
}

// FindIndex searches for keywords in text and returns their start indices.
func (ac *AhoCorasick) FindIndex(text string) (map[string][]int, error) {
	if ac.schemaVersion == SchemaV2 {
		return ac.findIndexV2(text)
	}
	return ac.findIndexV1(text)
}

// Flush removes all keywords and trie data from Redis.
func (ac *AhoCorasick) Flush() error {
	if ac.schemaVersion == SchemaV2 {
		return ac.flushV2()
	}
	return ac.flushV1()
}

// Info returns statistics about the Aho-Corasick automaton.
func (ac *AhoCorasick) Info() (*AhoCorasickInfo, error) {
	if ac.schemaVersion == SchemaV2 {
		return ac.infoV2()
	}
	return ac.infoV1()
}

// Suggest returns keywords that start with the given input prefix.
func (ac *AhoCorasick) Suggest(input string) ([]string, error) {
	if ac.schemaVersion == SchemaV2 {
		return ac.suggestV2(input)
	}
	return ac.suggestV1(input)
}

// SuggestIndex returns keywords matching the prefix with their indices.
func (ac *AhoCorasick) SuggestIndex(input string) (map[string][]int, error) {
	if ac.schemaVersion == SchemaV2 {
		return ac.suggestIndexV2(input)
	}
	return ac.suggestIndexV1(input)
}

// Debug prints the current state of the Aho-Corasick automaton for debugging.
func (ac *AhoCorasick) Debug() {
	kKey := keywordKey(ac.name)
	fmt.Println("-", ac.redisClient.SMembers(ac.ctx, kKey))

	pKey := prefixKey(ac.name)
	fmt.Println("-", ac.redisClient.ZRange(ac.ctx, pKey, 0, -1))

	sKey := suffixKey(ac.name)
	fmt.Println("-", ac.redisClient.ZRange(ac.ctx, sKey, 0, -1))

	outputs := make([]string, 0)
	pKeywords := ac.redisClient.ZRange(ac.ctx, pKey, 0, -1).Val()
	for _, keyword := range pKeywords {
		oKeywords, err := ac._output(keyword)
		if err != nil {
			fmt.Println("-", err)
			return
		}
		outputs = append(outputs, oKeywords...)
	}
	fmt.Println("-", outputs)

	nodes := make([]string, 0)
	kKeywords := ac.redisClient.SMembers(ac.ctx, kKey).Val()
	for _, keyword := range kKeywords {
		nKey := nodeKey(ac.name, keyword)
		nKeywords := ac.redisClient.SMembers(ac.ctx, nKey).Val()
		nodes = append(nodes, nKeywords...)
	}
	fmt.Println("-", nodes)
}
