# proto/ — message types (the app surface)

Protobuf schemas for handler I/O and shared types. `common.proto` (result + `IlcError`) +
`<app>.proto` (e.g. `dlc.proto`).

**Codegen (`buf`):**
- **`protoc-gen-go-lite`** → Go (engine + native host) — **reflection-free / TinyGo-safe** (the reason we
  don't use the official generator).
- **`protoc-gen-es-lite`** → TypeScript (web host).
- On disk: proto3 **canonical JSON**; on the wire: binary. `buf lint` + `buf breaking` guard evolution.

See [plan](../docs/DEVALBO-ILC-GO-PLAN.md) §7.2.
