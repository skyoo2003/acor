---
title: "Installation"
weight: 1
---

# Installation

## Prerequisites

- **Go**: Version 1.24 or later
- **Redis**: Version 3.0 or later

## Install the Package

```bash
go get -u github.com/skyoo2003/acor
```

## Verify Installation

Create a test file to verify ACOR is installed correctly:

```go
package main

import (
    "fmt"
    "github.com/skyoo2003/acor/pkg/acor"
)

func main() {
    args := &acor.AhoCorasickArgs{
        Addr: "localhost:6379",
        Name: "test",
    }
    
    ac, err := acor.Create(args)
    if err != nil {
        panic(err)
    }
    defer ac.Close()
    
    fmt.Println("ACOR installed successfully!")
}
```

## CLI Installation

Install the command-line tool:

```bash
go install github.com/skyoo2003/acor/cmd/acor@latest
```

Verify the CLI:

```bash
acor --help
```

## Next Steps

- [Quick Start](/getting-started/quick-start/) - Build your first application
