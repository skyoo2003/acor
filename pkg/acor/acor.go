// Package acor implements an Aho-Corasick string matching automaton backed by Redis.
//
// ACOR (Aho-Corasick On Redis) provides efficient multi-pattern matching with O(n + m)
// time complexity where n is the input text length and m is the total number of matches.
// The automaton state is stored in Redis, enabling distributed access and persistence.
//
// # Features
//
//   - Redis-backed storage for distributed state and persistence
//   - Support for multiple Redis topologies: Standalone, Sentinel, Cluster, and Ring
//   - Two schema versions: V1 (legacy) and V2 (optimized with fewer keys)
//   - Thread-safe operations with optimistic locking (V2)
//   - Batch operations for bulk keyword management
//   - Parallel text matching for improved performance on large texts
//   - Prefix-based keyword suggestions
//
// # Quick Start
//
// Basic usage with a standalone Redis instance:
//
//	ac, err := acor.Create(&acor.AhoCorasickArgs{
//	    Addr: "localhost:6379",
//	    Name: "my-collection",
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer ac.Close()
//
//	// Add keywords to the automaton
//	ac.Add("hello")
//	ac.Add("world")
//
//	// Find all matches in a text
//	matches, err := ac.Find("hello world")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Println(matches) // Output: [hello world]
//
// # Redis Topologies
//
// ACOR supports multiple Redis deployment modes:
//
// Standalone (default):
//
//	ac, _ := acor.Create(&acor.AhoCorasickArgs{
//	    Addr: "localhost:6379",
//	    Name: "my-collection",
//	})
//
// Sentinel for high availability:
//
//	ac, _ := acor.Create(&acor.AhoCorasickArgs{
//	    Addrs:      []string{"sentinel1:26379", "sentinel2:26379"},
//	    MasterName: "mymaster",
//	    Name:       "my-collection",
//	})
//
// Cluster for horizontal scaling:
//
//	ac, _ := acor.Create(&acor.AhoCorasickArgs{
//	    Addrs: []string{"node1:6379", "node2:6379", "node3:6379"},
//	    Name:  "my-collection",
//	})
//
// Ring for client-side sharding:
//
//	ac, _ := acor.Create(&acor.AhoCorasickArgs{
//	    RingAddrs: map[string]string{
//	        "shard1": "redis1:6379",
//	        "shard2": "redis2:6379",
//	    },
//	    Name: "my-collection",
//	})
//
// # Schema Versions
//
// V1 (SchemaVersion: 1): Legacy schema using multiple Redis keys for each prefix/suffix/output.
// Suitable for small collections but creates many keys.
//
// V2 (SchemaVersion: 2, default): Optimized schema consolidating data into 3 keys.
// Recommended for most use cases. Uses Lua scripts for atomic operations.
//
// # Batch Operations
//
// Use AddMany and RemoveMany for bulk operations:
//
//	result, err := ac.AddMany([]string{"foo", "bar", "baz"}, nil)
//	fmt.Printf("Added: %d, Failed: %d\n", len(result.Added), len(result.Failed))
//
// # Parallel Matching
//
// For large texts, use FindParallel to split work across multiple goroutines:
//
//	matches, err := ac.FindParallel(largeText, &acor.ParallelOptions{
//	    Workers:   8,
//	    ChunkSize: 10000,
//	})
//
// # Local Caching
//
// For read-heavy workloads, enable local caching to eliminate Redis round-trips:
//
//	ac, _ := acor.Create(&acor.AhoCorasickArgs{
//	    Addr:        "localhost:6379",
//	    Name:        "my-collection",
//	    EnableCache: true,
//	})
//
// Cache synchronization uses Redis Pub/Sub. When any instance modifies the collection,
// all instances receive an invalidation message and reload on next Find().
//
// # Thread Safety
//
// All operations are safe for concurrent use. V2 schema uses optimistic locking
// with automatic retries for write operations.
package acor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"
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
	// ErrRedisConflictingTopology is returned when conflicting Redis topology settings are provided
	// (e.g., specifying both sentinel and cluster addresses).
	ErrRedisConflictingTopology = errors.New("redis topology settings are conflicting")
	// ErrRedisSentinelAddrs is returned when sentinel mode is specified without at least one
	// sentinel address in the Addrs field.
	ErrRedisSentinelAddrs = errors.New("redis sentinel requires at least one address")
	// ErrRedisClusterDB is returned when attempting to select a database (DB > 0) with
	// cluster mode, which does not support database selection.
	ErrRedisClusterDB = errors.New("redis cluster does not support DB selection")
	// ErrRedisRingAddrs is returned when ring mode is specified without at least one
	// shard address in the RingAddrs field.
	ErrRedisRingAddrs = errors.New("redis ring requires at least one shard address")
	// ErrInvalidName is returned when the collection name contains characters
	// that conflict with internal delimiters (e.g., ':').
	ErrInvalidName = errors.New("collection name must not contain ':'")
)

