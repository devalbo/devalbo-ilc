#!/usr/bin/env bash
#
# verify-scaffold.sh — `dlc new` produces a project that actually BUILDS and RUNS.
#
# A scaffolder whose output does not compile is worse than no scaffolder: the
# failure lands on a new user, in a tree they did not write, with no idea which
# part is wrong. So the check is the real thing end to end — scaffold, generate,
# test, build, run — against a throwaway directory.
#
# This is the §11 "Scaffolder" row: templates cannot silently rot, because a
# template edit that breaks the build fails here instead of on someone's laptop.
#
# WHAT THIS CHECK DOES NOT COVER: it runs the scaffold's `make gen` with THIS
# repo's toolchain on PATH, so a tool the template's devbox.json forgets to
# declare is invisible here — ours is already installed. That is a real bug we
# have shipped, and verify-scaffold-env.sh is the check that stands outside and
# catches it. Keep the two separate: this one is fast and runs in B2, that one
# resolves a fresh devbox environment and runs nightly.
#
# Run inside `devbox shell` (needs go, buf).
set -uo pipefail
cd "$(dirname "$0")/.." || exit 2
REPO="$PWD"
if [ -t 1 ]; then G=$'\033[32m'; R=$'\033[31m'; Z=$'\033[0m'; else G=''; R=''; Z=''; fi

WORK="$(mktemp -d)"
# HYPHENATED on purpose: the app name is used verbatim (binary, README, version
# string) but also DERIVED into an identifier for the proto package and Go
# imports, which cannot contain dashes. A single-word name exercises neither
# path distinctly, and `my-app` is what people actually type.
APP=scaffold-check
trap 'rm -rf "$WORK"' EXIT

fail() { printf "  ${R}✗${Z} %s\n" "$1"; exit 1; }
step() { printf "  %s\n" "$1"; }

echo "-------------------------------------------------"
echo "scaffold: dlc new -> gen -> test -> build -> run"

# The dlc under test must be current: templates are EMBEDDED, so a stale binary
# scaffolds a stale tree and the check silently tests the wrong thing.
go build -buildvcs=false -o "$WORK/dlc" ./hosts/native || fail "building dlc"

step "dlc new $APP"
( cd "$WORK" && "$WORK/dlc" new --tiers native --tiers web --module "example.com/$APP" --platform-path "$REPO" "$APP" >/dev/null ) \
	|| fail "dlc new"

PROJ="$WORK/$APP"
PKG="$(printf '%s' "$APP" | tr -c 'a-zA-Z0-9' '_')" # identifier-safe, as the renderer derives it
[ -f "$PROJ/go.mod" ] || fail "no go.mod in the scaffold"

# `make gen`, not raw `buf generate`: the project's gen target ALSO runs
# `dlc gen`, which emits the dlcconfig package the engine imports. Calling buf
# directly skips it and the build fails on a missing import — this check must
# exercise what a user actually runs, not a shortcut past it.
step "make gen"
( cd "$PROJ" && PATH="$WORK:$PATH" make gen ) >/dev/null 2>&1 || fail "codegen in the scaffolded project"

# The id lock must be created by the first generate — a scaffolded app is
# guarded from its first build, not from whenever someone remembers.
[ -f "$PROJ/proto/method-ids.lock" ] || fail "no method-ids.lock after generate"

step "go mod tidy"
( cd "$PROJ" && go mod tidy ) >/dev/null 2>&1 || fail "go mod tidy"

