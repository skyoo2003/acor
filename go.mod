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

// Published in error; upgrade to v1.5.0 or later.
// Only the line above reaches users: the go command truncates a retraction
// rationale at the first newline, so keep it a complete sentence.
// v1.5.0 is the first supported v1 release. Never let this range reach it.
// See RELEASE.md.
retract [v1.0.0, v1.4.0]
