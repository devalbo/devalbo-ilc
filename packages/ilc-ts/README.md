# @devalbo/ilc-ts (name TBD)

Generated `ConsoleIo`, `Environment`, and host factories (`createNodeEnvironment`, `createBrowserEnvironment`, `createDevToolsEnvironment`, `createTestEnvironment`).

Regenerate types:

```bash
cargo run -p ilc-compiler -- compile ../../wit --out ..
```

Generated output: `src/generated/types.ts` — do not hand-edit.

## Hosts

- `createNodeEnvironment()` — stdout / stderr / stdin
- `createServerlessEnvironment()` — logs only, `readLine` → `null`
- `createBrowserEnvironment()` / `createDevToolsEnvironment()` — `console.*` + `prompt()`
- `createTestEnvironment()` — in-memory logs + queued stdin

## Smoke

```bash
npm install
npm run hello:test    # in-memory test host
npm run hello:node    # Node process host
```

## Hello handler

```ts
import { hello, createTestEnvironment } from "@devalbo/ilc-ts";

await hello(createTestEnvironment());
```

Handlers use `env.consoleIo` only — never `console.log` / `process.stdout` directly.
