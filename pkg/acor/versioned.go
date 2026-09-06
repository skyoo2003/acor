// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	redis "github.com/redis/go-redis/v9"

	matchengine "github.com/skyoo2003/acor/internal/engine"
)

// Version is an opaque, collection-bound token. Only equality is meaningful.
type Version string

const (
	v3BucketCount     = 4096
	v3IDLength        = 32
	v3PollDefault     = 30 * time.Second
	v3LeaseDefault    = 5 * time.Minute
	v3WaitPoll        = 10 * time.Millisecond
	v3CleanupTimeout  = 5 * time.Second
	v3PairLength      = 2
	v3MillisPerSecond = 1000
	v3PageHint        = 1024
	v3DebounceDefault = 20 * time.Millisecond
)

var (
	// ErrVersionedClosed means the collection or snapshot was closed.
	ErrVersionedClosed = errors.New("acor: versioned handle closed")
	// ErrLeaseExpired means a generation can no longer be safely read.
	ErrLeaseExpired = errors.New("acor: generation lease expired")
	// ErrMaintenance means Prune currently excludes new readers and writers.
	ErrMaintenance = errors.New("acor: collection maintenance in progress")
	// ErrCasePolicy means the stored case policy differs from the requested one.
	ErrCasePolicy = errors.New("acor: collection case policy mismatch")
	// ErrInvalidVersion means a token is malformed or belongs to another collection.
	ErrInvalidVersion = errors.New("acor: invalid collection version")
	// ErrCommitUnknown means the commit response was lost; inspect OperationID using ResolveOperation.
	ErrCommitUnknown = errors.New("acor: commit outcome unknown")
	// ErrVersionedCorrupt means immutable stored data failed validation.
	ErrVersionedCorrupt = errors.New("acor: corrupt versioned data")
)

// VersionedOptions configures V3. Redis supplies connection fields and Name only;
// legacy schema, cache and engine options in Redis are ignored.
type VersionedOptions struct {
	Redis         AhoCorasickArgs
	CaseSensitive bool
	Preset        Preset
	// PollInterval defaults to 30 seconds; negative values are invalid.
	PollInterval time.Duration
	// RefreshDebounce coalesces bursts before a build, default 20ms. Negative values are invalid.
	// The fixed window is not extended by incoming events, so continuous writes cannot starve refresh.
	RefreshDebounce time.Duration
	// LeaseDuration defaults to five minutes; LeaseRefresh defaults to one minute.
	LeaseDuration time.Duration
	LeaseRefresh  time.Duration
}

// VersionedStatus describes locally observed storage and engine state.
type VersionedStatus struct {
	ActiveVersion  Version
	ServingVersion Version
	Building       bool
	BuildStarted   time.Time
	BuildDuration  time.Duration
	LastError      string
	// DownloadedBuckets and ReusedBuckets describe the most recent successful build.
	DownloadedBuckets int
	ReusedBuckets     int
	// CompletedBuilds counts successful engines installed by this instance.
	CompletedBuilds uint64
}

// WriteResult describes an atomic commit. OperationID remains available on an
// ambiguous commit error; never automatically reapply that operation.
type WriteResult struct {
	PreviousVersion Version
	Version         Version
	OperationID     string
	Added           int
	Removed         int
}

type v3Manifest struct {
	Version  Version
	Sequence uint64
	Buckets  [v3BucketCount]v3Bucket
	Count    int
}
type v3Bucket struct {
	Chunks   []string
	Count    int
	Checksum string
}
type v3Engine struct {
	engine   *matchengine.Engine
	version  Version
	sequence uint64
	manifest *v3Manifest
	buckets  *[v3BucketCount][]string
}

// VersionedCollection owns a V3 dictionary and a background engine refresher.
// Writes commit to Redis before local search changes. Use WaitForVersion when
// read-after-write behavior is required. Close releases its Redis connection.
type VersionedCollection struct {
	client    redis.UniversalClient
	opts      VersionedOptions
	prefix    string
	id        string
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	closed    atomic.Bool
	current   atomic.Pointer[v3Engine]
	refreshMu sync.Mutex
	mu        sync.Mutex
	status    VersionedStatus
	leases    map[*v3Lease]struct{}
	wake      chan struct{}
}

