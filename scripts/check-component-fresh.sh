#!/usr/bin/env bash
#
# check-component-fresh.sh — refuse to ship a guest built from older source.
#
# WHY THIS EXISTS. `make badge-cwasm` tested only that the component EXISTS:
#
#     @test -f example-apps/hello/build/engine.component.wasm || { echo "first: ..."; exit 1; }
#
# Existence is not freshness. The badge therefore packed whatever component was
# lying around, and because that input never changed, the packed payload hashed
# identically on every rebuild — which read as "nothing changed, no need to
# reflash" while the platform underneath it moved.
#
# The cost was a full debugging session. The badge ran a hello built before
# `SetStatus` and before the `fmt.Println` existed at all, so it printed nothing
# and emitted no status event, and every stage reported OK. QEMU ran the CURRENT
# guest and printed fine, so the two disagreed for a reason that looked like an
# encoder bug and was not. Hours went into field numbers and outlet enums; the
# answer was one stale artifact.
#
# STALENESS IS A BUILD ERROR, not a warning. A warning here scrolls past in the
# middle of a firmware build and gets flashed anyway — which is precisely the
# outcome this prevents.
#
# NOT A REBUILD, deliberately. Building the component needs TinyGo, `jco` and the
# `dlc` binary on PATH, and this script runs inside firmware targets that a
# cargo-only machine can otherwise complete. Failing with the exact command to
# run is honest about the dependency; silently invoking a toolchain that may not
# exist is not.
#
# Usage: scripts/check-component-fresh.sh <artifact> <source dir> [more dirs...]
set -euo pipefail

cd "$(dirname "$0")/.."

artifact=${1:?usage: check-component-fresh.sh <artifact> <source dir>...}
shift

if [ ! -f "$artifact" ]; then
	echo "  x $artifact does not exist" >&2
	echo "    build it first — see the target that calls this script" >&2
	exit 1
fi

# Every Go and proto file that ends up INSIDE the component. A newer one means
# the artifact predates code it should contain.
#
# `-newer` compares mtime, which is the same test `make` itself would apply — so
# this is the dependency edge the Makefile was missing, written out explicitly
# because the artifact is produced by a different toolchain in a different
# directory and cannot be a prerequisite.
# GENERATED TREES ARE EXCLUDED, and that is a judgement worth defending.
#
# `buf generate` rewrites every file under gen/ on every run, so their mtimes
# churn without their CONTENT changing — the first version of this check fired
# on `wasi/sockets/udp` bindings nobody had touched. A check that fails on
# correct behaviour trains people to ignore it, and an ignored freshness gate is
# worse than none: it is the same silent-stale outcome with an extra step.
#
# The signal lives in hand-written source anyway. Generated code only changes
# because a .proto changed, and .proto files ARE watched — so a real change is
# still caught, one step earlier and without the noise.
newer=$(find "$@" \
	\( -name '*.go' -o -name '*.proto' \) \
	-not -path '*/.devbox/*' \
	-not -path '*/build/*' \
	-not -path '*/gen/*' \
	-newer "$artifact" \
	-print 2>/dev/null | head -5)

if [ -n "$newer" ]; then
	echo "  x $artifact is STALE — these are newer:" >&2
	echo "$newer" | sed 's/^/       /' >&2
	echo "" >&2
	echo "    A stale guest runs the OLD app while every stage reports OK, which" >&2
	echo "    is the hardest kind of wrong to see. Rebuild it:" >&2
	echo "" >&2
	echo "        cd example-apps/hello && make build-web" >&2
	echo "" >&2
	exit 1
fi

echo "  component is current: $artifact"
