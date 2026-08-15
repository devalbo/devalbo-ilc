#!/usr/bin/env bash
#
# check-fmt.sh — gofmt over the tree, reporting rather than rewriting.
#
# `gofmt -l .` alone is not enough here, for two separate reasons:
#
#   templates/  is not valid Go — the files carry {{.Tokens}} — so gofmt reports
#               parse errors that look like failures and are not. The .tmpl
#               suffix keeps the Go *build* out; this keeps gofmt out too.
#
#   .devbox/    now holds the GO MODULE CACHE. devbox.json's `env` moved GOCACHE
#               and GOMODCACHE under $PWD/.devbox/cache/ so CI can cache one
#               declared path instead of querying five tools — which also put
#               every downloaded dependency inside the repo tree, where anything
#               walking `.` finds it. This check went red on third-party sources
#               (json-iterator-lite, protobuf) the moment that landed. Their
#               formatting is not ours to fix or to judge.
#
# The general hazard, worth remembering before adding another tree walker: `.` is
# no longer only our code. Prefer an explicit file list (`scripts/*.sh`, as
# lint-scripts.sh does) over walking the root.
#
# Generated code is included on purpose — protoc-gen-dlc-registry formats its
# output, and if it ever stops, this is what notices.
set -uo pipefail
cd "$(dirname "$0")/.." || exit 2

unformatted="$(gofmt -l . 2>/dev/null | grep -Ev '^(templates|\.devbox)/' || true)"
if [ -z "$unformatted" ]; then
	echo "gofmt: clean"
	exit 0
fi
echo "gofmt: these files need formatting (run: gofmt -w <file>)"
printf '  %s\n' $unformatted
exit 1
