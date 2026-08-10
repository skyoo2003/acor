module github.com/skyoo2003/acor/benchmarks

go 1.25.0

replace github.com/skyoo2003/acor => ../

require (
	github.com/alicebob/miniredis/v2 v2.38.0
	github.com/skyoo2003/acor v0.10.1
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/redis/go-redis/v9 v9.22.0 // indirect
	github.com/yuin/gopher-lua v1.1.1 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.45.0 // indirect
)
