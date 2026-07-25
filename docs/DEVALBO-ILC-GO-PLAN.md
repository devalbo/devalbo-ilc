# DEVALBO-ILC — Go Plan

A Go-centric re-plan of ILC (Inverted Line of Command). Supersedes the tri-language / Rust-lead
direction in [`DEVALBO-ILC-PLAN.md`](./archives/DEVALBO-ILC-PLAN.md) for the purpose of this build; incorporates
[`DEVALBO-ILC-WITH-GO-DRAFT.md`](./archives/DEVALBO-ILC-WITH-GO-DRAFT.md),
[`DEVALBO-ILC-WITH-GO-2-DRAFT.md`](./archives/DEVALBO-ILC-WITH-GO-2-DRAFT.md) (split-storage / WASI-filesystem /
SQLite-index / events), and the design interview recorded 2026-07-24.

_Plan date: 2026-07-24._

> **How this plan is organized:** it **leads with setup & organization** so the repo can be cleaned and
> streamlined first — §2 repo cleanup → §3 target structure → §4 toolchain & dev-env. Then the
> **architecture & capability design** (§5–§10), then **verification, phases & pilot** (§11–§13), then
> **reference** (§14–§15). §0–§1 are orientation (thesis + locked decisions). This plan is **both a reusable
platform and a reference app** (§0.1); **§16** covers spinning up new apps from it.

> **Naming:** the framework/concept is **ILC** (Inverted Line of Command); its devalbo CLI is **`dlc`**
> (**D**evalbo **L**ine of **C**ommand). The tool binary, its subcommands, and the project manifest
> (`dlc.toml`) use `dlc`; **framework artifacts keep the `ilc` name** — `ilc.wit`, the `devalbo:ilc` WIT
> package, and the `ilc-platform` module.

---

## 0. Thesis

**One business-logic language (Go), one portable artifact, many hosts.** Instead of a bespoke
WIT→3-language compiler and hand-written per-language SDKs, ILC-Go leans on the **WASM Component Model as
the real runtime substrate for capability injection** and on **protobuf as the one serialization story**
(disk + wire + capability boundary). The "environment" a handler runs in is no longer implicit — it is a
set of capabilities a per-platform **host** wires into a portable **engine**.

This makes the Component Model *be* the injected `Environment` (enforced by the sandbox, not by
convention), and collapses most of the old cross-language tooling: `wit-bindgen-go` +
`buf`/`protoc-gen-go` + standard component runtimes replace the custom Rust compiler.

### The two bits (the load-bearing idea)

| Bit | What | Language | Portability |
| --- | --- | --- | --- |
| **Engine** | All business logic; imports the ILC capability world; exports handlers | **Go** (→ TinyGo wasm) | **environment-independent**, one shared `engine.wasm` |
| **Host / Environment** | Injects the capabilities the engine imports; owns platform specifics | **Go** (native/desktop/CLI/RP2040) · **C** (ESP32-S3, ESP-IDF/WAMR) · **TypeScript** (web glue+UI) | **per-platform** (a Component-Model host is *supposed* to be) |

The engine **core module** is byte-identical across every WASM tier (the wasip2 component is derived from it
via the adapter; RP2040 links it natively); the host is whatever each platform needs.
**The host language is free — Go, TypeScript, or C — because a host only *provides* capabilities; all
business logic is the Go engine.** Business logic never lives in TS or C.

### 0.1 Platform vs App — this plan is a template

This plan does double duty: a **reusable platform** (spin up many apps) and a **reference app** (the
notes/list pilot that proves it). The two-bit split *is* the template boundary — the hosts + capability
contract are the platform; the engine is the app.

| Platform (written once, reused) | App (swapped per instance) |
| --- | --- |
| Capability WIT world + `caps_*.go` build seam (§6, §5.3) | Engine handlers — the business logic (§13 is one example) |
| Host adapters: native / web / desktop / WAMR (§10) | The app's `.proto` messages (§7.2) |
| Build pipeline: core-module + adapter + native (§5.3) | **Which capabilities** it uses — opt-in |
| Toolchain / Devbox / buf (§4) | **Which tiers** it targets — opt-in (§10) |
| Verification harness, per tier (§11) | Its UI (React / TFT / none) |
| CLI shim + universal `execute-cli` (§8) | *Optionally* a storage pattern (§7.1) |

A new app sets two knobs — **capabilities used** and **tiers targeted** — and inherits everything else;
start as a CLI, add UI or embedded later without touching the engine (**§16**).

**App-choice decisions (not platform invariants):** in §1, Decisions **7** (split-storage), **16** (the
pilot), and **17** (tiers) are *app choices* — a different app may use no persistence, a different message
set, or fewer tiers. The rest are platform invariants.

---

## 1. Locked decisions (from the 2026-07-24 interview)

