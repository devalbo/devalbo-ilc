#!/usr/bin/env bash
#
# verify-parity.sh — B2 wasm-parity check (Decision 26).
#
# The native in-process engine (hosts/native) and the wasip2 component
# (cmd/engine-component) must produce identical (success, output) for a set of
# golden argv vectors. Native is the developer-convenience path; the wasm
# component is the contract. A mismatch means the two builds of engine.Execute
# have diverged (TinyGo vs native Go) — investigate before trusting the tool.
#
# Run inside `devbox shell` (needs go, tinygo, node/jco).
set -uo pipefail
cd "$(dirname "$0")/.." || exit 2
if [ -t 1 ]; then G=$'\033[32m'; R=$'\033[31m'; Z=$'\033[0m'; else G=''; R=''; Z=''; fi

VEC=verify/parity/vectors.json
DLC="$(mktemp -d)/dlc"

# 1. native dlc — engine linked in-process
go build -buildvcs=false -o "$DLC" ./hosts/native || { echo "native build failed"; exit 1; }

# 2. wasip2 component (wasip2-direct) + jco transpile
tinygo build -target=wasip2 --wit-package ./wit --wit-world engine \
	-o engine.component.wasm ./cmd/engine-component || { echo "component build failed"; exit 1; }
( cd verify/parity \
	&& npm install --silent --no-audit --no-fund \
	&& npx jco transpile ../../engine.component.wasm -o out >/dev/null ) \
	|| { echo "transpile failed"; exit 1; }

# 3. run the SAME vectors through each, as `<success>\t<base64(output)>` per line
native_stream() {
	python3 - "$DLC" "$VEC" <<'PY'
import base64, json, subprocess, sys
dlc, vec = sys.argv[1], sys.argv[2]
for args in json.load(open(vec)):
    p = subprocess.run([dlc, *args], capture_output=True)
    ok = "true" if p.returncode == 0 else "false"
    print(f"{ok}\t{base64.b64encode(p.stdout).decode()}")
PY
}
component_stream() { ( cd verify/parity && node harness.mjs "../../$VEC" ); }

native="$(native_stream)"
component="$(component_stream)"
count="$(python3 -c "import json;print(len(json.load(open('$VEC')))) ")"

echo "-------------------------------------------------"
echo "wasm-parity: $count golden vectors (native vs wasip2 component)"
if [ "$native" = "$component" ]; then
	printf "${G}✓${Z} native == component — Decision 26 parity holds\n"
	exit 0
fi
printf "${R}✗${Z} PARITY MISMATCH (< native  > component):\n"
diff <(printf '%s\n' "$native") <(printf '%s\n' "$component")
exit 1
