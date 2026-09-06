// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

type v3Lease struct {
	owner   *VersionedCollection
	member  string
	set     string
	expired atomic.Bool
	closed  atomic.Bool
}

// Redis time is authoritative for leases, retention, and fencing.
const v3Now = `local t=redis.call('TIME'); local now=t[1]*1000+math.floor(t[2]/1000); `

func (v *VersionedCollection) acquire(ctx context.Context, expected Version, writer bool) (*v3Lease, Version, error) {
	if err := v.check(ctx); err != nil {
		return nil, "", err
	}
	set := "leases"
	if writer {
		set = "writers"
	}
	const script = v3Now + `
 if redis.call('EXISTS',KEYS[1])==1 then return {'maintenance'} end
 local active=redis.call('GET',KEYS[2])
 if ARGV[1]~='' and active~=ARGV[1] then return {'conflict'} end
 local member=active..'/'..ARGV[2]
 redis.call('ZADD',KEYS[3],now+ARGV[3],member)
 return {active,member}`
	keys := []string{v.key("maintenance"), v.key("active"), v.key(set)}
	r, err := v.client.Eval(ctx, script, keys, string(expected), v3ID(), v.opts.LeaseDuration.Milliseconds()).StringSlice()
	if err != nil {
		return nil, "", err
	}
	if len(r) == 1 {
		if r[0] == "maintenance" {
			return nil, "", ErrMaintenance
		}
		return nil, "", ErrConcurrencyConflict
	}
	l := &v3Lease{owner: v, member: r[1], set: set}
	v.mu.Lock()
	if v.closed.Load() {
		v.mu.Unlock()
		_ = l.close(ctx)
		return nil, "", ErrVersionedClosed
	}
	v.leases[l] = struct{}{}
	v.mu.Unlock()
	return l, Version(r[0]), nil
}
func (l *v3Lease) check(ctx context.Context) error {
	if l.closed.Load() {
		return ErrVersionedClosed
	}
	if l.expired.Load() {
		return ErrLeaseExpired
	}
	if err := l.owner.check(ctx); err != nil {
		return err
	}
	const script = v3Now + `local e=redis.call('ZSCORE',KEYS[1],ARGV[1]); if not e or tonumber(e)<=now then return 0 end; return 1`
	ok, err := l.owner.client.Eval(ctx, script, []string{l.owner.key(l.set)}, l.member).Int()
	if err != nil {
		return err
	}
	if ok == 0 {
		l.expired.Store(true)
		return ErrLeaseExpired
	}
	return nil
}
func (l *v3Lease) renew(ctx context.Context) {
	if l.expired.Load() {
		return
	}
	const script = v3Now + `local e=redis.call('ZSCORE',KEYS[1],ARGV[1]);
 if not e or tonumber(e)<=now then return 0 end;
 redis.call('ZADD',KEYS[1],now+ARGV[2],ARGV[1]); return 1`
	ok, err := l.owner.client.Eval(ctx, script, []string{l.owner.key(l.set)}, l.member, l.owner.opts.LeaseDuration.Milliseconds()).Int()
	if err == nil && ok == 0 {
		l.expired.Store(true)
	}
}
func (l *v3Lease) close(ctx context.Context) error {
	l.closed.Store(true)
	l.expired.Store(true)
	l.owner.mu.Lock()
	delete(l.owner.leases, l)
	l.owner.mu.Unlock()
	return l.owner.client.ZRem(ctx, l.owner.key(l.set), l.member).Err()
}
func (v *VersionedCollection) renewLoop() {
	defer v.wg.Done()
	t := time.NewTicker(v.opts.LeaseRefresh)
	defer t.Stop()
	for {
		select {
		case <-v.ctx.Done():
			return
		case <-t.C:
			v.mu.Lock()
			ls := make([]*v3Lease, 0, len(v.leases))
			for l := range v.leases {
				ls = append(ls, l)
			}
			v.mu.Unlock()
			for _, l := range ls {
				l.renew(v.ctx)
			}
		}
	}
}

// Snapshot is a leased, immutable dictionary view. Close it after use. Its
// cursors are bound to its version and must not be interpreted by callers.
type Snapshot struct {
	owner    *VersionedCollection
	manifest *v3Manifest
	lease    *v3Lease
}

// Snapshot pins the active generation before reading its manifest.
func (v *VersionedCollection) Snapshot(ctx context.Context) (*Snapshot, error) {
	l, t, err := v.acquire(ctx, "", false)
	if err != nil {
		return nil, err
	}
	m, err := v.manifest(ctx, t)
	if err != nil {
		_ = l.close(ctx)
		return nil, err
	}
	return &Snapshot{owner: v, manifest: m, lease: l}, nil
}

// Version returns the pinned opaque token.
func (s *Snapshot) Version() Version { return s.manifest.Version }

// Count returns the pinned keyword count without I/O.
func (s *Snapshot) Count() int { return s.manifest.Count }

// Close releases the generation lease. Subsequent reads fail.
func (s *Snapshot) Close(ctx context.Context) error { return s.lease.close(ctx) }

// DictionaryPage lists keywords in bucket number, then lexical order. An empty
// NextCursor marks the end; an empty input cursor starts the snapshot.
type DictionaryPage struct {
	Keywords   []string
	NextCursor string
	Version    Version
}

type v3Cursor struct {
	Version Version
	Bucket  int
	Offset  int
}

