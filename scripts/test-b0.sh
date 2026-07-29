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
for d in engine hosts/native hosts/web dlc-platform dlc-platform/web dlc-platform/wit proto templates spikes scripts docs; do exists "$d"; done

echo; echo "T-B0.4 — core files present:"
for f in go.mod devbox.json Makefile .gitignore AGENTS.md scripts/preflight.sh \
         dlc-platform/wit/ilc.wit dlc-platform/proto/devalbo/ilc/v1/common.proto proto/devalbo/dlc/v1/commands.proto \
         buf.yaml buf.gen.yaml buf.gen.platform.yaml dlc-platform/go.mod; do exists "$f"; done

echo; echo "boundary READMEs present:"
for r in engine hosts dlc-platform/wit proto templates spikes scripts; do exists "$r/README.md"; done

echo; echo ".gitignore behaves:"
for p in gen/x engine/x.wasm node_modules/x .devbox/x; do
  git check-ignore -q "$p" && pass "ignored: $p" || bad "NOT ignored: $p"
done

echo; echo "T-B0.3 — clean migration (retired tri-language paths stay gone):"
for x in compiler packages Cargo.toml Cargo.lock dlc-platform/wit/environment.wit dlc-platform/wit/console-io.wit; do absent "$x"; done
# The phase1-tri-language tag is a local/history checkpoint only — not a CI gate.
# Requiring it on every clone forced fetch-tags / a pushed tag for no ongoing value;
# the absent-path checks above are what keep the migration honest.

echo; echo "-------------------------------------------------"
if [ "$fail" -eq 0 ]; then
  echo "→ B0 GREEN"
  exit 0
else
  echo "→ $fail check(s) failing (see above). Structure/config green + migration red is EXPECTED"
  echo "  until you run the tri-language removal; then B0 goes green."
  exit 1
fi