# ---- the extraction claim (§16.4), checked rather than asserted ------------
#
# A scaffolded app depends on dlc-platform and NOT on the dlc repo. That is the
# whole point of the extraction, and it is the kind of claim that decays
# silently: one convenience import of something still living in devalbo-ilc and
# the module graph quietly grows a dependency nobody notices, because in THIS
# tree the dlc checkout is always right there.
#
# Read precisely: an app still needs the dlc BINARY as a build tool (`make gen`
# above runs `dlc gen`, and `dlc build web` supplies the WIT world). What it must
# not need is the dlc MODULE. Those are different claims and only the second is
# being made here.
#
# THE MATCH IS EXACT, and that stopped being a detail when the platform moved to
# `github.com/devalbo/devalbo-ilc/dlc-platform` (2026-08-02). A substring test
# for "devalbo-ilc" now matches the platform itself, so the sloppy version of
# this check fails on a perfectly good scaffold — and the lazy fix, dropping the
# check, would retire the only thing enforcing the extraction. `go list -m all`
# prints "<path> <version>" per line, so the module path is the first field and
# comparing it is exact by construction.
step "the module graph contains dlc-platform and not the dlc module"
graph="$( cd "$PROJ" && go list -m all 2>/dev/null )" || fail "go list -m all"

# Written to a file rather than piped: `grep -q` exits at the first match and
# SIGPIPEs the writer, which under `pipefail` reports a SUCCESSFUL match as a
# failed pipeline. lint-scripts.sh knows about this one because it already
# reached CI once.
printf '%s\n' "$graph" | awk '{print $1}' > "$WORK/module-paths"
if ! grep -qx 'github.com/devalbo/devalbo-ilc/dlc-platform' "$WORK/module-paths"; then
	fail "the scaffold does not depend on dlc-platform at all"
fi
if grep -qx 'github.com/devalbo/devalbo-ilc' "$WORK/module-paths"; then
	fail "the scaffold depends on the dlc MODULE, not just the platform that lives in its repo"
fi

# And it must BUILD with no network: a dependency that is only satisfiable by
# fetching is one the module graph did not really contain.
step "builds offline (nothing to fetch)"
( cd "$PROJ" && GOPROXY=off GOFLAGS=-mod=mod go build ./... ) \
	|| fail "the scaffold cannot build without the network — something is not resolved by replace"

step "go test ./..."
( cd "$PROJ" && go test ./... ) || fail "the scaffold's own tests"

step "go build"
( cd "$PROJ" && go build -o "$APP" ./hosts/native ) || fail "building the scaffolded app"

step "run it"
version="$( cd "$PROJ" && "./$APP" version )" || fail "$APP version"
greet="$( cd "$PROJ" && "./$APP" greet --name ILC )" || fail "$APP greet"
case "$greet" in *ILC*) ;; *) fail "greet did not echo the name: $greet" ;; esac

# The inherited platform verbs must work in a scaffolded app too — that is the
# whole point of depending on the platform rather than copying it.
bundle="$( cd "$PROJ" && "./$APP" export-fs )" || fail "inherited export-fs"
case "$bundle" in '{'*'"type": "directory"'*) ;; *) fail "export-fs did not emit BFT" ;; esac

# ---- the web tier: the same engine, built to wasm --------------------------
#
# `dlc build web` supplies the WIT world from dlc's own embedded copy, so the
# scaffold carries none and cannot be stranded on a stale world. This is the
# claim that makes "write once, run everywhere" true for GENERATED apps and not
# just for dlc itself.
#
# The browser RUN of a scaffolded app is checked separately (test-b3): it needs
# npm install + Playwright, which is minutes, not seconds.
step "dlc build web"
( cd "$PROJ" && PATH="$WORK:$PATH" make build-web ) \
	|| fail "dlc build web on the scaffolded app"

[ -f "$PROJ/build/engine.component.wasm" ] || fail "no wasm component was produced"
# The web assets go INSIDE the Vite root (jco fetches the core .wasm at
# runtime, so a dev server has to be able to serve it); the component stays in
# build/, where nothing serves it.
[ -f "$PROJ/hosts/web/src/wasm/engine.component.js" ] || fail "jco transpile produced no JS in the web root"
# The app's own generated ids must reach the web tier — hand-mirroring them into
# TypeScript is the hole protoc-gen-dlc-registry exists to close.
grep -q "MethodGreet" "$PROJ/gen/ts/$PKG/v1/commands.registry.pb.ts" 2>/dev/null \
	|| fail "no generated TypeScript method ids for the web tier"

echo "-------------------------------------------------"
printf "${G}✓${Z} the scaffold builds and runs (native + wasm) — %s / %s\n" "$version" "$greet"
