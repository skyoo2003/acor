// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"math"
	"unicode/utf8"
)

// bloomFilter is a space-efficient probabilistic data structure for testing
// membership of rune values. Used as a pre-filter to skip trie traversal for
// characters that cannot start any keyword.
type bloomFilter struct {
	bits    []uint64
	numBits uint64
	hashes  int
}

// newBloomFilter creates a bloom filter optimized for the expected number of items
// at the given false positive rate.
func newBloomFilter(expectedItems int, fpRate float64) *bloomFilter {
	if expectedItems <= 0 {
		return &bloomFilter{bits: make([]uint64, 1), numBits: 64, hashes: 1}
	}

	numBits := uint64(math.Ceil(-float64(expectedItems) * math.Log(fpRate) / (math.Ln2 * math.Ln2)))
	hashes := int(math.Ceil((float64(numBits) / float64(expectedItems)) * math.Ln2))
	if hashes < 1 {
		hashes = 1
	}
	if hashes > 16 {
		hashes = 16
	}

	size := (numBits + 63) / 64
	if size == 0 {
		size = 1
	}

	return &bloomFilter{
		bits:    make([]uint64, size),
		numBits: numBits,
		hashes:  hashes,
	}
}

func (bf *bloomFilter) add(r rune) {
	h1, h2 := bf.hashPair(r)
	for i := 0; i < bf.hashes; i++ {
		pos := (h1 + uint64(i)*h2) % bf.numBits
		bf.bits[pos/64] |= 1 << (pos % 64)
	}
}

func (bf *bloomFilter) mightContain(r rune) bool {
	h1, h2 := bf.hashPair(r)
	for i := 0; i < bf.hashes; i++ {
		pos := (h1 + uint64(i)*h2) % bf.numBits
		if bf.bits[pos/64]&(1<<(pos%64)) == 0 {
			return false
		}
	}
	return true
}

func (bf *bloomFilter) memoryBytes() int64 {
	return int64(len(bf.bits)) * 8
}

// skipAtRoot reports whether ch can be skipped: it cannot start any keyword and
// traversal is at the root state. A nil filter never skips (pre-filter disabled).
func (bf *bloomFilter) skipAtRoot(atRoot bool, r rune) bool {
	return bf != nil && atRoot && !bf.mightContain(r)
}

// buildFirstRuneBloom builds a Bloom filter of the first rune of each keyword,
// used as a root-state pre-filter to skip characters that cannot start a match.
func buildFirstRuneBloom(keywords map[string]struct{}) *bloomFilter {
	bf := newBloomFilter(len(keywords), 0.01)
	for kw := range keywords {
		if r, size := utf8.DecodeRuneInString(kw); size > 0 {
			bf.add(r)
		}
	}
	return bf
}

func (bf *bloomFilter) hashPair(r rune) (hash1, hash2 uint64) { //nolint:gosec
	var h1 uint64 = 14695981039346656037
	h1 ^= uint64(r) //nolint:gosec
	h1 *= 1099511628211

	var h2 uint64 = 6959950808824887261
	h2 ^= uint64(r) //nolint:gosec
	h2 *= 1099511628211
	h2 ^= uint64(r >> 8) //nolint:gosec
	h2 *= 1099511628211

	return h1, h2
}
