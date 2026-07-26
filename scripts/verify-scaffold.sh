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
( cd "$WORK" && "$WORK/dlc" new --module "example.com/$APP" --platform-path "$REPO" "$APP" >/dev/null ) \
	|| fail "dlc new"

PROJ="$WORK/$APP"
[ -f "$PROJ/go.mod" ] || fail "no go.mod in the scaffold"

step "buf generate"
( cd "$PROJ/proto" && buf lint && buf generate ) >/dev/null 2>&1 || fail "codegen in the scaffolded project"

# The id lock must be created by the first generate — a scaffolded app is
# guarded from its first build, not from whenever someone remembers.
[ -f "$PROJ/proto/method-ids.lock" ] || fail "no method-ids.lock after generate"

step "go mod tidy"
( cd "$PROJ" && go mod tidy ) >/dev/null 2>&1 || fail "go mod tidy"

step "go test ./..."
( cd "$PROJ" && go test ./... ) >/dev/null 2>&1 || fail "the scaffold's own tests"

step "go build"
( cd "$PROJ" && go build -o "$APP" ./hosts/native ) || fail "building the scaffolded app"

step "run it"
version="$( cd "$PROJ" && "./$APP" version )" || fail "$APP version"
greet="$( cd "$PROJ" && "./$APP" greet ILC )" || fail "$APP greet"
case "$greet" in *ILC*) ;; *) fail "greet did not echo the name: $greet" ;; esac

# The inherited platform verbs must work in a scaffolded app too — that is the
# whole point of depending on the platform rather than copying it.
bundle="$( cd "$PROJ" && "./$APP" export-fs )" || fail "inherited export-fs"
case "$bundle" in '{'*'"type": "directory"'*) ;; *) fail "export-fs did not emit BFT" ;; esac

echo "-------------------------------------------------"
printf "${G}✓${Z} the scaffold builds and runs — %s / %s\n" "$version" "$greet"
