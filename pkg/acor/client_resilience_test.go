// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"testing"
	"time"

	redis "github.com/redis/go-redis/v9"
)

// TestRedisResilienceKnobsPassThrough verifies that the connection-tuning
// fields on AhoCorasickArgs reach the underlying go-redis options, for both
// topologies that yield a *redis.Client: standalone builds its options through
// UniversalOptions.Simple(), sentinel through Failover().
func TestRedisResilienceKnobsPassThrough(t *testing.T) {
	knobs := func(args AhoCorasickArgs) *AhoCorasickArgs {
		args.Password, args.DB = "secret", 3
		args.DialTimeout, args.ReadTimeout, args.WriteTimeout = 7*time.Second, 8*time.Second, 9*time.Second
		args.MaxRetries, args.PoolSize = 7, 42
		return &args
	}

	for name, args := range map[string]*AhoCorasickArgs{
		"standalone": knobs(AhoCorasickArgs{Addr: "localhost:6379"}),
		"sentinel":   knobs(AhoCorasickArgs{Addrs: []string{"127.0.0.1:26379"}, MasterName: "mymaster"}),
	} {
		t.Run(name, func(t *testing.T) {
			client, err := newRedisClient(args)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = client.Close() }()

			opt := client.(*redis.Client).Options()
			if opt.DB != 3 {
				t.Errorf("DB = %d, want 3", opt.DB)
			}
			if opt.Password != "secret" {
				t.Errorf("Password = %q, want secret", opt.Password)
			}
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
		})
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
