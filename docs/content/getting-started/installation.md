---
title: "Installation"
weight: 1
---

# Installation

Requires **Go 1.25+** and **Redis 3.0+** or **Valkey 7.2+**.

ACOR speaks RESP through [go-redis v9](https://github.com/redis/go-redis), so any
Redis- or Valkey-compatible server works. RESP3 is negotiated on connect and falls back
to RESP2 on servers older than `HELLO`.

## Library

```bash
go get github.com/skyoo2003/acor/pkg/acor@latest
```

Verify it with the [Quick Start](../quick-start/) program.

## CLI

```bash
go install github.com/skyoo2003/acor/cmd/acor@latest

acor --help     # command list and every flag's default
acor version    # needs no Redis; prints `dev` for a locally built binary
```

What the commands do — option ordering, batch modes, matching shapes, parallel chunking,
the local cache — is the [CLI](../../cli/) section.
