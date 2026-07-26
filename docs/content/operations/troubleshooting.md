---
title: "Troubleshooting"
weight: 3
---

# Troubleshooting

Common issues and their solutions.

## Common Errors

### ErrRedisConflictingTopology

**Cause:** Multiple Redis topologies specified simultaneously.

**Solution:** Use only one configuration:

```go
// Correct: Standalone
args := &acor.AhoCorasickArgs{
    Addr: "localhost:6379",
    Name: "my-collection",
}

// Correct: Sentinel
args := &acor.AhoCorasickArgs{
    Addrs:      []string{"localhost:26379"},
    MasterName: "mymaster",
    Name:       "my-collection",
}

// Wrong: Mixing configurations
args := &acor.AhoCorasickArgs{
    Addr:       "localhost:6379",      // Wrong!
    Addrs:      []string{"..."},        // Wrong!
    MasterName: "mymaster",             // Wrong!
    Name:       "my-collection",
}
```

### ErrEmptyKeyword

**Cause:** Empty string passed to `Add()`.

**Solution:** Validate input:

```go
keyword := strings.TrimSpace(input)
if keyword == "" {
    return errors.New("keyword cannot be empty")
}
_, err = ac.Add(keyword)
```

### ErrInvalidChunkSize

**Cause:** Non-positive chunk size in parallel matching.

**Solution:** Use positive values:

```go
opts := &acor.ParallelOptions{
    Workers:   4,
    ChunkSize: 1000, // Must be > 0
}
```

### ErrRedisAlreadyClosed

**Cause:** Operation on closed AhoCorasick instance.

**Solution:** Ensure `Close()` is called only once, typically with `defer`:

```go
ac, err := acor.Create(args)
if err != nil {
    log.Fatal(err)
}
defer ac.Close() // Called once at function exit
```

## Redis Connection Issues

### Connection Refused

```text
redis GET on key "...": connection refused
```

**Checklist:**
1. Redis is running: `redis-cli ping`
2. Address is correct
3. Firewall allows connection
4. Network connectivity

### Authentication Failed

```text
redis GET on key "...": NOAUTH Authentication required
```

**Solution:** Provide password:

```go
args := &acor.AhoCorasickArgs{
    Addr:     "localhost:6379",
    Password: "your-password",
    Name:     "my-collection",
}
```

### Timeout Errors

```text
redis GET on key "...": context deadline exceeded
```

**Solutions:**
1. Tune `DialTimeout`, `ReadTimeout`, or `WriteTimeout` when measurements show
   the defaults are too short
2. Check Redis load
3. Check network latency
4. Set `MaxRetries` for transient failures or adjust `PoolSize` for measured
   connection contention
5. Scale Redis cluster

Zero values keep the go-redis defaults across Standalone, Sentinel, Cluster,
and Ring modes.

<!-- doccheck -->
```go
args := &acor.AhoCorasickArgs{
    Addr:         "localhost:6379",
    Name:         "my-collection",
    DialTimeout:  5 * time.Second,
    ReadTimeout:  3 * time.Second,
    WriteTimeout: 3 * time.Second,
    MaxRetries:   3,
}
_ = args
```

### Preset Cache Appears Stale

**Cause:** Preset mode normally reloads through best-effort Redis Pub/Sub. A
disconnected subscriber can miss an invalidation.

**Solution:** In multi-instance deployments, set
`InvalidationPollInterval` to the maximum acceptable staleness window:

```go
args.InvalidationPollInterval = 30 * time.Second
```

The option is disabled by default and ignored outside Preset mode.

## Performance Issues

### Slow Find Operations

**Diagnostic:**
1. Check schema version: `acor -name collection schema-version`
2. Check collection size: `acor -name collection info`

**Solutions:**
- Migrate to V2 schema
- Use parallel matching for large texts
- Increase Redis memory

### High Memory Usage

**Diagnostic:**
1. Check Redis memory: `redis-cli info memory`
2. Check keyword count: `acor -name collection info`

**Solutions:**
- Remove unused keywords
- Use V2 schema (lower memory)
- Scale Redis cluster

## Debugging

### Enable Debug Logging

```go
logger := logging.NewLogger(os.Stdout, "debug")
```

### CLI Debug Mode

```bash
acor -name mycollection -debug find "test text"
```

### Check Redis Keys

```bash
redis-cli keys "{mycollection}:*"
```
