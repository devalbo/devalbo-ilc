#!/usr/bin/env bash
#
# stage-artifact.sh — put a freshly built .uf2 where a person drags from, and
# re-hash the directory.
#
# WHY BOTH IN ONE SCRIPT. They are the same act. A firmware that is built but not
# staged leaves the previous one sitting in `flash-testing/` looking current, and
# a staged file whose hash is not re-read leaves a ledger describing the file it
# replaced. Either half alone produces the same failure: you flash last week's
# firmware, it runs perfectly, and you debug a fix that is not on the board.
#
# That already happened once here — the README recorded `288285c1` while the file
# on disk hashed to `c1f8f215`, and nothing noticed, because staging was a manual
# copy and the hash was typed in by hand.
#
# SO IT IS NOT A STEP ANYONE HAS TO REMEMBER. Every target that produces a
# flashable image calls this on its way out. Build one firmware or all three, in
# any order, and the directory and its ledger are both correct afterwards.
#
# THE LEDGER IS REGENERATED WHOLE, not patched line by line. It costs four
# `shasum` calls and removes a class of bug: a file that arrives some other way
# still gets an entry. The factory MicroPython image is exactly that — dropped in
# by hand, never built here, and the way back from a bad flash, so it is the last
# file that should be missing a checksum.
#
# Usage: scripts/stage-artifact.sh build/badge-bringup.uf2 [more...]
set -euo pipefail

cd "$(dirname "$0")/.."

DEST=flash-testing
LEDGER="$DEST/CHECKSUMS.txt"

if [ "$#" -eq 0 ]; then
	echo "stage-artifact.sh: no files given" >&2
	exit 2
fi

mkdir -p "$DEST"

for file in "$@"; do
	if [ ! -f "$file" ]; then
		# Loud, because the caller is a build target that believed it had just
		# produced this. Staging nothing silently would hide a failed build
		# behind a green make and leave the old image in place — the precise
		# outcome this script exists to prevent.
		echo "stage-artifact.sh: no such file: $file" >&2
		exit 1
	fi
	cp "$file" "$DEST/"
	printf '  staged %s\n' "$DEST/$(basename "$file")"
done

# Every .uf2 present, not just the ones staged in this call.
( cd "$DEST" && shasum -a 256 ./*.uf2 >CHECKSUMS.txt )
sed 's/^/  /' "$LEDGER"
