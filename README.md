# ACOR

ACOR means Aho-Corasick automation working On Redis, Written in Go

[![Current Release](https://img.shields.io/github/release/skyoo2003/acor.svg)](https://github.com/skyoo2003/acor/releases/latest)
[![CI Status](https://github.com/skyoo2003/acor/actions/workflows/go.yaml/badge.svg)](https://github.com/skyoo2003/acor/actions/workflows/go.yaml)
[![Go Reference](https://pkg.go.dev/badge/github.com/skyoo2003/acor.svg)](https://pkg.go.dev/github.com/skyoo2003/acor)
[![Go Report Card](https://goreportcard.com/badge/github.com/skyoo2003/acor)](https://goreportcard.com/report/github.com/skyoo2003/acor)
[![License](https://img.shields.io/github/license/mashape/apistatus.svg)](LICENSE)

# Prerequisites

* Golang >= 1.11
* Redis >= 3.0

# Getting Started

```sh
$ go get -u https://github.com/skyoo2003/acor
```

```go
package main

import (
	"fmt"
	"github.com/skyoo2003/acor"
)

func main() {
	args := &acor.AhoCorasickArgs{
		Addr:     "localhost:6379",
		Password: "",
		DB:       0,
		Name:     "sample",
	}
	ac := acor.Create(args)
	defer ac.Close()

	keywords := []string{"he", "her", "him"}
	for _, k := range keywords {
		ac.Add(k)
	}

	matched := ac.Find("he is him")
	fmt.Println(matched)

	ac.Flush() // If you want to remove all of data 
}
```

# Contributing

Pull requests are welcome. For major changes, please open an issue first to discuss what you would like to change.

Please make sure to update tests as appropriate.

# [License](LICENSE)

Copyright (c) 2016-2021 Sung-Kyu Yoo.

This project is MIT license.