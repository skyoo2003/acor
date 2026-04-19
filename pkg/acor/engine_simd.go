// SPDX-License-Identifier: Apache-2.0

package acor

// simdScanner provides fast byte-level pre-filtering using a 256-bit presence
// bitmap. Before entering the trie scan, it checks whether the current byte
// appears in any keyword's first character, skipping non-matching regions.
type simdScanner struct {
	bitmap [4]uint64 // 256-bit bitmap: bit i set if byte i appears in any keyword
}

func newSIMDScanner(keywords map[string]struct{}) *simdScanner {
	s := &simdScanner{}
	s.buildPresenceBitmap(keywords)
	return s
}

func (s *simdScanner) buildPresenceBitmap(keywords map[string]struct{}) {
	for kw := range keywords {
		runes := []rune(kw)
		if len(runes) > 0 {
			r := runes[0]
			if r < 256 {
				s.bitmap[r/64] |= 1 << (r % 64)
			}
			if r >= 256 {
				s.bitmap[0] = ^uint64(0)
				s.bitmap[1] = ^uint64(0)
				s.bitmap[2] = ^uint64(0)
				s.bitmap[3] = ^uint64(0)
				return
			}
		}
	}
}

func (s *simdScanner) mightMatch(r rune) bool {
	if r >= 256 {
		return true
	}
	return s.bitmap[r/64]&(1<<(r%64)) != 0
}

func (s *simdScanner) memoryBytes() int64 {
	return 32
}
