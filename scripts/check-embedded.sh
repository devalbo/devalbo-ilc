#!/usr/bin/env bash
#
# check-embedded.sh — does the RUST inherited runtime build EVERY WAY IT IS
# CONSUMED?
#
# SIBLING, and the names do not distinguish them: `check-baremetal.sh` asks the
# same question of the GO capability seam under TinyGo. This one is
# `dlc-platform-embedded` under rustup. Different toolchains entirely, which is
# why they are two scripts and not one.
#
# WHY THIS EXISTS. `dlc-platform-embedded` has two profiles:
#
#   default (`std`)          the laptop tools — minimal-host, pulley-probe,
#                            run-component — and `host.rs`, which needs
#                            wasmtime-wasi and Cranelift.
#   --no-default-features    what the badge and the QEMU firmware link: no OS,
#                            no compiler, `minimal.rs` as the only host.
#
# NEITHER WAS EXERCISED BY CI. The parity runner is its own crate and does not
# depend on this one, so nothing else here builds either profile — a `use std::`
# in a portable module, or a break in `host.rs`, would compile locally, pass every
# test, keep the parity column green, and surface only when somebody
# cross-compiled for a microcontroller.
#
# That is the SAME failure mode as `check-baremetal.sh`, one language over: a
# build configuration nothing routinely builds rots silently. There the trigger
# was a Go build constraint selecting a wasm-only file for a board; here it is a
# `std` import in a crate that must not have one. Both are cheap to check and
# expensive to diagnose, because neither surfaces where it was introduced.
#
# It is a BUILD, not a run — no emulator, no hardware, no flashing. `make qemu`
# is the one that actually executes something.
#
# TWO no_std TARGETS, and the pair is the point (EMBEDDED-PLAN D8). thumbv8m is
# the badge in ARM mode; riscv32imac is the badge's Hazard3 cores AND every
# RISC-V ESP32. Building both is what keeps "one Rust target family for badge and
# ESP" a check rather than a hope — a regression that only broke RISC-V would
# otherwise wait for the ESP32 phase to be discovered.
#
# WITH `--firmware`, also builds the two crates that CONSUME the library. A lib
# that compiles is not the same as a firmware that links: an API change keeps the
# lib green and breaks its callers, which is exactly how `PulleyWidth` went
# missing once. Slower (a fresh wasmtime build per target), so `ci.sh full` runs
# the fast half and `ci.sh all` adds this.
#
# Its own script rather than an inline step because dlc-platform/embedded is a
# separate crate tree with its own locks, outside the Go module's workspace.
#
# Run inside `devbox shell` (needs rustup; targets come from rust-toolchain.toml).
set -uo pipefail
cd "$(dirname "$0")/../dlc-platform/embedded" || exit 2

if ! command -v cargo >/dev/null 2>&1; then
	echo "  cargo not found on PATH — run inside \`devbox shell\`" >&2
	exit 1
fi

want_firmware=0
[ "${1:-}" = "--firmware" ] && want_firmware=1

rc=0
try() { # try <label> <dir> <cargo args...>
	local label=$1 dir=$2
	shift 2
	echo "  $label"
	if ! (cd "$dir" && cargo build --quiet "$@"); then
		echo "  FAILED: $label" >&2
		rc=1
	fi
}

# --- the laptop profile ------------------------------------------------------
# `--all-targets` so the bins are built too, not just the library. They are
# `required-features = ["std"]`, so this is the only run that compiles them, and
# they are how a human actually drives this crate.
try "std profile, all targets (host)" . --all-targets

# --- the board profile -------------------------------------------------------
# `--lib`, because every bin here takes argv and reads files. The library is the
# part a board links.
for target in thumbv8m.main-none-eabihf riscv32imac-unknown-none-elf; do
	try "no_std lib for $target" . --lib --no-default-features --target "$target"
done

# --- the consumers ------------------------------------------------------------
if [ "$want_firmware" = 1 ]; then
	# Each carries its own .cargo/config.toml naming its target, so no --target
	# here — passing one would override the crate's own choice.
	try "qemu harness firmware (thumbv7m)" qemu-armv7m --release
	try "badge bring-up firmware (thumbv8m)" rp2350 --release
fi

exit $rc
