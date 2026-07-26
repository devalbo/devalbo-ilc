// Native dlc host (Decision 26): links the engine in-process — no wasm runtime in
// the run path, sidestepping the wasmtime-go Component-Model gap. It is a thin
// forwarder: os.Args → engine.Execute → stdout/stderr + exit code. Parsing stays
// in-engine (Spike 4). This is the reference `dlc` binary for terminal use.
//
// Build:
//
//	go build -o dlc ./hosts/native
package main

import (
	"os"

	"github.com/devalbo/devalbo-ilc/engine"
)

func main() {
	r := engine.Execute(os.Args[1:])
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
