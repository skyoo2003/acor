// SPDX-License-Identifier: Apache-2.0

package engine

const (
	// hasOutputBit shares an int32 transition entry with its target state. Valid
	// packed state IDs are [0, hasOutputBit), leaving the sign bit clear.
	hasOutputBit int32 = 1 << 30
	// outNone marks the end of an output-link chain.
	outNone = -1
)

func requirePackableStateCount(count int) {
	if count >= int(hasOutputBit) {
		panic("engine: too many states for packed transitions")
	}
}

func packState(state int, hasOutput bool) int32 {
	if state < 0 || state >= int(hasOutputBit) {
		panic("engine: state does not fit in packed transition")
	}
	entry := int32(state) //nolint:gosec // G115: guarded above.
	if hasOutput {
		entry |= hasOutputBit
	}
	return entry
}

func appendOutputChain(dst []string, state int, own []string, outLink []int32) []string {
	for s := state; s != outNone; s = int(outLink[s]) {
		if kw := own[s]; kw != "" {
			if dst == nil {
				dst = make([]string, 0, findResultHint)
			}
			dst = append(dst, kw)
		}
	}
	return dst
}

func collectOutputChain(c *setCollector, state int, own []string, outLink []int32) {
	if !c.addState(state) {
		return
	}
	for s := state; s != outNone; s = int(outLink[s]) {
		if kw := own[s]; kw != "" {
			c.addKeyword(kw)
		}
	}
}

func indexOutputChain(dst map[string][]int, state, end int, own []string, outLink []int32, asciiOnly bool) {
	for s := state; s != outNone; s = int(outLink[s]) {
		if kw := own[s]; kw != "" {
			dst[kw] = append(dst[kw], end-runeLen(kw, asciiOnly))
		}
	}
}

func emitOutputChain(state, end int, own []string, outLink []int32, asciiOnly bool, emit func(keyword string, start, end int) bool) bool {
	for s := state; s != outNone; s = int(outLink[s]) {
		if kw := own[s]; kw != "" && !emit(kw, end-runeLen(kw, asciiOnly), end) {
			return false
		}
	}
	return true
}