// Logger defines the interface for logging operations used by AhoCorasick.
// Implement this interface to provide custom logging behavior. By default,
// a standard logger writing to io.Discard is used (or stdout when Debug is true).
type Logger interface {
	// Printf logs a formatted message.
	Printf(format string, v ...interface{})
	// Println logs a message with a newline.
	Println(v ...interface{})
}

// AhoCorasickArgs contains configuration options for creating an AhoCorasick instance.
// All fields are optional except Name, which identifies the pattern collection.
//
// # Redis Topology Selection
//
// The Redis topology is automatically determined based on which fields are set:
//   - Ring: RingAddrs is set (map of shard names to addresses)
//   - Sentinel: MasterName is set (Addrs used as sentinel addresses)
//   - Cluster: Addrs has multiple entries (no MasterName)
//   - Standalone: Addr is set (default: "localhost:6379")
type AhoCorasickArgs struct {
	// Addr is the Redis server address for standalone mode (e.g., "localhost:6379").
	// Ignored if Addrs or RingAddrs is set.
	Addr string
	// Addrs is a list of Redis addresses. Used for:
	//   - Sentinel mode: list of sentinel addresses (requires MasterName)
	//   - Cluster mode: list of cluster node addresses
	Addrs []string
	// MasterName specifies the master name for Sentinel mode.
	// When set, Addrs is interpreted as sentinel addresses.
	MasterName string
	// RingAddrs maps shard names to addresses for Ring mode (client-side sharding).
	// Example: {"shard1": "redis1:6379", "shard2": "redis2:6379"}
	RingAddrs map[string]string
	// Password is the Redis authentication password (optional).
	Password string
	// DB is the Redis database number to select (0-15, default: 0).
	// Not supported in cluster mode.
	DB int
	// Name identifies the pattern collection. All keywords added to this instance
	// are stored under this namespace in Redis. Required.
	Name string
	// Debug enables debug logging output to stdout.
	Debug bool
	// Logger provides a custom logger implementation. If nil and Debug is false,
	// logging is disabled.
	Logger Logger
	// SchemaVersion specifies the storage schema to use:
	//   - 0 or 2: V2 schema (default, optimized, 3 keys)
	//   - 1: V1 schema (legacy, multiple keys per prefix)
	SchemaVersion int
	// EnableCache enables local in-memory caching of trie data for Find/FindIndex operations.
	// When enabled, prefixes and outputs are cached after the first read and invalidated
	// via Redis Pub/Sub when any instance modifies the collection. Reduces Redis round-trips
	// for read-heavy workloads at the cost of increased memory usage.
	EnableCache bool
}

// AhoCorasick represents an Aho-Corasick automaton backed by Redis.
// It provides efficient multi-pattern string matching with O(n + m) complexity
// where n is the text length and m is the total match count.
//
// Instances are created using Create and should be closed with Close when done.
// All methods are safe for concurrent use across multiple goroutines.
type AhoCorasick struct {
	ctx           context.Context
	name          string
	logger        Logger
	storage       KVStorage             // DI: all Redis ops go through this
	ops           operations            // Strategy: V1 or V2 implementation
	redisClient   redis.UniversalClient // kept for migration.go (out of scope)
	buildTrieHook func(string) error
	schemaVersion int // kept for SchemaVersion() and migration.go

	cache     *trieCache
	pubsub    Subscription
	stopCh    chan struct{}
	closeOnce sync.Once
}

// AhoCorasickInfo contains statistics about the Aho-Corasick automaton.
// Returned by the Info method to provide insight into the current state.
type AhoCorasickInfo struct {
	// Keywords is the number of keywords currently stored in the automaton.
	Keywords int
	// Nodes is the number of trie nodes (prefixes) in the automaton.
	// This is typically larger than Keywords as it includes all prefixes.
	Nodes int
}

