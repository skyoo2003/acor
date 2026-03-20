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
	// EnableCache enables local caching for read operations.
	// When enabled, Find() and FindIndex() use a local cache to avoid
	// Redis round-trips. Cache is invalidated via Pub/Sub when other
	// instances modify the collection.
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
	redisClient   redis.UniversalClient
	name          string
	logger        Logger
	buildTrieHook func(string) error
	schemaVersion int

	cache  *trieCache
	pubsub *redis.PubSub
	stopCh chan struct{}
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

	if args.EnableCache {
		ac.cache = &trieCache{}
		if err := ac.startCacheListener(); err != nil {
			ac.logger.Printf("cache initialization failed: %v", err)
			ac.cache = nil
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

// Close closes the Redis client connection. Always call Close when done with
// an AhoCorasick instance to release resources. Returns ErrRedisAlreadyClosed
// if the connection was already closed.
func (ac *AhoCorasick) Close() error {
	if ac.redisClient == nil {
		return ErrRedisAlreadyClosed
	}
	err := ac.redisClient.Close()
	ac.redisClient = nil
	return err
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
	if ac.schemaVersion == SchemaV2 {
		return ac.addV2(keyword)
	}
	return ac.addV1(keyword)
}

// Remove deletes a keyword from the Aho-Corasick automaton.
// The keyword is normalized to lowercase before lookup.
// This operation also prunes any trie nodes that are no longer needed.
//
// Returns:
//   - remaining keyword count after removal
//   - error if the operation fails
//
// For V2 schema, this operation uses optimistic locking with automatic retries.
func (ac *AhoCorasick) Remove(keyword string) (int, error) {
	if ac.schemaVersion == SchemaV2 {
		return ac.removeV2(keyword)
	}
	return ac.removeV1(keyword)
}

// Find searches for all keywords in the given text and returns matched keywords.
// Each keyword that appears in the text is included in the result (duplicates
// may appear if a keyword matches multiple times).
//
// Matching is case-insensitive because keywords are stored in lowercase and
// input text is normalized to lowercase before matching.
//
// Returns an empty slice if no matches are found or text is empty.
func (ac *AhoCorasick) Find(text string) ([]string, error) {
	if ac.schemaVersion == SchemaV2 {
		return ac.findV2(text)
	}
	return ac.findV1(text)
}

// FindIndex searches for keywords in text and returns their start indices.
// The returned map has keywords as keys and slices of start positions (0-indexed)
// as values.
//
// Example: For text "hello world" with keyword "world":
//
//	matches, _ := ac.FindIndex("hello world")
//	// matches["world"] == []int{6}
func (ac *AhoCorasick) FindIndex(text string) (map[string][]int, error) {
	if ac.schemaVersion == SchemaV2 {
		return ac.findIndexV2(text)
	}
	return ac.findIndexV1(text)
}

// Flush removes all keywords and trie data from Redis for this collection.
// Use with caution as this operation is irreversible. The collection remains
// usable after flushing and can have new keywords added.
func (ac *AhoCorasick) Flush() error {
	if ac.schemaVersion == SchemaV2 {
		return ac.flushV2()
	}
	return ac.flushV1()
}

// Info returns statistics about the Aho-Corasick automaton including
// the number of keywords and trie nodes currently stored.
func (ac *AhoCorasick) Info() (*AhoCorasickInfo, error) {
	if ac.schemaVersion == SchemaV2 {
		return ac.infoV2()
	}
	return ac.infoV1()
}

// Suggest returns keywords that start with the given input prefix.
// This is useful for implementing autocomplete functionality.
// The input is normalized to lowercase before matching.
//
// Returns an empty slice if no keywords match the prefix.
func (ac *AhoCorasick) Suggest(input string) ([]string, error) {
	if ac.schemaVersion == SchemaV2 {
		return ac.suggestV2(input)
	}
	return ac.suggestV1(input)
}

// SuggestIndex returns keywords matching the prefix with their indices.
// Similar to Suggest but returns a map for consistency with FindIndex.
// All matched keywords have an index of 0.
func (ac *AhoCorasick) SuggestIndex(input string) (map[string][]int, error) {
	if ac.schemaVersion == SchemaV2 {
		return ac.suggestIndexV2(input)
	}
	return ac.suggestIndexV1(input)
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

func (ac *AhoCorasick) debugV2() {
	trieData, err := ac.redisClient.HGetAll(ac.ctx, trieKey(ac.name)).Result()
	if err != nil {
		fmt.Println("Error reading trie:", err)
		return
	}
	fmt.Println("Trie data:")
	for key, value := range trieData {
		fmt.Printf("  %s: %s\n", key, value)
	}

	outputsData, err := ac.redisClient.HGetAll(ac.ctx, outputsKey(ac.name)).Result()
	if err != nil {
		fmt.Println("Error reading outputs:", err)
		return
	}
	fmt.Println("Outputs data:")
	for key, value := range outputsData {
		fmt.Printf("  %s: %s\n", key, value)
	}

	nodesData, err := ac.redisClient.HGetAll(ac.ctx, nodesKey(ac.name)).Result()
	if err != nil {
		fmt.Println("Error reading nodes:", err)
		return
	}
	fmt.Println("Nodes data:")
	for key, value := range nodesData {
		fmt.Printf("  %s: %s\n", key, value)
	}
}
