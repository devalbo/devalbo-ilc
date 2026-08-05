#!/usr/bin/env bash
#
# Spike 5 — async probe: does stock jco reach a Promise host import?
# See docs/WASI-UPGRADES.md and spikes/async/README.md.
#
# Rich/JSPI needs Node ≥24 with --experimental-wasm-jspi (devbox pins nodejs@24).
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
  echo "node $(node -v)"
  npm install --silent --no-audit --no-fund
  tinygo build -target=wasip2 --wit-package "$ROOT/dlc-platform/wit" --wit-world async-engine \
    -o "$SPIKE/engine.component.wasm" "$SPIKE"
  npx jco transpile engine.component.wasm -o out-sync \
    --map 'devalbo:ilc/host-delay=../host-delay.js'
  # Import + export must both be async for JSPI: sync execute-cli cannot suspend
  # into a Suspending import ("trying to suspend without WebAssembly.promising").
  npx jco transpile engine.component.wasm -o out-jspi \
    --async-mode jspi \
    --async-imports 'devalbo:ilc/host-delay#delay' \
    --async-exports 'execute-cli' \
    --map 'devalbo:ilc/host-delay=../host-delay.js'
  node --experimental-wasm-jspi harness.mjs
) || fail=1

echo
echo "-------------------------------------------------"
if [ "$fail" -eq 0 ]; then
  echo "→ Spike 5 finished (JSPI should be GREEN on Node ≥24)"
  exit 0
fi
echo "→ Spike 5 infrastructure failure"
exit 1
