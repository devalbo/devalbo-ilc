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

# Cleanup must SUCCEED, not merely be attempted. `rm` itself failed once (ARG_MAX
# blown by a huge captured variable), the probe survived, and every later tinygo
# build in that CI run silently embedded no templates. A cleanup you do not
# verify is a cleanup you do not have.
cleanup() {
	rm -f "$PROBE" 2>/dev/null || true
	[ -e "$PROBE" ] || return 0
	# Second attempt, with nothing else in flight to blow the arg limit.
	rm -f "$PROBE" 2>/dev/null || true
	[ ! -e "$PROBE" ]
}
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

# Output goes to a FILE, never into a shell variable.
#
# The parity report contains whole scaffold trees as base64, which is hundreds of
# kilobytes. Holding that in a variable pushed every subsequent `exec` past
# Linux's ARG_MAX, so `rm` and `grep` both died with "Argument list too long" —
# and because the failing `rm` was the probe cleanup, the tinygo-only probe
# SURVIVED and silently emptied the template FS for every later wasm build in the
# same CI run. One unquotable variable took out two suites.
OUT="$(mktemp)"
trap 'cleanup; rm -f "$OUT"' EXIT INT TERM

./scripts/verify-parity.sh >"$OUT" 2>&1
status=$?

# Restore the tree BEFORE judging the result, and stop dead if that failed —
# leaving the probe behind would poison every wasm build that follows, in this
# suite and in the next one. That is worse than any verdict this script reports.
if ! cleanup; then
	printf "${R}✗${Z} could not remove %s — it MUST go before anything else builds\n" "$PROBE"
	printf "  it is //go:build tinygo and empties the template FS, so wasm builds ship no templates\n"
	printf "  run:  rm %s\n" "$PROBE"
	exit 2
fi

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
# Grep a FILE, not a pipe and not a herestring. A pipe would make `grep -q`'s
# early exit SIGPIPE the writer, which `set -o pipefail` turns into a failure on
# a SUCCESSFUL match; a variable would blow ARG_MAX. Both bite only once the
# output grows, which is how each of them reached CI unnoticed.
[ "$status" -ne 0 ]; check "parity check exited non-zero" $?
grep -q "PARITY MISMATCH" "$OUT"; check "reported a PARITY MISMATCH" $?
grep -q "wasm-parity \[method\]" "$OUT"; check "ran the method boundary" $?
# The probe empties the template FS, so the written tree must differ too — the
# FS-snapshot half of the check has to be load-bearing, not decorative.
grep -q "TREE MISMATCH" "$OUT"; check "the filesystem tree caught it" $?
[ ! -e "$PROBE" ]; check "probe removed (tree restored)" $?

echo "-------------------------------------------------"
if [ "$fail" -eq 0 ]; then
	printf "${G}✓${Z} the parity check detects native↔wasm drift — it can fail, so its green means something\n"
	exit 0
fi
printf "${R}✗${Z} the parity check did NOT catch injected drift — verify-parity.sh is not protecting you\n"

# A surviving probe poisons every later tinygo build in this run (it empties the
# template FS), so it is not just this check failing — it is a booby trap. Say so
# unmistakably and stop.
if [ -e "$PROBE" ]; then
	printf "${R}✗${Z} %s SURVIVED — delete it before building anything else; wasm builds will embed no templates\n" "$PROBE"
fi

# The WHOLE report, not a tail. This is the one place the parity output exists —
# it is not streamed (an expected-to-fail run would be pure noise on a green
# suite), so truncating it here means the evidence is simply gone. It is large;
# that is the correct trade when a check that guards every other check is broken.
echo "--- full parity output was: ---"
cat "$OUT"
exit 1
