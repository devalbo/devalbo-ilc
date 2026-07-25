# hosts/native/ — CLI host (standard Go + wasmtime)

The terminal entry point. Standard Go (full reflection), so the **rich CLI lives HERE, not in the engine**.

**Flow:** `kong` parses argv → typed struct → proto `TInput` → wasmtime instantiates the engine component
→ run → `command-result` → exit code. Constructs the native Environment (stdio, FS root = cwd/config dir).

**Rule:** `kong`/`encoding/json` are fine here (standard Go); they must never leak into `engine/`.

See [plan](../../docs/DEVALBO-ILC-GO-PLAN.md) §5.4, §8, Decision 22.
