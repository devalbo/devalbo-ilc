#!/usr/bin/env bash
#
# verify-scaffold-web.sh — a SCAFFOLDED app runs in a browser.
#
# `verify-scaffold` proves a generated project builds and runs natively, and
# builds to wasm. This is the other half: the wasm actually boots under jco in a
# real browser and answers a command, using the app's own generated method ids.
#
# Why separate: this needs `npm install` in a fresh project plus a Chromium
# download — minutes, not seconds — so it belongs in the web tier (B3) rather
# than slowing the engine suite (B2) on every run.
#
# The Playwright spec is NOT written here. It ships in the template, so a
# generated app comes with its own browser test; this script only scaffolds and
# runs `npm test`. That keeps the check honest about what a user actually gets.
#
# Run inside `devbox shell` (needs go, tinygo, buf, node/jco).
set -uo pipefail
cd "$(dirname "$0")/.." || exit 2
REPO="$PWD"
if [ -t 1 ]; then G=$'\033[32m'; R=$'\033[31m'; Z=$'\033[0m'; else G=''; R=''; Z=''; fi

WORK="$(mktemp -d)"
APP=webcheck
trap 'rm -rf "$WORK"' EXIT

fail() { printf "  ${R}✗${Z} %s\n" "$1"; exit 1; }
step() { printf "  %s\n" "$1"; }

echo "-------------------------------------------------"
echo "scaffold (web): dlc new -> build web -> run it in Chromium"

# Templates are EMBEDDED, so a stale dlc scaffolds a stale tree and this would
# silently test the wrong thing.
go build -buildvcs=false -o "$WORK/dlc" ./hosts/native || fail "building dlc"

step "dlc new $APP"
( cd "$WORK" && "$WORK/dlc" new --module "example.com/$APP" --platform-path "$REPO" "$APP" >/dev/null ) \
	|| fail "dlc new"
PROJ="$WORK/$APP"

# Codegen BEFORE tidy: engine/ imports the generated messages, so a fresh
# scaffold cannot resolve its own module graph until they exist. (`go mod tidy`
# first fails with "unrecognized import path .../gen/go/...", which reads like a
# network problem and is not.)
step "make gen"
( cd "$PROJ" && make gen ) >/dev/null 2>&1 || fail "make gen"

step "go mod tidy"
( cd "$PROJ" && go mod tidy ) >/dev/null 2>&1 || fail "go mod tidy"

step "make build-web (dlc build web)"
( cd "$PROJ" && PATH="$WORK:$PATH" make build-web ) >/dev/null 2>&1 || fail "make build-web"
[ -f "$PROJ/frontend/src/wasm/engine.component.js" ] || fail "no transpiled component in the web root"

step "npm install (fresh project — slow)"
( cd "$PROJ/frontend" && npm install --silent --no-audit --no-fund ) >/dev/null 2>&1 \
	|| fail "npm install in the scaffolded frontend"

step "playwright install chromium"
( cd "$PROJ/frontend" && npx playwright install chromium ) >/dev/null 2>&1 \
	|| fail "playwright browser download"

# The spec that runs here came from the template — it is the test the user gets.
step "npm test (the app's own browser test)"
if ! ( cd "$PROJ/frontend" && npm test ); then
	fail "the scaffolded app's browser test"
fi

echo "-------------------------------------------------"
printf "${G}✓${Z} a scaffolded app runs in the browser, from its own shipped test\n"
