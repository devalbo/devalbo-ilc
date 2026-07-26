#!/usr/bin/env bash
#
# Spike — go-lite + custom options (Decision 29 registry gate).
# 1) TinyGo wasip2 guest round-trips messages whose .proto carries options.
# 2) Host reads method_id / field options from a buf FileDescriptorSet.
#
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT" || exit 2
SPIKE="$ROOT/spikes/options"
fail=0

echo "======== Spike options / guest (go-lite + TinyGo wasip2) ========"
(
  set -e
  cd "$SPIKE"
  npm install --silent --no-audit --no-fund
  tinygo build -target=wasip2 --wit-package "$ROOT/wit" --wit-world engine \
    -o "$SPIKE/engine.component.wasm" "$SPIKE"
  npx jco transpile engine.component.wasm -o out
  node harness.mjs
) || fail=1

echo
echo "======== Spike options / host (FileDescriptorSet + protoreflect) ========"
(
  set -e
  IMAGE="$SPIKE/image.bin"
  (cd "$ROOT/proto" && buf build -o "$IMAGE")
  go run "$SPIKE/cmd/host-introspect" "$IMAGE"
) || fail=1

echo
echo "======== Pass criteria (C1–C3) ========"
if ! "$ROOT/scripts/check-options-criteria.sh"; then
  fail=1
fi

echo
echo "-------------------------------------------------"
if [ "$fail" -eq 0 ]; then
  echo "→ Spike options GREEN (guest + host + C1–C3)"
  exit 0
fi
echo "→ Spike options RED"
exit 1
