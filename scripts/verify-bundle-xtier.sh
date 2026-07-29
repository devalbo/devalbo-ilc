#!/usr/bin/env bash
#
# verify-bundle-xtier.sh — a BFT bundle exported in the BROWSER imports in the
# TERMINAL, and the resulting tree is byte-identical to what the CLI itself
# produces from the same inputs.
#
# This is the payoff of §7.3: state is portable across tiers because the bundle
# is one text blob and the engine that reads it is the same engine either way.
# The parity check proves the two builds agree; this proves their *artifacts*
# interchange.
set -uo pipefail
cd "$(dirname "$0")/.." || exit 2
if [ -t 1 ]; then G=$'\033[32m'; R=$'\033[31m'; Z=$'\033[0m'; else G=''; R=''; Z=''; fi

WORK="$(mktemp -d)"
BUNDLE="$WORK/from-browser.bft.json"

go build -buildvcs=false -o "$WORK/dlc" ./hosts/native || { echo "native build failed"; exit 1; }

# 1. Browser: scaffold + export, saving the bundle to disk.
( cd hosts/web \
  && npm install --silent --no-audit --no-fund \
  && npx playwright install chromium >/dev/null 2>&1 \
  && XTIER_OUT="$BUNDLE" npx playwright test xtier.spec.ts >/dev/null 2>&1 ) \
  || { echo "browser export failed"; exit 1; }

[ -s "$BUNDLE" ] || { echo "no bundle was produced"; exit 1; }

# 2. Terminal: import that bundle.
mkdir -p "$WORK/imported"
( cd "$WORK/imported" && "$WORK/dlc" import-fs "$BUNDLE" >/dev/null ) \
  || { echo "CLI import of the browser bundle failed"; exit 1; }

# 3. Terminal: build the same tree natively, from the same inputs.
mkdir -p "$WORK/native"
( cd "$WORK/native" && "$WORK/dlc" new --tiers native --tiers web --module github.com/acme/xtier xtier >/dev/null ) \
  || { echo "native scaffold failed"; exit 1; }

echo "-------------------------------------------------"
echo "bundle interchange: browser export -> CLI import ($(wc -c < "$BUNDLE" | tr -d ' ') bytes of BFT)"
if diff -r "$WORK/native" "$WORK/imported" >/dev/null 2>&1; then
	printf "  ${G}✓${Z} the browser's bundle rebuilds the CLI's tree exactly\n"
	rm -rf "$WORK"
	exit 0
fi
printf "  ${R}✗${Z} MISMATCH (< native scaffold  > imported from browser):\n"
diff -r "$WORK/native" "$WORK/imported" 2>&1 | sed 's/^/  /'
exit 1
