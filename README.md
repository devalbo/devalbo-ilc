# devalbo-ilc

> **⚠️ Direction change (2026-07):** This project is migrating to a **Go-centric** architecture — one
> shared Go `engine.wasm` + per-platform hosts (Go / TypeScript / C), the WASM Component Model as the
> injection substrate, and protobuf for serialization. The authoritative plan is
> **[`docs/DEVALBO-ILC-GO-PLAN.md`](docs/DEVALBO-ILC-GO-PLAN.md)**. The tri-language structure described
> below (Rust compiler + TS/Py/Rust packages) is **being retired** per §15 of that plan and remains only
> until the migration (Phase 0a) runs.

**Interface for a Line of Command (ILC)** — decouple handler logic from runtime (CLI, browser, DevTools, tests) via generated capability contracts and host-injected `Environment`.

Phase 1 defines **`ConsoleIo`** (logger output + stdin) and minimal **`Environment`** (`consoleIo` only), with WIT as the single schema and codegen into TypeScript, Python, and Rust.

## Repository

| Path | Purpose |
| --- | --- |
| [`wit/`](wit/) | WIT IDL (`console-io`, `environment`) |
| [`compiler/`](compiler/) | Rust `ilc` compiler (WIT → language packages) |
| [`packages/ilc-ts`](packages/ilc-ts/) | TypeScript SDK + hosts |
| [`packages/ilc-py`](packages/ilc-py/) | Python SDK + hosts |
| [`packages/ilc-rs`](packages/ilc-rs/) | Rust SDK + hosts |
| [`docs/archives/PHASE1.md`](docs/archives/PHASE1.md) | Phase 1 checklist and decisions |

## WIT (Phase 1)

```wit
// WIT (kebab-case): console-io — info, error, read-line
// environment: console-io() -> console-io
// emitted (camelCase): env.consoleIo() -> ConsoleIo; readLine()
```

Handlers never choose Node vs browser bindings — the **entry point** constructs `Environment` (`createNodeEnvironment`, `createBrowserEnvironment`, etc.).

## Status

- [x] WIT stubs for Phase 1
- [x] Repo layout
- [x] Rust compiler + golden tests (TypeScript emitter)
- [x] Generated types in `packages/ilc-ts`, `ilc-py`, `ilc-rs`
- [x] Cross-language smoke examples + process/test hosts

## Related docs

Planning and feasibility review live in [devalbo/site-devalbo.com `docs/lab/`](https://github.com/devalbo/site-devalbo.com/tree/main/docs/lab) (`DEVALBO-ILC.md`, `DEVALBO-ILC-PLAN.md`).

## License

See [LICENSE](LICENSE).
