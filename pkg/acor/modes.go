// SPDX-License-Identifier: Apache-2.0

package acor

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
