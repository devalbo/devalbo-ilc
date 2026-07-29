# frontend/ — React + Vite web UI

The web tier's user interface. **Presentation only** — it drives the engine through the `hosts/web`
adapter (`execute(method, request)`); it holds **no business logic** (that's `engine/`).

The form *is* this tier's command parser (Decision 28): it collects fields, encodes a `NewRequest` with the
generated es-lite messages, and hands the bytes to the adapter. What `new` means lives in `engine/`, shared
with the CLI — which is why the browser and the terminal produce the same tree and the same error text.

**Run it:**

```bash
make dev-web        # build the component + serve the UI
make verify-web     # headless browser test: dlc new → OPFS → survives reload
```

**Derived, not source** (gitignored, produced by `make build-wasm` / `make gen-web`):
- `src/wasm/` — the jco-transpiled component
- `src/gen/` — es-lite messages copied from `gen/ts` (bundlers resolve `@aptre/*` from the *importing*
  file's tree, and `gen/` sits outside this root — Spike 2 finding)

`vite.config.ts` carries three non-obvious pieces: browser export conditions for the shim, a **pin** to the
patched `preview2-shim` filesystem, and `worker.format: "es"` (the engine worker uses dynamic import).

See [plan](../docs/DEVALBO-ILC-GO-PLAN.md) §5.1, §16.2, Decision 28.
