// SPDX-License-Identifier: Apache-2.0

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
//   - Two schema versions: V2 (optimized, default) and V1 (deprecated, read-only)
//   - Thread-safe operations with optimistic locking (V2)
//   - Batch operations for bulk keyword management
//   - Parallel text matching for improved performance on large texts
//   - Prefix-based keyword suggestions
//
// # Compatibility
//
// Every v1.x.y release keeps this package's exported identifiers compiling and behaving
// as documented. v1.5.0 is the first supported v1 release. Three conditions apply to
// calling code: construct AhoCorasickArgs, MatchOptions, BatchOptions, ParallelOptions,
// and MigrationOptions with field names, since those structs gain fields in minor
// releases; do not expect Logger — the one interface callers implement — to gain
// methods, since it will not inside v1; and do not dot-import this package, since a
// name added in a minor release can then collide with a declaration of your own.
//
// The full policy — including the on-Redis format rules that make mixed-version fleets
// safe, and what the promise excludes — is at
// https://skyoo2003.github.io/acor/reference/compatibility/
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
// V2 (SchemaVersion: 2, default): Optimized schema consolidating data into 3 keys.
// Recommended for every use case. Uses Lua scripts for atomic operations.
//
// V1 (SchemaVersion: 1): Deprecated legacy schema using multiple Redis keys for each
// prefix/suffix/output. Kept for existing collections only; migrate with
// MigrateV1ToV2. New collections should not select it.
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
// CacheStats reports how that is going — hit rate, rebuild cost, and the lag of the last
// invalidation received — without touching Redis, so it is cheap to scrape on a timer.
//
// # Thread Safety
//
// All operations are safe for concurrent use. V2 schema uses optimistic locking
// with automatic retries for write operations.
//
// # Case-Sensitive Matching
//
// By default, ACOR performs case-insensitive matching: keywords are lowercased
// on insertion and search text is lowercased during matching. To enable
// case-sensitive matching, set CaseSensitive to true:
//
//	ac, err := acor.Create(&acor.AhoCorasickArgs{
//	    Addr:          "localhost:6379",
//	    Name:          "my-collection",
//	    CaseSensitive: true,
//	})
//	if err != nil {
//	    // handle error
//	}
//	defer ac.Close()
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

	redis "github.com/redis/go-redis/v9"
)

const (
	initScore   = 0.0
	memberScore = 1.0

	defaultRollbackTimeout = 10 * time.Second
)

// resolveRollbackTimeout returns d if positive, otherwise the default.
func resolveRollbackTimeout(d time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return defaultRollbackTimeout
}

