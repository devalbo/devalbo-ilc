// baremetal-probe — does the platform LINK for a microcontroller?
//
// Not a program anybody runs. It exists so `tinygo build -target=pico2` has a
// main package to link, which is the only way to ask this question: the capability
// seam picks a file by build tag, and a wrong tag is invisible to `go vet`, to
// every unit test, and to both wasm builds. It shows up as a linker error naming
// a generated symbol — `could not find symbol …wasmimport_Emit` — which points at
// the generated code and not at the constraint that asked for it.
//
// That is exactly what `//go:build tinygo` did before it became `tinygo && wasm`:
// TinyGo also compiles for boards, so the wasip2 half of the seam was selected for
// a target that has no host to import from. Native builds, wasip2 builds and the
// whole test suite stayed green throughout.
//
// It imports the platform and calls into it, deliberately. An import alone can be
// dropped by the linker before any symbol is resolved, and a probe that passes by
// eliding what it is probing is worse than none.
package main

import (
	platform "github.com/devalbo/devalbo-ilc/dlc-platform"
)

func main() {
	// Execute reaches dispatch, which reaches the event seam — the file this
	// probe exists to link. A no-op main would link nothing and prove nothing.
	res := platform.Execute(0, nil)
	if res.Success {
		platform.Emit("probe.linked", nil)
	}
}
