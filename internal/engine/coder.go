// SPDX-License-Identifier: Apache-2.0

package engine

import "unicode/utf8"

// alphabetCoder maps runes to compact alphabet indices with an ASCII fast path.
// It is embedded by both the Speed (flat) and Balanced/Ultimate (DAT) engines,
// which resolve nearly every input character to an index and must agree on the
// encoding.
type alphabetCoder struct {
	index map[rune]int
	// asciiCode is a direct-index fast path for index: for an ASCII rune r in the
	// alphabet, asciiCode[r] = index+1 (0 means "not in alphabet"), avoiding a map
	// hash on nearly every character.
	asciiCode [128]int32
}

// build populates the coder from the sorted alphabet runes.
func (c *alphabetCoder) build(runes []rune) {
	c.index = make(map[rune]int, len(runes))
	c.asciiCode = [128]int32{}
	for i, r := range runes {
		c.index[r] = i
		if r < utf8.RuneSelf {
			c.asciiCode[r] = int32(i) + 1
		}
	}
}

// code resolves a rune to its alphabet index via the ASCII fast path, falling
// back to the map for non-ASCII runes. ok is false if ch is not in the alphabet.
func (c *alphabetCoder) code(ch rune) (int, bool) {
	// ch >= 0 guards the ASCII fast path: a negative rune (e.g. an invalid rune
	// from a caller-supplied Engine.Stream source) also passes ch < RuneSelf and
	// would index asciiCode out of bounds. Treat it as not in the alphabet.
	if ch >= 0 && ch < utf8.RuneSelf {
		x := c.asciiCode[ch]
		return int(x) - 1, x != 0
	}
	x, ok := c.index[ch]
	return x, ok
}
