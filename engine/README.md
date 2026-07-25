# engine/ — the shared business logic (Go → wasm)

The ONE portable artifact (`engine.core.wasm`). All app logic lives here and **nowhere else**.

**Rules:**
- Imports the ILC capability world (`gen/`); **never** calls a platform API directly.
- MUST stay **TinyGo-safe and reflection-free** — no `alecthomas/kong`, no `encoding/json`, no
  reflection-heavy deps. (That's why serialization is protobuf-go-lite.) CLI parsing lives in the host.
- The capability import seam is build-tagged: `caps_wasip2.go` / `caps_wasip1.go` / `caps_native.go`;
  the logic above the seam is identical across tiers.

See [plan](../docs/DEVALBO-ILC-GO-PLAN.md) §5, §5.3, Decision 22.
