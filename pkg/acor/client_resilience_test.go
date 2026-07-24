// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"testing"
	"time"

	redis "github.com/redis/go-redis/v9"
)

// TestRedisResilienceKnobsPassThrough verifies that the connection-tuning
// fields on AhoCorasickArgs reach the underlying go-redis options.
func TestRedisResilienceKnobsPassThrough(t *testing.T) {
	client, err := newRedisClient(&AhoCorasickArgs{
		Addr:         "localhost:6379",
		DialTimeout:  7 * time.Second,
		ReadTimeout:  8 * time.Second,
		WriteTimeout: 9 * time.Second,
		MaxRetries:   7,
		PoolSize:     42,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()

	opt := client.(*redis.Client).Options()
	if opt.DialTimeout != 7*time.Second {
		t.Errorf("DialTimeout = %v, want 7s", opt.DialTimeout)
	}
	if opt.ReadTimeout != 8*time.Second {
		t.Errorf("ReadTimeout = %v, want 8s", opt.ReadTimeout)
	}
	if opt.WriteTimeout != 9*time.Second {
		t.Errorf("WriteTimeout = %v, want 9s", opt.WriteTimeout)
	}
	if opt.MaxRetries != 7 {
		t.Errorf("MaxRetries = %d, want 7", opt.MaxRetries)
	}
	if opt.PoolSize != 42 {
		t.Errorf("PoolSize = %d, want 42", opt.PoolSize)
	}
}

// TestRedisResilienceZeroFallsBackToDefaults verifies that leaving the knobs
// unset yields go-redis's built-in defaults rather than zero timeouts.
func TestRedisResilienceZeroFallsBackToDefaults(t *testing.T) {
	client, err := newRedisClient(&AhoCorasickArgs{Addr: "localhost:6379"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()

	opt := client.(*redis.Client).Options()
	if opt.MaxRetries != 3 { // go-redis default
		t.Errorf("MaxRetries = %d, want go-redis default 3", opt.MaxRetries)
	}
	if opt.DialTimeout <= 0 {
		t.Errorf("DialTimeout = %v, want positive go-redis default", opt.DialTimeout)
	}
}
