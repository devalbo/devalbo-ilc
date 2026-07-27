#!/usr/bin/env bash
#
# test-b1.sh — the surviving B1 spike.
#
# Spikes 1–4, oneof, and options were RETIRED once product code covered their
# claims: the real engine builds to wasip2 and runs under jco (B2/B3), the real
# proto pipeline exercises go-lite ↔ es-lite every build, B3 asserts OPFS
# persistence with the real engine, and protoc-gen-dlc-registry walks the
# descriptor/dynamicpb path on every generate. Spike 4's premise died outright
# with Decision 22. Keeping them meant maintaining a WIT world for a boundary we
# had deliberately deleted.
#
# Their FINDINGS live on in spikes/README.md — that is the part that mattered.
#
# Spike 5 stays because nothing else covers it: there is no async capability
# yet, so this is the only evidence for how one would work.
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

run_spike "T-B1.5" "async ecosystem probe (Rich/CM JSPI + Portable)" spike-async

echo "-------------------------------------------------"
if [ "$fail" -eq 0 ]; then
  echo "→ B1 GREEN (all implemented spikes passing)"
  exit 0
else
  echo "→ $fail spike(s) failing (see above)"
  exit 1
fi
