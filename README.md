# ACOR (Aho-Corasick automation On Redis)
Golang implementation of Aho-Corasick algorithm, working on redis

[![Current Release](https://img.shields.io/github/release/skyoo2003/acor.svg)](https://github.com/skyoo2003/acor/releases/latest)
[![Build Status](https://github.com/skyoo2003/acor/workflows/Go/badge.svg)](https://github.com/skyoo2003/acor)
[![Godoc](http://img.shields.io/badge/godoc-reference-blue.svg?style=flat)](https://godoc.org/github.com/skyoo2003/acor)
[![License](https://img.shields.io/github/license/mashape/apistatus.svg)](LICENSE)

# Prerequisite

* Golang >= 1.11
* Redis >= 3.0

# Usage

```
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

# Contribution

If you want to participate, you can create an issue or request a 'Pull Request'.

Welcome any and all suggestions.

# License

MIT License

# References

* Refered to project : [judou/redis-ac-keyword](https://github.com/judou/redis-ac-keywords)
* Aho-Corasick paper link : [Efficient string matching: an aid to bibliographic search](http://dl.acm.org/citation.cfm?id=360855)
* Aho-Corasick wikipedia : [Aho-Corasick algorithm wiki](https://en.wikipedia.org/wiki/Aho%E2%80%93Corasick_algorithm)
