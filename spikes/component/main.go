// Spike 1 — component round-trip (T-B1.1).
//
// A deliberately trivial engine: it implements the WIT export
// execute-cli(args) -> command-result and returns "ok:"+args[0]. Its only job is
// to prove the pipeline end to end:
//
//	TinyGo (-target=wasip2, --wit-package ./wit --wit-world engine) -> component
//	  -> jco transpile -> JS -> called from Node, returns "ok:hi".
//
// We target wasip2 (native Component Model) rather than wasip1+wasm-tools adapter:
// TinyGo supplies cabi_realloc and wires _initialize for reactors, whereas the
// wasip1 adapter path needs a manual cabi_realloc that crashes on init with
// current tool versions (see docs/DEVALBO-DLC-TEST-STEPS.md T-B1.1 findings).
//
// Kept as a standing regression (see docs/DEVALBO-DLC-TEST-STEPS.md T-B1.1).
package main

import (
	"github.com/devalbo/devalbo-ilc/gen/go/devalbo/ilc/engine"
	"github.com/devalbo/devalbo-ilc/gen/go/devalbo/ilc/types"
	"go.bytecodealliance.org/cm"
)

func init() {
	engine.Exports.ExecuteCli = executeCli
}

func executeCli(args cm.List[string]) engine.CommandResult {
	first := ""
	if s := args.Slice(); len(s) > 0 {
		first = s[0]
	}
	out := []byte("ok:" + first)
	return types.CommandResult{
		Success: true,
		Output:  cm.ToList(out),
		Error:   cm.None[string](),
	}
}

// main is required by the toolchain but does nothing: the component is driven
// entirely through the exported execute-cli function.
func main() {}