var (
	// ErrRedisAlreadyClosed is returned when attempting to close an already closed Redis client.
	ErrRedisAlreadyClosed = errors.New("redis client was already closed")
	// ErrRedisConflictingTopology is returned when conflicting Redis topology settings are provided
	// (e.g., specifying both sentinel and cluster addresses).
	ErrRedisConflictingTopology = errors.New("redis topology settings are conflicting")
	// ErrRedisAddrs is returned when the Addrs field is set but holds no usable
	// address (every entry is empty or whitespace).
	ErrRedisAddrs = errors.New("redis addrs must contain at least one address")
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

	// The following knobs tune connection resilience. They are passed straight
	// through to go-redis; a zero value means "use the go-redis default", as
	// documented in go-redis's Options. They apply across all topologies
	// (standalone, cluster, sentinel, ring).
	// See https://pkg.go.dev/github.com/redis/go-redis/v9#Options for details.

	// DialTimeout is the timeout for establishing new connections.
	DialTimeout time.Duration
	// ReadTimeout is the timeout for socket reads. Use -1 for no timeout.
	ReadTimeout time.Duration
	// WriteTimeout is the timeout for socket writes. Use -1 for no timeout.
	WriteTimeout time.Duration
	// MaxRetries is the number of retries before giving up on a command.
	// Use -1 to disable retries.
	MaxRetries int
	// PoolSize is the maximum number of socket connections per server —
	// per node in cluster mode, per shard in ring mode, and per
	// master/replica in sentinel mode.
	PoolSize int
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
	//   - 1: V1 schema (deprecated and read-only — see SchemaV1)
	//
	// Selecting 1 opens an existing V1 collection for reading and migration. Add and
	// Remove return ErrV1ReadOnly, so a new V1 collection can never be populated.
	SchemaVersion int
	// EnableCache enables local in-memory caching of trie data for Find/FindIndex operations.
	// When enabled, prefixes and outputs are cached after the first read and invalidated
	// via Redis Pub/Sub when any instance modifies the collection. Reduces Redis round-trips
	// for read-heavy workloads at the cost of increased memory usage.
	//
	// Requires V2 (ErrCacheRequiresV2 otherwise) and cannot be combined with Preset,
	// which already serves reads from a local engine (ErrCacheWithPreset).
	EnableCache bool
	// SelfInvalidationCleanupInterval controls how often the expired self-invalidation
	// sweep runs relative to publishInvalidate calls. Every N publishes triggers one O(n)
	// sweep of the pending self-invalidations map. Lower values reduce memory usage at the
	// cost of more frequent cleanup; higher values trade memory for less CPU overhead.
	// Applies to both EnableCache and Preset mode. Defaults to 128 if unset or zero.
	SelfInvalidationCleanupInterval uint64
	// CaseSensitive controls whether keyword matching is case-sensitive.
	// When false (default), keywords are lowercased on Add/Remove and search text
	// is lowercased in Find/FindIndex/Suggest, providing case-insensitive matching.
	// When true, keywords and search text are matched as-is for full case-sensitive matching.
	//
	// Case-insensitive matching uses Go's simple, locale-independent lowercasing
	// (strings.ToLower), not full Unicode case folding: "ß" does not match "SS",
	// and Turkish dotted/dotless i follow the default mapping. Pre-fold the
	// keywords and text yourself if you need either.
	CaseSensitive bool
	// RollbackTimeout controls the timeout for V1 rollback operations when buildTrie
	// fails after a keyword has been added. Defaults to 10 seconds if unset or zero.
	// A fresh context with this timeout is used intentionally so that rollback can
	// complete even if the caller's context is already canceled.
	RollbackTimeout time.Duration

	// Preset selects the architecture for the local match engine.
	// When set, uses Redis-backed engine with a local preset-optimized automaton
	// for fast reads. Forces V2 schema.
	// When unset (zero), the original Aho-Corasick engine is used.
	Preset Preset

	// InvalidationPollInterval enables a background safety net for the Preset
	// engine: every interval it compares the collection's stored version against
	// the local one and reloads if they differ. Cross-instance invalidation is
	// normally driven by best-effort Redis Pub/Sub, which has no delivery
	// guarantee — a dropped message leaves a node serving stale data until the
	// next local write. This poll bounds that staleness to the interval.
	//
	// Disabled by default (zero). Recommended for multi-instance deployments
	// (e.g. 30 * time.Second). Only applies to Preset mode; ignored otherwise.
	InvalidationPollInterval time.Duration
}

// AhoCorasick represents an Aho-Corasick automaton backed by Redis.
// It provides efficient multi-pattern string matching with O(n + m) complexity
// where n is the text length and m is the total match count.
//
// Instances are created using Create and should be closed with Close when done.
// All methods are safe for concurrent use across multiple goroutines.
type AhoCorasick struct {
	ctx           context.Context
	cancel        context.CancelFunc
	name          string
	logger        Logger
	storage       kvStorage             // DI: all Redis ops go through this
	ops           operations            // Strategy: V1 or V2 implementation
	redisClient   redis.UniversalClient // kept for migration.go (out of scope)
	buildTrieHook func(string) error
	schemaVersion int // kept for SchemaVersion() and migration.go

	rollbackTimeout time.Duration
	caseSensitive   bool

	cache     *trieCache
	stats     *cacheStats
	pubsub    subscription
	stopCh    chan struct{}
	closeOnce sync.Once
	mode      backendMode
	closeFn   func() error
}

