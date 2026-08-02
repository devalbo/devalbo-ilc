#!/usr/bin/env bash
#
# verify-npm-package.sh — what @devalbo/dlc-web SHIPS is what it promises.
#
# WHY THIS EXISTS. A published package has two lists that must agree and nothing
# compares them: `exports` says what consumers may import, and `files` says what
# actually goes in the tarball. They are edited at different times for different
# reasons, and when they disagree the failure is invisible from inside this repo
# — here every file is on disk, so every import resolves. It breaks only for
# someone who installed the package, with a module-not-found error naming a file
# they have never heard of.
#
# Same class as verify-scaffold-env.sh and verify-platform-gen.sh: broken only
# from OUTSIDE, therefore uncheckable from within. The fix is the same shape —
# build the artifact a stranger would get, install it somewhere else, and look.
#
# Two things are checked, and the second matters more:
#
#   EXPORTS    every path in `exports` exists in the tarball
#   IMPORTS    every relative import inside the shipped sources also exists —
#              this is the one that catches an internal module (worker.ts,
#              crosstab.ts) that nothing exports but everything needs
#
# NOT a publish, and it touches no registry: `npm pack` builds the tarball
# locally and the consumer is a temp directory.
#
# Run inside `devbox shell` (needs node/npm).
set -uo pipefail
cd "$(dirname "$0")/.." || exit 2
REPO="$PWD"
if [ -t 1 ]; then G=$'\033[32m'; R=$'\033[31m'; Z=$'\033[0m'; else G=''; R=''; Z=''; fi

PKG_DIR="$REPO/dlc-platform/web"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

echo "-------------------------------------------------"
echo "npm package: @devalbo/dlc-web ships what it advertises"

cd "$PKG_DIR" || exit 2
TGZ="$(npm pack --silent 2>/dev/null | tail -1)"
if [ -z "$TGZ" ] || [ ! -f "$TGZ" ]; then
	printf "  ${R}✗${Z} npm pack produced nothing\n"
	exit 1
fi
mv "$TGZ" "$WORK/pkg.tgz"

# A consumer, not a workspace: installed from a tarball into an unrelated
# directory, which is the only way the `files` allowlist is actually exercised.
mkdir -p "$WORK/consumer" || exit 2
cd "$WORK/consumer" || exit 2
npm init -y >/dev/null 2>&1
if ! npm install "$WORK/pkg.tgz" >/dev/null 2>&1; then
	printf "  ${R}✗${Z} the tarball does not install\n"
	exit 1
fi

PKG="node_modules/@devalbo/dlc-web"
fail=0

for entry in $(node -e "console.log(Object.values(require('./$PKG/package.json').exports).join(' '))"); do
	if [ ! -f "$PKG/${entry#./}" ]; then
		printf "  ${R}✗${Z} %s is in \`exports\` but not in the tarball\n" "$entry"
		fail=1
	fi
done

# Relative imports inside the shipped sources. An export can be present while
# the module it imports is missing, and that failure reads like a broken package
# rather than a missing file.
for f in "$PKG"/*.ts; do
	[ -f "$f" ] || continue
	for imp in $(grep -oE 'from "\./[^"]+"' "$f" | sed 's/from "//; s/"//'); do
		base="${imp#./}"
		if [ ! -f "$PKG/$base" ] && [ ! -f "$PKG/$base.ts" ] && [ ! -f "$PKG/$base.js" ]; then
			printf "  ${R}✗${Z} %s is imported by %s but not in the tarball\n" "$base" "$(basename "$f")"
			fail=1
		fi
	done
done

if [ "$fail" -ne 0 ]; then
	echo
	echo "  Fix the \`files\` allowlist in dlc-platform/web/package.json."
	exit 1
fi

count="$(find "$PKG" -type f | wc -l | tr -d ' ')"
printf "  ${G}✓${Z} %s files, every export and internal import resolves\n" "$count"
