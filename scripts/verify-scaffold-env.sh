#!/usr/bin/env bash
#
# verify-scaffold-env.sh — a scaffold generates in ITS OWN declared environment.
#
# WHY THIS EXISTS, and why it is not part of verify-scaffold.sh. That check runs
# the scaffold's `make gen` with THIS repo's toolchain on PATH: our GOBIN, our
# npm prefix, our nix profile. So it answers "do the templates produce working
# code", and it silently also answers "…given every tool devalbo-ilc happens to
# have installed". The second half is a lie a new user cannot benefit from.
#
# It has already cost us. The template's devbox.json installed protoc-gen-go-lite
# and not protoc-gen-es-lite, which the generated buf.gen.yaml needs the moment a
# web tier exists — so the very first command `dlc new` tells you to run was the
# one that failed. Every check in this repo was green, because in this checkout
# the missing plugin was always already on PATH.
#
# Same class as verify-platform-gen.sh: broken only from OUTSIDE, therefore
# invisible from inside. The fix is the same shape — stand outside on purpose.
#
#   1. scaffold into a throwaway directory
#   2. REMOVE this repo's tool directories from PATH, and prove they are gone
#   3. run `make gen` through the SCAFFOLD's own `devbox run`, so the only tools
#      available are the ones its devbox.json declares
#
# Step 2 is the load-bearing one and is asserted, not assumed: `devbox run`
# inherits the parent PATH, so without the scrub the leak survives into the
# subshell and this script would pass while proving nothing.
#
# The `dlc` binary itself stays on PATH deliberately. An app needs the dlc BINARY
# as a build tool (`make gen` runs `dlc gen`) the same way it needs `go` — it is
# something the user installs, not something the template must provision. What is
# under test is the environment the scaffold DECLARES for itself.
#
# SLOW and network-dependent: the scaffold has no devbox.lock, so its `@latest`
# packages are resolved fresh. That is why it is a nightly (`ci.sh all`) step and
# not part of B2.
#
# Falsify it: delete the protoc-gen-es-lite line from
# templates/component-model/devbox.json.tmpl and re-run — it must go red, and
# verify-scaffold.sh must stay green. That pair IS the blind spot, demonstrated.
#
# Needs: devbox, go, and a network. Run inside `devbox shell` or via `devbox run`.
set -uo pipefail
cd "$(dirname "$0")/.." || exit 2
REPO="$PWD"
if [ -t 1 ]; then G=$'\033[32m'; R=$'\033[31m'; Z=$'\033[0m'; else G=''; R=''; Z=''; fi

fail() { printf "  ${R}✗${Z} %s\n" "$1"; exit 1; }
step() { printf "  %s\n" "$1"; }

echo "-------------------------------------------------"
echo "scaffold env: the generated project generates in its OWN devbox"

command -v devbox >/dev/null 2>&1 \
	|| fail "devbox not found — this check is specifically about the scaffold's declared environment"

WORK="$(mktemp -d)"
APP=scaffold-env-check
trap 'rm -rf "$WORK"' EXIT

# Same reason as verify-scaffold.sh: templates are EMBEDDED, so a stale binary
# scaffolds a stale tree and the check tests the wrong thing.
go build -buildvcs=false -o "$WORK/dlc" ./hosts/native || fail "building dlc"

step "dlc new $APP"
( cd "$WORK" && "$WORK/dlc" new --tiers native --tiers web --module "example.com/$APP" --platform-path "$REPO" "$APP" >/dev/null ) \
	|| fail "dlc new"
PROJ="$WORK/$APP"
[ -f "$PROJ/devbox.json" ] || fail "the scaffold declares no devbox.json — there is no environment to check"