func v3ID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func v3Hash(b []byte) string                       { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func (v *VersionedCollection) key(s string) string { return v.prefix + s }
func (v *VersionedCollection) valid(t Version) bool {
	s := string(t)
	return strings.HasPrefix(s, v.id+".") && len(s) == len(v.id)+1+v3IDLength
}
func (v *VersionedCollection) check(ctx context.Context) error {
	if v.closed.Load() {
		return ErrVersionedClosed
	}
	return ctx.Err()
}

// OpenVersioned creates or opens a V3 collection and builds its first engine.
// ctx bounds initialization, not the returned collection's lifetime.
func OpenVersioned(ctx context.Context, opts *VersionedOptions) (*VersionedCollection, error) {
	o, err := versionedOptions(opts)
	if err != nil {
		return nil, err
	}
	// A digest hash tag prevents braces in user names from splitting Cluster slots.
	client, err := newRedisClient(&o.Redis)
	if err != nil {
		return nil, err
	}
	bg, cancel := context.WithCancel(context.Background())
	v := &VersionedCollection{
		client: client, opts: o, prefix: "{acor-v3-" + v3Hash([]byte(o.Redis.Name)) + "}:",
		ctx: bg, cancel: cancel, leases: make(map[*v3Lease]struct{}), wake: make(chan struct{}, 1),
	}
	if err = v.initialize(ctx); err != nil {
		cancel()
		_ = client.Close()
		return nil, err
	}
	v.wg.Add(1)
	go v.renewLoop()
	if err = v.refresh(ctx); err != nil {
		_ = v.Close()
		return nil, err
	}
	v.wg.Add(1)
	go v.refreshLoop()
	return v, nil
}

func (v *VersionedCollection) initialize(ctx context.Context) error {
	id := v3ID()
	token := Version(id + "." + v3ID())
	m := v3Manifest{Version: token, Sequence: 1}
	data, _ := json.Marshal(m)
	const script = `
 if redis.call('EXISTS',KEYS[1])==0 then
 redis.call('HSET',KEYS[1],'id',ARGV[1],'case',ARGV[2])
 redis.call('SET',KEYS[2],ARGV[3])
 redis.call('SET',KEYS[3],ARGV[4])
 redis.call('SET',KEYS[5],'1')
 local t=redis.call('TIME'); redis.call('ZADD',KEYS[4],t[1],ARGV[3])
 end
 return redis.call('HMGET',KEYS[1],'id','case')`
	keys := []string{v.key("meta"), v.key("active"), v.key("gen:" + string(token)), v.key("generations"), v.key("committed:" + string(token))}
	r, err := v.client.Eval(ctx, script, keys, id, fmt.Sprint(v.opts.CaseSensitive), string(token), data).Slice()
	if err != nil {
		return err
	}
	if len(r) != v3PairLength || r[0] == nil {
		return ErrVersionedCorrupt
	}
	v.id = fmt.Sprint(r[0])
	if fmt.Sprint(r[1]) != fmt.Sprint(v.opts.CaseSensitive) {
		return ErrCasePolicy
	}
	return nil
}

// Status returns local observations without Redis I/O.
func (v *VersionedCollection) Status() VersionedStatus {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.status
}
func (v *VersionedCollection) signal() {
	select {
	case v.wake <- struct{}{}:
	default:
	}
}

func (v *VersionedCollection) refresh(ctx context.Context) error {
	v.refreshMu.Lock()
	defer v.refreshMu.Unlock()
	if err := v.check(ctx); err != nil {
		return err
	}
	active, err := v.client.Get(ctx, v.key("active")).Result()
	if err != nil {
		return err
	}
	v.mu.Lock()
	v.status.ActiveVersion = Version(active)
	v.mu.Unlock()
	if e := v.current.Load(); e != nil && e.version == Version(active) {
		return nil
	}
	s, err := v.Snapshot(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close(ctx) }()
	v.mu.Lock()
	v.status.ActiveVersion = s.Version()
	v.mu.Unlock()
	if e := v.current.Load(); e != nil && e.version == s.Version() {
		return nil
	}
	start := time.Now()
	v.mu.Lock()
	v.status.Building = true
	v.status.BuildStarted = start
	v.mu.Unlock()
	defer func() {
		v.mu.Lock()
		v.status.Building = false
		v.status.BuildDuration = time.Since(start)
		v.mu.Unlock()
	}()
	buckets, downloaded, reused, err := v.engineBuckets(ctx, s)
	if err != nil {
		return err
	}
	e := matchengine.New(enginePreset(v.opts.Preset))
	if buildErr := e.BuildSequenceContext(ctx, bucketSequence(buckets), s.Count()); buildErr != nil {
		return buildErr
	}
	if err := s.lease.check(ctx); err != nil {
		return err
	}
	if err := v.check(ctx); err != nil {
		return err
	}
	v.installEngine(s, e, buckets, downloaded, reused)
	return nil
}
func (v *VersionedCollection) installEngine(s *Snapshot, e *matchengine.Engine, buckets *[v3BucketCount][]string, downloaded, reused int) {
	v.current.Store(&v3Engine{engine: e, version: s.Version(), sequence: s.manifest.Sequence, manifest: s.manifest, buckets: buckets})
	v.mu.Lock()
	v.status.ServingVersion = s.Version()
	v.status.LastError = ""
	v.status.DownloadedBuckets = downloaded
	v.status.ReusedBuckets = reused
	v.status.CompletedBuilds++
	v.mu.Unlock()
}
func (v *VersionedCollection) refreshLoop() {
	defer v.wg.Done()
	sub := v.client.Subscribe(v.ctx, v.key("events"))
	defer func() { _ = sub.Close() }()
	ch := sub.Channel()
	ticker := time.NewTicker(v.opts.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-v.ctx.Done():
			return
		case <-ticker.C:
		case <-v.wake:
		case _, ok := <-ch:
			if !ok {
				ch = nil
			}
		}
		timer := time.NewTimer(v.opts.RefreshDebounce)
		select {
		case <-v.ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		// Drain only events already buffered. A fixed bound prevents a busy publisher
		// from turning this into an unbounded trailing-edge debounce.
		pending := len(ch)
		for range pending {
			<-ch
		}
		select {
		case <-v.wake:
		default:
		}
		if err := v.refresh(v.ctx); err != nil && v.ctx.Err() == nil {
			v.mu.Lock()
			v.status.LastError = err.Error()
			v.mu.Unlock()
		}
	}
}

// WaitForVersion waits for an engine at the given committed version or a later
// commit. Committed-version receipts survive generation pruning.
func (v *VersionedCollection) WaitForVersion(ctx context.Context, version Version) error {
	if !v.valid(version) {
		return ErrInvalidVersion
	}
	sequence, err := v.client.Get(ctx, v.key("committed:"+string(version))).Uint64()
	if err != nil {
		return err
	}
	v.signal()
	t := time.NewTicker(v3WaitPoll)
	defer t.Stop()
	for {
		if err := v.check(ctx); err != nil {
			return err
		}
		if e := v.current.Load(); e != nil && e.sequence >= sequence {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-v.ctx.Done():
			return ErrVersionedClosed
		case <-t.C:
		}
	}
}

// Close stops background work and invalidates all owned snapshots.
func (v *VersionedCollection) Close() error {
	if v.closed.Swap(true) {
		return nil
	}
	v.cancel()
	v.wg.Wait()
	v.current.Store(nil)
	v.mu.Lock()
	leases := make([]*v3Lease, 0, len(v.leases))
	for l := range v.leases {
		leases = append(leases, l)
	}
	v.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), v3CleanupTimeout)
	defer cancel()
	for _, l := range leases {
		_ = l.close(ctx)
	}
	return v.client.Close()
}

