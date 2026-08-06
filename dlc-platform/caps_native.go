//go:build !tinygo.wasm

package platform

// The NATIVE half of the capability seam (§5.3) — and the BARE-METAL half too.
//
// Here a capability is a direct Go call: the host linked the engine in-process
// (Decision 26), so there is no boundary to cross and no serialization to pay.
// The wasip2 half of this seam is caps_wasip2.go, which calls a WIT import
// instead. Business logic above the seam is byte-identical, and the parity check
// exists to keep that claim honest.
//
// TWO FILES, NOT THREE — and the plan said three, so this is worth defending.
// The third was to be a bare-metal seam for TinyGo-on-a-board, on the assumption
// that three build targets need three bindings. They do not: a board host links
// the engine into the same process and hands it a sink, which is what this file
// already does. A third file would have been a copy of this one under a
// different tag, and copies drift.
//
// So the seam splits on the ONE thing that actually differs — whether `emit`
// crosses a wasm boundary — and the two constraints are exact complements
// (`tinygo.wasm` / `!tinygo.wasm`). No target can select both, and none can
// select neither; a third file would have had to carve a hole in one of them and
// left that guarantee to a reviewer's attention.
//
// `tinygo.wasm` rather than `wasm`: `-target=wasip2` reports GOARCH=arm, so the
// GOARCH tag is false for the component build. See caps_wasip2.go.

// emit delivers to the in-process sink, if a host installed one.
//
// A sink that panics must not take the engine down with it. A host's UI callback
// is not the engine's business, and a command that succeeded should not report
// failure because a listener was buggy — the event has already happened, and the
// filesystem write it describes is already committed.
func emit(topic string, payload []byte) {
	fn := sink
	if fn == nil {
		return
	}
	defer func() { _ = recover() }()
	fn(topic, payload)
}
