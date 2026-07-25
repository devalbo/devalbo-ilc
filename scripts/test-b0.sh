#!/usr/bin/env bash
#
# test-b0.sh — Phase B0 repo-integrity checks. Runs WITHOUT the toolchain
# (no Go/TinyGo/buf needed). Prerequisites are a separate concern: `make doctor`.
# Steps documented in docs/DEVALBO-DLC-TEST-STEPS.md (T-B0.*).
#
set -uo pipefail
cd "$(dirname "$0")/.." || exit 2
if [ -t 1 ]; then G=$'\033[32m'; R=$'\033[31m'; Z=$'\033[0m'; else G=''; R=''; Z=''; fi

fail=0
pass() { printf "  ${G}✓${Z} %s\n" "$1"; }
bad()  { printf "  ${R}✗${Z} %s\n" "$1"; fail=$((fail+1)); }
exists() { [ -e "$1" ] && pass "$1" || bad "$1 (missing)"; }
absent() { [ ! -e "$1" ] && pass "$1 removed" || bad "$1 still present — run the tri-language removal"; }

echo "T-B0.4 — structure present:"
for d in engine hosts/native hosts/web wit proto frontend templates spikes scripts docs; do exists "$d"; done

echo; echo "T-B0.4 — core files present:"
for f in go.mod devbox.json Makefile .gitignore scripts/preflight.sh \
         wit/ilc.wit proto/devalbo/ilc/v1/common.proto proto/devalbo/dlc/v1/dlc.proto \
         proto/buf.yaml proto/buf.gen.yaml; do exists "$f"; done

echo; echo "boundary READMEs present:"
for r in engine hosts wit proto templates frontend spikes scripts; do exists "$r/README.md"; done

echo; echo ".gitignore behaves:"
for p in gen/x engine/x.wasm node_modules/x .devbox/x; do
  git check-ignore -q "$p" && pass "ignored: $p" || bad "NOT ignored: $p"
done

echo; echo "T-B0.3 — clean migration (fails until the removal is run):"
for x in compiler packages Cargo.toml Cargo.lock wit/environment.wit wit/console-io.wit; do absent "$x"; done
git rev-parse --verify -q phase1-tri-language >/dev/null 2>&1 \
  && pass "tag: phase1-tri-language" || bad "tag phase1-tri-language missing"

echo; echo "-------------------------------------------------"
if [ "$fail" -eq 0 ]; then
  echo "→ B0 GREEN"
  exit 0
else
  echo "→ $fail check(s) failing (see above). Structure/config green + migration red is EXPECTED"
  echo "  until you run the tri-language removal; then B0 goes green."
  exit 1
fi