func (v *VersionedCollection) manifest(ctx context.Context, t Version) (*v3Manifest, error) {
	if !v.valid(t) {
		return nil, ErrInvalidVersion
	}
	b, err := v.client.Get(ctx, v.key("gen:"+string(t))).Bytes()
	if err != nil {
		return nil, err
	}
	var m v3Manifest
	if json.Unmarshal(b, &m) != nil || m.Version != t || m.Sequence == 0 {
		return nil, ErrVersionedCorrupt
	}
	count := 0
	for _, bucket := range &m.Buckets {
		if bucket.Count < 0 || bucket.Count > m.Count-count {
			return nil, ErrVersionedCorrupt
		}
		count += bucket.Count
	}
	if count != m.Count {
		return nil, ErrVersionedCorrupt
	}
	return &m, nil
}

func versionedOptions(opts *VersionedOptions) (VersionedOptions, error) {
	if opts == nil {
		return VersionedOptions{}, errors.New("acor: VersionedOptions required")
	}
	o := *opts
	if strings.TrimSpace(o.Redis.Name) == "" {
		return VersionedOptions{}, errors.New("acor: collection name required")
	}
	if o.PollInterval == 0 {
		o.PollInterval = v3PollDefault
	}
	if o.LeaseDuration == 0 {
		o.LeaseDuration = v3LeaseDefault
	}
	if o.LeaseRefresh == 0 {
		o.LeaseRefresh = time.Minute
	}
	if o.RefreshDebounce == 0 {
		o.RefreshDebounce = v3DebounceDefault
	}
	if o.RefreshDebounce < 0 {
		return VersionedOptions{}, errors.New("acor: invalid refresh debounce")
	}
	if o.PollInterval < 0 || o.LeaseRefresh <= 0 || o.LeaseDuration <= o.LeaseRefresh {
		return VersionedOptions{}, errors.New("acor: invalid refresh intervals")
	}
	if o.Preset == PresetNone {
		o.Preset = PresetMemoryEfficient
	}
	if o.Preset < PresetSpeed || o.Preset > PresetMemoryEfficient {
		return VersionedOptions{}, errors.New("acor: invalid preset")
	}
	return o, nil
}
