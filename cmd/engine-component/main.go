//go:build tinygo

// Wasip2 component entrypoint for the dlc engine (Decision 26: this is the
// parity/portability artifact — web loads it via jco, and the CI parity check
// diffs its output against the native host).
//
// Nothing lives here. The WIT glue is app-agnostic and belongs to the platform
// (engine/platform/wasm); this file is now IDENTICAL in shape to every
// scaffolded app's component main — import the engine to register its commands,
// import the platform's wasm package to wire the exports, and stop.
//
// Build (never natively — the //go:build tinygo tag keeps `go build ./...` off it):
//
//	tinygo build -target=wasip2 --wit-package ./wit --wit-world engine \
//	  -o engine.component.wasm ./cmd/engine-component
package main

import (
	_ "github.com/devalbo/devalbo-ilc/engine"               // registers dlc's commands
	_ "github.com/devalbo/devalbo-ilc/engine/platform/wasm" // wires the WIT exports
)

func main() {}
