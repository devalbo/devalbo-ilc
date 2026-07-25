// Portable / WAMR-track probe (Spike 5).
//
// TinyGo -target=wasip1 core module + //go:wasmimport host function — the same
// shape WAMR uses for native-function registration (no Component Model).
// The host is allowed to block (time.Sleep); there is no JS event loop.
//
// See docs/WASI-UPGRADES.md (Portable/WAMR track, separate from Rich/CM jco probe).
package main

//go:wasmimport env host_delay
func hostDelay(ms uint32) uint32

// runWait is called by the host after instantiate. It blocks in the guest on
// hostDelay; the host implementation sleeps, then returns the echoed ms.
//
//export run_wait
func runWait(ms uint32) uint32 {
	return hostDelay(ms)
}

func main() {}
