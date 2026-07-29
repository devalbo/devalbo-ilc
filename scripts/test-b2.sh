#!/usr/bin/env bash
#
# test-b2.sh — Phase B2 as standing regression: the engine's command boundary.
# REQUIRES the devbox toolchain (run inside `devbox shell` or via
# `devbox run make test-b2`). See docs/DEVALBO-DLC-TEST-STEPS.md Phase B2.
#
# Three layers, cheapest first — each catches something the others cannot:
#
#   correctness   go test ./engine/     does the engine do the right thing?
#   parity        verify-parity.sh      do the native and wasm builds agree?
#   meta          …-selftest.sh         can the parity check fail at all?
#   product       verify-scaffold.sh    does `dlc new` emit something that WORKS?
#
set -uo pipefail
cd "$(dirname "$0")/.." || exit 2
if [ -t 1 ]; then G=$'\033[32m'; R=$'\033[31m'; Z=$'\033[0m'; else G=''; R=''; Z=''; fi

fail=0
# Sanity check, not a tree audit. The self-test injects a `//go:build tinygo`
# probe; a survivor silently changes what later checks compile, on one tier only.
# One `ls` is enough to turn that into a named failure.
tree_clean() { [ -z "$(ls engine/zz_*.go 2>/dev/null)" ]; }

run_check() { # id  label  command...
	local id="$1" label="$2"; shift 2
	printf "%s — %s:\n" "$id" "$label"
	tree_clean || { printf "  ${R}✗${Z} stray engine/zz_*.go BEFORE %s — delete it\n" "$id"; exit 2; }
	if "$@"; then
		printf "  ${G}✓${Z} pass\n\n"
	else
		printf "  ${R}✗${Z} FAILED\n\n"; fail=$((fail+1))
	fi
	tree_clean || { printf "  ${R}✗${Z} %s left engine/zz_*.go behind\n" "$id"; exit 2; }
}

run_check "T-B2.0" "engine command registry + dispatch (unit)" go test ./engine/
run_check "T-B2.1" "native↔wasm parity (results + filesystem)" ./scripts/verify-parity.sh
run_check "T-B2.2" "parity self-test (the check can detect drift)" ./scripts/verify-parity-selftest.sh
run_check "T-B2.3" "scaffold builds and runs (dlc new -> gen -> build -> run)" ./scripts/verify-scaffold.sh
run_check "T-B2.4" "scaffold golden (the tree is exactly what we meant)" make -s verify-scaffold-golden
run_check "T-B2.5" "example apps build and pass (they consume the platform)" ./scripts/verify-example-apps.sh
run_check "T-B2.6" "platform gen is committed and current (consumers cannot run buf)" ./scripts/verify-platform-gen.sh

echo "-------------------------------------------------"
if [ "$fail" -eq 0 ]; then
	echo "→ B2 GREEN (engine boundary verified: correctness · parity · falsification)"
	exit 0
fi
echo "→ $fail check(s) failing (see above)"
exit 1
