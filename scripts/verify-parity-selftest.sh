#!/usr/bin/env bash
#
# verify-parity-selftest.sh — prove the parity check can actually FAIL.
#
# A regression check that cannot fail is worse than no check: it reports green
# forever and nobody notices. This is the falsification test for
# verify-parity.sh — it injects a known native↔wasm divergence, asserts the
# parity check reports a MISMATCH on every column that should see it, then
# removes the injection.
#
# EVERY COLUMN, checked by name. Both probes perturb the COMPONENT, and since
# Pulley runs that same component all three columns should react. A check that
# only grepped for "PARITY MISMATCH" would stay green if the Pulley column
# silently stopped comparing anything — which is exactly how a third column
# rots.
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

# Two probes, because parity now compares THREE things (results, filesystem,
# events) and each dimension has to be shown to fail on its own. A probe that
# breaks all three at once proves only that *something* is watching.
#
#   templates  breaks results + filesystem, leaves events untouched
#   events     breaks ONLY the event stream — results and trees stay identical,
#              so if it is caught, the event comparison is what caught it
write_probe() { # <name>
	case "$1" in
	templates)
		cat > "$PROBE" <<'EOF'
//go:build tinygo

// TEMPORARY — written and deleted by scripts/verify-parity-selftest.sh.
// If you are reading this in a committed tree, a self-test run died; delete it.
//
// It empties the embedded template FS in the TinyGo/wasm build ONLY, so the wasm
// engine scaffolds nothing while the native one scaffolds a full tree. That
// diverges the command results AND the written filesystems.
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
		;;
	events)
		cat > "$PROBE" <<'EOF'
//go:build tinygo

// TEMPORARY — written and deleted by scripts/verify-parity-selftest.sh.
//
// Emits one extra event in the wasm build ONLY. Nothing else changes: the same
// commands run, the same responses come back, the same files get written. The
// ONLY observable difference is one line in the event stream — so if the parity
// check goes red here, the event comparison is the thing that caught it, and the
// third dimension is load-bearing rather than decorative.
package engine

import "github.com/devalbo/devalbo-ilc/dlc-platform"

func init() { platform.Emit("probe.tinygo-only", []byte("drift")) }
EOF
		;;
	esac
}

fail=0
check() { # label  condition-already-evaluated
	if [ "$2" -eq 0 ]; then
		printf "  ${G}✓${Z} %s\n" "$1"
	else
		printf "  ${R}✗${Z} %s\n" "$1"; fail=$((fail+1))
	fi
}

OUT="$(mktemp)"
trap 'cleanup; rm -f "$OUT"' EXIT INT TERM

# run_scenario <probe name> <headline>
#
# Output goes to a FILE, never a shell variable: the report contains whole
# scaffold trees as base64, and holding that in a variable pushed every later
# exec past Linux ARG_MAX — `rm` and `grep` died, the failing `rm` was this
# script's own probe cleanup, and the surviving probe emptied the template FS for
# every wasm build that followed. One variable, two broken suites.
#
# Grep the FILE, not a pipe and not a herestring: a pipe makes `grep -q`'s early
# exit SIGPIPE the writer, which `set -o pipefail` turns into a failure on a
# SUCCESSFUL match. Both traps only bite once the output grows.
run_scenario() {
	local probe="$1" headline="$2"
	echo "-------------------------------------------------"
	echo "$headline"

	write_probe "$probe"
	./scripts/verify-parity.sh >"$OUT" 2>&1
	local status=$?

	# Restore the tree BEFORE judging, and stop dead if that failed — leaving the
	# probe behind poisons every wasm build that follows, here and in later
	# suites. That is worse than any verdict this script could report.
	if ! cleanup; then
		printf "${R}✗${Z} could not remove %s — it MUST go before anything else builds\n" "$PROBE"
		printf "  run:  rm %s\n" "$PROBE"
		exit 2
	fi

	# Exit status alone is not enough: a compile error also exits non-zero and
	# would let a broken check masquerade as a working one. Require the actual
	# mismatch report.
	[ "$status" -ne 0 ]; check "parity check exited non-zero" $?
	grep -q "PARITY MISMATCH" "$OUT"; check "reported a PARITY MISMATCH" $?
	grep -q "wasm-parity \[method\]" "$OUT"; check "ran the method boundary" $?

	# THE PULLEY COLUMN SPECIFICALLY, not just "some column went red". Both probes
	# are `//go:build tinygo` files, so they change the COMPONENT — and Pulley runs
	# that same component, which means a generic "PARITY MISMATCH" above could be
	# entirely the jco column while Pulley silently agreed with nobody. Asserting
	# the pulley diff by name is what makes the third column load-bearing rather
	# than decorative; it is the same argument the events dimension makes below.
	grep -q "wasm-parity \[method pulley\]" "$OUT"; check "ran the pulley interpreter" $?
	grep -q "MISMATCH (< native  > pulley)" "$OUT"; check "the pulley column caught it too" $?

	case "$probe" in
	templates)
		# The probe empties the template FS, so the written tree must differ too —
		# the FS-snapshot half has to be load-bearing, not decorative.
		grep -q "TREE MISMATCH" "$OUT"; check "the filesystem tree caught it" $?
		;;
	events)
		# The precise claim: the EVENT stream diverged and nothing else did. If a
		# tree mismatch showed up here the probe is doing more than it says, and
		# this scenario would no longer isolate the event dimension.
		grep -q "EVENT" "$OUT"; check "the event stream caught it" $?
		! grep -q "TREE MISMATCH" "$OUT"; check "and ONLY the event stream (trees identical)" $?
		;;
	esac
	[ ! -e "$PROBE" ]; check "probe removed (tree restored)" $?

	if [ "$fail" -ne 0 ]; then
		echo "--- full parity output was: ---"
		cat "$OUT"
	fi
}

run_scenario templates "parity self-test 1/2: emptying the template FS in wasm — expecting results + tree to diverge"
run_scenario events    "parity self-test 2/2: one extra event in wasm — expecting ONLY the event stream to diverge"

echo "-------------------------------------------------"
if [ "$fail" -eq 0 ]; then
	printf "${G}✓${Z} parity detects native↔wasm drift on all three dimensions — results, filesystem, events\n"
	exit 0
fi
printf "${R}✗${Z} the parity check did NOT catch injected drift — verify-parity.sh is not protecting you\n"
if [ -e "$PROBE" ]; then
	printf "${R}✗${Z} %s SURVIVED — delete it before building anything else\n" "$PROBE"
fi
exit 1
