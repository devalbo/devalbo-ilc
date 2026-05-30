# ilc-compiler

Rust CLI: reads `wit/*.wit`, emits types into `packages/ilc-ts`, `packages/ilc-py`, `packages/ilc-rs`.

Phase 1 targets: `console-io` (interface), `environment` (world importing `console-io`).

Emitters map WIT kebab-case to **camelCase** (`ConsoleIo`, `consoleIo`, `readLine`).

## CLI

```bash
cargo run -p ilc-compiler -- compile ./wit --out ./packages
```

Writes:

- `packages/ilc-ts/src/generated/types.ts`
- `packages/ilc-py/src/devalbo_ilc/generated/types.py`
- `packages/ilc-rs/src/generated/types.rs`

## Tests

```bash
cargo test -p ilc-compiler
```

Golden snapshot: `compiler/src/emit/snapshots/`.

## Status

- [x] WIT → IR (`wit-parser`)
- [x] TypeScript emitter + golden test
- [x] Python emitter + golden test
- [x] Rust emitter + golden test
