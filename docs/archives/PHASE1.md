# Phase 1 — ConsoleIo + Environment

Source of truth: `wit/console-io.wit` (interface), `wit/environment.wit` (world importing `console-io`).

## Locked decisions

| Topic | Choice |
| --- | --- |
| **Naming** | WIT source: kebab-case (`console-io`, `read-line`). **Emitted API: camelCase** — type `ConsoleIo`, accessor `env.consoleIo()`, method `readLine`. |
| Output | `info` / `error` (void, logger-shaped) |
| Input | WIT `read-line()` → emitted `readLine()` → `option<string>` (`none` = EOF) |
| Node host | `info` → stdout, `error` → stderr, `readLine` → stdin |
| Browser / DevTools | `info` → `console.log`, `error` → `console.error`, `readLine` → `window.prompt()` |
| Handlers | Use `env.consoleIo` only — never runtime I/O imports |
| Emit | Separate packages: `packages/ilc-ts`, `ilc-py`, `ilc-rs` |

## Layout

```
wit/           WIT IDL (not compiled to WASM)
compiler/      Rust `ilc` CLI (`cargo run -p ilc-compiler -- compile wit --out packages`)
packages/      Per-language generated + host SDK
```

Design notes and review: [site-devalbo.com `docs/lab/DEVALBO-ILC-PLAN.md`](https://github.com/devalbo/site-devalbo.com/blob/main/docs/lab/DEVALBO-ILC-PLAN.md) (sibling planning repo).

## Exit criteria

- [x] `ilc compile` updates all three language packages from WIT
- [x] Golden snapshot tests (TypeScript, Python, Rust emitters)
- [x] Smoke handler + hosts (TS: Node / browser / test; Py/Rust: process / test)
- [ ] DevTools pasteable workflow documented (browser host exists as `createDevToolsEnvironment`)
