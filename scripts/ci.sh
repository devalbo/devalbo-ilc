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

# Environment sanity: a leftover parity-drift probe is `//go:build tinygo` and
# empties the template FS, so native stays green while every wasm build ships
# nothing. Cheap to check, confusing to debug.
if ls engine/zz_*.go >/dev/null 2>&1; then
	printf "${R}✗ stray engine/zz_*.go — a self-test died and left it; wasm builds will embed no templates${Z}\n"
	ls engine/zz_*.go
	exit 2
fi

failed=()
LOGS="$(mktemp -d)"
trap 'rm -rf "$LOGS"' EXIT

# Each step streams as it runs AND is captured, so a failure can be re-printed at
# the very end. On a CI page the interesting output is thousands of lines up,
# which is how a B2 failure went unexamined while a B3 failure got all the
# attention — the summary is the only part anyone reads.
step() { # <label> <command...>
	local label="$1"; shift
	local log="$LOGS/$(printf '%s' "$label" | tr -c 'a-zA-Z0-9' '_')"
	printf "\n${B}▶ %s${Z}\n" "$label"
	if "$@" 2>&1 | tee "$log"; then
		printf "${G}✓ %s${Z}\n" "$label"
	else
		printf "${R}✗ %s${Z}\n" "$label"
		failed+=("$label")
	fi
}

# ---- fast: no wasm toolchain, no browser --------------------------------
step "repo structure (B0)"      run make test-b0
step "formatting"               run ./scripts/check-fmt.sh
# gen/ is gitignored — a fresh clone (CI) has no bindings until we produce them.
# B3/B2's wasm targets call `make gen` themselves; vet + unit tests do not, so
# without this step a clean tree fails with "no matching versions for …/gen/go/…".
step "codegen"                  run make gen
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

# Re-print the tail of every failed step, so the summary at the bottom of the log
# contains the actual errors rather than pointing at them.
for f in "${failed[@]}"; do
	log="$LOGS/$(printf '%s' "$f" | tr -c 'a-zA-Z0-9' '_')"
	printf "\n${R}--- last 40 lines of: %s ---${Z}\n" "$f"
	tail -40 "$log" 2>/dev/null || echo "(no output captured)"
done
exit 1
