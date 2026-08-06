#!/usr/bin/env bash
#
# verify-parity.sh — B2 wasm-parity check (Decision 26).
#
# The native in-process engine and the wasip2 component (cmd/engine-component)
# must produce identical results for a set of golden vectors. Native is the
# developer-convenience path; the wasm component is the contract. A mismatch
# means the two builds of the engine have diverged (TinyGo vs native Go) —
# investigate before trusting the tool.
#
# ONE boundary: `execute(method, request)` (Decision 28/31). The argv stream
# retired with the execute-cli shim — hosts parse and build requests now, so
# there is no second boundary to compare. The native side is cmd/parity-runner,
# which dispatches the same way `dlc` does.
#
# THREE COLUMNS, not two. Native and jco are both compiled-and-JITted; the badge
# runs an INTERPRETER, and until Pulley joined this check nothing outside the
# board exercised one. A Pulley-only divergence would otherwise surface on
# hardware over a UART with no way to bisect it. The Pulley column runs the same
# component through the same interpreter the badge uses (pulley64 here, pulley32
# there — same bytecode semantics, different pointer width).
#
# Run inside `devbox shell` (needs go, tinygo, node/jco, cargo).
set -uo pipefail
cd "$(dirname "$0")/.." || exit 2
if [ -t 1 ]; then G=$'\033[32m'; R=$'\033[31m'; Z=$'\033[0m'; else G=''; R=''; Z=''; fi

METHOD_VEC=verify/parity/method-vectors.json
BIN="$(mktemp -d)"

# 1. native builds — engine linked in-process
go build -buildvcs=false -o "$BIN/parity-runner" ./cmd/parity-runner || { echo "runner build failed"; exit 1; }

# 2. wasip2 component (wasip2-direct) + jco transpile
tinygo build -target=wasip2 --wit-package ./dlc-platform/wit --wit-world engine \
	-o engine.component.wasm ./cmd/engine-component || { echo "component build failed"; exit 1; }
( cd verify/parity \
	&& npm install --silent --no-audit --no-fund \
	&& npx jco transpile ../../engine.component.wasm -o out \
		--map 'devalbo:ilc/events=../events-sink.mjs' >/dev/null ) \
	|| { echo "transpile failed"; exit 1; }

# 3. run the SAME vectors through each side.
#
# Each stream gets its OWN empty directory as its filesystem root: commands like
# `new` write real files, so a shared or reused root would make vector N depend
# on whether vector N-1 already created the tree (and would litter the repo).
# Native binds the root by chdir'ing; the component binds it with a WASI preopen
# (§5.2 — the host binds the root, the engine just uses `os`).
REPO="$PWD"
fresh_root() { local d="$BIN/root-$1"; rm -rf "$d"; mkdir -p "$d"; printf '%s' "$d"; }

native_method() { ( cd "$(fresh_root native-method)" && "$BIN/parity-runner" "$REPO/$METHOD_VEC" ); }
component_stream() {
	( cd verify/parity && PARITY_ROOT="$(fresh_root "component-$1")" node harness.mjs "$1" "$REPO/$2" )
}
count() { python3 -c "import json,sys;print(len(json.load(open(sys.argv[1]))))" "$1"; }

echo "-------------------------------------------------"
status=0

# compare <label> <native stream> <component stream> <vector count>
compare() {
	local label="$1" native="$2" component="$3" n="$4"
	echo "wasm-parity [$label]: $n golden vectors (native vs wasip2 component)"
	if [ "$native" = "$component" ]; then
		printf "  ${G}✓${Z} native == component\n"
		return 0
	fi
	printf "  ${R}✗${Z} PARITY MISMATCH (< native  > component):\n"
	diff <(printf '%s\n' "$native") <(printf '%s\n' "$component") | sed 's/^/  /'
	return 1
}

# compare_pulley <label> <native stream> <pulley stream> <vector count>
#
# Diffed against NATIVE rather than against the jco column, deliberately. Native
# is the reference every other column is already measured against, so a Pulley
# failure reads as "Pulley disagrees with the engine" rather than as "two wasm
# runtimes disagree", which would leave open which one was wrong.
compare_pulley() {
	local label="$1" native="$2" pulley="$3" n="$4"
	echo "wasm-parity [$label]: $n golden vectors (native vs pulley64 interpreter)"
	if [ "$native" = "$pulley" ]; then
		printf "  ${G}✓${Z} native == pulley\n"
		return 0
	fi
	printf "  ${R}✗${Z} PARITY MISMATCH (< native  > pulley):\n"
	diff <(printf '%s\n' "$native") <(printf '%s\n' "$pulley") | sed 's/^/  /'
	return 1
}

# compare_trees <label> <native root> <component root>
#
# The golden FS snapshot (§11): commands like `new` write real files, so the
# trees the two engines produced must match byte-for-byte — filenames, nesting,
# and contents. This is a strictly stronger claim than the response streams,
# which only prove the two engines *said* the same thing. `diff -r` covers
# missing files, extra files, and differing contents in one shot.
compare_trees() {
	local label="$1" native="$2" component="$3"
	local n; n="$(find "$native" -type f | wc -l | tr -d ' ')"
	echo "wasm-parity [$label]: $n files written (native tree vs component tree)"
	if diff -r "$native" "$component" >/dev/null 2>&1; then
		printf "  ${G}✓${Z} trees identical\n"
		return 0
	fi
	printf "  ${R}✗${Z} TREE MISMATCH (< native  > component):\n"
	diff -r "$native" "$component" 2>&1 | sed 's/^/  /'
	return 1
}

compare "method" "$(native_method)" "$(component_stream method "$METHOD_VEC")" "$(count "$METHOD_VEC")" || status=1

# The streams above ran first, so the roots now hold whatever each engine wrote.
compare_trees "method fs" "$BIN/root-native-method" "$BIN/root-component-method" || status=1

# ---- the Pulley column (EMBEDDED-PLAN D2/DoD #4) -------------------------
#
# The component is already built above — the SAME file, not a rebuild. Rebuilding
# for this column would be the one thing that could make it pass while the badge
# fails.
if ! cargo build --quiet --manifest-path dlc-platform/embedded/parity/Cargo.toml; then
	printf "  ${R}✗${Z} could not build the pulley parity runner\n"
	status=1
else
	pulley_root="$(fresh_root pulley-method)"
	pulley_out="$(./dlc-platform/embedded/parity/target/debug/dlc-parity-pulley \
		"$REPO/engine.component.wasm" "$REPO/$METHOD_VEC" "$pulley_root" 2>"$BIN/pulley.err")"
	if [ -z "$pulley_out" ]; then
		printf "  ${R}✗${Z} the pulley runner produced nothing:\n"
		sed 's/^/  /' "$BIN/pulley.err"
		status=1
	else
		compare_pulley "method pulley" "$(native_method)" "$pulley_out" "$(count "$METHOD_VEC")" || status=1
		compare_trees "method fs pulley" "$BIN/root-native-method" "$pulley_root" || status=1
	fi
fi

if [ "$status" -eq 0 ]; then
	printf "${G}✓${Z} Decision 26 parity holds — same results AND same filesystem, across native, wasip2 and pulley\n"
else
	printf "${R}✗${Z} parity broken — see the diff above\n"
fi
exit "$status"