// Create initializes and returns a new AhoCorasick instance connected to Redis.
// It establishes the Redis connection based on the topology settings in args
// and initializes the automaton's data structures.
//
// The Name field in args is required and identifies the pattern collection.
// Multiple AhoCorasick instances with different names can coexist on the same
// Redis server.
//
// Returns an error if:
//   - Redis connection fails
//   - Conflicting topology settings are provided
//   - Required topology settings are missing (e.g., sentinel without addresses)
//
// Example:
//
//	ac, err := acor.Create(&acor.AhoCorasickArgs{
//	    Addr:          "localhost:6379",
//	    Name:          "my-patterns",
//	    SchemaVersion: acor.SchemaV2,
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer ac.Close()
func Create(args *AhoCorasickArgs) (*AhoCorasick, error) {
	if strings.Contains(args.Name, ":") {
		return nil, ErrInvalidName
	}

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

	schemaVersion := args.SchemaVersion
	switch schemaVersion {
	case 0, SchemaV2:
		schemaVersion = SchemaV2
	case SchemaV1:
	default:
		_ = redisClient.Close()
		return nil, fmt.Errorf("unsupported schema version: %d", schemaVersion)
	}

	if args.EnableCache && schemaVersion == SchemaV1 {
		_ = redisClient.Close()
		return nil, ErrCacheRequiresV2
	}

	storage := newRedisStorage(redisClient)

	var cache *trieCache
	if args.EnableCache {
		cache = &trieCache{}
	}

	ac := &AhoCorasick{
		redisClient:   redisClient,
		storage:       storage,
		ctx:           context.Background(),
		name:          args.Name,
		logger:        logger,
		schemaVersion: schemaVersion,
		cache:         cache,
	}

	if schemaVersion == SchemaV2 {
		ac.ops = &v2Operations{
			storage: storage,
			client:  redisClient,
			name:    args.Name,
			ctx:     ac.ctx,
			cache:   cache,
			logger:  logger,
		}
	} else {
		ac.ops = &v1Operations{
			storage: storage,
			name:    args.Name,
			ctx:     ac.ctx,
			logger:  logger,
			ac:      ac,
		}
	}

	if err := ac.init(); err != nil {
		_ = storage.Close()
		return nil, err
	}

	if args.EnableCache {
		if err := ac.startCacheListener(); err != nil {
			_ = storage.Close()
			return nil, err
		}
	}

	return ac, nil
}

// SchemaVersion returns the current schema version used by the AhoCorasick instance.
// Returns SchemaV1 (1) for legacy schema or SchemaV2 (2) for the optimized schema.
func (ac *AhoCorasick) SchemaVersion() int {
	return ac.schemaVersion
}

func (ac *AhoCorasick) init() error {
	if ac.schemaVersion == SchemaV2 {
		exists, err := ac.storage.Exists(ac.ctx, trieKey(ac.name))
		if err != nil {
			return fmt.Errorf("failed to check trie key: %w", err)
		}
		if exists == 0 {
			err := ac.storage.HSet(ac.ctx, trieKey(ac.name), map[string]interface{}{
				"keywords": "[]",
				"prefixes": "[\"\"]",
				"suffixes": "[\"\"]",
				"version":  time.Now().UnixNano(),
			})
			if err != nil {
				return err
			}
		}
		return nil
	}

	prefixKey := prefixKey(ac.name)
	member := &Z{
		Score:  initScore,
		Member: "",
	}
	if err := ac.storage.ZAdd(ac.ctx, prefixKey, member); err != nil {
		return err
	}
	return nil
}

// Close closes the Redis client connection. Always call Close when done with
// an AhoCorasick instance to release resources. Returns ErrRedisAlreadyClosed
// if the connection was already closed.
func (ac *AhoCorasick) Close() error {
	var closeErr error
	alreadyClosed := true
	ac.closeOnce.Do(func() {
		if ac.storage == nil {
			return
		}
		alreadyClosed = false
		ac.stopCacheListener()
		closeErr = ac.storage.Close()
		ac.storage = nil
	})
	if alreadyClosed {
		return ErrRedisAlreadyClosed
	}
	return closeErr
}

