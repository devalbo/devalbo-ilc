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

**Two kinds of verb** (Decision 30): TOOLCHAIN verbs (`build`) are handled here and never reach the
engine — they spawn processes and inspect the machine, neither of which a browser tab can do. IN-ENGINE
verbs are parsed by `commands.go` into a proto request and dispatched by method id.

**Status:** the argv shim is gone; `commands.go` is the parser. It uses stdlib `flag` today — any parser
would do (cobra, `huh` menus), which is the point of parsing living host-side.

See [plan](../../docs/DEVALBO-ILC-GO-PLAN.md) §5.4, §8, Decisions 26 + 28.