# ---- step 2: stand outside ------------------------------------------------
#
# Drop every PATH entry that lives under this repo. That is where devbox puts
# what OUR devbox.json provisions — .devbox/nix/profile (go, buf, node),
# .devbox/gobin (protoc-gen-go-lite, wit-bindgen-go), .devbox/npm-global/bin
# (jco, protoc-gen-es-lite) and node_modules/.bin. Everything the scaffold needs
# has to come from its own devbox.json instead.
#
# This is a precaution, not the proof — see step 3 for why it cannot be the proof
# and where the actual assertion lives.
CLEAN_PATH=""
old_ifs="$IFS"; IFS=:
for p in $PATH; do
	case "$p" in
		"$REPO"|"$REPO"/*) continue ;;
	esac
	CLEAN_PATH="${CLEAN_PATH:+$CLEAN_PATH:}$p"
done
IFS="$old_ifs"
# …plus the dlc under test, which is a user-installed build tool (see the header).
CLEAN_PATH="$WORK:$CLEAN_PATH"

# ---- step 3: generate through the scaffold's own devbox --------------------
#
# `devbox run` and not `devbox shell`: non-interactive, and it runs the same
# init_hook, which is where the template installs its protoc plugins.
#
# **`devbox run` REBUILDS PATH.** It does not simply inherit the caller's, so the
# scrub above is a precaution and not the guarantee — and anything this script
# prepends (the dlc binary) is dropped on the way in. Both facts were found the
# hard way: the first version of this check failed with `dlc: No such file or
# directory` while claiming to have proved something about protoc plugins.
#
# So the tool audit happens INSIDE the environment under test, immediately before
# the command that uses those tools, and the assertion is the precise one: no
# tool the scaffold builds with may resolve to a path under THIS REPO. That is
# the leak that has actually bitten us (our npm prefix supplying a plugin the
# template never declared), and unlike "not on PATH at all" it does not misfire
# on a developer who has buf installed machine-wide.
#
# dlc is re-prepended inside, deliberately: an app needs the dlc BINARY as a
# build tool the same way it needs `go` — that is a thing the user installs, not
# a thing the template must provision (see the header).
#
# GOMODCACHE/GOCACHE are deliberately NOT scrubbed. A shared module cache is not
# a tool leak — it is what `go` would use on any developer's machine — and
# re-downloading the module graph would add minutes for no extra coverage.
# The inner half lives in a FILE, not in `devbox run -- sh -c '…'`. Devbox joins
# its arguments back into one shell string and expands them on the way through,
# which turned an inline script into `p="" || …` — every variable emptied, the
# newlines literal `\n`, and an audit that could no longer see anything. A file
# takes one unambiguous argument and is read by the shell that actually runs it.
cat >"$WORK/in-scaffold.sh" <<'INNER'
#!/usr/bin/env bash
# Runs INSIDE the scaffold's devbox environment. $DLC_REPO = the dlc checkout,
# $DLCBIN = where the dlc binary under test lives.
set -uo pipefail

# Re-scrub HERE, because the nesting puts the repo back: this script is reached
# through `devbox run` for the DLC repo (that is how CI invokes the suite), and
# that run re-adds the repo's nix profile and virtenv to PATH. The scaffold's own
# entries come first, so its declared tools still win — which is precisely why
# the blind spot is invisible: a tool the template FORGOT is quietly found in the
# repo's profile further down the list. Removing those entries is what turns
# "forgot to declare it" into a failure.
clean=""
old_ifs="$IFS"; IFS=:
for p in $PATH; do
	case "$p" in
		"$DLC_REPO"|"$DLC_REPO"/*) continue ;;
	esac
	clean="${clean:+$clean:}$p"
done
IFS="$old_ifs"
# dlc itself is a user-installed build tool, like go — not something the template
# must provision. See the header of verify-scaffold-env.sh.
export PATH="$DLCBIN:$clean"

# Belt and braces: after the scrub nothing should resolve into the repo. If
# something does, the scrub is wrong and the result below would be meaningless,
# so say so in a way the caller can distinguish from a genuine failure.
for t in buf protoc-gen-es-lite protoc-gen-go-lite; do
	p="$(command -v "$t")" || continue   # missing is a REAL failure; let the build report it
	case "$p" in
		"$DLC_REPO"/*) echo "SCAFFOLD-ENV: $t leaked from the dlc repo: $p"; exit 4 ;;
	esac
done

exec "$@"
INNER
chmod +x "$WORK/in-scaffold.sh"

step "devbox run -- make gen  (inside $APP)"
gen_log="$WORK/gen.log"
if ! ( cd "$PROJ" && PATH="$CLEAN_PATH" DLCBIN="$WORK" DLC_REPO="$REPO" \
	devbox run -- "$WORK/in-scaffold.sh" make gen ) >"$gen_log" 2>&1; then
	if grep -q "SCAFFOLD-ENV: .* leaked from the dlc repo" "$gen_log"; then
		printf "  ${R}✗${Z} %s\n" "INCONCLUSIVE: this repo's toolchain reached inside the scaffold"
		grep "SCAFFOLD-ENV:" "$gen_log" | sed 's/^/      /'
		echo "      The scaffold's own devbox.json is not what would have been under test,"
		echo "      so a pass here would have meant nothing. Fix the PATH scrub above."
		exit 1
	fi
	printf "  ${R}✗${Z} %s\n" "the scaffold cannot generate in the environment it declares"
	echo "      Its devbox.json is missing something that this repo's shell was providing."
	echo "      Last 30 lines:"
	tail -30 "$gen_log" | sed 's/^/      /'
	exit 1
fi

[ -f "$PROJ/proto/method-ids.lock" ] || fail "no method-ids.lock after generate"

# Generating is the failure we have actually seen, but the same argument applies
# to building: a package the scaffold's devbox.json forgets is equally invisible
# from in here. Native only — the wasm/browser build is B3's job and needs a
# toolchain this check has no opinion about.
#
# No tool audit here: `go` is the only thing this needs, it comes from the
# scaffold's own `packages`, and if it had leaked from the repo the audit above
# would already have said so about `buf` sitting in the same nix profile.
step "devbox run -- go build ./...  (inside $APP)"
build_log="$WORK/build.log"
if ! ( cd "$PROJ" && PATH="$CLEAN_PATH" DLCBIN="$WORK" DLC_REPO="$REPO" \
	devbox run -- "$WORK/in-scaffold.sh" go build ./... ) >"$build_log" 2>&1; then
	printf "  ${R}✗${Z} %s\n" "the scaffold cannot build in the environment it declares"
	tail -30 "$build_log" | sed 's/^/      /'
	exit 1
fi

echo "-------------------------------------------------"
printf "${G}✓${Z} the scaffold generates and builds using only its own devbox.json\n"
