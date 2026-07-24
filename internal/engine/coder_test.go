// SPDX-License-Identifier: Apache-2.0

package engine

import "testing"

// A negative rune (an invalid rune reachable via a caller-supplied Engine.Stream
// source) also passes the ch < utf8.RuneSelf ASCII fast-path test and used to
// index asciiCode out of bounds. It must instead be reported as not in the
// alphabet, without panicking.
func TestAlphabetCoderNegativeRune(t *testing.T) {
	var c alphabetCoder
	c.build([]rune{'a', 'b', 'c'})

	if idx, ok := c.code(-1); ok {
		t.Errorf("code(-1) = (%d, true); want not-in-alphabet", idx)
	}
	if _, ok := c.code('a'); !ok {
		t.Error("code('a') should be in alphabet")
	}
	if _, ok := c.code('z'); ok {
		t.Error("code('z') should not be in alphabet")
	}
}
