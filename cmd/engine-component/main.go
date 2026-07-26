//go:build tinygo

// Wasip2 component entrypoint for the dlc engine (Decision 26: this is the
// parity/portability artifact — web loads it via jco, and the CI parity check
// diffs its output against the native host).
//
// Almost nothing lives here. The WIT glue is app-agnostic and belongs to the
// platform (engine/platform/wasm); this file is the shape every scaffolded app's
// component main has — import the engine to register its commands, import the
// platform's wasm package to wire the exports, and stop.
//
// The one extra line is dlc's own: it still has an in-engine argv shim, which a
// scaffolded app does not (its host builds requests — Decision 28). That line
// disappears when the shim retires.
//
// Build (never natively — the //go:build tinygo tag keeps `go build ./...` off it):
//
//	tinygo build -target=wasip2 --wit-package ./wit --wit-world engine \
//	  -o engine.component.wasm ./cmd/engine-component
package main

import (
	"github.com/devalbo/devalbo-ilc/engine"
	"github.com/devalbo/devalbo-ilc/engine/platform/wasm"
)

func init() { wasm.SetExecuteCli(engine.Execute) }

func main() {}