// List reads at most limit entries; limit must be positive. It does not load the
// entire dictionary. A large individual keyword can exceed the chunk target.
func (s *Snapshot) List(ctx context.Context, cursor string, limit int) (*DictionaryPage, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("acor: page limit must be positive")
	}
	if err := s.lease.check(ctx); err != nil {
		return nil, err
	}
	c, err := s.cursor(cursor)
	if err != nil {
		return nil, err
	}
	p := &DictionaryPage{Version: s.Version(), Keywords: make([]string, 0, min(limit, v3PageHint))}
	for c.Bucket < v3BucketCount {
		words, err := s.owner.bucket(ctx, s.manifest.Buckets[c.Bucket])
		if err != nil {
			return nil, err
		}
		if c.Offset > len(words) {
			return nil, ErrInvalidVersion
		}
		n := min(limit-len(p.Keywords), len(words)-c.Offset)
		p.Keywords = append(p.Keywords, words[c.Offset:c.Offset+n]...)
		c.Offset += n
		if c.Offset == len(words) {
			c.Bucket++
			c.Offset = 0
		}
		if len(p.Keywords) == limit {
			break
		}
	}
	// Skip empty buckets so the final nonempty page has no continuation.
	for c.Bucket < v3BucketCount && s.manifest.Buckets[c.Bucket].Count == 0 {
		c.Bucket++
	}
	if c.Bucket < v3BucketCount {
		b, _ := json.Marshal(c)
		p.NextCursor = base64.RawURLEncoding.EncodeToString(b)
	}
	if err := s.lease.check(ctx); err != nil {
		return nil, err
	}
	return p, nil
}
func (v *VersionedCollection) bucket(ctx context.Context, b v3Bucket) ([]string, error) {
	words := make([]string, 0, b.Count)
	if b.Count == 0 {
		if len(b.Chunks) != 0 {
			return nil, ErrVersionedCorrupt
		}
		return words, nil
	}
	for _, h := range b.Chunks {
		data, err := v.client.Get(ctx, v.key("chunk:"+h)).Bytes()
		if err != nil {
			return nil, err
		}
		if v3Hash(data) != h {
			return nil, ErrVersionedCorrupt
		}
		var part []string
		if json.Unmarshal(data, &part) != nil {
			return nil, ErrVersionedCorrupt
		}
		words = append(words, part...)
	}
	data, _ := json.Marshal(words)
	if len(words) != b.Count || v3Hash(data) != b.Checksum || !slices.IsSorted(words) {
		return nil, ErrVersionedCorrupt
	}
	return words, nil
}
func (s *Snapshot) all(ctx context.Context) ([]string, error) {
	if err := s.lease.check(ctx); err != nil {
		return nil, err
	}
	words := make([]string, 0, s.Count())
	for _, b := range &s.manifest.Buckets {
		w, err := s.owner.bucket(ctx, b)
		if err != nil {
			return nil, err
		}
		words = append(words, w...)
	}
	if err := s.lease.check(ctx); err != nil {
		return nil, err
	}
	return words, nil
}

// DictionaryDiff compares a target with a pinned snapshot without writing.
// Each slice is sorted lexically after normalization and deduplication.
type DictionaryDiff struct {
	Added    []string
	Removed  []string
	Retained []string
	Version  Version
}

// Diff reports additions, removals and retained keywords for a full replacement.
func (s *Snapshot) Diff(ctx context.Context, target []string) (*DictionaryDiff, error) {
	normalized, err := v3Normalize(target, s.owner.opts.CaseSensitive)
	if err != nil {
		return nil, err
	}
	words, err := s.all(ctx)
	if err != nil {
		return nil, err
	}
	old := make(map[string]struct{}, len(words))
	for _, w := range words {
		old[w] = struct{}{}
	}
	d := &DictionaryDiff{Version: s.Version(), Added: []string{}, Removed: []string{}, Retained: []string{}}
	for _, w := range normalized {
		if _, ok := old[w]; ok {
			d.Retained = append(d.Retained, w)
			delete(old, w)
		} else {
			d.Added = append(d.Added, w)
		}
	}
	for w := range old {
		d.Removed = append(d.Removed, w)
	}
	slices.Sort(d.Removed)
	return d, nil
}
func v3Normalize(words []string, sensitive bool) ([]string, error) {
	out := make([]string, 0, len(words))
	for _, w := range words {
		if !utf8.ValidString(w) {
			return nil, fmt.Errorf("acor: keyword is not valid UTF-8")
		}
		w = strings.TrimSpace(w)
		if !sensitive {
			w = strings.ToLower(w)
		}
		if w == "" {
			return nil, ErrEmptyKeyword
		}
		out = append(out, w)
	}
	slices.Sort(out)
	return slices.Compact(out), nil
}
func v3BucketNumber(w string) int {
	h := v3Hash([]byte(w))
	n, _ := strconv.ParseUint(h[:3], 16, 12)
	return int(n)
}

func (s *Snapshot) cursor(cursor string) (v3Cursor, error) {
	c := v3Cursor{Version: s.Version()}
	if cursor != "" {
		b, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil || json.Unmarshal(b, &c) != nil || c.Version != s.Version() || c.Bucket < 0 || c.Bucket >= v3BucketCount || c.Offset < 0 {
			return c, ErrInvalidVersion
		}
	}
	return c, nil
}
