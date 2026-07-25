# DEVALBO-DLC — Implementation Tasks

Task breakdown derived from [`DEVALBO-ILC-GO-PLAN.md`](./DEVALBO-ILC-GO-PLAN.md) (authoritative). Naming:
**ILC** = the framework; **`dlc`** = the CLI tool (Devalbo Line of Command).

**Current focus: the BOOTSTRAP.** Everything else lives in [Backlog / Next implementation](#backlog--next-implementation).

_Created 2026-07-25._

---

## 🎯 Bootstrap milestone (definition of done)

**`dlc` (App #1) runs in the terminal *and* the browser, from one shared engine, using only Console +
Filesystem, with a React UI on the web tier.**

Concretely:
- **Terminal:** `dlc new myapp` scaffolds a working project to disk; `dlc` output goes to stdio.
- **Browser:** `dlc` runs under jco with a **React UI**, scaffolds into **OPFS**, survives reload, and can
  download the result.
- **One artifact:** the same `engine.core.wasm` drives both (component under wasmtime for CLI, jco for web).
- **Capabilities:** Console (WASI stdio) + Filesystem (WASI) only — no SQLite, Events, Display, or embedded.

Reference tiers only for bootstrap: **CLI + Web**. (Desktop, embedded, and the notes app are Backlog.)

---

## Phase B0 — Repo migration & scaffolding (§2, §12 Phase 0a)

- [ ] Tag the current state: `git tag phase1-tri-language` (recoverable checkpoint)
- [ ] Remove retired tri-language machinery: `compiler/`, `packages/ilc-{ts,py,rs}/`, root `Cargo.toml` + `Cargo.lock`
- [ ] Remove `wit/environment.wit` and `wit/console-io.wit` (console is now WASI stdio — Decision 20)
- [ ] Create `go.mod` (module path; TinyGo-compatible deps only in `engine/`)
- [ ] Create the directory skeleton (§3): `wit/`, `proto/`, `engine/`, `gen/` (gitignored), `hosts/{native,web}/`, `frontend/`, `Makefile`
- [ ] Extend `.gitignore` (§2.3): `/gen/`, `*.wasm`, `.devbox/`, `frontend/dist/`
- [ ] Rewrite `README.md` → point at the Go plan; mark `DEVALBO-ILC-GO-PLAN.md` authoritative
- [ ] `devbox.json` core devshell (§4.1): Go, TinyGo, `wit-bindgen-go`, `wasm-tools`, buf + `protoc-gen-go-lite` + `protoc-gen-es-lite`, `wasmtime`, `nodejs`
- [ ] `make doctor` preflight — assert git/Nix/Devbox present + the devbox toolchain resolves ([`DEVALBO-DLC-PREREQUISITES.md`](./DEVALBO-DLC-PREREQUISITES.md))

**Exit:** clean working tree matching §3 (CLI+web subset); `devbox shell` provisions the toolchain; `make doctor` green.

---

## Phase B1 — De-risking spikes (§11 Phase-0 spikes, bootstrap subset)

Do these *before* committing to the build shape; any red spike reshapes the plan.

- [ ] **Spike 1 — component round-trip:** TinyGo `-target=wasip1` → `wasm-tools` adapter → component; jco transpiles + runs a trivial `execute-cli` returning a value
- [ ] **Spike 2 — protobuf-go-lite under TinyGo:** binary **and** canonical-JSON round-trip compiles + runs under `tinygo -target=wasip1`; `protobuf-es-lite` decodes the same bytes in JS
- [ ] **Spike 3 — OPFS filesystem:** `setPreopens({'/': opfsRoot})` lets the engine's `os.WriteFile` persist to OPFS and survive a page reload
- [ ] **Spike 4 — kong boundary:** kong (standard Go, native host) parses argv → proto `TInput` → hands bytes to the wasmtime-hosted engine (confirms kong stays host-side, engine reflection-free — Decision 22)
- [ ] **Spike 5 — async in browser:** an engine capability call that "blocks" works under jco via TinyGo's Asyncify (no event-loop deadlock)

**Exit:** all five green (or the plan is adjusted with findings recorded).

---

## Phase B2 — `dlc` engine + terminal (CLI host)

### Contract & codegen
- [ ] `wit/ilc.wit` — bootstrap world: `import wasi:filesystem`, `wasi:cli` stdio; `export execute-cli(args) -> command-result` (§6)
- [ ] `wit-bindgen-go` generates the capability bindings into `gen/`
- [ ] `proto/common.proto` — `command-result`, `IlcError` (`buf.yaml`, `buf.gen.yaml` with go-lite + es-lite)
- [ ] `proto/dlc.proto` — messages for the bootstrap commands (`new`, `export-fs`, `import-fs`)
- [ ] `buf generate` wired into the Makefile (go-lite → `gen/go`, es-lite → `gen/ts`)

### Engine (Go → wasm; business logic only)
- [ ] `engine/main.go` — implements `execute-cli`; reflection-free dispatch (route table + go-lite decode)
- [ ] `engine/caps_wasip2.go` / `caps_wasip1.go` build seam for capability imports (§5.3)
- [ ] `export-fs` / `import-fs` handlers over the WASI filesystem (§7.3) — needed because scaffolding = `import-fs`
- [ ] Scaffolding handler (`new`): `import-fs` a template bundle → write tree → token-substitute (`{{.Module}}`, `{{.AppName}}`)

### Templates (its own area, §16.6)
- [ ] `templates/` dir with a **minimal self-shaped skeleton** (engine + CLI host stub + go.mod + devbox + wit + proto)
- [ ] `go:embed` the templates into the `dlc` engine so `dlc new` is self-contained (offline + browser)

### Build pipeline
- [ ] Makefile: `build-engine` (TinyGo → `engine.core.wasm`) + `component` (wasm-tools adapter → `engine.component.wasm`)

### CLI host (native Go + wasmtime — standard Go, full reflection)
- [ ] `hosts/native/` — `main()`: **kong** parses argv → proto `TInput` → wasmtime instantiates the component → run → `command-result` → exit code
- [ ] Two-phase launch (§5.5): construct the native Environment (FS root = cwd/config dir, stdio) → invoke the engine
- [ ] Wire `dlc new <app> [--module …]` end-to-end

### Verify (terminal)
- [ ] `dlc new myapp` produces a buildable project tree on disk
- [ ] Define the **golden FS snapshot** for a known `dlc new` invocation (§11 Scaffolder row)

**Exit:** `dlc new myapp` scaffolds + runs a working CLI project from the terminal; golden FS snapshot passes.

---

## Phase B3 — `dlc` in the browser + React UI (web host)

### Web host (TypeScript — glue + UI only, no business logic)
- [ ] `hosts/web/worker.ts` — jco instantiate the component + `preview2-shim`; `setPreopens({'/': opfsRoot})` (OPFS)
- [ ] Inject capabilities: WASI stdio → `console.*`; filesystem → OPFS
- [ ] Expose the engine to the main thread via **Comlink**
- [ ] `hosts/web/api.ts` — environment-agnostic adapter (`executeCli(args)`)

### React UI (the UI capability)
- [ ] `frontend/` — React + Vite app that drives `dlc` via the adapter
- [ ] A scaffolding UI: run `dlc new`, browse the generated tree in OPFS, show console output
- [ ] **Download the result** — `export-fs` the OPFS tree → zip/BFT → browser download

### Async + build
- [ ] Asyncify path verified in the real app (from Spike 5)
- [ ] Makefile: `build-wasm` (TinyGo → jco transpile → `frontend/src/wasm/`), `dev-web` (Vite)

### Verify (browser + cross-tier)
- [ ] `dlc new` runs in the browser; project persists in OPFS across reload
- [ ] React UI renders and drives the engine; DevTools shows no host-capability errors
- [ ] **Cross-tier identity:** the `engine.core.wasm` used by CLI and web is byte-identical (sha256); a shared golden vector produces identical `command-result` bytes on both

**Exit:** `dlc` runs in terminal **and** browser (React UI) from one engine — the bootstrap milestone is met.

---

## Bootstrap exit criteria (roll-up)

- [ ] One `engine.core.wasm` drives CLI (wasmtime component) and web (jco) — byte-identical
- [ ] `dlc new myapp` works from the terminal (scaffold to disk) and the browser (scaffold to OPFS + download)
- [ ] React UI on the web tier drives the engine
- [ ] Capabilities limited to Console + Filesystem; graceful behavior where a cap is absent
- [ ] `devbox run verify` green for CLI + web; golden FS snapshot stable

---

## Backlog / Next implementation

Deferred until after the bootstrap. Grouped; roughly priority-ordered within each group.

### Second app — notes/list (App #2, the breadth pilot) — §13
- [ ] Scaffold notes/list **with `dlc new`** (proves the scaffolder on a real app)
- [ ] `proto/record.proto`; handlers: `create/list/open/update/delete-record`, `rebuild-index`
- [ ] Drives out the capabilities/tiers below; `dlc new`'s flags co-evolve (§16.4)

### Capabilities
- [ ] **SQLite-index** (§6.2): native `modernc.org/sqlite`; web `@sqlite.org/sqlite-wasm` (OPFS); `unavailable` fallback → file scan
- [ ] **Split-storage** write flow + `rebuild-index` (§7.1): lock-file discipline, atomic writes
- [ ] **Events** capability + reactivity loop (§6.3): `data-changed` → UI invalidate/refetch (`useEngineEvent`)
- [ ] **Display** capability (§6.4): `describe()` + draw-command list + retained widget tree
- [ ] **Network** (deferred): `wasi:http` when needed

### Tiers / hosts
- [ ] **Desktop** host — Wails v2 (webview + native Environment) — §5.4, §10
- [ ] **ESP32-S3** — WAMR (official ESP-IDF component) + PlatformIO C host firmware; TFT Display; serial REPL — §4, §5.3
- [ ] **RP2350** — WAMR-via-arduino-pico *or* native TinyGo fallback (§14 risk 2)
- [ ] **RP2040** — native TinyGo build (no wasm) — §5.3
- [ ] WAMR embedded spike (the deferred §11 spike 3)

### Filesystem export/import (§7.3)
- [ ] `--format=zip` and `--format=proto` (BFT is bootstrap; these are additive)
- [ ] BFT **deflate** variant (size)
- [ ] Two-apps/versions **BFT interchange** workflow (diff/migrate/merge)

### Platform & tooling
- [ ] Extract **`ilc-platform`** module once App #2 shares it (§16.4) — templates depend-on, never inline
- [ ] `dlc` command surface: `dlc build`, `dlc verify`, `dlc proto`, `dlc host add <tier>` (§16.7)
- [ ] **Project manifest** `dlc.toml` (§16.8) — capabilities/tiers/storage/ui/launch/platform pin; drives build/verify/host-add/launch
- [ ] Regenerate / upgrade (re-apply templates against a newer `platform` pin)
- [ ] Scaffolder verify in CI: `dlc new … → devbox run verify` (§11 Scaffolder row)
- [ ] `--tiers` / `--caps` / `--ui` / `--storage` flag expansion + fragment overlays (§16.6)
- [ ] FSA-granted directory backend for the web host (write to a user-picked local folder — Chromium) — §5.2
- [ ] `dlc doctor` — automate the prerequisite/preflight assessment (per-tier readiness) — [`DEVALBO-DLC-PREREQUISITES.md`](./DEVALBO-DLC-PREREQUISITES.md)

### Dispatch / distributed (Phase 5 territory)
- [ ] Protobuf **envelope** + multi-handler routing + explicit registration (§8, §10 Decision 10)
- [ ] **File LWW sync** (§9); Automerge per-doc as the upgrade path
- [ ] Symmetric **Input** capability (beyond host-side input-map) — §14 risk 5
- [ ] Cross-language: Rust/C-via-WASI handlers on the neutral WIT boundary (§4, §15)

### WASI standards adoption (§6.6)
- [ ] Test host via **WASI Virt** (compose virtual FS + captured stdio) on wasip2 tiers
- [ ] Mirror/adopt: `wasi:messaging` (events), `wasi-gfx` (display), `wasi:keyvalue` (index) as they stabilize
- [ ] Standard `wasi:cli/command` `run()` entry for the pure one-shot CLI

### Deferred hosts
- [ ] Serverless host (Lambda-style entry; `readLine`→`none`) — §5.4
