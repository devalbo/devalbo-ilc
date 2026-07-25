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

- [x] Tag the current state: `git tag phase1-tri-language` (recoverable checkpoint)
- [x] Remove retired tri-language machinery: `compiler/`, `packages/ilc-{ts,py,rs}/`, root `Cargo.toml` + `Cargo.lock`
- [x] Remove `wit/environment.wit` and `wit/console-io.wit` (console is now WASI stdio — Decision 20)
- [x] Create `go.mod` — `module github.com/devalbo/devalbo-ilc`, `go 1.23` (TinyGo-compatible deps only in `engine/`)
- [x] Create the **flat bootstrap** directory skeleton — each major folder carries a **thin boundary README** (what belongs / what must NOT / link to plan §). Stay flat; defer the `platform/`+`apps/` split (§16.6) to App #2. Folders: `wit/ proto/ engine/ hosts/{native,web}/ frontend/ templates/ spikes/ scripts/`; `gen/` gitignored
- [x] Refresh `.gitignore` for Go/wasm/devbox: `/gen/`, `*.wasm`, `.devbox/`, `node_modules/`, `frontend/dist/`, `/target/` (transitional)
- [x] Rewrite `README.md` → real project README (present-tense), points at the Go plan / tasks / prereqs / test-steps; tri-language content removed
- [x] `devbox.json` core devshell (§4.1): Go, TinyGo, `wit-bindgen-go`, `wasm-tools`, buf + `protoc-gen-go-lite` + `protoc-gen-es-lite`, `wasmtime`, `nodejs` — authored (init_hook installs the go/npm-delivered plugins); *provisioning verified under `devbox shell` = T-B0.2, pending*
- [x] `make doctor` preflight — assert git/Nix/Devbox present + the devbox toolchain resolves; this is the **Layer 0** pre-toolchain gate (pure bash, no `dlc` needed) that gets you to a first `dlc`, later complemented by `dlc doctor` (Layer 1, Phase B2) ([`DEVALBO-DLC-PREREQUISITES.md`](./DEVALBO-DLC-PREREQUISITES.md))

**Exit:** clean working tree matching §3 (CLI+web subset); `devbox shell` provisions the toolchain; `make doctor` green.

---

## Phase B1 — De-risking spikes (§11 Phase-0 spikes, bootstrap subset)

Do these *before* committing to the build shape; any red spike reshapes the plan.

**Standing rule — document findings as part of the spike.** Every spike's findings go in `spikes/README.md`
(one section per spike), capturing *what worked · what we assumed and didn't · why · implications*. A finding that contradicts the
plan **updates the plan** (Spike 1 → wasip2-direct is the reference example). The writeup is a deliverable,
not optional — it's what turns a throwaway spike into durable regression + design knowledge.

- [x] **Spike 1 — component round-trip:** TinyGo **`-target=wasip2`** (native Component Model) → component; jco transpiles + runs a trivial `execute-cli` returning `"ok:hi"`. `spikes/component/`, `make spike-component` / `make test-b1`. **Pivoted off the wasip1+`wasm-tools` adapter path** — the guest `cabi_realloc` it requires is unimportable (`cm x/cabi` module mis-declared) and a hand-vendored one crashes pre-`_initialize`; wasip2 has TinyGo supply `cabi_realloc`+`_initialize`. World now `include`s `wasi:cli/imports@0.2.0`; WASI WIT vendored to `wit/deps/` via `wkg wit fetch` (see [test-steps T-B1.1 findings](./DEVALBO-DLC-TEST-STEPS.md))
- [x] **Spike 2 — protobuf-go-lite under TinyGo:** binary **and** canonical-JSON round-trip under `tinygo -target=wasip2`; `protobuf-es-lite` decodes the same bytes/JSON in JS. `proto/devalbo/spike/v1/spike.proto`, `spikes/proto/`, `make spike-proto` / `make test-b1`. Findings → [`spikes/README.md`](../spikes/README.md) (Spike 2): copy generated `.pb.ts` into the spike for Node resolution; `encoding/json` in the full spike deps comes from `cm`, not go-lite.
- [x] **Spike 3 — OPFS filesystem:** engine `os.WriteFile` persists via WASI preopen → OPFS and survives reload. `spikes/opfs/`, `make spike-opfs` / `make spike-opfs-watch` (headed). Findings → [`spikes/README.md`](../spikes/README.md): preview2-shim browser wants FileData not DirectoryHandle; stock shim breaks TinyGo writes on bigint offsets (vendored `shim/filesystem.js`)
- [x] **Spike 4 — in-engine CLI interpreter:** parser lives **inside** the TinyGo engine; host forwards argv → `execute-cli`. Bake-off measured (see [`spikes/README.md`](../spikes/README.md)): flag / ffcli / hand / go-arg matrix-green; cobra almost (fails `-name`); kong panics (`MethodByName`); subcommands unusable (hardcodes `os.Args`). **Default: ffcli** (hand / go-arg remain measured fall-backs if we split later). `spikes/cli/`, `make spike-cli` / `make test-b1`. Decision 22 + 25.
- [x] **Spike 5 — dual-track async probe:** **Rich 🟡** (jco Promise import gap / no JSPI runtime) · **Portable ✅** (TinyGo wasip1 + blocking `env.host_delay`; wazero stand-in for WAMR native fns). No ILC async shims. Findings → [`spikes/README.md`](../spikes/README.md) + [`WASI-UPGRADES.md`](./WASI-UPGRADES.md). `spikes/async/`, `make spike-async`. Decision 11 + 25.

