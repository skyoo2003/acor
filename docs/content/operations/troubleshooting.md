---
title: "Troubleshooting"
weight: 3
---

# Troubleshooting

Common issues and their solutions.

## Common Errors

### ErrConflictingTopology

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

### ErrInvalidWorkerCount

**Cause:** Negative worker count in parallel matching.

**Solution:** Use non-negative values. Zero defaults to runtime.NumCPU():

```go
opts := &acor.ParallelOptions{
    Workers: 0, // Defaults to runtime.NumCPU()
}
```

### ErrNoBoundariesFound

**Cause:** Text cannot be split for parallel matching.

**Solution:** Use smaller chunk size or different boundary type:

```go
opts := &acor.ParallelOptions{
    Workers:       4,
    ChunkBoundary: acor.ChunkWord,
    ChunkSize:     100, // Smaller chunks
}
```

### ErrRedisClosed

**Cause:** Operation on closed AhoCorasick instance.

**Solution:** Ensure `Close()` is called only once, typically with `defer`:

```go
ac, _ := acor.Create(args)
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
1. Increase timeout
2. Check Redis load
3. Check network latency
4. Scale Redis cluster

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