// AhoCorasickInfo contains statistics about the Aho-Corasick automaton.
// Returned by the Info method to provide insight into the current state.
type AhoCorasickInfo struct {
	// Keywords is the number of keywords currently stored in the automaton.
	Keywords int
	// Nodes is the number of trie nodes (prefixes) in the automaton.
	// This is typically larger than Keywords as it includes all prefixes.
	Nodes int
	// Preset is the engine architecture preset, or PresetNone when using the
	// original non-preset engine.
	Preset Preset
	// MemoryBytes is the estimated memory usage in bytes.
	// Zero when using the original non-preset engine.
	MemoryBytes int64
	// TrieDepth is the maximum trie depth.
	// Zero when using the original non-preset engine.
	TrieDepth int
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
	return CreateContext(context.Background(), args)
}

// CreateContext is Create with an explicit context governing the setup I/O: the
// schema check and initialization write, and the initial keyword load. A canceled
// or expired ctx fails the construction.
//
// ctx does not bound the returned instance's lifetime. Background work that must
// outlive construction — the Pub/Sub subscribe, the invalidation listener, and the
// version poller — runs on an internal context tied to Close instead, so passing a
// request-scoped ctx here cannot leave a live instance whose listener has silently
// stopped. That also means ctx does not bound the subscribe itself.
// Per-operation cancellation is what the *Context methods (FindContext, AddContext,
// …) are for.
func CreateContext(ctx context.Context, args *AhoCorasickArgs) (*AhoCorasick, error) {
	if args == nil {
		return nil, ErrNilArgs
	}
	if strings.Contains(args.Name, ":") {
		return nil, ErrInvalidName
	}

	// --- Preset-Optimized Redis mode ---
	if args.Preset != PresetNone && args.Preset != presetDefault {
		if !args.hasAnyRedisConfig() {
			return nil, ErrPresetRequiresRedis
		}
		if args.SchemaVersion == SchemaV1 {
			return nil, ErrPresetRequiresV2
		}
		// Rejected rather than ignored: preset mode never reads args.EnableCache, so
		// setting both used to silently drop the caching the caller asked for.
		if args.EnableCache {
			return nil, ErrCacheWithPreset
		}
		return createPresetRedis(ctx, args)
	}

	// --- Branch 3: Original mode (unchanged) ---
	return createOriginal(ctx, args)
}

func newLogger(args *AhoCorasickArgs) Logger {
	stdLogger := log.New(io.Discard, "ACOR: ", log.LstdFlags|log.Lshortfile)
	if args.Debug {
		stdLogger.SetOutput(os.Stdout)
	}
	if args.Logger != nil {
		return args.Logger
	}
	return stdLogger
}

// createPresetRedis creates a Redis-backed AhoCorasick with a local preset-optimized
// engine. ctx covers setup only; newRedisBacked keeps its own Background-derived
// context for the long-lived Pub/Sub listener and reloads, per CreateContext.
func createPresetRedis(ctx context.Context, args *AhoCorasickArgs) (*AhoCorasick, error) {
	rbAC, err := newRedisBacked(ctx, args)
	if err != nil {
		return nil, err
	}

	ac := &AhoCorasick{
		name:          args.Name,
		logger:        newLogger(args),
		schemaVersion: SchemaV2,
		ops:           rbAC,
		// Same counters the engine records into, not a second set: newRedisBacked owns
		// them because it builds the first automaton before this struct exists.
		stats:         rbAC.stats,
		mode:          modePresetRedis,
		caseSensitive: args.CaseSensitive,
		ctx:           context.Background(),
		cancel:        func() {},
		closeFn:       rbAC.Close,
	}
	return ac, nil
}