**Exit:** Spikes 1–4 green; Spike 5 green **or** documented yellow/red ecosystem gap; **each with a findings section in `spikes/README.md`**; plan-contradicting findings folded back (as Spike 1 did).

---

## Phase B2 — `dlc` engine + terminal (CLI host)

### Contract & codegen
- [x] `wit/ilc.wit` — bootstrap world (Console via WASI stdio + Filesystem via WASI, provided by the target/host); `export execute-cli(args) -> command-result` (§6)
- [x] `proto/devalbo/ilc/v1/common.proto` — `IlcError` taxonomy; `proto/devalbo/dlc/v1/dlc.proto` — `new` / `export-fs` / `import-fs` messages (versioned packages, idiomatic buf layout)
- [x] `proto/buf.yaml` + `proto/buf.gen.yaml` (go-lite → `gen/go`, es-lite → `gen/ts`); `make gen` wires wit-bindgen-go + buf
- [x] **Validate under devbox:** `make gen` runs clean (`buf lint` + generate); shakes out `buf.gen.yaml` opts + go_package (Spike 2) — verified: TinyGo 0.41.1, buf `STANDARD` lint green with the versioned layout, go-lite + es-lite output in distinct `devalbo/*/v1` dirs
- [x] `wit-bindgen-go generate` produces the capability bindings in `gen/go` (Spike 1) — `gen/go/devalbo/ilc/{engine,types}/`

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
- [ ] `hosts/native/` — `main()`: **thin argv forwarder** — `os.Args` → wasmtime instantiates the component → `execute-cli(args)` → `command-result` → exit code (parsing is in-engine, Spike 4)
- [ ] Two-phase launch (§5.5): construct the native Environment (FS root = cwd/config dir, stdio) → invoke the engine
- [ ] Wire `dlc new <app> [--module …]` end-to-end
- [ ] `dlc doctor` — the **command form** of preflight (§16.7): assess system prereqs + per-tier toolchain/host readiness, exit non-zero if a prereq is missing. Layer 1 (assumes a `dlc` binary); `scripts/preflight.sh` stays as the pre-toolchain **Layer 0** bootstrap gate that gets you a first `dlc` ([`DEVALBO-DLC-PREREQUISITES.md`](./DEVALBO-DLC-PREREQUISITES.md))

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
- [ ] **ABI-mode toggle at setup** (§5.6, Decision 25): `dlc new` derives portable byte-ABI vs rich Component-Model ABI from the `tiers` (`--wamr` forces portable); scaffold the matching capability-boundary + build targets (wasip1 core seam only when portable); a lint/check that keeps a portable-mode engine core-wasm-safe
- [→] `dlc doctor` — **promoted to Phase B2** (CLI host): the command form of preflight, assessing per-tier readiness — [`DEVALBO-DLC-PREREQUISITES.md`](./DEVALBO-DLC-PREREQUISITES.md)

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
