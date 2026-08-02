#!/usr/bin/env bash
#
# release-dlc-web.sh — publish @devalbo/dlc-web AS A GIT REF, not to a registry.
#
# WHY A BRANCH AND NOT A SUBDIRECTORY. npm cannot install a package that lives in
# a subdirectory of a git repo. The `#fragment` on a git URL is a committish, and
# npm has no equivalent of pip's `#subdirectory=` — verified, not assumed:
#
#	npm i "git+file:///repo#subdirectory=sub/pkg"
#	  → git checkout subdirectory=sub/pkg
#	  → error: pathspec 'subdirectory=sub/pkg' did not match any file(s)
#
# So the package has to BE a repo root. `git subtree split` rewrites the history
# of one directory onto its own branch, whose root is exactly `dlc-platform/web`
# — and npm installs that happily. No registry account, no publish, no tarball
# hosting, and the source of truth stays this repo.
#
# WHAT IT DOES NOT DO: push, or tag anything you have not looked at. It prepares
# the branch and prints the two commands you would run next, because pushing is
# how a mistake becomes public.
#
# Usage:  ./scripts/release-dlc-web.sh [version]     (default: package.json's)
set -uo pipefail
cd "$(dirname "$0")/.." || exit 2
if [ -t 1 ]; then G=$'\033[32m'; R=$'\033[31m'; B=$'\033[1m'; Z=$'\033[0m'; else G=''; R=''; B=''; Z=''; fi

PKG=dlc-platform/web
BRANCH=dlc-web-dist

fail() { printf "  ${R}✗${Z} %s\n" "$1"; exit 1; }

VERSION="${1:-$(node -e "console.log(require('./$PKG/package.json').version)" 2>/dev/null)}"
[ -n "$VERSION" ] || fail "could not read a version (pass one, or fix $PKG/package.json)"
TAG="dlc-web-v$VERSION"

echo "-------------------------------------------------"
echo "release @devalbo/dlc-web $VERSION as $TAG"

# A dirty tree would split a state nobody reviewed. The subtree command reads
# committed history, so uncommitted work is silently EXCLUDED — the failure mode
# is a release missing the change you just made, which is worse than an error.
if [ -n "$(git status --porcelain "$PKG")" ]; then
	fail "$PKG has uncommitted changes — commit them first, or they will not be in the release"
fi

# The package must ship what it advertises before it ships at all.
./scripts/verify-npm-package.sh || fail "the package does not ship what its exports advertise"

printf "\n  splitting %s onto %s\n" "$PKG" "$BRANCH"
git branch -D "$BRANCH" >/dev/null 2>&1 # a previous run's branch is not history worth keeping
git subtree split --prefix="$PKG" -b "$BRANCH" >/dev/null || fail "git subtree split"

printf "  ${G}✓${Z} %s now has %s at its root\n" "$BRANCH" "$PKG"

cat <<EOF

${B}Next, and these are yours to run:${Z}

    git tag $TAG $BRANCH
    git push origin $BRANCH $TAG

Then a scaffolded app resolves the package with no checkout of this repo:

    "@devalbo/dlc-web": "github:devalbo/devalbo-ilc#$TAG"

That string is ${B}engine.PlatformWebRef${Z} (engine/commands.go), which \`dlc new\`
writes into every scaffold that was not given --platform-path. Bump it in the
same commit as the version, or new scaffolds will point at the previous release.
EOF
