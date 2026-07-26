#!/usr/bin/env bash
#
# verify-parity.sh — B2 wasm-parity check (Decision 26).
#
# The native in-process engine and the wasip2 component (cmd/engine-component)
# must produce identical results for a set of golden vectors. Native is the
# developer-convenience path; the wasm component is the contract. A mismatch
# means the two builds of the engine have diverged (TinyGo vs native Go) —
# investigate before trusting the tool.
#
# Both boundaries are checked:
#
#   argv    execute-cli(args)              the bootstrap shim; native side is the
#                                          real `dlc` binary, end to end
#   method  execute(method, request)       the real boundary (Decision 28/31);
#                                          native side is cmd/parity-runner until
#                                          hosts/native builds requests host-side
#
# Run inside `devbox shell` (needs go, tinygo, node/jco).
set -uo pipefail
cd "$(dirname "$0")/.." || exit 2
if [ -t 1 ]; then G=$'\033[32m'; R=$'\033[31m'; Z=$'\033[0m'; else G=''; R=''; Z=''; fi

ARGV_VEC=verify/parity/argv-vectors.json
METHOD_VEC=verify/parity/method-vectors.json
BIN="$(mktemp -d)"

# 1. native builds — engine linked in-process
go build -buildvcs=false -o "$BIN/dlc" ./hosts/native || { echo "native build failed"; exit 1; }
go build -buildvcs=false -o "$BIN/parity-runner" ./cmd/parity-runner || { echo "runner build failed"; exit 1; }

# 2. wasip2 component (wasip2-direct) + jco transpile
tinygo build -target=wasip2 --wit-package ./wit --wit-world engine \
	-o engine.component.wasm ./cmd/engine-component || { echo "component build failed"; exit 1; }
( cd verify/parity \
	&& npm install --silent --no-audit --no-fund \
	&& npx jco transpile ../../engine.component.wasm -o out >/dev/null ) \
	|| { echo "transpile failed"; exit 1; }

# 3. run the SAME vectors through each side
native_argv() {
	python3 - "$BIN/dlc" "$ARGV_VEC" <<'PY'
import base64, json, subprocess, sys
dlc, vec = sys.argv[1], sys.argv[2]
for args in json.load(open(vec)):
    p = subprocess.run([dlc, *args], capture_output=True)
    ok = "true" if p.returncode == 0 else "false"
    print(f"{ok}\t{base64.b64encode(p.stdout).decode()}")
PY
}
native_method() { "$BIN/parity-runner" "$METHOD_VEC"; }
component_stream() { ( cd verify/parity && node harness.mjs "$1" "../../$2" ); }
count() { python3 -c "import json,sys;print(len(json.load(open(sys.argv[1]))))" "$1"; }

echo "-------------------------------------------------"
status=0

# compare <label> <native stream> <component stream> <vector count>
compare() {
	local label="$1" native="$2" component="$3" n="$4"
	echo "wasm-parity [$label]: $n golden vectors (native vs wasip2 component)"
	if [ "$native" = "$component" ]; then
		printf "  ${G}✓${Z} native == component\n"
		return 0
	fi
	printf "  ${R}✗${Z} PARITY MISMATCH (< native  > component):\n"
	diff <(printf '%s\n' "$native") <(printf '%s\n' "$component") | sed 's/^/  /'
	return 1
}

compare "argv" "$(native_argv)" "$(component_stream argv "$ARGV_VEC")" "$(count "$ARGV_VEC")" || status=1
compare "method" "$(native_method)" "$(component_stream method "$METHOD_VEC")" "$(count "$METHOD_VEC")" || status=1

if [ "$status" -eq 0 ]; then
	printf "${G}✓${Z} Decision 26 parity holds on both boundaries\n"
else
	printf "${R}✗${Z} parity broken — see the diff above\n"
fi
exit "$status"
