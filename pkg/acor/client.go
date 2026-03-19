package acor

import (
	"context"
	"fmt"
	"strings"

	redis "github.com/go-redis/redis/v8"
)

func newRedisClient(args *AhoCorasickArgs) (redis.UniversalClient, error) {
	addrs := normalizeAddrs(args.Addr, args.Addrs)
	ringAddrs := normalizeRingAddrs(args.RingAddrs)

	if err := validateRedisTopology(args, addrs, ringAddrs); err != nil {
		return nil, err
	}

	switch {
	case len(ringAddrs) > 0:
		return newRingRedisClient(args, ringAddrs), nil
	case strings.TrimSpace(args.MasterName) != "":
		return newSentinelRedisClient(args, addrs), nil
	case len(args.Addrs) > 0:
		return newClusterRedisClient(args, addrs)
	default:
		return newStandaloneRedisClient(args, addrs), nil
	}
}

func validateRedisTopology(args *AhoCorasickArgs, addrs []string, ringAddrs map[string]string) error {
	if strings.TrimSpace(args.Addr) != "" && len(args.Addrs) > 0 {
		return ErrRedisConflictingTopology
	}

	hasSentinel := strings.TrimSpace(args.MasterName) != ""
	hasRing := len(ringAddrs) > 0
	hasCluster := !hasSentinel && len(addrs) > 0

	selectedTopologies := 0
	if hasSentinel {
		selectedTopologies++
	}
	if hasRing {
		selectedTopologies++
	}
	if hasCluster {
		selectedTopologies++
	}
	if selectedTopologies > 1 {
		return ErrRedisConflictingTopology
	}
	if hasSentinel && len(addrs) == 0 {
		return ErrRedisSentinelAddrs
	}
	if len(args.RingAddrs) > 0 && len(ringAddrs) == 0 {
		return ErrRedisRingAddrs
	}
	if hasCluster && args.DB != 0 {
		return ErrRedisClusterDB
	}

	return nil
}

func newRingRedisClient(args *AhoCorasickArgs, ringAddrs map[string]string) redis.UniversalClient {
	return redis.NewRing(&redis.RingOptions{
		Addrs:    ringAddrs,
		Password: args.Password,
		DB:       args.DB,
	})
}

func newSentinelRedisClient(args *AhoCorasickArgs, addrs []string) redis.UniversalClient {
	return redis.NewFailoverClient(&redis.FailoverOptions{
		SentinelAddrs: addrs,
		MasterName:    strings.TrimSpace(args.MasterName),
		Password:      args.Password,
		DB:            args.DB,
	})
}

func newClusterRedisClient(args *AhoCorasickArgs, addrs []string) (redis.UniversalClient, error) {
	client := redis.NewClusterClient(&redis.ClusterOptions{
		Addrs:    addrs,
		Password: args.Password,
	})
	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		return nil, fmt.Errorf("failed to connect to Redis cluster: %w", err)
	}
	return client, nil
}

func newStandaloneRedisClient(args *AhoCorasickArgs, addrs []string) redis.UniversalClient {
	addr := strings.TrimSpace(args.Addr)
	if addr == "" && len(addrs) > 0 {
		addr = addrs[0]
	}
	return redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: args.Password,
		DB:       args.DB,
	})
}

func normalizeAddrs(addr string, addrs []string) []string {
	normalized := make([]string, 0, len(addrs)+1)
	seen := make(map[string]struct{}, len(addrs)+1)
	appendAddr := func(value string) {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return
		}
		if _, exists := seen[trimmed]; exists {
			return
		}
		seen[trimmed] = struct{}{}
		normalized = append(normalized, trimmed)
	}

	appendAddr(addr)
	for _, candidate := range addrs {
		appendAddr(candidate)
	}

	return normalized
}

func normalizeRingAddrs(addrs map[string]string) map[string]string {
	normalized := make(map[string]string, len(addrs))
	for name, addr := range addrs {
		trimmedName := strings.TrimSpace(name)
		trimmedAddr := strings.TrimSpace(addr)
		if trimmedName == "" || trimmedAddr == "" {
			continue
		}
		normalized[trimmedName] = trimmedAddr
	}
	return normalized
}
