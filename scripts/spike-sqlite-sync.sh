#!/usr/bin/env bash
#
# Phase 0 gate for the SQLite index — can sqlite-wasm answer a query
# SYNCHRONOUSLY, in the worker where the engine runs, over OPFS?
# See docs/INDEX-PLAN.md D9 and spikes/sqlite-sync/README.md.
#
# Needs a Chromium from Playwright (the same one the web suites use).
#
set -uo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SPIKE="$ROOT/spikes/sqlite-sync"

echo "======== Phase 0 gate / synchronous sqlite over OPFS ========"
(
  set -e
  cd "$SPIKE"
  echo "node $(node -v)"
  npm install --silent --no-audit --no-fund
  npx playwright test
)
status=$?

if [ $status -eq 0 ]; then
  echo "GATE: 🟢 GREEN — a query returns rows with no await in the call path"
else
  echo "GATE: 🔴 RED — see spikes/sqlite-sync/README.md; do NOT reach for JSPI"
fi
exit $status