func createOriginal(ctx context.Context, args *AhoCorasickArgs) (*AhoCorasick, error) {
	logger := newLogger(args)

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
		// Set before the cache is shared with the listener goroutine; selfSkipSet
		// reads it without synchronization. Zero means the default interval.
		cache.selfSkip.cleanupEvery = args.SelfInvalidationCleanupInterval
	}

	ac := &AhoCorasick{
		redisClient:   redisClient,
		storage:       storage,
		name:          args.Name,
		logger:        logger,
		schemaVersion: schemaVersion,
		cache:         cache,
		stats:         &cacheStats{},
		mode:          modeOriginal,
	}
	ac.rollbackTimeout = resolveRollbackTimeout(args.RollbackTimeout)
	ac.caseSensitive = args.CaseSensitive
	// Background, not the caller's ctx: this context outlives Create and is what
	// Close cancels. See CreateContext.
	ac.ctx, ac.cancel = context.WithCancel(context.Background()) //nolint:gosec // G118: storing cancel func is intentional for lifecycle management

	if schemaVersion == SchemaV2 {
		ac.ops = ac.newV2Ops(cache)
	} else {
		ac.ops = ac.newV1Ops()
	}

	if err := ac.init(ctx); err != nil {
		ac.cancel()
		_ = storage.Close()
		return nil, err
	}

	if args.EnableCache {
		if err := ac.startCacheListener(); err != nil {
			ac.cancel()
			_ = storage.Close()
			return nil, err
		}
	}

	ac.closeFn = func() error {
		ac.stopCacheListener()
		return ac.storage.Close()
	}

	return ac, nil
}

// SchemaVersion returns the current schema version used by the AhoCorasick instance.
// Returns SchemaV1 (1) for legacy schema or SchemaV2 (2) for the optimized schema.
func (ac *AhoCorasick) SchemaVersion() int {
	return ac.schemaVersion
}

// init performs the one-time schema setup. ctx is the caller's construction
// context, not ac.ctx: a caller that gave up waiting should not leave Create
// blocked on Redis.
func (ac *AhoCorasick) init(ctx context.Context) error {
	if ac.schemaVersion == SchemaV2 {
		exists, err := ac.storage.Exists(ctx, trieKey(ac.name))
		if err != nil {
			return fmt.Errorf("failed to check trie key: %w", err)
		}
		if exists == 0 {
			err := ac.storage.HSet(ctx, trieKey(ac.name), emptyTrieFields())
			if err != nil {
				return fmt.Errorf("failed to initialize V2 trie: %w", err)
			}
		}
		return nil
	}

	prefixKey := prefixKey(ac.name)
	member := &zMember{
		Score:  initScore,
		Member: "",
	}
	if err := ac.storage.ZAdd(ctx, prefixKey, member); err != nil {
		return fmt.Errorf("failed to initialize V1 prefix key: %w", err)
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
		alreadyClosed = false
		if ac.cancel != nil {
			ac.cancel()
		}
		if ac.closeFn != nil {
			closeErr = ac.closeFn()
		}
	})
	if alreadyClosed {
		return ErrRedisAlreadyClosed
	}
	return closeErr
}

func (ac *AhoCorasick) newV2Ops(cache *trieCache) operations {
	return &v2Operations{
		storage:       ac.storage,
		client:        ac.redisClient,
		name:          ac.name,
		cache:         cache,
		logger:        ac.logger,
		caseSensitive: ac.caseSensitive,
		stats:         ac.stats,
		// The memo shares the same counters, so an uncached V2 instance still reports a
		// hit rate: it skips the rebuild even though the freshness read remains.
		engines: engineMemo{stats: ac.stats},
	}
}

func (ac *AhoCorasick) newV1Ops() operations {
	return &v1Operations{
		storage:         ac.storage,
		name:            ac.name,
		logger:          ac.logger,
		ac:              ac,
		caseSensitive:   ac.caseSensitive,
		rollbackTimeout: ac.rollbackTimeout,
		engines:         engineMemo{stats: ac.stats},
	}
}

// CacheStats returns a snapshot of this instance's local cache activity: hit rate,
// rebuild cost, and the lag of the last invalidation received from a peer. See
// CacheStats for what each field counts and what it does not.
//
// It performs no Redis I/O, takes no lock the read path contends on, and is safe to
// call concurrently and after Close — so it is cheap enough to scrape on a timer.
// The counters are process-local: in a fleet, scrape every instance.
func (ac *AhoCorasick) CacheStats() CacheStats {
	return ac.stats.snapshot()
}

