#!/usr/bin/env bash
#
# Spike 4 bake-off — build each parser variant under TinyGo wasip2, transpile,
# run the matrix harness, record size. Exit 0 if ≥1 lean variant (flag / ffcli /
# hand) is green (B1 gate). Bake-off rows may be red.
#
set -uo pipefail
cd "$(dirname "$0")/.." || exit 2

SPIKE=spikes/cli
WASM="$SPIKE/engine.component.wasm"     # wasip2 component (the current/gating target)
WASM1="$SPIKE/engine.core.wasm"         # wasip1 core (Portable/WAMR-shaped size probe)
RESULTS=()
lean_ok=0

run_variant() {
  local name="$1" tags="$2" lean="$3"
  local tag_args=()
  if [ -n "$tags" ]; then
    tag_args=(-tags "$tags")
  fi

  printf '\n== variant: %s' "$name"
  if [ -n "$tags" ]; then printf ' (-tags %s)' "$tags"; fi
  printf ' ==\n'

  local compile=no matrix=no size='—' size1='—'
  local build_log
  build_log=$(mktemp)
  if tinygo build -target=wasip2 "${tag_args[@]}" \
      --wit-package ./wit --wit-world engine \
      -o "$WASM" "./$SPIKE" >"$build_log" 2>&1; then
    compile=yes
    size=$(wc -c <"$WASM" | tr -d ' ')
    if ( cd "$SPIKE" && npx jco transpile engine.component.wasm -o out >/dev/null && node harness.mjs ); then
      matrix=yes
      if [ "$lean" = lean ]; then
        lean_ok=1
      fi
    else
      matrix=no
    fi
  else
    echo "  compile FAILED (wasip2):"
    sed 's/^/    /' "$build_log" | tail -n 40
  fi

  # Portable/WAMR-shaped size: the same package built as a wasip1 core module.
  # Embedded/WAMR is a deferred tier — this is a forward-looking size probe, not a
  # gate. wasip1 takes no WIT flags (it isn't a component).
  if tinygo build -target=wasip1 "${tag_args[@]}" \
      -o "$WASM1" "./$SPIKE" >"$build_log" 2>&1; then
    size1=$(wc -c <"$WASM1" | tr -d ' ')
  fi
  rm -f "$build_log"

  RESULTS+=("$name|$compile|$matrix|$size|$size1")
  printf '  → compile=%s matrix=%s wasip2=%s wasip1=%s\n' "$compile" "$matrix" "$size" "$size1"
}

cd "$SPIKE" && npm install --silent --no-audit --no-fund
cd - >/dev/null

run_variant flag   ""        lean
run_variant ffcli  cliffcli  lean
run_variant hand   clihand   lean
run_variant sub    clisub    heavy
run_variant cobra  clicobra  heavy
run_variant kong   clikong   heavy
run_variant goarg  cligoarg  heavy

echo
echo "-------------------------------------------------"
echo "Spike 4 bake-off summary"
printf '%-8s %-8s %-8s %-13s %s\n' "variant" "compile" "matrix" "wasip2 bytes" "wasip1 core"
printf '%-8s %-8s %-8s %-13s %s\n' "--------" "-------" "------" "------------" "-----------"
for row in "${RESULTS[@]}"; do
  IFS='|' read -r name compile matrix size size1 <<<"$row"
  printf '%-8s %-8s %-8s %-13s %s\n' "$name" "$compile" "$matrix" "$size" "$size1"
done
echo "-------------------------------------------------"

if [ "$lean_ok" -eq 1 ]; then
  echo "→ B1 gate GREEN (≥1 lean parser passed matrix)"
  exit 0
fi
echo "→ B1 gate RED (no lean parser passed)"
exit 1
