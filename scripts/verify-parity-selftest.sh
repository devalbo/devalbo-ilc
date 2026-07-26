#!/usr/bin/env bash
#
# verify-parity-selftest.sh — prove the parity check can actually FAIL.
#
# A regression check that cannot fail is worse than no check: it reports green
# forever and nobody notices. This is the falsification test for
# verify-parity.sh — it injects a known native↔wasm divergence, asserts the
# parity check reports a MISMATCH on **both** boundaries, then removes the
# injection.
#
# The divergence is a `//go:build tinygo` file, so it changes engine behavior in
# the wasm build ONLY. That is precisely the failure class Decision 26 accepts
# risk on: native is the convenience path, wasm is the contract, and the danger
# is the two builds of one source disagreeing (a build tag, a TinyGo stdlib gap,
# map iteration order leaking into output).
#
# NOTE what this does NOT prove: parity says nothing about the engine being
# *correct*. Edit shared engine code and both builds change together and still
# agree — verify-parity.sh rebuilds both sides every run. Correctness is
# `go test ./engine/` and the golden FS snapshot.
#
# Run inside `devbox shell` (needs go, tinygo, node/jco). Slow: one extra
# tinygo build + jco transpile.
set -uo pipefail
cd "$(dirname "$0")/.." || exit 2
if [ -t 1 ]; then G=$'\033[32m'; R=$'\033[31m'; Z=$'\033[0m'; else G=''; R=''; Z=''; fi

PROBE=engine/zz_parity_drift_probe.go

# Never clobber a real file; a leftover probe means a previous run died.
if [ -e "$PROBE" ]; then
	printf "${R}✗${Z} %s already exists — a previous self-test left it behind. Delete it and re-run.\n" "$PROBE"
	exit 2
fi

cleanup() { rm -f "$PROBE"; }
trap cleanup EXIT INT TERM

cat > "$PROBE" <<'EOF'
//go:build tinygo

// TEMPORARY — written and deleted by scripts/verify-parity-selftest.sh.
// If you are reading this in a committed tree, a self-test run died; delete it.
// It changes engine behavior in the TinyGo/wasm build ONLY, which is the
// divergence the parity check exists to catch.
package engine

func init() { scaffoldTemplates[2].path = "READ.md" }
EOF

echo "-------------------------------------------------"
echo "parity self-test: injecting a tinygo-only divergence, expecting FAILURE"

out="$(./scripts/verify-parity.sh 2>&1)"
status=$?

cleanup
trap - EXIT INT TERM

fail=0
check() { # label  condition-already-evaluated
	if [ "$2" -eq 0 ]; then
		printf "  ${G}✓${Z} %s\n" "$1"
	else
		printf "  ${R}✗${Z} %s\n" "$1"; fail=$((fail+1))
	fi
}

# Exit status alone is not enough: a compile error also exits non-zero and would
# let a broken check masquerade as a working one. Require the actual mismatch
# report, on each boundary.
[ "$status" -ne 0 ]; check "parity check exited non-zero" $?
printf '%s' "$out" | grep -q "PARITY MISMATCH"; check "reported a PARITY MISMATCH" $?
printf '%s' "$out" | grep -q "wasm-parity \[argv\]"; check "ran the argv boundary" $?
printf '%s' "$out" | grep -q "wasm-parity \[method\]"; check "ran the method boundary" $?
[ "$(printf '%s' "$out" | grep -c "PARITY MISMATCH")" -eq 2 ]; check "both response streams caught it" $?
# The probe renames a scaffolded file, so the written trees must differ too —
# the FS-snapshot half of the check has to be load-bearing, not decorative.
[ "$(printf '%s' "$out" | grep -c "TREE MISMATCH")" -eq 2 ]; check "both filesystem trees caught it" $?
[ ! -e "$PROBE" ]; check "probe removed (tree restored)" $?

echo "-------------------------------------------------"
if [ "$fail" -eq 0 ]; then
	printf "${G}✓${Z} the parity check detects native↔wasm drift — it can fail, so its green means something\n"
	exit 0
fi
printf "${R}✗${Z} the parity check did NOT catch injected drift — verify-parity.sh is not protecting you\n"
echo "--- parity output was: ---"
printf '%s\n' "$out"
exit 1
