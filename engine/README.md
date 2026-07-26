# engine/ — the shared business logic (Go → wasm)

The ONE portable artifact (`engine.core.wasm`). All app logic lives here and **nowhere else**.

**One entry point:** `ExecuteMethod(method, request)` — a permanent `method_id` plus flat proto-encoded
request bytes, dispatched through the registry in [`platform/`](./platform). The engine never sees argv:
**parsing is host-side** (Decision 28, superseding Decision 22). There is no argv shim; it retired once
every host built requests, taking the `execute-cli` export and the ffcli dependency with it.

**This directory is `dlc`'s APP code** — its own verbs (`new`, `echo`) and its templates, in the
**100+** method id range. The verbs every app inherits (`version`, `export-fs`, `import-fs`, `reset-fs`,
ids 1–99) live in [`platform/`](./platform), which becomes the `ilc-platform` module. `dlc` depends on
the platform exactly the way a scaffolded app does — that is what keeps the boundary honest.

**Rules:**
- Imports the ILC capability world (`gen/`); **never** calls a platform API directly.
- MUST stay **TinyGo-safe**. Serialization is **reflection-free** (protobuf-go-lite — no `encoding/json`).
  The registry's typed-handler adapter uses **generics, not reflection**, for the same reason.
- Handlers are what must compile under TinyGo. The parser moved host-side, so its TinyGo constraints
  (Spike 4's bake-off) now apply to `hosts/`, not here.
- Anything touching the filesystem goes through `platform.SafeJoin` / `platform.WriteTree` — never
  `os.WriteFile` with a caller-supplied path.
- The capability import seam is build-tagged: `caps_wasip2.go` / `caps_wasip1.go` / `caps_native.go`;
  the logic above the seam is identical across tiers.

See [plan](../docs/DEVALBO-ILC-GO-PLAN.md) §5, §5.3, §8, Decisions 28 + 29.
