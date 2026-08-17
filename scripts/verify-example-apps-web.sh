#!/usr/bin/env bash
#
# verify-example-apps-web.sh — the example apps run in a browser.
#
# Each app ships its own browser test (hosts/web/test/). This builds the web tier
# and runs that test, so the app's OWN check is what verifies it — the same
# thing a user would run, rather than a parallel harness that could drift.
#
# For notes specifically this is also the only coverage of the platform's
# per-file API (ReadFile/ListDir/RemoveFile) under wasm: dlc itself does not use
# those, so without this they are native-only tested.
set -uo pipefail
cd "$(dirname "$0")/.." || exit 2
if [ -t 1 ]; then G=$'\033[32m'; R=$'\033[31m'; Z=$'\033[0m'; else G=''; R=''; Z=''; fi

BIN="$(mktemp -d)"
trap 'rm -rf "$BIN"' EXIT
go build -buildvcs=false -o "$BIN/dlc" ./hosts/native || { echo "building dlc failed"; exit 1; }

fail=0
echo "-------------------------------------------------"
echo "example apps (web): build-web -> the app's own browser test"

for app in example-apps/*/; do
	name="$(basename "$app")"
	[ -d "$app/hosts/web/test" ] || continue
	printf "\n  %s\n" "$name"

	# Stream the build. A SKIPPED compiler error is how this script reported
	# "notes: make build-web" with no reason on 2026-08-17 — notes referenced
	# AppServiceCLI, the generator emits NotesServiceCLI, and the mismatch was
	# thrown away. The native suite already prints go test; this one had been
	# the only remaining /dev/null on a compile.
	if ! ( cd "$app" && PATH="$BIN:$PATH" make build-web ); then
		printf "  ${R}✗${Z} %s: make build-web\n" "$name"; fail=$((fail+1)); continue
	fi
	if ! ( cd "$app/hosts/web" && npm install --silent --no-audit --no-fund >/dev/null 2>&1 ); then
		printf "  ${R}✗${Z} %s: npm install\n" "$name"; fail=$((fail+1)); continue
	fi
	if ! ( cd "$app/hosts/web" && npx playwright install chromium >/dev/null 2>&1 ); then
		printf "  ${R}✗${Z} %s: chromium download\n" "$name"; fail=$((fail+1)); continue
	fi
	if ! ( cd "$app/hosts/web" && npm test ); then
		printf "  ${R}✗${Z} %s: browser test\n" "$name"; fail=$((fail+1)); continue
	fi
	printf "  ${G}✓${Z} %s runs in a browser\n" "$name"
done

echo "-------------------------------------------------"
if [ "$fail" -eq 0 ]; then
	printf "${G}✓${Z} example apps green in the browser\n"
	exit 0
fi
printf "${R}✗${Z} %d example app(s) failing in the browser\n" "$fail"
exit 1
