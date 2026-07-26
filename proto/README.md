# proto/ — message types (the app surface)

Protobuf schemas for handler I/O and shared types, laid out the idiomatic buf way — versioned packages in
matching directories: `devalbo/ilc/v1/common.proto` (`IlcError` + shared types),
`devalbo/<app>/v1/commands.proto` (e.g. `devalbo/dlc/v1/commands.proto` — the `service` + `method_id`
command surface, Decision 29), `devalbo/options/v1/options.proto` (the `method_id` / field-metadata
custom options every app's command schema imports), and spike-only
`devalbo/spike/v1/spike.proto` (T-B1.2). Packages carry a `vN` suffix so `buf lint`
(`STANDARD`) passes and the wire format can evolve.

**Codegen (`buf`):**
- **`protoc-gen-go-lite`** → Go (engine + native host) — **reflection-free / TinyGo-safe** (the reason we
  don't use the official generator).
- **`protoc-gen-es-lite`** → TypeScript (web host).
- On disk: proto3 **canonical JSON**; on the wire: binary. `buf lint` + `buf breaking` guard evolution.

See [plan](../docs/DEVALBO-ILC-GO-PLAN.md) §7.2.
