# ACOR

ACOR means Aho-Corasick automation working On Redis, Written in Go

[![Current Release](https://img.shields.io/github/release/skyoo2003/acor.svg)](https://github.com/skyoo2003/acor/releases/latest)
[![CI Status](https://github.com/skyoo2003/acor/actions/workflows/ci.yaml/badge.svg)](https://github.com/skyoo2003/acor/actions/workflows/ci.yaml)
[![Go Reference](https://pkg.go.dev/badge/github.com/skyoo2003/acor.svg)](https://pkg.go.dev/github.com/skyoo2003/acor)
[![Go Report Card](https://goreportcard.com/badge/github.com/skyoo2003/acor)](https://goreportcard.com/report/github.com/skyoo2003/acor)
[![License](https://img.shields.io/github/license/mashape/apistatus.svg)](LICENSE)

# Prerequisites

* Golang >= 1.23
* Redis >= 3.0

# Getting Started

```sh
$ go get -u https://github.com/skyoo2003/acor
```

```go
package main

import (
	"fmt"
	"github.com/skyoo2003/acor/pkg/acor"
)

func main() {
	args := &acor.AhoCorasickArgs{
		Addr:     "localhost:6379",
		Password: "",
		DB:       0,
		Name:     "sample",
	}
	ac, err := acor.Create(args)
	if err != nil {
		panic(err)
	}
	defer ac.Close()

	keywords := []string{"he", "her", "him"}
	for _, k := range keywords {
		if _, err := ac.Add(k); err != nil {
			panic(err)
		}
	}

	matched, err := ac.Find("he is him")
	if err != nil {
		panic(err)
	}
	fmt.Println(matched)

	if err := ac.Flush(); err != nil { // If you want to remove all of data
		panic(err)
	}
}
```

Use the same `Create` API for Redis Sentinel, Cluster, and Ring by setting the topology-specific fields on `AhoCorasickArgs`. Use `Addr` for standalone Redis, use `Addrs` for Sentinel or Cluster seed nodes, and keep `DB` at `0` for Cluster:

```go
sentinelArgs := &acor.AhoCorasickArgs{
	Addrs:      []string{"localhost:26379", "localhost:26380"},
	MasterName: "mymaster",
	Password:   "",
	DB:         0,
	Name:       "sample",
}

clusterArgs := &acor.AhoCorasickArgs{
	Addrs:    []string{"localhost:7000", "localhost:7001", "localhost:7002"},
	Password: "",
	Name:     "sample",
}

ringArgs := &acor.AhoCorasickArgs{
	RingAddrs: map[string]string{
		"shard-1": "localhost:7000",
		"shard-2": "localhost:7001",
	},
	Password: "",
	DB:       0,
	Name:     "sample",
}
```

# Contributing

Pull requests are welcome. For major changes, please open an issue first to discuss what you would like to change.

Please make sure to update tests as appropriate.

# [License](LICENSE)

Copyright (c) 2016-2021 Sung-Kyu Yoo.

This project is MIT license.
