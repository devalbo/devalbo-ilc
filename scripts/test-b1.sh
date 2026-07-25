#!/usr/bin/env bash
#
# test-b1.sh — Phase B1 spikes as standing regression. REQUIRES the devbox
# toolchain (run inside `devbox shell` or via `devbox run make test-b1`).
# Each spike is a minimal proof of one load-bearing assumption; see
# docs/DEVALBO-DLC-TEST-STEPS.md Phase B1.
#
set -uo pipefail
cd "$(dirname "$0")/.." || exit 2
if [ -t 1 ]; then G=$'\033[32m'; R=$'\033[31m'; Z=$'\033[0m'; else G=''; R=''; Z=''; fi

fail=0
run_spike() { # id  label  make-target
  printf "%s — %s:\n" "$1" "$2"
  if make -s "$3"; then
    printf "  ${G}✓${Z} %s\n\n" "$3"
  else
    printf "  ${R}✗${Z} %s failed\n\n" "$3"; fail=$((fail+1))
  fi
}

run_spike "T-B1.1" "component round-trip (TinyGo → wasip2 → component → jco)" spike-component
run_spike "T-B1.2" "protobuf-go-lite ↔ es-lite under wasip2" spike-proto

echo "-------------------------------------------------"
if [ "$fail" -eq 0 ]; then
  echo "→ B1 GREEN (implemented spikes passing)"
  exit 0
else
  echo "→ $fail spike(s) failing (see above)"
  exit 1
fi
