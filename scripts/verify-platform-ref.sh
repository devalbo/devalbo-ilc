#!/usr/bin/env bash
#
# verify-platform-ref.sh — the version `dlc new` names is a version that EXISTS.
#
# WHY THIS EXISTS. A scaffold's dependency on the platform is a pinned ref
# (`engine.PlatformWebRef`), and a pin has a failure mode nothing else here can
# see: bump the constant, forget to push the tag, and every check stays green.
# CI passes `--platform-path`, so every suite resolves the platform from this
# working tree and never asks a remote anything. The break lands on the first
# person to run `dlc new` without that flag — someone who does not have this
# repo, which is the entire audience the pin exists for.
#
# Same shape as verify-scaffold-env.sh: the thing under test is what an OUTSIDER
# gets, so the check has to stop using the local copy and go fetch.
#
# It scaffolds rather than testing the ref directly, deliberately. `npm install
# github:…#tag` in a temp directory would prove the tag exists; it would not
# prove that the tag is the one `dlc` WRITES. The gap between the constant and
# the release is the bug, so the check has to cross it.
#
# SLOW and NETWORK-DEPENDENT (it clones from GitHub), which is why it runs in the
# nightly `all` tier rather than on every push.
#
# Run inside `devbox shell` (needs go, node/npm, and a network).
set -uo pipefail
cd "$(dirname "$0")/.." || exit 2
REPO="$PWD"
if [ -t 1 ]; then G=$'\033[32m'; R=$'\033[31m'; Z=$'\033[0m'; else G=''; R=''; Z=''; fi

WORK="$(mktemp -d)"
APP=platform-ref-check
trap 'rm -rf "$WORK"' EXIT

fail() { printf "  ${R}✗${Z} %s\n" "$1"; exit 1; }
step() { printf "  %s\n" "$1"; }

echo "-------------------------------------------------"
echo "platform ref: what \`dlc new\` pins actually resolves"

# Templates are EMBEDDED, so a stale binary pins a stale ref and the check tests
# the wrong constant.
go build -buildvcs=false -o "$WORK/dlc" ./hosts/native || fail "building dlc"

# NO --platform-path. That flag is what every other check passes, and it is
# exactly what hides this failure: with it, the dependency is a file: path into
# this tree and no ref is ever consulted.
step "dlc new $APP (no --platform-path)"
( cd "$WORK" && "$WORK/dlc" new "$APP" --tiers native --tiers web --module "example.com/$APP" >/dev/null ) \
	|| fail "dlc new"

WEB="$WORK/$APP/hosts/web"
[ -f "$WEB/package.json" ] || fail "the scaffold has no web tier to check"

ref="$(node -e "console.log(require('$WEB/package.json').dependencies['@devalbo/dlc-web'] || '')")"
[ -n "$ref" ] || fail "the scaffold does not depend on @devalbo/dlc-web at all"
case "$ref" in
	file:*) fail "the scaffold pinned a LOCAL path with no --platform-path given: $ref" ;;
esac
step "pinned: $ref"

step "npm install (from the remote, not this tree)"
if ! ( cd "$WEB" && npm install --no-audit --no-fund ) >"$WORK/install.log" 2>&1; then
	printf "  ${R}✗${Z} %s\n" "the pinned ref does not resolve: $ref"
	echo "      Most likely the constant was bumped without pushing the tag:"
	echo "          ./scripts/release-dlc-web.sh && git tag … && git push origin …"
	tail -15 "$WORK/install.log" | sed 's/^/      /'
	exit 1
fi

# Resolving is not the same as being usable — a tag can point at a commit whose
# tree is missing half the package, which is precisely what the `files`
# allowlist gets wrong.
installed="$WEB/node_modules/@devalbo/dlc-web"
[ -f "$installed/package.json" ] || fail "the package resolved but is not on disk"
for entry in $(node -e "console.log(Object.values(require('$installed/package.json').exports).join(' '))"); do
	[ -f "$installed/${entry#./}" ] || fail "$entry is missing from the published ref"
done

version="$(node -e "console.log(require('$installed/package.json').version)")"
printf "  ${G}✓${Z} npm: %s resolves to @devalbo/dlc-web %s, complete\n" "$ref" "$version"

# ---- the Go half, which pins a version rather than a ref -------------------
#
# Same failure, different mechanism: `engine.PlatformGoVersion` names a version
# that only exists if someone tagged `dlc-platform/vX.Y.Z`. Go's subdirectory
# rule means the tag carries the prefix while the recorded version does not, so
# the two are easy to get out of step in exactly the way nothing here would see.
PROJ="$WORK/$APP"
goreq="$(grep 'devalbo-ilc/dlc-platform' "$PROJ/go.mod" | head -1)"
case "$goreq" in
	*replace*) fail "the scaffold wrote a replace directive with no --platform-path: $goreq" ;;
esac
step "go.mod requires:${goreq#require}"

if grep -q '^replace github.com/devalbo/devalbo-ilc/dlc-platform' "$PROJ/go.mod"; then
	fail "a replace directive would override the pinned version"
fi

step "go mod download (through the proxy, as a stranger would)"
if ( cd "$PROJ" && GOFLAGS=-mod=mod go mod download github.com/devalbo/devalbo-ilc/dlc-platform ) \
	>"$WORK/godl.log" 2>&1; then
	printf "  ${G}✓${Z} go: %s resolves from its tag\n" "${goreq#require }"
	exit 0
fi

# TWO DIFFERENT FAILURES WEAR THE SAME ERROR, and telling them apart is the
# reason this second attempt exists:
#
#   the tag was never pushed        — a real break; every user hits it, forever
#   the proxy has not caught up     — transient; sum.golang.org caches the 404
#                                     it got when someone asked BEFORE the tag
#                                     existed, and self-heals in minutes
#
# Both print "unknown revision dlc-platform/vX.Y.Z", so a check that stopped at
# the first attempt would either cry wolf on every release (you tag, then check
# immediately) or teach everyone to ignore it. Going direct answers the only
# question that matters: does the tag exist at the source?
step "…the proxy said no; asking GitHub directly to find out why"
if ( cd "$PROJ" && GOPROXY=direct GOSUMDB=off GOFLAGS=-mod=mod \
	go mod download github.com/devalbo/devalbo-ilc/dlc-platform ) >"$WORK/godirect.log" 2>&1; then
	printf "  ${G}✓${Z} go: %s exists at the source\n" "${goreq#require }"
	printf "  ${R}!${Z} but the module proxy has not caught up yet — TRANSIENT\n"
	echo "      sum.golang.org cached a 404 from before the tag was pushed. It expires"
	echo "      on its own; nothing here is wrong. To confirm by hand right now:"
	echo "          GOPROXY=direct GOSUMDB=off go mod download <module>"
	exit 0
fi

printf "  ${R}✗${Z} %s\n" "the pinned platform version does not exist"
echo "      Not proxy lag — GitHub does not have the tag either. Most likely the"
echo "      version was bumped without pushing one:"
echo "          git tag dlc-platform/<version> && git push origin dlc-platform/<version>"
echo "      Go's subdirectory rule: the TAG carries the dlc-platform/ prefix,"
echo "      the version recorded in go.mod does not."
tail -15 "$WORK/godirect.log" | sed 's/^/      /'
exit 1
