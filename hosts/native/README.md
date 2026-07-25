# hosts/native/ — CLI host (standard Go + wasmtime)

The terminal entry point. Standard Go (full reflection OK **in the host only**).

**Flow (Decision 22):** thin **argv forwarder** — `os.Args` → wasmtime instantiates the engine
component → `execute-cli(args)` → `command-result` → exit code. **App command parsing lives in the
engine** (Spike 4 / ffcli), not here. The host may keep its own process flags (e.g. engine path) if
needed; those must not be the app command surface.

Constructs the native Environment (stdio, FS root = cwd/config dir).

**Rule:** `encoding/json` / reflection are fine here (standard Go); they must never leak into `engine/`.

**B2 note:** `wasmtime-go` does not yet expose the Component Model API — the embed path may need the
wasmtime C API / another host language until Go bindings land. Spike 5 confirmed a **blocking** custom
WIT import works under Rust wasmtime with the same guest component.

See [plan](../../docs/DEVALBO-ILC-GO-PLAN.md) §5.4, §8, Decision 22.
