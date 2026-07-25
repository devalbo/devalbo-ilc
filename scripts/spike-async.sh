#!/usr/bin/env bash
#
# Spike 5 — dual-track async probe (Rich/CM + Portable/WAMR-shaped).
# See docs/WASI-UPGRADES.md and spikes/async/README.md.
#
set -uo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT" || exit 2
SPIKE="$ROOT/spikes/async"
fail=0

echo "======== Spike 5 / Rich/CM (jco + Promise host) ========"
(
  set -e
  cd "$SPIKE"
  npm install --silent --no-audit --no-fund
  tinygo build -target=wasip2 --wit-package "$ROOT/wit" --wit-world async-engine \
    -o "$SPIKE/engine.component.wasm" "$SPIKE"
  npx jco transpile engine.component.wasm -o out-sync \
    --map 'devalbo:ilc/host-delay=../host-delay.js'
  npx jco transpile engine.component.wasm -o out-jspi \
    --async-mode jspi \
    --async-imports 'devalbo:ilc/host-delay#delay' \
    --map 'devalbo:ilc/host-delay=../host-delay.js' \
    || echo "jspi transpile failed (recorded as gap)"
  node harness.mjs
) || fail=1

echo
echo "======== Spike 5 / Portable/WAMR-shaped (wasip1 + blocking host) ========"
(
  set -e
  tinygo build -target=wasip1 -o "$SPIKE/engine.core.wasm" "$SPIKE/portable"
  go run "$SPIKE/cmd/portable-host" "$SPIKE/engine.core.wasm" 50
) || fail=1

echo
echo "-------------------------------------------------"
if [ "$fail" -eq 0 ]; then
  echo "→ Spike 5 probes finished (Rich may be YELLOW; Portable should be GREEN)"
  exit 0
fi
echo "→ Spike 5 infrastructure failure"
exit 1
