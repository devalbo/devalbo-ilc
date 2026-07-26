#!/usr/bin/env bash
#
# ci.sh — the ONE entry point for continuous integration, whoever is running it.
#
#   ./scripts/ci.sh fast     structure + unit + fmt/vet          (~1 min, no wasm)
#   ./scripts/ci.sh full     fast + engine boundary + web tier   (the default)
#   ./scripts/ci.sh all      full + the B1 de-risking spikes     (slow; nightly)
#
# DELIBERATELY PROVIDER-AGNOSTIC. Nothing here — and nothing in the suites it
# calls — knows about GitHub, GitLab, Jenkins, or anything else. No provider env
# vars, no per-provider caching hooks, no annotations. A CI config is a thin
# adapter that installs devbox and runs one of the three lines above; switching
# providers means rewriting that adapter, not the tests.
#
# The corollary that matters more: **CI runs exactly what you run locally.**
# `./scripts/ci.sh full` on your laptop is the same command, in the same
# toolchain, producing the same result — so "passes locally, fails in CI" is a
# devbox problem, not a mystery.
#
# Toolchain: provisioned by devbox. If you are already inside `devbox shell`
# this runs commands directly; otherwise it wraps each one in `devbox run`.
set -uo pipefail
cd "$(dirname "$0")/.." || exit 2

TIER="${1:-full}"
if [ -t 1 ]; then G=$'\033[32m'; R=$'\033[31m'; B=$'\033[1m'; Z=$'\033[0m'; else G=''; R=''; B=''; Z=''; fi

# Inside devbox already? Then don't re-enter it for every step (slow, and it
# resets the working directory).
if command -v tinygo >/dev/null 2>&1 && command -v buf >/dev/null 2>&1; then
	run() { "$@"; }
else
	if ! command -v devbox >/dev/null 2>&1; then
		echo "ci: devbox not found — install it (https://www.jetify.com/docs/devbox/installing-devbox)" >&2
		echo "ci: or run inside a shell that already provides go, tinygo, buf, node" >&2
		exit 2
	fi
	run() { devbox run -- "$@"; }
fi

failed=()
step() { # <label> <command...>
	local label="$1"; shift
	printf "\n${B}▶ %s${Z}\n" "$label"
	if "$@"; then
		printf "${G}✓ %s${Z}\n" "$label"
	else
		printf "${R}✗ %s${Z}\n" "$label"
		failed+=("$label")
	fi
}

# ---- fast: no wasm toolchain, no browser --------------------------------
step "repo structure (B0)"      run make test-b0
step "formatting"               run ./scripts/check-fmt.sh
step "vet"                      run go vet ./engine/... ./cmd/... ./hosts/...
step "unit tests"               run go test ./engine/...

# ---- full: the engine boundary and the web tier -------------------------
if [ "$TIER" != "fast" ]; then
	# B2 covers unit + native↔wasm parity + the parity self-test + the scaffold
	# actually building and running. B3 is the browser tier.
	step "engine boundary (B2)"  run make test-b2
	step "web tier (B3)"         run make test-b3
fi

# ---- all: the de-risking spikes -----------------------------------------
if [ "$TIER" = "all" ]; then
	# Slow (several TinyGo builds + a headless browser) and rarely broken by
	# day-to-day work, so this belongs on a schedule rather than every push —
	# which is also what DEVALBO-DLC-TEST-STEPS.md recommends.
	step "de-risking spikes (B1)" run make test-b1
fi

echo
echo "-------------------------------------------------"
if [ ${#failed[@]} -eq 0 ]; then
	printf "${G}✓ ci (%s) green${Z}\n" "$TIER"
	exit 0
fi
printf "${R}✗ ci (%s): %d step(s) failed${Z}\n" "$TIER" "${#failed[@]}"
for f in "${failed[@]}"; do echo "    - $f"; done
exit 1
