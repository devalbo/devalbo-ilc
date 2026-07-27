#!/usr/bin/env bash
#
# lint-scripts.sh — shell patterns that only fail once output grows.
#
# Every rule here is a bug that ALREADY reached CI, stayed green locally, and
# cost real time to find. They share a shape: correct on a small suite, broken on
# a big one, and invisible on macOS. A Linux container would catch them too, but
# only if someone remembers to run it — this runs in every suite, everywhere.
set -uo pipefail
cd "$(dirname "$0")/.." || exit 2
if [ -t 1 ]; then G=$'\033[32m'; R=$'\033[31m'; Y=$'\033[33m'; Z=$'\033[0m'; else G=''; R=''; Y=''; Z=''; fi

fail=0
report() { # file:line  rule  explanation
	printf "  ${R}✗${Z} %s\n      %s\n      %s\n" "$1" "$2" "$3"
	fail=$((fail+1))
}

scripts=(scripts/*.sh)

# RULE 1 — never capture a verify script's output into a variable.
#
# `out="$(./scripts/verify-parity.sh 2>&1)"` held a whole scaffold tree as
# base64. On Linux that pushed every later exec past ARG_MAX, so `rm` and `grep`
# died with "Argument list too long" — and the failing `rm` was a probe cleanup,
# so a //go:build tinygo file survived and emptied the template FS for every
# wasm build that followed. Two suites, one variable. Redirect to a file.
for f in "${scripts[@]}"; do
	while IFS=: read -r line text; do
		[ -z "${line:-}" ] && continue
		report "$f:$line" \
			"captures a verify script's output into a variable" \
			"redirect to a file instead: cmd >\"\$OUT\" 2>&1  (unbounded output blows Linux ARG_MAX)"
	done < <(grep -nE '^[[:space:]]*[A-Za-z_][A-Za-z0-9_]*="?\$\((\./)?(scripts/)?(verify|test)-[^)]*\)' "$f" 2>/dev/null)
done

# RULE 2 — never pipe unbounded output into `grep -q`.
#
# `grep -q` exits on its first match and closes the pipe; the writer takes
# SIGPIPE, and `set -o pipefail` turns that into the pipeline's status — so a
# SUCCESSFUL match reports failure. Only once the writer is slow enough to still
# be going, i.e. only after the output grows. Grep a file.
for f in "${scripts[@]}"; do
	while IFS=: read -r line text; do
		[ -z "${line:-}" ] && continue
		report "$f:$line" \
			"pipes into \`grep -q\` (SIGPIPE + pipefail = success reported as failure)" \
			"write to a file first, then: grep -q PATTERN \"\$FILE\""
	done < <(grep -nE '\|[[:space:]]*grep[[:space:]]+-[a-zA-Z]*q' "$f" 2>/dev/null)
done

# RULE 3 — a script that writes into the repo must clean up on a trap.
#
# The parity self-test writes a build-tagged probe into engine/. Without a trap,
# an interrupted run leaves it there and silently changes what LATER suites
# compile, on one tier only.
for f in "${scripts[@]}"; do
	if grep -qE '^[[:space:]]*(cat|printf|echo)[^|]*>[[:space:]]*"?\$?\{?(PROBE|engine/)' "$f" 2>/dev/null; then
		grep -q '^[[:space:]]*trap ' "$f" || report "$f" \
			"writes into the repo but sets no trap" \
			"add: trap cleanup EXIT INT TERM  — an interrupted run must not leave build-tagged files behind"
	fi
done

echo "-------------------------------------------------"
if [ "$fail" -eq 0 ]; then
	printf "${G}✓${Z} shell scripts clean (%d checked)\n" "${#scripts[@]}"
	exit 0
fi
printf "${R}✗${Z} %d shell issue(s) — each one is a bug that already reached CI once\n" "$fail"
exit 1
