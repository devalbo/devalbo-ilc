#!/usr/bin/env bash
#
# check-fmt.sh — gofmt over the tree, reporting rather than rewriting.
#
# `gofmt -l .` alone is not enough here: it walks templates/, whose files are not
# valid Go (they carry {{.Tokens}}), and reports parse errors that look like
# failures but are not. The .tmpl suffix keeps the Go *build* out of there; this
# keeps gofmt out too.
#
# Generated code is included on purpose — protoc-gen-dlc-registry formats its
# output, and if it ever stops, this is what notices.
set -uo pipefail
cd "$(dirname "$0")/.." || exit 2

unformatted="$(gofmt -l . 2>/dev/null | grep -v '^templates/' || true)"
if [ -z "$unformatted" ]; then
	echo "gofmt: clean"
	exit 0
fi
echo "gofmt: these files need formatting (run: gofmt -w <file>)"
printf '  %s\n' $unformatted
exit 1
