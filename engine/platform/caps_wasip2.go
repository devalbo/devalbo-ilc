//go:build tinygo

package platform

// The WASIP2 half of the capability seam (§5.3).
//
// Here a capability is a WIT import: the host supplies `emit` when it
// instantiates the component, and jco (browser) or wasmtime (harness) routes it
// out. The native half is caps_native.go, which calls a Go function directly.
//
// This file is why the seam exists at all. Everything above it — platform.Emit,
// every app handler — is identical on both tiers; only these few lines differ.

import (
	"go.bytecodealliance.org/cm"

	"github.com/devalbo/devalbo-ilc/gen/go/devalbo/ilc/events"
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
