// SPDX-License-Identifier: Apache-2.0

package acor

import "strings"

// backendMode identifies which engine backend is active.
type backendMode int

const (
	modeOriginal    backendMode = iota // V1 or V2 Redis-backed (original behavior)
	modeInMemory                       // Pure in-memory matchEngine
	modePresetRedis                    // Redis persistence + local matchEngine
)

// hasAnyRedisConfig returns true if any Redis connection field is set.
func (a *AhoCorasickArgs) hasAnyRedisConfig() bool {
	return a.Addr != "" || len(a.Addrs) > 0 ||
		a.MasterName != "" || len(a.RingAddrs) > 0 ||
		a.Password != "" || a.DB != 0
}

// normalizeKeyword trims whitespace and optionally lowercases a keyword.
func normalizeKeyword(keyword string, caseSensitive bool) string {
	keyword = strings.TrimSpace(keyword)
	if !caseSensitive {
		keyword = strings.ToLower(keyword)
	}
	return keyword
}

// normalizeText optionally lowercases search text.
func normalizeText(text string, caseSensitive bool) string {
	if !caseSensitive {
		return strings.ToLower(text)
	}
	return text
}
