// Native dlc host (Decision 26): links the engine in-process — no wasm runtime in
// the run path, sidestepping the wasmtime-go Component-Model gap. This is the
// reference `dlc` binary for terminal use.
//
// TWO KINDS OF VERB pass through here, and the split is Decision 30:
//
//   - TOOLCHAIN verbs (`build`) spawn processes and inspect the machine, so they
//     are handled HOST-SIDE and never reach the engine — the engine also runs in
//     a browser tab, where neither is possible.
//   - IN-ENGINE verbs (`new`, `version`, `export-fs`, …) are forwarded to the
//     engine, which is the same code the browser runs.
//
// The forwarding still uses the transitional argv shim; Decision 28 replaces it
// with a host-side parser that builds a request. The toolchain branch below is
// the first piece of that parser — it has to be host-side by definition.
//
// Build:
//
//	go build -o dlc ./hosts/native
package main

import (
	"os"

	"github.com/devalbo/devalbo-ilc/engine"
)

// toolchainVerbs never cross into the engine (Decision 30).
var toolchainVerbs = map[string]func([]string) error{
	"build": runBuild,
}

func main() {
	args := os.Args[1:]

	if len(args) > 0 {
		if handler, ok := toolchainVerbs[args[0]]; ok {
			if err := handler(args[1:]); err != nil {
				os.Stderr.WriteString("dlc: " + err.Error() + "\n")
				os.Exit(1)
			}
			return
		}
	}

	r := engine.Execute(args)
	if len(r.Output) > 0 {
		os.Stdout.Write(r.Output)
	}
	if !r.Success {
		if r.Err != "" {
			os.Stderr.WriteString("dlc: " + r.Err + "\n")
		}
		os.Exit(1)
	}
}
