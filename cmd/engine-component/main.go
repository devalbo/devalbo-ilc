//go:build tinygo

// Wasip2 component entrypoint for the dlc engine (Decision 26: this is the
// parity/portability artifact — web loads it via jco, and a CI/`dlc verify` step
// diffs its output against the native host). It only adapts the WIT export to
// engine.Execute; it holds NO business logic.
//
// Build (never natively — the //go:build tinygo tag keeps `go build ./...` off it):
//
//	tinygo build -target=wasip2 --wit-package ./wit --wit-world engine \
//	  -o engine.component.wasm ./cmd/engine-component
package main

import (
	"github.com/devalbo/devalbo-ilc/engine"
	wit "github.com/devalbo/devalbo-ilc/gen/go/devalbo/ilc/engine"
	"github.com/devalbo/devalbo-ilc/gen/go/devalbo/ilc/types"
	"go.bytecodealliance.org/cm"
)

func init() { wit.Exports.ExecuteCli = executeCli }

func executeCli(args cm.List[string]) wit.CommandResult {
	r := engine.Execute(args.Slice())
	res := types.CommandResult{
		Success: r.Success,
		Output:  cm.ToList(r.Output),
	}
	if r.Err != "" {
		res.Error = cm.Some(r.Err)
	} else {
		res.Error = cm.None[string]()
	}
	return res
}

func main() {}
