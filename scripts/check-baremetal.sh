#!/usr/bin/env bash
#
# check-baremetal.sh — does the platform LINK for a microcontroller?
#
# WHY THIS EXISTS. The capability seam (§5.3) picks its implementation with a
# build constraint, and a wrong constraint is invisible to everything else here:
# `go vet` is happy, every unit test passes, and both wasm builds are green,
# because none of them ever compile for a board. It surfaces as a linker error
# naming a GENERATED symbol —
#
#     linker could not find symbol …/gen/go/devalbo/ilc/events.wasmimport_Emit
#
# — which points at generated code rather than at the constraint that asked for
# it, so the diagnosis is a lot more expensive than the fix.
#
# That is not hypothetical: `caps_wasip2.go` was `//go:build tinygo`, and TinyGo
# compiles for microcontrollers as well as for wasm, so the WIT-import half of
# the seam was selected for a chip that has no host to import from. It is now
# `tinygo && wasm`, with `!(tinygo && wasm)` on the native half — exact
# complements, so no target selects both and none selects neither.
#
# It is a LINK, not a run. Nothing here needs hardware, an emulator, or a flash:
# resolving every symbol for a bare-metal target is the whole question.
#
# Its own script rather than an inline step because dlc-platform is a separate
# module and `tinygo` has no `-C`.
#
# Run inside `devbox shell` (needs tinygo).
set -uo pipefail
cd "$(dirname "$0")/../dlc-platform" || exit 2

if ! command -v tinygo >/dev/null 2>&1; then
	echo "  tinygo not found on PATH — run inside \`devbox shell\`" >&2
	exit 1
fi

# pico2 is RP2350: the badge's chip, and the tier this seam was broken for.
exec tinygo build -target=pico2 -o /dev/null ./cmd/baremetal-probe
