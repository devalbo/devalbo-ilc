#!/usr/bin/env bash
#
# verify-platform-gen.sh — dlc-platform's generated code is COMMITTED and CURRENT.
#
# WHY THIS EXISTS. `/gen/` is ignored everywhere in this repo except one place:
# `dlc-platform/gen/` is committed, because a consumer of that module cannot run
# `buf` or `wit-bindgen` (AGENTS.md §6). That exception is load-bearing and it
# rots in a direction nobody here can see — WE always regenerate, so a stale or
# untracked tree builds perfectly in this checkout and ships broken to everyone
# else. The failure lands on someone who did nothing wrong, with no way to tell
# what went stale.
#
# Two distinct failures, so two checks:
#
#   STALE     the committed bytes disagree with what the .proto/.wit say now
#   UNTRACKED the files exist locally but were never added, so a fresh clone of
#             the module has nothing at all
#
# Only `git` READS happen here — status and ls-files. Nothing is staged or
# committed; that is the maintainer's job (AGENTS.md §6).
#
# Run inside `devbox shell` (needs go, buf, wit-bindgen-go).
set -uo pipefail
cd "$(dirname "$0")/.." || exit 2
if [ -t 1 ]; then G=$'\033[32m'; R=$'\033[31m'; Z=$'\033[0m'; else G=''; R=''; Z=''; fi

GEN=dlc-platform/gen
fail() { printf "  ${R}✗${Z} %s\n" "$1"; exit 1; }

echo "-------------------------------------------------"
echo "platform gen: committed and current"

[ -d "$GEN" ] || fail "$GEN does not exist — run: make gen"

# ---- UNTRACKED -------------------------------------------------------------
#
# Checked BEFORE regenerating: `make gen` would happily recreate files that are
# present-but-unadded, and the check would then pass on a tree a consumer cannot
# obtain.
tracked="$(git ls-files "$GEN" | wc -l | tr -d ' ')"
[ "$tracked" -gt 0 ] || fail "$GEN is not tracked by git at all — a consumer of the module would get nothing"

untracked="$(git ls-files --others --exclude-standard "$GEN")"
if [ -n "$untracked" ]; then
	printf "  ${R}✗${Z} %s\n" "$GEN has files that were never committed:"
	printf '      %s\n' $untracked
	echo "      A consumer gets the module WITHOUT these. Add them."
	exit 1
fi

# ---- STALE -----------------------------------------------------------------
#
# Regenerate and see whether anything moved. `make gen` is what a maintainer
# runs, so this compares against the real pipeline rather than a reimplementation
# of it.
before="$(git status --porcelain "$GEN")"
if [ -n "$before" ]; then
	printf "  ${R}✗${Z} %s\n" "$GEN has uncommitted changes before regenerating:"
	printf '%s\n' "$before" | sed 's/^/      /'
	echo "      Commit them, or discard them — this check cannot tell which you meant."
	echo "      Tip: if the only diff is '// protoc-gen-go-lite version: …', CI installed"
	echo "      @latest and drifted the banner — pin the plugin to go.mod's version."
	exit 1
fi

make gen >/dev/null 2>&1 || fail "make gen failed"

after="$(git status --porcelain "$GEN")"
if [ -n "$after" ]; then
	printf "  ${R}✗${Z} %s\n" "$GEN is STALE — regenerating changed it:"
	printf '%s\n' "$after" | sed 's/^/      /'
	echo
	echo "  The committed bytes disagree with the schema. Anyone consuming"
	echo "  dlc-platform is building against the old ones. Re-run:"
	echo
	echo "      make gen"
	echo
	echo "  and commit the result with the schema change that caused it."
	exit 1
fi

printf "  ${G}✓${Z} %s\n" "$tracked file(s) tracked, and regenerating changes nothing"
