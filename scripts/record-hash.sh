#!/usr/bin/env bash
#
# record-hash.sh — write a built artifact's sha256 into build/CHECKSUMS.txt.
#
# WHY THIS EXISTS. `flash-testing/README.md` carried sha256 prefixes typed in
# beside the files, and they went stale immediately: the recorded prefix said
# `288285c1` while the file on disk hashed to `c1f8f215`, and nothing anywhere
# noticed. A hash written by hand next to a file that is rebuilt by a different
# command is a claim with no mechanism behind it.
#
# The failure it produces is the expensive kind. A stale .uf2 flashes perfectly
# and runs last week's firmware, so you sit debugging a fix that is not on the
# board — the badge looks broken in exactly the way the fix was meant to cure.
#
# So the hash is recorded BY THE BUILD, at the moment the artifact appears.
# Nothing to remember and nothing to ask for: a target that produces a flashable
# file calls this on its way out, and the record cannot lag the file because the
# same command writes both.
#
# IDEMPOTENT AND ORDER-FREE. Each call rewrites only its own line, so targets can
# run in any order, individually or together, and re-running one does not
# disturb the others' entries. That matters because these artifacts are built
# separately far more often than together — `make display-probe` alone is the
# fast path for panel work.
#
# Usage: scripts/record-hash.sh build/badge-bringup.uf2 [more files...]
set -euo pipefail

cd "$(dirname "$0")/.."

LEDGER=build/CHECKSUMS.txt

if [ "$#" -eq 0 ]; then
	echo "record-hash.sh: no files given" >&2
	exit 2
fi

mkdir -p build
touch "$LEDGER"

for file in "$@"; do
	if [ ! -f "$file" ]; then
		# Loud, because the caller is a build target that believed it had just
		# produced this. A missing file here means the artifact was not written,
		# and silently recording nothing would hide that behind a green build.
		echo "record-hash.sh: no such file: $file" >&2
		exit 1
	fi

	name=$(basename "$file")
	sum=$(shasum -a 256 "$file" | awk '{print $1}')

	# Drop any previous line for this basename, then append the current one.
	# `grep -v` with a trailing-space anchor so `badge-payload.uf2` cannot match
	# a hypothetical `badge-payload.uf2.bak`.
	tmp=$(mktemp)
	grep -v "  ${name}\$" "$LEDGER" >"$tmp" || true
	printf '%s  %s\n' "$sum" "$name" >>"$tmp"
	# Sorted, so the ledger reads the same however the targets were invoked and a
	# diff shows a changed hash rather than a reordering.
	sort -k2 "$tmp" >"$LEDGER"
	rm -f "$tmp"

	printf '  sha256 %s  %s\n' "${sum:0:16}" "$name"
done
