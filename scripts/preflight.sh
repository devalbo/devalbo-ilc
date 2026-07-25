#!/usr/bin/env bash
#
# preflight.sh — assess prerequisites for the dlc bootstrap (CLI + Web tiers).
#
# Pure bash, ZERO toolchain dependency: it must run BEFORE Devbox provisions
# anything (it checks for Nix/Devbox themselves). Superseded later by `dlc doctor`.
#
# Usage:   ./scripts/preflight.sh            (or: make doctor)
# Docs:    docs/DEVALBO-DLC-PREREQUISITES.md
#
set -uo pipefail

# Colors only when stdout is a terminal (clean output under pipes / CI / make).
if [ -t 1 ]; then G=$'\033[32m'; R=$'\033[31m'; Z=$'\033[0m'; else G=''; R=''; Z=''; fi

miss_sys=0

chk() { # name  version-cmd  hint
  if command -v "$1" >/dev/null 2>&1; then
    printf "  %-18s ${G}✓${Z}  %s\n" "$1" "$(eval "$2" 2>&1 | head -1)"
    return 0
  else
    printf "  %-18s ${R}✗${Z}  %s\n" "$1" "$3"
    return 1
  fi
}

echo "OS: $(uname -s) $(uname -m)"
echo
echo "System prerequisites (install yourself):"
chk git    "git --version"    "install git"                                        || miss_sys=$((miss_sys+1))
chk nix    "nix --version"    "install: https://install.determinate.systems/"      || miss_sys=$((miss_sys+1))
chk devbox "devbox version"   "install: https://get.jetify.com/devbox"             || miss_sys=$((miss_sys+1))
chk direnv "direnv --version" "optional — auto-loads the devbox shell"             || true

# Are we inside a devbox shell? (devbox sets this env var)
in_devbox="${DEVBOX_SHELL_ENABLED:-}"

echo
if [ -n "$in_devbox" ]; then
  echo "Devbox-provisioned toolchain (inside devbox shell — these MUST be present):"
else
  echo "Devbox-provisioned toolchain (outside devbox shell — ✗ is EXPECTED; run 'devbox shell'):"
fi

miss_tool=0
for t in go tinygo wit-bindgen-go wasm-tools buf wasmtime node jco protoc-gen-go-lite protoc-gen-es-lite; do
  chk "$t" "$t --version" "provisioned by devbox.json" || miss_tool=$((miss_tool+1))
done

echo
echo "-------------------------------------------------------------------"
if [ "$miss_sys" -gt 0 ]; then
  echo "→ NOT READY: install the $miss_sys missing system prerequisite(s) above."
  exit 1
elif [ -z "$in_devbox" ]; then
  echo "→ System prereqs OK. Next: run 'devbox shell', then re-run this script."
  exit 0
elif [ "$miss_tool" -gt 0 ]; then
  echo "→ Inside devbox but $miss_tool tool(s) missing — fix devbox.json, not your machine."
  exit 1
else
  echo "→ READY: system prereqs + full toolchain present. 'make build-engine' to start."
  exit 0
fi
