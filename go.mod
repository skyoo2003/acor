module github.com/skyoo2003/acor

go 1.25.0

require (
	github.com/alicebob/miniredis/v2 v2.38.0
	github.com/redis/go-redis/v9 v9.21.0
	golang.org/x/sync v0.22.0
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/yuin/gopher-lua v1.1.1 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	golang.org/x/sys v0.45.0 // indirect
)

// Published in error; use the latest v0.x release instead.
// Only this first line reaches users - the go command truncates a retraction
// rationale at the first newline - so keep it a complete, actionable sentence.
// The v1.x tags were deleted from this repository, but proxy.golang.org caches
// versions permanently, so `go get github.com/skyoo2003/acor` resolved to
// v1.4.0 instead of the supported v0.x line. v1.4.1 is the retraction carrier
// and retracts itself, so @latest falls back to the highest v0.x.
// A real v1 starts at v1.5.0 - do not widen this range past v1.4.1.
retract [v1.0.0, v1.4.1]