// Add inserts a keyword into the Aho-Corasick automaton.
// The keyword is normalized to lowercase before storage.
//
// Returns:
//   - 1 if the keyword was successfully added
//   - 0 if the keyword already exists (no duplicate is added)
//   - error if the operation fails
//
// For V2 schema, this operation uses optimistic locking with automatic retries.
func (ac *AhoCorasick) Add(keyword string) (int, error) {
	return ac.ops.add(ac.ctx, keyword)
}

func (ac *AhoCorasick) Remove(keyword string) (int, error) {
	return ac.ops.remove(ac.ctx, keyword)
}

func (ac *AhoCorasick) Find(text string) ([]string, error) {
	return ac.ops.find(ac.ctx, text)
}

func (ac *AhoCorasick) FindIndex(text string) (map[string][]int, error) {
	return ac.ops.findIndex(ac.ctx, text)
}

func (ac *AhoCorasick) Flush() error {
	return ac.ops.flush(ac.ctx)
}

func (ac *AhoCorasick) Info() (*AhoCorasickInfo, error) {
	return ac.ops.info(ac.ctx)
}

func (ac *AhoCorasick) Suggest(input string) ([]string, error) {
	return ac.ops.suggest(ac.ctx, input)
}

func (ac *AhoCorasick) SuggestIndex(input string) (map[string][]int, error) {
	return ac.ops.suggestIndex(ac.ctx, input)
}

// Debug prints the current state of the Aho-Corasick automaton to stdout.
// This includes keywords, prefixes, suffixes, outputs, and nodes.
// Useful for debugging and understanding the trie structure.
// Note: Output format differs between V1 and V2 schemas.
func (ac *AhoCorasick) Debug() {
	if ac.schemaVersion == SchemaV2 {
		ac.debugV2()
		return
	}
	ac.debugV1()
}

func (ac *AhoCorasick) debugV1() {
	kKey := keywordKey(ac.name)
	kMembers, err := ac.storage.SMembers(ac.ctx, kKey)
	if err != nil {
		ac.logger.Println("-", err)
		return
	}
	ac.logger.Println("-", kMembers)

	pKey := prefixKey(ac.name)
	pMembers, err := ac.storage.ZRange(ac.ctx, pKey, 0, -1)
	if err != nil {
		ac.logger.Println("-", err)
		return
	}
	ac.logger.Println("-", pMembers)

	sKey := suffixKey(ac.name)
	sMembers, err := ac.storage.ZRange(ac.ctx, sKey, 0, -1)
	if err != nil {
		ac.logger.Println("-", err)
		return
	}
	ac.logger.Println("-", sMembers)

	outputs := make([]string, 0)
	for _, prefix := range pMembers {
		oOutputs, err := ac.collectOutputs(prefix)
		if err != nil {
			ac.logger.Println("-", err)
			return
		}
		outputs = append(outputs, oOutputs...)
	}
	ac.logger.Println("-", outputs)

	nodes := make([]string, 0)
	for _, kw := range kMembers {
		nKey := nodeKey(ac.name, kw)
		nodeMembers, err := ac.storage.SMembers(ac.ctx, nKey)
		if err != nil {
			ac.logger.Println("-", err)
			continue
		}
		nodes = append(nodes, nodeMembers...)
	}
	ac.logger.Println("-", nodes)
}

func (ac *AhoCorasick) debugV2() {
	trieData, err := ac.storage.HGetAll(ac.ctx, trieKey(ac.name))
	if err != nil {
		ac.logger.Println("Error reading trie:", err)
		return
	}
	ac.logger.Println("Trie data:")
	for key, value := range trieData {
		ac.logger.Printf("  %s: %s\n", key, value)
	}

	outputsData, err := ac.storage.HGetAll(ac.ctx, outputsKey(ac.name))
	if err != nil {
		ac.logger.Println("Error reading outputs:", err)
		return
	}
	ac.logger.Println("Outputs data:")
	for key, value := range outputsData {
		ac.logger.Printf("  %s: %s\n", key, value)
	}

	nodesData, err := ac.storage.HGetAll(ac.ctx, nodesKey(ac.name))
	if err != nil {
		ac.logger.Println("Error reading nodes:", err)
		return
	}
	ac.logger.Println("Nodes data:")
	for key, value := range nodesData {
		ac.logger.Printf("  %s: %s\n", key, value)
	}
}
