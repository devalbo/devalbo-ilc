# hosts/ — per-platform capability providers (the injected Environment)

A host is the entry point that **constructs the `Environment`** and runs the shared engine.

**Rules:**
- Hosts **provide** capabilities; they carry **no business logic** (that's `engine/`).
- One host per target; the engine is unchanged across them (the "two bits").
- Bootstrap tiers: `native/` (CLI, wasmtime) and `web/` (jco). Desktop/embedded arrive later.

See [plan](../docs/DEVALBO-ILC-GO-PLAN.md) §5.4, §0.1.
