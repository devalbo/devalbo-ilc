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
# missing once. Slower (a fresh wasmtime build per target): `ci.sh full` runs the
# lib half early and this half at the end, so a push that breaks a caller is red
# on that push rather than in the next morning's cron.
#
# `REQUIRE_BADGE_PAYLOAD=1` makes the two built-in-payload modes MANDATORY rather
# than skipped-if-absent. Set it wherever the coverage is claimed (`ci.sh all`)
# and leave it unset on a machine that has cargo but not the toolchain that
# produces `build/hello.pulley32.cwasm`.
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

try_test() { # try_test <label> <dir> <cargo args...>
	local label=$1 dir=$2
	shift 2
	echo "  $label"
	if ! (cd "$dir" && cargo test --quiet "$@"); then
		echo "  FAILED: $label" >&2
		rc=1
	fi
}

# --- the laptop profile ------------------------------------------------------
# `--all-targets` so the bins are built too, not just the library. They are
# `required-features = ["std"]`, so this is the only run that compiles them, and
# they are how a human actually drives this crate.
try "std profile, all targets (host)" . --all-targets

# THE TESTS, which this script did not run for its entire existence — it built
# and never tested, so the catalog format, the synthetic FAT volume, and the name
# rules shared with the Go side were all covered by tests nothing executed. A
# build proves it compiles; only this proves it is right.
try_test "embedded unit tests (host)" . --lib

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

	# EVERY FLASH-TIME MODE, not just the default one.
	#
	# `build.rs` turns BADGE_PAYLOAD / BADGE_REGION / BADGE_WORLD into `cfg`s, so
	# a mode nothing builds is a mode that ROTS — and one did: the built-in
	# payload path stopped compiling when `Payload` gained a field, and nothing
	# noticed because the default build cfg's that code out. Found by hand, which
	# is the wrong way to find it.
	#
	# A real .cwasm is needed for the built-in modes; if it is not built yet, skip
	# those rather than failing, because `make badge-cwasm` needs tinygo and this
	# script is also run in environments that only have cargo.
	try "badge firmware: empty loader (default)" rp2350 --release

	# `../..` — this script cd'd to dlc-platform/embedded, so the repo root is two
	# levels up. It said `../../..` until 2026-08-15, which resolved OUTSIDE the
	# repo, so the file was never there and these two modes were skipped on every
	# machine and every CI run since they were written. The skip is deliberate and
	# the message is printed, which is the only reason it was ever noticeable — a
	# silent one would have been invisible forever.
	payload="$PWD/../../build/hello.pulley32.cwasm"
	if [ -f "$payload" ]; then
		BADGE_PAYLOAD="$payload" \
			try "badge firmware: built-in payload + region" rp2350 --release
		BADGE_PAYLOAD="$payload" BADGE_REGION=off \
			try "badge firmware: built-in only, no region" rp2350 --release
	elif [ -n "${REQUIRE_BADGE_PAYLOAD:-}" ]; then
		# A SKIP IS NOT A PASS, WHEREVER COVERAGE IS CLAIMED. The path bug above
		# survived because skipping was always acceptable — so the tier that says
		# it builds every flash-time mode now refuses to build four of them and
		# call it green. `ci.sh` sets this; a cargo-only machine does not, and
		# still gets the skip below.
		echo "  FAILED: built-in payload modes — no $payload (run \`make badge-cwasm\`)" >&2
		rc=1
	else
		echo "  (skipping built-in payload modes — run \`make badge-cwasm\` first)"
	fi

	# THE BRING-UP PROBES. Not part of any shipped image — a separate firmware
	# for finding out whether a peripheral works, and why not. It is here because
	# a probe that stops compiling is discovered on the day something breaks and
	# you reach for it, which is the worst possible moment. Cheap to keep green:
	# it has no Wasmtime, so it builds in seconds.
	try "device bring-up probe" display-probe --release

	BADGE_WORLD=minimal try "badge firmware: minimal world" rp2350 --release
	BADGE_BEAT_MS=700 try "badge firmware: watchable bring-up" rp2350 --release

	# BADGE_SCREEN, which decides how much of the panel the app gets and is
	# therefore the input to the `ILC_COLS`/`ILC_ROWS` the app formats against.
	# `full` is the mode the default build never compiles: it changes a `match`
	# on a cfg-selected const and const-asserts the result is a usable number, so
	# a wrong band size is a BUILD error here and an unreadable screen otherwise.
	BADGE_SCREEN=full try "badge firmware: full-screen app" rp2350 --release
	# The combination, because the two knobs meet in one const expression and
	# each alone leaves the other branch unbuilt.
	BADGE_SCREEN=full BADGE_WORLD=minimal \
		try "badge firmware: minimal world, full screen" rp2350 --release

	# BADGE_INPUT, which decides whether the world can collect text at all
	# (WORLD-INPUT-PLAN D3a). `off` compiles the keyboard OUT, so it is a
	# different program rather than the same one with a flag — exactly the shape
	# that rots when nothing builds it. The default build covers `keyboard`.
	BADGE_INPUT=off try "badge firmware: no input surface" rp2350 --release
fi

exit $rc
