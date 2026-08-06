// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"hash/maphash"
	"sync"

	matchengine "github.com/skyoo2003/acor/internal/engine"
)

// engineMemo holds the automaton last built for a collection, so repeated
// searches over unchanged data skip the O(keywords) rebuild. Both schemas use
// it: V1 keys on the keyword set from SMEMBERS, V2 on the outputs map from its
// trie fetch.
//
// It does not remove the read that checks Redis for changes — freshness still
// costs a round trip. It removes only the rebuild that a read repeats when
// nothing changed.
type engineMemo struct {
	mu     sync.Mutex
	engine *matchengine.Engine
	digest uint64
	// stats is shared with the operations that own this memo, so a hit here counts
	// toward the same CacheStats as a hit in the other modes. Nil records nothing.
	stats *cacheStats
}

// engineDigestSeed keys the digests, which are only ever compared against
// digests taken in the same process.
var engineDigestSeed = maphash.MakeSeed()

// engineFor returns the memoized automaton when digest is unchanged, and
// otherwise builds a fresh one and memoizes it.
//
// Callers compute the digest from the rawest form of the data they have, so a
// hit skips not just the automaton build but any parsing behind it. build runs
// under the lock, so a burst of concurrent misses rebuilds once.
func (m *engineMemo) engineFor(digest uint64, build func() (*matchengine.Engine, error)) (*matchengine.Engine, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.engine != nil && m.digest == digest {
		m.stats.hit()
		return m.engine, nil
	}
	m.stats.miss()
	engine, err := timeRebuild(m.stats, build)
	if err != nil {
		return nil, err
	}
	m.engine = engine
	m.digest = digest
	return m.engine, nil
}

// digestKeywords fingerprints a keyword set. Summing per-member hashes makes it
// independent of the order SMEMBERS happened to return the members in.
func digestKeywords(kws []string) uint64 {
	var digest uint64
	for _, k := range kws {
		digest += maphash.String(engineDigestSeed, k)
	}
	return digest
}

// digestRawOutputs fingerprints the V2 outputs hash exactly as Redis returned
// it, before any JSON is parsed. Digesting the raw payload is what lets a hit
// skip the per-state unmarshal, which costs more than the automaton build it
// feeds. Map iteration order is random, so entries are summed rather than
// combined in sequence.
//
// Over-sensitivity here would cost at most one extra rebuild; under-sensitivity
// would serve a stale automaton, which is not a trade worth making.
func digestRawOutputs(raw map[string]string) uint64 {
	var digest uint64
	for state, jsonArr := range raw {
		digest += maphash.String(engineDigestSeed, state)
		digest += maphash.String(engineDigestSeed, jsonArr)
	}
	return digest
}
