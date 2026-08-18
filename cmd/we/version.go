package main

// version is stamped at link time with `-ldflags "-X main.version=0.1.0"` by
// `make build` and by the release workflow. An unstamped build — a plain
// `go build ./cmd/we` — reports "dev", which is the truthful answer for a
// binary with no release behind it.
var version = "dev"
