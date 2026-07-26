# engine/ — the shared business logic (Go → wasm)

The ONE portable artifact (`engine.core.wasm`). All app logic lives here and **nowhere else**.

**Entry point:** `ExecuteMethod(method, request)` — a permanent `method_id` plus flat proto-encoded
request bytes, dispatched through the `map[uint32]Handler` registry (`registry.go`, Decision 29). The
engine never sees argv: **parsing is host-side** (Decision 28, superseding Decision 22). `Execute(args)`
is a transitional argv shim that retires when the hosts build requests.

**Rules:**
- Imports the ILC capability world (`gen/`); **never** calls a platform API directly.
- MUST stay **TinyGo-safe**. Serialization is **reflection-free** (protobuf-go-lite — no `encoding/json`).
  The registry's typed-handler adapter uses **generics, not reflection**, for the same reason.
- Handlers are what must compile under TinyGo. The parser moved host-side, so its TinyGo constraints
  (Spike 4's bake-off) now apply to `hosts/`, not here.
- The capability import seam is build-tagged: `caps_wasip2.go` / `caps_wasip1.go` / `caps_native.go`;
  the logic above the seam is identical across tiers.

See [plan](../docs/DEVALBO-ILC-GO-PLAN.md) §5, §5.3, §8, Decisions 28 + 29.