// Add inserts a keyword into the Aho-Corasick automaton.
// When CaseSensitive is false (default), the keyword is normalized to lowercase
// before storage and duplicate detection is case-insensitive.
// When CaseSensitive is true, the keyword is stored verbatim.
//
// Returns:
//   - 1 if the keyword was successfully added
//   - 0 if the keyword already exists (no duplicate is added)
//   - 0 and no error if the keyword is empty or whitespace-only; the batch form
//     reports that case as ErrEmptyKeyword, this one does not
//   - error if the operation fails
//
// On a V1 collection every call fails with ErrV1ReadOnly instead, an empty keyword
// included: the read-only check comes before the keyword is looked at.
// For V2 schema, this operation uses optimistic locking with automatic retries.
func (ac *AhoCorasick) Add(keyword string) (int, error) {
	return ac.ops.add(ac.ctx, keyword)
}

// Remove removes a keyword from the Aho-Corasick automaton.
// Returns the number of keywords removed (0 or 1) or an error. An empty or
// whitespace-only keyword removes nothing and reports (0, nil); see Add, whose
// V1 caveat applies here too.
func (ac *AhoCorasick) Remove(keyword string) (int, error) {
	return ac.ops.remove(ac.ctx, keyword)
}

// Find searches the text for all keywords in the automaton and returns
// the matched keywords as a slice of strings.
func (ac *AhoCorasick) Find(text string) ([]string, error) {
	return ac.ops.find(ac.ctx, text)
}

// FindIndex searches the text for all keywords and returns a map of
// keyword to the slice of start indices where each keyword was found.
func (ac *AhoCorasick) FindIndex(text string) (map[string][]int, error) {
	return ac.ops.findIndex(ac.ctx, text)
}

// Flush removes all keywords from the automaton, effectively resetting it
// to an empty state.
func (ac *AhoCorasick) Flush() error {
	return ac.ops.flush(ac.ctx)
}

// Info returns diagnostic information about the automaton: the keyword and trie-node
// counts, and in Preset mode the engine preset with its memory and depth estimates.
// See AhoCorasickInfo for which fields each mode fills in.
//
// The schema version is not part of it — call SchemaVersion for that, which costs no
// Redis round trip.
func (ac *AhoCorasick) Info() (*AhoCorasickInfo, error) {
	return ac.ops.info(ac.ctx)
}

// Suggest returns keyword suggestions based on the given input prefix.
func (ac *AhoCorasick) Suggest(input string) ([]string, error) {
	return ac.ops.suggest(ac.ctx, input)
}

// SuggestIndex returns keyword suggestions based on the given input prefix,
// mapped to their start indices in the original keywords.
func (ac *AhoCorasick) SuggestIndex(input string) (map[string][]int, error) {
	return ac.ops.suggestIndex(ac.ctx, input)
}

// Debug dumps the collection's Redis state — keywords, prefixes, suffixes, outputs,
// and nodes — through the instance's Logger. Useful for understanding the trie
// structure.
//
// It writes to the Logger, not to stdout: on an instance built without Debug or a
// Logger the default logger discards everything, so Debug produces no output at all.
// Set AhoCorasickArgs.Debug to send it to stdout, or supply a Logger.
//
// Only the original V1/V2 Redis-backed mode dumps anything. Preset mode is a no-op —
// not for want of Redis trie state, which it keeps like V2 does, but because it reads
// that state through its own engine and createPresetRedis leaves the storage handle
// these dumps go through unset.
func (ac *AhoCorasick) Debug() {
	if ac.mode == modeOriginal && ac.schemaVersion == SchemaV2 {
		ac.debugV2()
		return
	}
	if ac.mode == modeOriginal {
		ac.debugV1()
		return
	}
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
		oOutputs, err := ac.outputWithContext(ac.ctx, prefix)
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
