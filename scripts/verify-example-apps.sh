#!/usr/bin/env bash
#
# verify-example-apps.sh — the example apps still build and pass.
#
# WHY THIS EXISTS: example-apps/ are the only things that CONSUME the platform
# rather than being it. dlc validates the platform against itself, which is a
# friendly critic — the same design produced both sides. An app that merely
# depends on `engine/platform` is what notices when an exported helper changes
# shape, or when the template drifts from what apps actually use.
#
# The spikes did exactly this for the toolchain and rotted unnoticed for hours
# because nothing ran them. Same trap, so: run these.
#
# Native only. The browser tier is checked by test-b3, which already pays for a
# Chromium download.
set -uo pipefail
cd "$(dirname "$0")/.." || exit 2
REPO="$PWD"
if [ -t 1 ]; then G=$'\033[32m'; R=$'\033[31m'; Z=$'\033[0m'; else G=''; R=''; Z=''; fi

fail=0
echo "-------------------------------------------------"
echo "example apps: gen -> test -> build -> run"

# dlc must be current AND on PATH: the apps' `make gen` runs `dlc gen`, and
# templates are embedded, so a stale binary would check a stale shape.
BIN="$(mktemp -d)"
trap 'rm -rf "$BIN"' EXIT
go build -buildvcs=false -o "$BIN/dlc" ./hosts/native || { echo "building dlc failed"; exit 1; }

for app in example-apps/*/; do
	name="$(basename "$app")"
	[ -f "$app/dlc.toml" ] || continue
	printf "\n  %s\n" "$name"

	if ! ( cd "$app" && PATH="$BIN:$PATH" make gen >/dev/null 2>&1 ); then
		printf "  ${R}✗${Z} %s: make gen\n" "$name"; fail=$((fail+1)); continue
	fi
	if ! ( cd "$app" && go mod tidy >/dev/null 2>&1 ); then
		printf "  ${R}✗${Z} %s: go mod tidy\n" "$name"; fail=$((fail+1)); continue
	fi
	if ! ( cd "$app" && go test ./... ); then
		printf "  ${R}✗${Z} %s: go test\n" "$name"; fail=$((fail+1)); continue
	fi
	if ! ( cd "$app" && go build -o "$BIN/$name" ./hosts/native ); then
		printf "  ${R}✗${Z} %s: go build\n" "$name"; fail=$((fail+1)); continue
	fi
	# It has to actually answer, not merely link.
	if ! version="$( cd "$(mktemp -d)" && "$BIN/$name" version )"; then
		printf "  ${R}✗${Z} %s: does not run\n" "$name"; fail=$((fail+1)); continue
	fi
	printf "  ${G}✓${Z} %s — %s\n" "$name" "$version"
done

echo "-------------------------------------------------"
if [ "$fail" -eq 0 ]; then
	printf "${G}✓${Z} example apps green\n"
	exit 0
fi
printf "${R}✗${Z} %d example app(s) failing\n" "$fail"
exit 1
