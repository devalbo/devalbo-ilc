//go:build tinygo.wasm

package platform

// The WASIP2 half of the capability seam (§5.3).
//
// Here a capability is a WIT import: the host supplies `emit` when it
// instantiates the component, and jco (browser) or wasmtime (harness) routes it
// out. The native half is caps_native.go, which calls a Go function directly.
//
// This file is why the seam exists at all. Everything above it — platform.Emit,
// every app handler — is identical on both tiers; only these few lines differ.
//
// `tinygo.wasm`, NOT `tinygo`. TinyGo also compiles for microcontrollers, and on
// a bare-metal target there is no host to import from — the bare `tinygo` tag
// selected this file for a `-target=pico2` build and the link died with "could
// not find symbol …wasmimport_Emit", which names a generated symbol and not the
// constraint that asked for it.
//
// AND NOT `tinygo && wasm`, which is the obvious fix and is WRONG. Ask TinyGo
// what it sets and the reason is immediate:
//
//	tinygo info -target=wasip2 → GOOS: linux   GOARCH: arm   tags: tinygo.wasm …
//	tinygo info -target=pico2  → GOOS: linux   GOARCH: arm   tags: baremetal …
//
// **`-target=wasip2` reports GOARCH=arm.** So `wasm` is false for the component
// build, and `tinygo && wasm` sent the browser tier to the NATIVE seam, where
// `emit` posts to a sink no component has — events silently stopped crossing the
// boundary while every build stayed green. The parity check caught it (missing
// `EVENT` rows on the wasm side); nothing else would have.
//
// `tinygo.wasm` is TinyGo's own answer to "does this compile to wasm", set for
// wasip1 and wasip2 and absent on a board. Derive the tag from `tinygo info`,
// never from GOARCH.

import (
	"go.bytecodealliance.org/cm"

	"github.com/devalbo/devalbo-ilc/dlc-platform/gen/go/devalbo/ilc/events"
)

// emit calls the host's import.
//
// SYNCHRONOUS, and it must stay that way. jco only supports a Promise-returning
// import under `--async-imports`, which we deliberately do not use (Decision 22;
// see docs/WASI-UPGRADES.md). The WIT declaration has no return value precisely
// so this cannot drift — but a host that makes its JS callback `async` would
// reintroduce the problem, which is why hosts/web/README.md says so too.
//
// SetEventSink is ignored on this tier: the "sink" is the component boundary
// itself, wired by whoever instantiated us.
func emit(topic string, payload []byte) {
	events.Emit(topic, cm.ToList(payload))
}
