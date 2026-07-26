# hosts/native/ — CLI host (standard Go, engine linked in-process)

The terminal entry point. Standard Go (full reflection OK **in the host only**).

**Flow (Decisions 26 + 28):** `os.Args` → **host-side parser** → `(method_id, request bytes)` → native
Environment → `engine.ExecuteMethod` **in-process** → `command-result` → exit code. There is **no wasm
runtime in the run path** (Decision 26), which sidesteps the `wasmtime-go` Component-Model gap; the wasm
component remains the parity contract, checked in CI by `make verify-parity`.

**App command parsing lives HERE, not in the engine** (Decision 28, superseding Decision 22). The host is
free to use any parser — cobra / ffcli / `huh` menus — because it is off the TinyGo leash. What keeps the
tiers aligned is the shared proto request schema, not a shared argv parser.

Constructs the native Environment (stdio, FS root = cwd/config dir).

**Rule:** `encoding/json` / reflection are fine here (standard Go); they must never leak into `engine/`.

**Status:** `main.go` is still the transitional argv forwarder over `engine.Execute` (the `execute-cli`
shim). The host-side parser + request construction is the open B2 task; keep the engine binding behind a
small lift-ready package so a wasm-runtime host can be swapped in later without rewriting that path.

See [plan](../../docs/DEVALBO-ILC-GO-PLAN.md) §5.4, §8, Decisions 26 + 28.