| # | Decision | Choice |
| --- | --- | --- |
| 1 | Implementation language | **Go** for all business logic (the engine); host glue may be **TS** (web) or **C** (ESP32-S3) — neither carries business logic |
| 2 | Runtime substrate | **Component Model** on web/desktop/CLI (jco / wasmtime); **core WASM + WASI Preview 1 + native host functions** on WAMR embedded — WAMR has **no** CM / WASI-0.2 yet (tracked divergence, §5.3) |
| 3 | Artifact model | Shared unit = **WASI-p1 core module** (`engine.core.wasm`); wasip2 runtimes componentize it via the **preview1 adapter**; WAMR runs the core module directly; RP2040 links native. Per-platform host (the "two bits"). |
| 4 | Capability boundary | **Neutral WIT world** — polyglot-capable; Rust/C-via-WASI can interop |
| 5 | Embedded floor | **Go/WASM is the floor** (no Rust `no_std` tier); Rust-via-WASI still welcome |
| 6 | FileSystem | **WASI standard** via Go `os`; host `setPreopens` the root — *not* a custom capability |
| 7 | Storage model | **Split:** JSON files = source of truth; **SQLite = disposable derived index** |
| 8 | On-disk format | **Proto-schema'd canonical JSON** (protobuf governs shape/evolution; binary proto on the wire) |
| 9 | Sync | **Plain file LWW** (sync JSON docs, rebuild index locally); no CRDT-SQLite |
| 10 | Dispatch | **Local-only in V1** (no envelope/routing yet) |
| 11 | Async bridge | **Asyncify/JSPI in the browser** (TinyGo already uses Asyncify); native/embedded block freely |
| 12 | Capability set | WASI FS · SQLite-index · Events · Display · **Console** (WASI stdio, not a custom interface — §6.5) |
| 13 | Display | **Discovery (`describe`) + draw-command list + retained widget tree**; output-only |
| 14 | Input | **Universal `execute-cli`**; host maps native input → command (serial REPL / input-map on MCUs) |
| 15 | Reactivity | Engine emits **events**; host subscribes; UI invalidates + re-fetches |
| 16 | Pilot | **Shared notes/list** local-first app |
| 17 | Tiers | **All six supported in V1** (Web · Desktop · CLI · ESP32-S3 · RP2350 · RP2040); an *app* picks a subset (§16.1) |
| 18 | Embedded exec | **Mixed by capacity:** ESP32-S3 / RP2350 → WAMR+wasm; RP2040 → native TinyGo |
| 19 | Compiler | **Retire** the custom Rust WIT→3-lang compiler; use `wit-bindgen-go` + `buf` |
| 20 | WASI standards reuse | Console → **WASI stdio** (drop custom `console-io`); CLI entry → **`wasi:cli/command`** + a custom persistent `execute-cli` export; test host → **WASI Virt**; `sqlite-host` / `event-host` / `display-host` mirror **wasi:keyvalue** / **wasi:messaging** / **wasi-gfx** shapes (§6.6) |
| 21 | Filesystem export/import | **First-class platform primitive** (§7.3): app state = a portable FS bundle (`--format=bft\|zip\|proto`) for test setup/teardown, golden snapshots, backup/restore, bug repro, cross-tier migration, node bootstrap, and **BFT interchange when 2 apps/versions share a store**. Engine-side, tier-agnostic (uses only the filesystem cap). `dlc new` = importing a template bundle. |
| 22 | Go CLI framework | **kong** (`alecthomas/kong`) — the Typer analog — **host-side only (standard Go)** for `dlc` + the per-app CLI shim (parse → typed struct → proto `TInput`). **Not compiled into the TinyGo engine** — kong is reflection-heavy and TinyGo's `reflect` is partial (no `encoding/json`). The **engine's `execute-cli` dispatch is reflection-free / TinyGo-safe** (generated route table + protobuf-go-lite, or a minimal `argv→TInput` mapper). Bind-don't-supplant: cobra / urfave / go-arg (all host-side). |
| 23 | Two-phase launch | Starting an app = **(1) launch the Environment** (host wires caps + mounts the FS root, optionally `import-fs`-seeded) then **(2) run the engine** (one-shot `execute-cli`→exit, or persistent → many invocations). One command does both; splittable for test / persistent / dev (§5.5). |
| 24 | Project metadata | An **`dlc.toml`** (or proto-schema'd `dlc.json`) manifest is the app's config source of truth — capabilities, tiers, storage, UI, launch mode/seed, and the pinned `platform` version (§16.8). `dlc` commands read/write it; it drives scaffold / build / verify / host-add / launch and enables regenerate/upgrade. |

---

## 2. Repo cleanup & migration (do this first)

This repo currently holds the **retired tri-language Phase-1** implementation: the Rust WIT→3-language
`compiler/`, the `ilc-ts` / `ilc-py` / `ilc-rs` SDK packages, a root Cargo workspace, and the original
`console-io` / `environment` WIT. The Go direction (Decision 19) drops that machinery. **Clean and
streamline the repo first**, before any new build. Cleanup is **reversible** — tag first, then remove — so
nothing is lost. **This section is plan-only; nothing here is executed yet.**

### 2.1 Disposition of existing files

| Path | Disposition | Why |
| --- | --- | --- |
| `compiler/` (Rust `dlc` WIT→3-lang) | **Remove** | Retired (Decision 19); `wit-bindgen-go` + `buf` replace it |
| `packages/ilc-ts`, `ilc-py`, `ilc-rs` | **Remove** | Tri-language matrix dropped; the web host is *new* TS under `hosts/web/`, not this SDK. Old `ilc-ts` host shapes stay a **reference** (recoverable via the tag), not reused code. |
| `Cargo.toml`, `Cargo.lock` (root) | **Remove** | Existed only for `compiler` + `ilc-rs`; the Go plan has no root Rust workspace |
| `wit/environment.wit` | **Replace** | `world environment` is superseded by the new `world engine` (§6) |
| `wit/console-io.wit` | **Remove** | Console is now **WASI stdio** (Decision 20), not a custom interface; the new `wit/ilc.wit` imports `wasi:cli` stdio instead |
| `README.md` | **Rewrite** | Currently describes tri-language ILC; retarget to the Go plan + two-bit architecture, and mark **`DEVALBO-ILC-GO-PLAN.md` authoritative** |
| `.gitignore` | **Extend** | Add Go/wasm/gen/node/devbox/platformio/Wails artifacts (§2.3) |
| `docs/*` | **Keep (history)** | `DEVALBO-ILC-GO-PLAN.md` is authoritative; the superseded docs now live under `docs/archives/` |
| `LICENSE` | **Keep** | — |

### 2.2 New scaffolding to create (the target is §3)

`go.mod` · `wit/ilc.wit` · `proto/{common,record,display}.proto` + `buf.yaml` / `buf.gen.yaml` ·
`engine/` · `gen/` (gitignored) · `hosts/{native,desktop,web,embedded}/` (embedded carries a
`platformio.ini`) · `frontend/` (React + Vite) · `Makefile` · `devbox.json`.

### 2.3 `.gitignore` additions

```gitignore
/gen/                   # generated wit-bindgen-go + buf output
*.wasm                  # engine build artifacts
.devbox/                # devbox state
hosts/embedded/.pio/    # platformio build
frontend/dist/
hosts/desktop/build/    # wails output
```

### 2.4 Recommended sequence (Phase 0a, before spikes)

1. `git tag phase1-tri-language` — preserve the retired work, recoverable forever.
2. `git rm -r compiler packages Cargo.toml Cargo.lock` — the tri-language machinery.
3. Rework `wit/` (add `ilc.wit`, delete `environment.wit`), rewrite `README.md`, extend `.gitignore`.
4. Scaffold §2.2 (`go.mod`, `devbox.json`, the directory skeleton).
5. Proceed to the Phase-0b spikes (§11) in the clean tree.

---

## 3. Target directory structure

```text
/
├── wit/
│   └── ilc.wit                     # the capability world (§6)
├── proto/
│   ├── common.proto                # shared types + errors
│   ├── record.proto                # notes/list domain (pilot)
│   ├── display.proto               # DrawList + WidgetTree
│   └── buf.yaml / buf.gen.yaml      # local buf (lint + breaking=WIRE_JSON; gen go/ts)
├── engine/                         # THE SHARED ENGINE (Go, → wasm)  ── business logic only
│   ├── main.go                     # implements exported execute-cli; imports generated WIT + proto
│   ├── records.go, index.go, ...   # handlers; use os + injected caps only
│   └── (build: TinyGo → engine.wasm; native TinyGo for RP2040)
├── gen/                            # generated: wit-bindgen-go (caps) + buf (proto) for go & ts
├── hosts/
│   ├── native/                     # Go host: wasmtime + os FS + modernc sqlite (desktop/CLI)
│   ├── desktop/                    # Wails wrapper over hosts/native + webview
│   ├── web/                        # TypeScript host: worker.ts (jco + OPFS + sqlite-wasm), api.ts, hooks/
│   └── embedded/                   # ESP32-S3/RP2350: C host (ESP-IDF/arduino-pico) embedding WAMR + platformio.ini;
│                                    #   RP2040: native TinyGo (Go) linking the engine
├── frontend/                       # React + Vite UI (web + desktop webview)
└── Makefile                        # build + verify targets per environment (§11)
```

> **Per-app vs repo-level:** this is the **per-app** layout. The bootstrap repo root also carries
> `scripts/`, `spikes/`, `templates/`, `docs/`, `devbox.json`, `go.mod`, and a **thin boundary README in
> each major folder**. Once the platform is extracted (§16.4) the *repo* splits into `platform/` +
> `templates/` + `apps/` (§16.6), and the app tree above lives under each `apps/<name>/`.

---

## 4. Toolchain & dev environment

| Concern | Tool |
| --- | --- |
| Capability bindings | **`wit-bindgen-go`** (WIT → Go), standard component tooling |
| Proto codegen (Go) | **`buf`** + **`protoc-gen-go-lite`** (Aperture Robotics) — **reflection-free**, TinyGo-safe; one generator emits **binary + canonical JSON + size/clone/equal**. Replaces `protoc-gen-go` entirely (do **not** run the official generator). |
| Proto codegen (TS) | **`protoc-gen-es-lite`** (`@aptre/protobuf-es-lite`) — matching reflection-free TS/JS, built to pair with `protobuf-go-lite` (same maintainer). Web host only. |
| Engine → wasm | **TinyGo** — `-target=wasip1` core module (the shared unit); componentize for wasmtime/jco via the **`wasm-tools` preview1 adapter** (or `-target=wasip2` directly). WAMR runs the p1 core module as-is. |
| Web instantiation | **`@bytecodealliance/jco`** transpile + `preview2-shim` (WASI/OPFS) |
| Web SQLite | **`@sqlite.org/sqlite-wasm`** (OPFS) |
| Native/desktop/CLI runtime | **wasmtime** (Go embedding) + `modernc.org/sqlite` + **Wails v2** |
| Embedded runtime | **WAMR** on ESP32-S3 / RP2350 (official Espressif WAMR ESP-IDF component); **TinyGo native** (RP2040) |
| Embedded build/flash/monitor | **PlatformIO** — host firmware (ESP-IDF / arduino-pico), flashing, `pio device monitor` (the serial REPL); TFT libs (TFT_eSPI/LovyanGFX) for the Display host. TinyGo flash for RP2040. |
| Async in browser | **Asyncify** (TinyGo built-in) / JSPI |
| Test host (wasip2) | **WASI Virt** — compose a virtual FS + captured stdio onto the component (standardized test injection) |
| CLI framework (Go) | **kong** (`alecthomas/kong`) — declarative struct→CLI (the Typer substitute); the `dlc` CLI + the per-app CLI shim. cobra / urfave / go-arg are drop-in alternatives (bind-don't-supplant) |
| Reproducible dev env | **`devbox`** (Jetify) — Nix-backed, pins the whole toolchain via `devbox.json`; PlatformIO owns the board toolchains (§4.1); the Docker alternative |

**Why the lite generators (resolves the old #1 risk):** the official `google.golang.org/protobuf` is
reflection-heavy and does **not** work under TinyGo (TinyGo implements only a subset of `reflect` —
tinygo issues #2667, #3376). `protobuf-go-lite` + `protobuf-es-lite` are reflection-free static codegen
built for exactly this; go-lite's bundled JSON output *also* satisfies the §7.2
canonical-JSON-on-disk decision from a single generator. **Constraint to respect:** go-lite drops
protobuf **fieldmasks and extensions** — V1's message surface (records, display commands, events,
command results) needs neither; don't introduce a dependency on them.

**Remaining maturity risk (tracked):** the *component-model* toolchain — TinyGo component output +
`wit-bindgen-go` + jco + WAMR's component support — is still young (see §14). The protobuf half is now a
narrow validate-not-invent spike (§11 Phase 0, spike 2).

### 4.1 Dev environment — Devbox (Nix-backed), not Docker

This toolchain is wide (TinyGo, `wit-bindgen-go`, `wasm-tools`, `buf` + two lite plugins, Node for jco,
`wasmtime`, `modernc`, Wails, WAMR, and per-board embedded flashers). Pin it with **Devbox** (Jetify) —
Nix + nixpkgs underneath, so you get exact-version reproducibility, but the config is a simple
`devbox.json` (list packages by name, lockfile-pinned) instead of hand-written Nix. Docker ships an opaque
image; Devbox ships a reproducible recipe that also runs **natively** on the dev's host.

- **Core devshell — `devbox.json`:** the non-embedded toolchain (Go, TinyGo, buf + `protoc-gen-go-lite` /
  `protoc-gen-es-lite`, `wit-bindgen-go`, `wasm-tools`, `nodejs` for jco, `wasmtime`, Wails deps). Scripts
  wrap the `make` targets; `devbox shell` / `direnv` auto-loads it; CI runs `devbox run verify-all`.
- **Embedded toolchain — delegated to PlatformIO (not fought in nixpkgs).** The old rough edge was
  packaging `esp-idf` / Pico SDK / WAMR in nixpkgs. Instead, Devbox installs just the **`platformio` CLI**,
  and **PlatformIO owns the board toolchains** (ESP-IDF for ESP32-S3, `arduino-pico` for RP2350),
  flashing, and `pio device monitor` (the serial REPL). WAMR arrives as the **official Espressif ESP-IDF
  component** (`espressif/wasm-micro-runtime`) or the `mlaass/wamr-esp32-arduino` PlatformIO lib; TFT
  display drivers (TFT_eSPI/LovyanGFX) come from PIO's registry for the Display host. Keep the embedded
  project self-contained under `hosts/embedded/` with its own `platformio.ini` so the core
  web/desktop/CLI loop never touches it.
- **Two-layer reproducibility (accepted tradeoff):** Nix/Devbox pins the *host* tools; PlatformIO pins the
  *board* packages via `platformio.ini` version pins + a committed lockfile. Not all-Nix-pure, but the
  pragmatic embedded answer — both layers are declarative and pinned.
- **RP2040** stays outside PlatformIO — it's a **native TinyGo** build (`tinygo flash -target=pico`), whose
  toolchain Devbox already pins. **RP2350-under-WAMR is the least-proven combo** (official WAMR is
  ESP-centric); if porting WAMR onto arduino-pico is too costly it falls back to native TinyGo like RP2040
  (see §14 risk 2).
- **Docker** stays only as a last-resort CI fallback for a board toolchain that resists both — not the
  default.

---

## 5. Architecture

### 5.1 Execution model

```
                    ┌───────────────────────── engine.wasm (Go, shared, portable) ─────────────────────────┐
                    │  imports: wasi:filesystem, wasi:cli(stdio), sqlite-host, event-host, display-host    │
                    │  exports: execute-cli(args) -> command-result                                          │
                    └───────────────────────────────────────────────────────────────────────────────────────┘
   web        desktop         cli            esp32-s3         rp2350           rp2040
   jco        wasmtime        wasmtime       WAMR             WAMR             (native TinyGo — same Go source,
   + TS host  + Go host       + Go host      + C host         + C/Go host       no wasm; engine linked in)
```

- **The engine imports only the ILC world + standard WASI, and is built once as a WASI-p1 core module**
  (`engine.core.wasm`). wasip2 runtimes (jco web, wasmtime desktop/CLI) run it **componentized via the
  preview1 adapter**; **WAMR** (ESP32-S3 / RP2350) runs the **core module directly** — no Component Model,
  so capabilities bind as WAMR native functions. What we build per tier and where interfaces differ is
  tracked in **§5.3**.
- **RP2040 is the one exception (Decision 18):** 264 KB RAM is too tight for a comfortable WASM runtime,
  so the *same Go engine source* is compiled **natively** with TinyGo and linked directly against a native
  host. One source, per-tier build — L3 everywhere it fits, native fallback where it doesn't.
- **The host is the only per-platform code**, and it is the injected `Environment`.

### 5.2 Why WASI makes FileSystem free

The engine writes files with plain Go `os` (`os.WriteFile`, `filepath.Join`, …). TinyGo lowers these to
WASI calls. Each host binds the WASI root:

| Host | WASI root preopen |
| --- | --- |
| Web | **OPFS** (`navigator.storage.getDirectory()`) **or** an **FSA-granted directory** (`showDirectoryPicker`, Chromium) — both are `FileSystemDirectoryHandle`s — via `@bytecodealliance/preview2-shim` `setPreopens` |
| Desktop / CLI | native user-data dir (`~/.config/<app>`) via wasmtime WASI preopen |
| ESP32-S3 / RP2350 | on-chip flash / littlefs, preopened by the WAMR host |
| RP2040 (native) | littlefs mounted directly; `os` shim maps to it |

**WASI preopens replace the old ILC mount-table / `VirtualPath` scheme entirely** — the runtime already
provides a bounded, host-selected filesystem root. That is the entire FileSystem capability.

### 5.3 What we build per tier (tracked)

**WAMR reality (researched 2026-07-24):** WAMR supports **core WASM + WASI Preview 1 only** — no Component
Model, no WASI 0.2. So "one artifact everywhere" is true at the **core-module** level, not the component
level. TinyGo builds `engine.core.wasm` (`-target=wasip1`) as the shared unit; wasip2 runtimes get it
**componentized via the official adapter** (`wasm-tools component new engine.core.wasm --adapt
wasi_snapshot_preview1.wasm`); WAMR runs the core module directly; RP2040 links the engine natively. The
component is a *derived wrapper*, not a separate source. **Micro divergence is accepted for now — this
table is where we track it.**

| Tier | Artifact built | Build pipeline | Capability binding | Divergence from the wasip2 baseline |
| --- | --- | --- | --- | --- |
| **Web** | `engine.component.wasm` | TinyGo→wasip1 → adapter → **jco** | WIT imports (Component Model) | baseline |
| **Desktop** | `engine.component.wasm` | TinyGo→wasip1 → adapter → **wasmtime** | WIT imports (Component Model) | baseline |
| **CLI** | `engine.component.wasm` | TinyGo→wasip1 → adapter → **wasmtime** | WIT imports; may also expose standard `wasi:cli/command` `run()` | baseline |
| **ESP32-S3** | `engine.core.wasm` | TinyGo→wasip1 → **WAMR** (no adapter) | **WAMR native-function registration** (C) | **no CM / WASI-0.2**; console = WASI-p1 fds → UART; sqlite = `unavailable`; caps are C native fns |
| **RP2350** | `engine.core.wasm` *or* native | TinyGo→wasip1 → WAMR **if** it ports, else native TinyGo | WAMR native fns, or direct Go linkage | as ESP32-S3, or as RP2040 |
| **RP2040** | native firmware | TinyGo→`pico` (**no wasm**) | **direct Go linkage** (no imports, no WASI) | no wasm, no WASI; caps are direct Go calls |

**The one build seam in the engine.** Capability *imports* are declared behind a build tag —
WIT-generated bindings on `wasip2`, `//go:wasmimport` on `wasip1`/WAMR, direct Go calls on native
(RP2040). The **business logic above the seam is identical**; only the binding layer + build target
change. Keep it in `engine/caps_wasip2.go` / `caps_wasip1.go` / `caps_native.go`. On WAMR the capability
*shapes* are the same — they're just bound as native functions instead of Component-Model imports, and
`wasi:filesystem` / stdio degrade to their WASI-p1 equivalents. **No handler code changes across tiers.**

**Core-wasm capability ABI (why micro is not lossy).** The Component Model only supplies the *typed*
wiring (Canonical ABI); it is **not** the source of capabilities, so its absence on WAMR loses none. On
WAMR the same caps are injected via **WASI Preview 1** (filesystem → littlefs, stdio → UART) plus **WAMR
native-function registration** for the custom ones (`sqlite-host` / `event-host` / `display-host`), which
the engine imports as `//go:wasmimport`. What core-wasm can't do is marshal rich types automatically — so
the custom caps exchange **protobuf bytes over `(ptr, len)`** in guest linear memory (the *same* `list<u8>`
payloads used on the Component-Model boundary). The micro boundary is therefore **defined and
capability-complete, just hand-marshaled**. The only caps that degrade on micro do so for **hardware**
reasons, not the ABI — `sqlite-host` → `unavailable` (file-scan fallback) and full networking — both
surfacing as the `unavailable` variant the engine already handles.

### 5.4 Host selection & launch model

**Principle: the host is the entry point *and* the injected Environment; the engine never selects its
host** (the two-bit invariant). "Which environment to run as" is decided in two places — never in engine
logic:

**1. Build / deploy time (primary) — one host artifact per target.** No single binary auto-detects its
world; you build and ship a *specific host* wrapping the shared engine. `dlc --tiers` (§16) selects which
are built.

| Host artifact | Entry point (constructs the Environment) | Selected by |
| --- | --- | --- |
| **native** (wasmtime) | `main()` — argv → native Environment → run engine → exit code | shipping the CLI binary |
| **desktop** (Wails) | Wails startup hook → native Environment + webview | building the desktop app |
| **web** (jco) | `worker.ts` boot → jco instantiate + browser Environment | serving the web app |
| **embedded** (WAMR / native) | firmware boot → board Environment (peripherals) | flashing the firmware |

**2. Launch time (secondary) — mode *within* a multi-mode host.** Some hosts serve more than one mode (the
native host can run headless-CLI *or* launch a GUI). Decide explicitly, sensible default, always
overridable — never ambient magic:

- **explicit subcommand/flag** — `myapp <command>` (one-shot CLI) vs `myapp gui` / `myapp serve`;
- **default by context** — args present → CLI one-shot; none → GUI/REPL (a *default*, overridable);
- **env/config** — `MYAPP_MODE=cli|gui`.

This is the ILC "detect a default, but always overridable" rule, kept at the **host/entry-point layer
only**. **Serverless / new hosts** are just more entry points (a Lambda handler builds the Environment,
`readLine`→`none`) — adding one is writing a host, not touching the engine.

### 5.5 Two-phase launch: environment, then app

Starting an ILC app is two phases — conceptually always, operationally *usually one command*:

1. **Launch the environment.** The host constructs the injected `Environment`: preopen the WASI filesystem
   root (native dir / OPFS·FSA / littlefs), open the capability providers (sqlite, events, display),
   assemble the capability set for this tier. May be **seeded** — e.g. `import-fs` a fixture or snapshot
   (§7.3), which is exactly test/dev setup.
2. **Launch the app.** Instantiate the engine (component on wasip2, core module on WAMR, native link on
   RP2040) and run it against the environment — either **one-shot** (`execute-cli` once → exit code) or
   **persistent** (the environment stays up; `execute-cli` is invoked many times — the reactive UI /
   server model).

**One command does both** (`myapp <cmd>`, `dlc run`), so the split is invisible in the common case. It
earns its keep when the phases are separated deliberately:

| Scenario | Phase 1 (environment) | Phase 2 (app) |
| --- | --- | --- |
| **One-shot CLI** | up | one invocation → tear down |
| **Persistent** (GUI / web / embedded / server) | up **once** | **many** invocations against it |
| **Test** | up with an **imported fixture FS** (§7.3) | run → export → diff golden; teardown = `reset-fs` |
| **Dev / REPL** | up once | swap / re-run the app without rebuilding the environment |

The boundary is the two-bit split at runtime: **phase 1 = construct the Environment (host); phase 2 =
`handler(env, input)` (engine).** The engine never participates in phase 1.

---

## 6. Capabilities (the WIT world)

`wit/ilc.wit` (sketch — kebab-case source, emitted camelCase by `wit-bindgen-go`):

```wit
package devalbo:ilc;

// Console I/O is NOT a custom interface — it uses standard WASI stdio
// (wasi:cli/stdout|stderr|stdin on wasip2; fd_write/fd_read on WAMR's WASI-p1).

interface sqlite-host {
  // Derived index only. May be unavailable (embedded) → engine degrades to a file scan.
  // (wasi:keyvalue is the standard KV alternative; we keep SQL because the index needs ORDER BY.)
  execute-query: func(sql: string, params: list<string>) -> result<string /*rows-json*/, string>;
}

interface event-host {
  // shape mirrors wasi:messaging (producer) — lands the deferred MQTT/sync story on the standard
  emit-event: func(topic: string, payload: list<u8>);   // payload = protobuf bytes
}

interface display-host {
  // shapes mirror wasi-gfx:frame-buffer + :surface (input); not hard-depended on (Phase 2, unstable)
  describe: func() -> display-info;                      // capability discovery
  draw:     func(commands: list<u8>) -> result<_, string>;   // protobuf DrawList
  render:   func(tree: list<u8>)     -> result<_, string>;   // protobuf WidgetTree
}

record display-info {
  width: u32, height: u32,
  color-format: color-format,           // rgb565 | rgb888 | mono1 | ...
  supports-draw-list: bool,
  supports-widget-tree: bool,
  has-input: bool,
}
enum color-format { rgb565, rgb888, mono1, gray8 }

record command-result {
  success: bool,
  output:  list<u8>,                    // protobuf-encoded TOutput (or canonical-JSON bytes)
  error:   option<string>,
}

world engine {
  import wasi:filesystem/types;         // standard WASI, host-preopened
  import wasi:cli/stdout;               // console = standard WASI stdio (not a custom interface)
  import wasi:cli/stderr;
  import wasi:cli/stdin;
  import sqlite-host;
  import event-host;
  import display-host;
  // `execute-cli` is the custom PERSISTENT entry (callable many times on a live instance, for reactive
  // UI). A one-shot CLI can additionally use the standard wasi:cli/command `run` + get-arguments.
  export execute-cli: func(args: list<string>) -> command-result;
}
```

### 6.1 FileSystem — WASI standard (§5.2). Not a custom interface.

### 6.2 SQLite-index — split storage

- **SQLite is only an index** over the JSON source-of-truth files. It is imported (not linked) because
  TinyGo cannot CGO a real driver; the host provides it: **web** → `@sqlite.org/sqlite-wasm` on OPFS;
  **desktop/CLI** → `modernc.org/sqlite` (pure Go); **embedded** → usually **`unavailable`**.
- **Graceful degradation:** on ESP32-S3 / RP2350 / RP2040, `execute-query` returns `unavailable`; the
  engine falls back to scanning the JSON directory (fine for small on-device datasets). Same handler,
  no index required — the split-storage model makes the index genuinely optional.

### 6.3 Events — reactivity

`emit-event(topic, payload)` pushes structured (protobuf) events. **Web** → routed to React callbacks via
Comlink; **desktop** → Wails `runtime.EventsEmit`; **embedded** → local dispatch / optional serial.

### 6.4 Display — discovery + two render paths (output-only)

The handler calls `describe()` first, then emits whichever mode the host advertises:

- **Draw-command list** (`draw`) — high-level ops (rect/text/image) as protobuf; host rasterizes.
  ESP32-S3 TFT → TFT driver calls; React → canvas. Compact, resolution-independent.
- **Retained widget tree** (`render`) — VDOM-like tree as protobuf; host diffs+renders. React → elements;
  a widget-capable MCU host → LVGL-style. App-friendly.
- `describe()` reports resolution, color format, and which paths the host supports, so **one handler
  targets a 320×240 TFT and a browser canvas from the same logic** by branching on the reported caps.

### 6.5 Console — standard WASI stdio (not a custom interface)

`info`→stdout, `error`→stderr, `readLine`→stdin, provided by the engine over **WASI stdio**
(`wasi:cli/stdout|stderr|stdin` on wasip2; `fd_write`/`fd_read` on WAMR's WASI-p1). Hosts wire the streams
to their sink: native stdio; `console.*` / `prompt()` on web; UART/RTT on embedded; `readLine`→`none`
where there is no console.

### 6.6 Interface ↔ WASI standard (reuse vs custom)

Tracks what we take from the standard ecosystem vs. keep custom. **"Mirror"** = custom implementation, but
the interface *shape* follows the standard so we can adopt or bridge to it later.

| ILC need | Standard | Decision | Status |
| --- | --- | --- | --- |
| Console I/O | **WASI stdio** (`wasi:cli/stdout\|stderr\|stdin`; p1 `fd_write`/`fd_read`) | **Adopt** — no custom interface | stable |
| CLI entry (one-shot) | **`wasi:cli/command`** (`run` + `get-arguments` + `exit`) | **Adopt** for the CLI tier; keep the custom persistent `execute-cli` for reactive/multi-call tiers | stable |
| Filesystem | **`wasi:filesystem`** | already adopted (§5.2) | stable |
| Test host | **WASI Virt** (compose virtual FS + captured stdio) | **Adopt** on wasip2 tiers | available |
| Index / store | `wasi:keyvalue` (KV); `wasi-sql` | **Keep custom** `sqlite-host` (needs `ORDER BY`); note KV as the simple-case standard | keyvalue Phase 2; sql early |
| Events / pub-sub | `wasi:messaging` (producer) | **Mirror** in `event-host`; lands the deferred MQTT/sync story on the standard | Phase 2/3 |
| Display | `wasi-gfx:frame-buffer` / `:surface` (+input) | **Mirror** in `display-host`; don't hard-depend (Phase 2, unstable, WebGPU-centric) | Phase 2 |
| Network (deferred) | **`wasi:http`** / `wasi:sockets` | future Network capability | http stable |

---

## 7. Storage & serialization

**Platform vs app (§0.1):** the **serialization** (protobuf boundary/wire, §7.2) is a **platform
invariant** — every app uses it. The **split-storage model** (§7.1) is an **optional pattern an app opts
into**: apps needing indexed local persistence use it; a stateless transformer or calculator skips it
entirely (console + maybe filesystem, no SQLite index).

### 7.1 Split-storage model *(optional, app opt-in)*

- **Source of truth:** proto-schema'd **canonical-JSON** files on the (WASI) filesystem.
- **Index:** SQLite, purely to avoid full-directory scans; **disposable and rebuildable**.
- **Write flow (atomic):** `create <id>.lock` → write `<id>.json` → update SQLite index → remove
  `<id>.lock` → `emit-event("data-changed", …)`.
- **Recovery:** an `execute-cli(["rebuild-index"])` handler scans all JSON files and rebuilds the index
  from scratch. The index is never authoritative.

### 7.2 One serialization story (protobuf)

- **Schemas:** protobuf `.proto` govern every document/message shape + evolution (field numbers).
- **On disk:** proto3 **canonical JSON** (human-readable, inspectable, diff-able) — the source of truth.
- **On the wire** (future sync / network): **binary protobuf**.
- **At the capability boundary:** protobuf bytes (`list<u8>`) for structured payloads (event payloads,
  display commands, command results).
- Codegen: `buf` + `protoc-gen-go-lite` (Go engine + native host); `protoc-gen-es-lite` (TS web host). One
  schema, both projections; `buf breaking` guards evolution.

### 7.3 Filesystem export/import (first-class)

Because an app's entire state is a **filesystem tree** (the split-storage source-of-truth JSON docs; the
SQLite index is disposable), the whole environment is portable as a **filesystem bundle**. Export/import is
a **platform primitive** — engine-side, using only the filesystem capability, so it behaves identically on
every tier (native disk, browser OPFS/FSA, in-memory test FS, embedded littlefs).

- **Bundle formats** (choose per use; `--format=bft|zip|proto`):

  | Format | Niche |
  | --- | --- |
  | **BFT** — single JSON file ([devalbo/brute-force-transfer](https://github.com/devalbo/brute-force-transfer)) | **human-readable, diffable, git-friendly, text-channel-transmittable, self-bootstrapping, deterministic** — the interchange / inspection / migration format. Especially clean here: our source of truth is *already* canonical JSON, so BFT = JSON-of-JSON (lossless; alphabetical order → byte-stable). A **deflate** variant answers size. |
  | **zip/tar** | bulk binary transfer, streaming, large / binary-heavy stores |
  | **protobuf `Snapshot { repeated Entry { path, bytes } }`** | compact wire / embedded (one-serialization-story) |

- **Platform handlers** (via `execute-cli`, every tier): `export-fs [prefix] --format=<fmt> -> bundle`,
  `import-fs <bundle> [prefix]`, `reset-fs [prefix]`. **Import** writes the tree, then runs `rebuild-index`.

**Why first-class — robustness + testability:**

| Use | How |
| --- | --- |
| **Test setup/teardown** | import a fixture bundle → run handler → export result → diff a golden bundle. Deterministic env per test; teardown = `reset-fs`. Pairs with the in-memory test host / WASI Virt (§6.6). |
| **Golden FS snapshots** | the §11 golden vectors become golden *filesystem snapshots* — byte-diff the exported tree across tiers |
| **Backup / restore** | export = backup; import = restore |
| **Bug reproduction** | a user exports the exact FS state that triggers a bug; the dev imports it and reproduces it deterministically |
| **Cross-tier migration** | export from browser OPFS → import into native; proto schema evolution absorbs doc-format drift |
| **Two apps / versions, one store** | **BFT** as the neutral interchange: `diff` what each app/version wrote, script migrations on the JSON, git-merge concurrent writes, embed provenance in BFT comments. Text + schema beats a binary blob for shared-store inspection. |
| **Node bootstrap (sync)** | import a full snapshot to seed a new node, then sync incrementally (Decision 9) |
| **Scaffolding** | `dlc new` **is** an import of a **template bundle**; browser-`dlc`'s "download project" **is** an export — the scaffolder and this primitive are the same operation |

Export/import is the **unifying primitive** under scaffolding, browser download, backup, testing, and node
bootstrap — all "move a filesystem tree in or out," backend-agnostic because the FS is a WASI capability.

**BFT's home:** BFT was the original ILC pilot (the duplicated `bft.js` / `bft.py` codec); porting it to the
single Go engine core is now a real platform feature — *this* format — not a throwaway demo, and it retires
the duplication the old pilot targeted.

---

## 8. Invocation & input

**Universal principle (Decision 14): every environment executes handlers CLI-style.** The engine's single
export is `execute-cli(args) -> command-result`; the host maps its native input into a command invocation.

| Tier | How input becomes a command |
| --- | --- |
| CLI | argv → `execute-cli(args)` (the reference path) |
| Desktop | Wails IPC / UI events → `execute-cli(args)` |
| Web | React events → `executeCli(args)` over Comlink |
| ESP32-S3 / RP2350 / RP2040 | **(a) UART/USB-serial command REPL** — literal typed commands (dev/debug + scripting); **(b) input-map** — host translates touch/button events into commands (tap list row → `open-record <id>`) |

Both embedded mechanisms feed the same `execute-cli` entry, so **there is one dispatch path on every
tier.** (This is the proposed method for "environments where a CLI isn't obvious.") Multi-handler routing
and a protobuf **envelope** are **deferred** (Decision 10) — local-only in V1.

**CLI binding (bind, don't supplant).** On the CLI tier the shim is thin: a Go CLI library parses argv → a
typed args struct → mapped to the proto `TInput` → `execute-cli` → `command-result` mapped to an exit code.
ILC's default is **kong** (Decision 22) — the Typer analog, where a struct's fields *are* the CLI, so it
maps almost 1:1 onto the proto input. Any parser works (cobra / urfave / go-arg); ILC only requires the
shim end at `execute-cli`. `dlc`'s own multi-subcommand CLI (§16.7) uses the same library.

**Where parsing runs (the TinyGo boundary).** The rich CLI parse (kong) runs in the **native host
(standard Go)**, *not* in the engine — kong is reflection-heavy and TinyGo's `reflect` is partial (no
`encoding/json`, missing `NumOut`; even stdlib `flag` is only partial). The host produces the proto
`TInput`; the **engine's dispatch is reflection-free** — a generated route table + protobuf-go-lite decode,
or a minimal hand-rolled `argv→TInput` mapper for the universal `execute-cli(args)` path. On **embedded**
there is no kong at all (the C/native host or the serial REPL feeds the engine's minimal parser).
**Invariant: nothing reflection-heavy compiles into the TinyGo engine** — the same rule that chose
protobuf-go-lite (§4).

**Reactivity loop:** UI issues `execute-cli(["list-records"])` to read; a write emits `data-changed`; the
host's subscription (React `useEngineEvent` hook / Wails `EventsOn`) invalidates and re-fetches.

---

## 9. Sync (deferred, simple)

Because SQLite is a disposable index, **multi-node sync operates only on the JSON documents** (Decision 9):
last-writer-wins document sync over a sync folder / simple transport; each node **rebuilds its index
locally**. No CRDT-SQLite engine, no cross-tier extension problem. (Automerge per-document is the
documented upgrade path if concurrent-edit loss becomes real — out of V1 scope.)

---

## 10. Environment matrix (explicit — Decision 17)

| Environment | Engine exec | Host lang | FS root | SQLite index | Display | Console/Input |
| --- | --- | --- | --- | --- | --- | --- |
| **Web** | TinyGo→wasm, **jco** | TypeScript | **OPFS** / **FSA-granted dir** | `sqlite-wasm` (worker) | React (canvas / widget-tree) | `console.*` / React events |
| **Desktop** | TinyGo→wasm, **wasmtime** | Go (Wails) | `~/.config/<app>` | `modernc.org/sqlite` | Wails webview React | stdio / Wails IPC |
| **CLI** | TinyGo→wasm, **wasmtime** | Go | cwd / config dir | `modernc.org/sqlite` | none (Console) | argv / stdin |
| **ESP32-S3** | wasm, **WAMR** | **C** (ESP-IDF) | flash / littlefs | *unavailable* → file scan | TFT (draw-list) | UART REPL / touch input-map |
| **RP2350** | wasm, **WAMR** | **C** (arduino-pico) / Go | flash / littlefs | *unavailable* → file scan | TFT (draw-list) | UART REPL / buttons |
| **RP2040** | **native TinyGo** (no wasm) | **Go** | littlefs | *unavailable* → file scan | TFT (draw-list, optional) | UART REPL / buttons |

Absent capabilities never crash — they return `unavailable` and the engine degrades (the ILC invariant).

---

## 11. Verification — build / load / run per environment

Every tier must have an automated **build → load → run → observe** check (mirrors the ILC "per-host
behavior test, not just codegen" standing rule). `make verify-<tier>` runs each; `make verify-all` runs the
matrix + the cross-tier identity check.

| Tier | Build | Load | Run | Observe (pass criteria) |
| --- | --- | --- | --- | --- |
| **Cross-tier** | `make build-engine` | — | — | **`engine.core.wasm` (the WASI-p1 core module) is byte-identical** across all wasm tiers (sha256); the wasip2 component is a derived adapter wrapper. A shared **golden vector** (create→list→rebuild-index) produces identical `command-result` bytes on every runtime |
| **CLI** | `tinygo build` + wasmtime host | load wasm in wasmtime | `app create-record …` / `list-records` / `rebuild-index` | JSON file written + `.lock` gone; index row present; `list` returns it; delete index file + `rebuild-index` reproduces it |
| **Desktop** | `wails build` | Wails boots wasmtime + webview | create/list/delete in UI | record renders; persisted under `~/.config/<app>`; `data-changed` event repaints; relaunch shows data |
| **Web** | `make build-wasm` (TinyGo→jco) | `worker.ts` sets OPFS preopen, boots jco, injects caps | create/list in browser | record persists in OPFS (survives reload); SQLite index file in OPFS; event repaints; DevTools shows no host-cap errors |
| **ESP32-S3** | `tinygo` builds `engine.core.wasm`; **`pio run -t upload`** flashes the WAMR host firmware (wasm embedded) | WAMR instantiates on boot | **`pio device monitor`**: `create-record …`; touch a row | JSON on littlefs; TFT renders the list via `draw`; `describe()` reports 320×240/rgb565; `execute-query`→`unavailable` path exercised (file-scan fallback) |
| **RP2350** | as ESP32-S3 via PlatformIO (`board=rpipico2`) **if** WAMR ports; else native TinyGo fallback | WAMR (or native) on boot | `pio device monitor` / buttons | same behavior from the *same* `engine.core.wasm` (WAMR) or same source (native); confirms the second embedded target |
| **RP2040** | `tinygo build` **native** (engine linked) + flash | firmware boot | serial REPL / buttons | same behavior from the **native** build — proves one-source parity where wasm doesn't fit |
| **Scaffolder** | `dlc new tmp --caps=… --tiers=…` | — | `devbox run verify` on the *generated* project | the scaffold **builds + passes its verify matrix** — templates can't silently rot (§16.6) |

**Phase-0 spikes (do before committing the plan):** (1) TinyGo→component→jco round-trip of a trivial
`execute-cli`; (2) `protobuf-go-lite` binary **and** canonical-JSON round-trip under
`tinygo build -target=wasip1` (+ `protobuf-es-lite` decodes the same bytes in the web host); (3) WAMR running
a TinyGo `engine.wasm` on ESP32-S3 with one host import; (4) OPFS preopen letting `os.WriteFile` persist
across reload; (5) `devbox` builds the core (non-embedded) toolchain reproducibly. Any red spike
reshapes the plan before the pilot.

---

## 12. Implementation phases

| Phase | Scope | Exit criterion |
| --- | --- | --- |
| **0a — Repo migration** | Tag + remove the retired tri-language Phase-1 code; scaffold the Go layout (**§2**) | working tree matches §3; `compiler`/`packages`/root Cargo removed (tagged); `go.mod` + `devbox.json` in place |
| **0b — Spikes** | The five §11 spikes | All green (or plan adjusted to the reality) |
| **1 — `dlc` app on CLI (App #1)** | Build **App #1 = `dlc`** (self-hosting scaffolder, §16.4): `wit/ilc.wit`, `proto/*`, `wit-bindgen-go` + `buf` wired; engine uses **console (WASI stdio) + WASI FS** only; **CLI host** (wasmtime); `dlc new` emits a minimal self-shaped skeleton; `dlc doctor` (readiness as a command, §16.7); `export-fs`/`import-fs` (§7.3) | `dlc new myapp` scaffolds + runs a working CLI project; `dlc doctor` reports readiness; golden **FS snapshot** defined |
| **2 — Web/Desktop hosts + notes (App #2)** | **`dlc` gains the web tier** (jco, OPFS/FSA — runs in the browser); begin **App #2 = notes/list**: SQLite-index (native + web), Events, lock-file write flow, `rebuild-index`; **Web + Desktop hosts** | `dlc new` runs in the browser; notes create/list/delete round-trips on web + desktop; `data-changed` repaints; `rebuild-index` reproduces the index; per-host behavior tests |
| **3 — Display + embedded** | Display capability (describe + draw-list) with React host + **ESP32-S3 WAMR host**; RP2350 + RP2040(native) | shared notes/list renders on browser **and** ESP32-S3 TFT from one engine; `unavailable` file-scan fallback verified; RP2040 native parity |
| **4 — Pilot hardening** | Shared notes/list end-to-end on all six tiers; `make verify-all` | byte-identical engine **core module** across the five wasm tiers; golden parity on all six; verification matrix green |
| **5 — (deferred)** | File LWW sync; protobuf envelope + multi-handler routing; Input capability; Automerge upgrade path | — |

---

## 13. Pilot — shared notes/list (Decision 16)

A local-first notes-or-tasks app. Records are proto-schema'd JSON files; SQLite indexes `{id, title,
updated_at}`. Handlers: `create-record`, `list-records`, `open-record <id>`, `update-record <id> …`,
`delete-record <id>`, `rebuild-index`. UI: React on web/desktop; a scrollable list on the ESP32-S3 TFT via
the Display draw-list. Input: React events / Wails IPC / serial REPL / touch input-map. Sync: file LWW
(Phase 5). It exercises **every V1 capability** (WASI FS, SQLite-index, Events, Display, Console) across
**every tier**, and is the concrete substrate for the §11 verification matrix.

---

## 14. Open questions / risks

1. **TinyGo + protobuf — largely resolved.** The old top risk (reflection-heavy official protobuf) is
   answered by **`protobuf-go-lite`** (reflection-free, TinyGo-built) + **`protobuf-es-lite`** for the web
   host (§4). Residual: confirm go-lite round-trips under `tinygo build -target=wasip1` (Phase-0 spike 2),
   and that dropping fieldmasks/extensions stays acceptable. No longer the highest risk.
2. **WAMR on RP2350 — footprint *and* port maturity.** 520 KB is comfortable-ish but TinyGo-runtime-in-wasm
   + WAMR + linear memory needs measuring; separately, **official WAMR is ESP-centric**, so RP2350 needs
   WAMR ported onto `arduino-pico`/Pico SDK (unproven). If either is too costly, RP2350 falls back to a
   native TinyGo build like RP2040. ESP32-S3 (official WAMR ESP-IDF component + PlatformIO) is the reference
   embedded tier; RP2350 is the stretch.
3. **"All six tiers in V1" is aggressive.** The phase order builds a reference tier first (CLI → web/desktop
   → embedded) so slippage degrades to "fewer tiers shipped," not "nothing works." Consider gating V1 on
   Phase 4 for CLI+web+desktop+ESP32-S3, with RP2350/RP2040 as fast-follows.
4. **Desktop/CLI: wasm-under-wasmtime vs native link.** The plan runs the shared wasm for uniformity; if
   wasmtime overhead is unwanted, the same Go source can link natively (build tag) like RP2040 — a
   documented alternative, defaulting to wasm.
5. **Display input on embedded** beyond the input-map (Decision 14) — a symmetric **Input capability** is
   the Phase-5 portable answer if host-side input proves limiting.
6. **Component Model async maturity** — WASI Preview 2 async is in flux; the engine stays on the
   Asyncify-backed blocking model to avoid depending on it.
7. **Dual-build maintenance (CM vs core-wasm).** WAMR has **no Component Model / WASI 0.2**, so the engine
   ships as a wasip2 component (web/desktop/CLI) *and* a wasip1 core module (WAMR), plus native (RP2040) —
   a build seam behind build tags (§5.3). Accepted for now; the risk is drift between the binding layers.
   Mitigation: capability *shapes* are identical across tiers and covered by the §11 per-tier behavior
   tests. Revisit if/when WAMR gains component support.

---

## 15. Relationship to the prior ILC plan

This plan **keeps** ILC's core (bounded, injected capabilities; host-only bindings; portable handler
logic; graceful `unavailable` degradation; introspectable capabilities — now via `describe()` and WASI
preopens) and **drops** what Go + the Component Model make unnecessary: the custom Rust WIT→3-language
compiler, hand-written per-language SDK parity, the bespoke FileSystem mount-table/`VirtualPath` scheme
(WASI preopens replace it), and — for now — the tri-language matrix and the Rust `no_std`/Embassy tier.
The neutral WIT boundary keeps the polyglot door open (Rust/C-via-WASI), so the matrix can return later
without re-architecting.

---

## 16. Spinning up a new app (using this as a template)

The platform (hosts, capability world, build/verify harness, CLI shim) is written once; each app is a new
engine + its protos + its UI, selecting the capabilities and tiers it needs. See the Platform-vs-App split
in §0.1.

### 16.1 The two knobs an app sets

- **Capabilities used (opt-in).** The WIT world is a *superset*; an app imports only what it needs — a
  calculator: console only; notes: filesystem + sqlite-index + display + events; a sensor logger:
  filesystem + network + display. Unused caps are simply not imported; a tier that lacks a *used* cap
  returns `unavailable` and the engine degrades (§6.2).
- **Tiers targeted (opt-in).** CLI-only, CLI+web, CLI+embedded, or all six (§10). Each tier is a host you
  enable in the build/verify matrix; the engine source is unchanged.

### 16.2 The progression (CLI-first, then add tiers)

| Stage | Do | Result | Engine change |
| --- | --- | --- | --- |
| 0 — scaffold | `dlc new myapp` → engine skeleton + CLI host + Devbox + wit/proto | working CLI (`execute-cli`) | — |
| 1 — logic | write handlers over the capabilities you need | CLI does real work | write |
| 2 — + UI | enable the web/desktop host; add a UI that calls `execute-cli` | same app in browser/window | **none** |
| 3 — + embedded | enable the WAMR host + native cap bindings; flash | same app on device | **none** (rebuilt to core-wasm) |

The engine is stable from Stage 1; later stages add **hosts**, not logic. Graceful `unavailable`
degradation means a cap added for one tier (e.g. Display) never breaks the tiers without it.

### 16.3 What each app writes vs inherits

- **Writes:** `engine/` handlers, `proto/<app>.proto`, the capability + tier selection, the UI (if any),
  optionally the split-storage pattern (§7.1).
- **Inherits unchanged:** host adapters, the `caps_*.go` build seam, the core-module + adapter pipeline,
  the Devbox/buf toolchain, the per-tier verification harness, and the CLI shim + `execute-cli` entry.

### 16.4 Build order — `dlc` is App #1

Two concrete apps come before the platform is abstracted:

1. **App #1 — `dlc` itself** (self-hosting). CLI + web; capabilities = **console + filesystem** only.
   Building `dlc` *is* establishing the platform skeleton — you can't build any app without `engine/`,
   `hosts/native`, `wit/`, the `caps_*.go` seam, Devbox, buf, the Makefile. `dlc new` emits `dlc`'s own
   shape with the app-specific parts blanked, so the template is **self-derived**; scaffolding is an
   `import-fs` of a template bundle (§7.3). `dlc` proves the two easy tiers (**CLI + web**, via OPFS/FSA)
   on the two simplest caps.
2. **App #2 — notes/list** (the breadth pilot, §13). Generated *by* `dlc new`, then filled in. Proves the
   parts `dlc` doesn't touch: Display, SQLite-index, desktop, embedded, rich protobuf. Its tiers/caps are
   added to both the platform and `dlc new`'s flags as they land — the scaffolder **co-evolves**.
3. **Extract `ilc-platform/`** once both apps share it (hosts, WIT, build seam, verify harness,
   scaffolder). **Concrete-first, extract-after** — two real consumers before the shared module.

Caveat: `dlc` exercises only console + filesystem, so `dlc new`'s flags start minimal (`--tiers=cli,web`)
and grow as notes drives out Display / SQLite / embedded.

### 16.5 Bootstrapping a new app: interview + scaffolder (two layers)

Two complementary layers, both derived from **this plan file**:

- **Deterministic `dlc new` scaffolder** — emits the invariant ~80% (structure, `go.mod` / `devbox.json` /
  `buf.*`, host adapters, the `caps_*.go` seam, verify harness, CLI wiring), parameterized by the two knobs
  (`--caps`, `--tiers`) + `--ui` / `--storage`. Reproducible, no LLM. Scaffolding = an **`import-fs` of a
  template bundle** (§7.3). Prior art: `cargo component new`, `wash new`.
- **Plan-guided interview** — an agent reads *this file* as its spec and interviews the developer to resolve
  the tailored ~20% a flag can't express (which caps/tiers, the domain `.proto`, handler shapes, storage).
  This is the very format that produced this plan.

**They chain:** interview **decides** the knobs + domain schema → **drives** `dlc new` (deterministic 80%)
→ the agent **writes** the app-specific stubs (protos, handler skeletons). The plan file is the shared
source of truth — the interview reads it as rules, the scaffolder encodes its invariants as templates.

**Sequencing:** the interview works **today** (this file exists); extract the deterministic scaffolder from
`dlc` once App #1 proves the structure (§16.4).

### 16.6 Template code — its own area

Templates are a **distinct top-level concern**, reasoned about separately from how the framework operates:

```
/
├── platform/     # the ILC framework (hosts, wit, caps seam, build pipeline, verify harness) → `ilc-platform` (§16.4)
├── templates/    # DISTINCT: what `dlc new` emits — base skeleton + per-knob fragments (BFT/zip bundles, §7.3)
└── apps/
    ├── dlc/      # App #1 — the scaffolder; go:embed's /templates
    └── notes/    # App #2 — breadth pilot
```

- **Depends-on, never inlines.** A template is an *app-shaped* skeleton whose `go.mod` depends on
  `ilc-platform` as a **versioned module** + a thin `main`; it never copies framework internals. So a
  template change (edit `/templates/`) and a framework change (bump the platform version) are separate
  concerns with a clean boundary — the whole point of the separation.
- **Anti-drift by validation, not derivation.** Templates are authored by hand in `/templates/` (their own
  PRs, their own mental model). Drift is caught by CI — the §11 **Scaffolder** row (`dlc new … → devbox run
  verify`) must pass. Decoupled authorship, coupled validation only.
- **Embedded, not cloned.** `dlc` `go:embed`s `/templates/`, so `dlc new` is self-contained — offline, and
  it works on the **browser tier** (templates ride inside `engine.wasm`). Key improvement over runtime-clone.
- **Composition + parameterization.** `--caps` / `--tiers` / `--ui` / `--storage` select a **base bundle +
  fragment overlays** (`import-fs`, §7.3); a token pass substitutes `{{.Module}}` / `{{.AppName}}` / the
  selected capability imports. Bundles are BFT/zip so they stay diffable.
- **Graduates to its own repo.** `/templates/` can later become a standalone repo (submodule optional),
  keeping template contribution independent of the framework — still `go:embed`'d into `dlc` at build.

**Prior art — Qroma** (`qromatech/qroma-project-generator`, `qroma.dev`). The author's earlier
embedded-Python generator validates this shape: templates lived in a **separate `qroma-project-template`
repo (submodule)**, `qroma new` substituted project identifiers, targeting firmware + protobuf + site +
optional app. ILC borrows the **separated-template model** and the **concern-scoped CLI** (§16.7), and
independently confirms two Qroma choices — **protobuf as the device↔app messaging layer** and **PlatformIO**
for firmware. ILC's deltas: **one WASM engine across every tier** (vs separate firmware/app/site artifacts),
**`dlc` is itself an ILC app** (self-hosting), and **`go:embed` not runtime-clone** (offline + browser).

### 16.7 The `dlc` command surface

Concern-scoped subcommands (Qroma-style `qroma new/build/pb/firmware/site`), built on **kong** (Decision 22):

| Command | Does |
| --- | --- |
| `dlc new <app> --caps=… --tiers=… --ui=… --storage=…` | scaffold (import base + fragments, §16.6); `--build --git --run` for one-step setup |
| `dlc doctor` | assess readiness — system prereqs + per-tier toolchain/host; the **command form** of `scripts/preflight.sh` (Layer 1; the pure-bash script stays as the pre-toolchain Layer 0 gate — see prereqs doc) |
| `dlc build` / `dlc verify` | build the engine (core-module + adapter/native) / run the §11 per-tier matrix |
| `dlc proto …` | edit / regenerate the app's `.proto` (buf under the hood) |
| `dlc host add <web\|desktop\|embedded>` | overlay a tier's host into an existing app (import a fragment) |
| `dlc export-fs` / `dlc import-fs` | the §7.3 filesystem primitive, surfaced as commands |

`dlc` is a CLI+web ILC app (App #1); these are its `execute-cli` handlers — **not bespoke tooling, the
platform used on itself.**

### 16.8 Project metadata — the manifest

The app's configuration lives in one **project manifest** (`dlc.toml` — or a proto-schema'd `dlc.json`),
the source of truth for everything an app *chose* (the two knobs of §16.1, plus the rest):

```toml
# dlc.toml (sketch)
[app]
name     = "notes"
module   = "github.com/me/notes"
platform = "ilc-platform@0.3.1"        # the versioned framework dependency (§16.6)

capabilities = ["console", "filesystem", "sqlite-index", "events", "display"]
tiers        = ["cli", "web", "desktop", "esp32-s3"]
storage      = "split"                 # or "none" (§7.1)
ui           = "react"                 # or "tft" / "none"

[launch]                               # §5.4 / §5.5
default-mode = "gui"                   # args → cli one-shot; none → gui
seed         = "fixtures/demo.bft"     # optional phase-1 import-fs (§7.3)
```

- **`dlc` reads/writes it (§16.7).** `dlc new` writes it from the interview/flags; `dlc build` / `dlc
  verify` read `tiers` / `capabilities` to know what to build + which §11 rows to run; `dlc host add web`
  appends to `tiers`; `dlc run` reads `[launch]` for the phase-1 setup + mode (§5.5).
- **The two knobs, made durable.** §16.1's *capabilities* + *tiers* opt-ins live here as one declarative
  record — edited as the app evolves — instead of scattered across build tags / Makefile.
- **Regenerate / upgrade.** Because it pins `platform` + records the config, `dlc` can re-apply templates
  against a newer platform version (a scaffold *upgrade*), not just first-time generation.
- **Dogfood option:** define the manifest as a proto `Project` message → canonical-JSON `dlc.json`
  (validated + `buf breaking`-evolvable + diffable, §7.2). **TOML** is the hand-edit-friendlier alternative.
- **Prior art:** Qroma's "one source of truth and bill of materials" — the same idea.
