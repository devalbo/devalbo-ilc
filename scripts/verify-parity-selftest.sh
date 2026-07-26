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
//
// It empties the embedded template FS in the TinyGo/wasm build ONLY, so the wasm
// engine scaffolds nothing while the native one scaffolds a full tree. That
// diverges the command results AND the written filesystems, which is exactly
// what the parity check exists to catch.
//
// Deliberately hooked to templates.FS rather than to any engine internal: it is
// an exported var that scaffolding cannot work without, so this probe stays
// valid as the engine changes. (An earlier probe poked a private slice that was
// later deleted — the self-test caught its own rot, which is the point.)
package engine

import (
	"embed"

	"github.com/devalbo/devalbo-ilc/templates"
)

func init() { templates.FS = embed.FS{} }
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
# HERESTRINGS, not pipes. `grep -q` exits on its first match, which closes the
# pipe under the writer; with `set -o pipefail` that SIGPIPE becomes the
# pipeline's exit status, so a SUCCESSFUL match reports failure. It only bites
# once the output is big enough for the writer to still be going — i.e. it looks
# fine until the suite grows, which is exactly how it got here.
[ "$status" -ne 0 ]; check "parity check exited non-zero" $?
grep -q "PARITY MISMATCH" <<<"$out"; check "reported a PARITY MISMATCH" $?
grep -q "wasm-parity \[method\]" <<<"$out"; check "ran the method boundary" $?
# The probe empties the template FS, so the written tree must differ too — the
# FS-snapshot half of the check has to be load-bearing, not decorative.
grep -q "TREE MISMATCH" <<<"$out"; check "the filesystem tree caught it" $?
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
