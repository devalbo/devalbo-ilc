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
> **reference** (§14–§15). §0–§1 are orientation (thesis + locked decisions).

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

The engine is byte-identical everywhere a WASM runtime exists; the host is whatever each platform needs.
**The host language is free — Go, TypeScript, or C — because a host only *provides* capabilities; all
business logic is the Go engine.** Business logic never lives in TS or C.

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
| 12 | Capability set | WASI FS · SQLite-index · Events · Display · ConsoleIo |
| 13 | Display | **Discovery (`describe`) + draw-command list + retained widget tree**; output-only |
| 14 | Input | **Universal `execute-cli`**; host maps native input → command (serial REPL / input-map on MCUs) |
| 15 | Reactivity | Engine emits **events**; host subscribes; UI invalidates + re-fetches |
| 16 | Pilot | **Shared notes/list** local-first app |
| 17 | Tiers (all V1) | **Web · Desktop · CLI · ESP32-S3 · RP2350 · RP2040** |
| 18 | Embedded exec | **Mixed by capacity:** ESP32-S3 / RP2350 → WAMR+wasm; RP2040 → native TinyGo |
| 19 | Compiler | **Retire** the custom Rust WIT→3-lang compiler; use `wit-bindgen-go` + `buf` |
| 20 | WASI standards reuse | Console → **WASI stdio** (drop custom `console-io`); CLI entry → **`wasi:cli/command`** + a custom persistent `execute-cli` export; test host → **WASI Virt**; `sqlite-host` / `event-host` / `display-host` mirror **wasi:keyvalue** / **wasi:messaging** / **wasi-gfx** shapes (§6.6) |

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
| `compiler/` (Rust `ilc` WIT→3-lang) | **Remove** | Retired (Decision 19); `wit-bindgen-go` + `buf` replace it |
| `packages/ilc-ts`, `ilc-py`, `ilc-rs` | **Remove** | Tri-language matrix dropped; the web host is *new* TS under `hosts/web/`, not this SDK. Old `ilc-ts` host shapes stay a **reference** (recoverable via the tag), not reused code. |
| `Cargo.toml`, `Cargo.lock` (root) | **Remove** | Existed only for `compiler` + `ilc-rs`; the Go plan has no root Rust workspace |
| `wit/environment.wit` | **Replace** | `world environment` is superseded by the new `world engine` (§6) |
| `wit/console-io.wit` | **Keep + fold** | The `console-io` interface is reused as-is; bring it under the new `wit/ilc.wit` package/world |
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
| Web | `navigator.storage.getDirectory()` (OPFS) via `@bytecodealliance/preview2-shim` `setPreopens({'/': opfsRoot})` |
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

### 7.1 Split-storage model

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
| **Web** | TinyGo→wasm, **jco** | TypeScript | OPFS | `sqlite-wasm` (worker) | React (canvas / widget-tree) | `console.*` / React events |
| **Desktop** | TinyGo→wasm, **wasmtime** | Go (Wails) | `~/.config/<app>` | `modernc.org/sqlite` | Wails webview React | stdio / Wails IPC |
| **CLI** | TinyGo→wasm, **wasmtime** | Go | cwd / config dir | `modernc.org/sqlite` | none (ConsoleIo) | argv / stdin |
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
| **ESP32-S3** | `tinygo` builds `engine.wasm`; **`pio run -t upload`** flashes the WAMR host firmware (wasm embedded) | WAMR instantiates on boot | **`pio device monitor`**: `create-record …`; touch a row | JSON on littlefs; TFT renders the list via `draw`; `describe()` reports 320×240/rgb565; `execute-query`→`unavailable` path exercised (file-scan fallback) |
| **RP2350** | as ESP32-S3 via PlatformIO (`board=rpipico2`) **if** WAMR ports; else native TinyGo fallback | WAMR (or native) on boot | `pio device monitor` / buttons | same behavior from the *same* `engine.wasm` (WAMR) or same source (native); confirms the second embedded target |
| **RP2040** | `tinygo build` **native** (engine linked) + flash | firmware boot | serial REPL / buttons | same behavior from the **native** build — proves one-source parity where wasm doesn't fit |

**Phase-0 spikes (do before committing the plan):** (1) TinyGo→component→jco round-trip of a trivial
`execute-cli`; (2) `protobuf-go-lite` binary **and** canonical-JSON round-trip under
`tinygo build -target=wasi` (+ `protobuf-es-lite` decodes the same bytes in the web host); (3) WAMR running
a TinyGo `engine.wasm` on ESP32-S3 with one host import; (4) OPFS preopen letting `os.WriteFile` persist
across reload; (5) `devbox` builds the core (non-embedded) toolchain reproducibly. Any red spike
reshapes the plan before the pilot.

---

## 12. Implementation phases

| Phase | Scope | Exit criterion |
| --- | --- | --- |
| **0a — Repo migration** | Tag + remove the retired tri-language Phase-1 code; scaffold the Go layout (**§2**) | working tree matches §3; `compiler`/`packages`/root Cargo removed (tagged); `go.mod` + `devbox.json` in place |
| **0b — Spikes** | The five §11 spikes | All green (or plan adjusted to the reality) |
| **1 — Contract + engine skeleton** | `wit/ilc.wit`, `proto/*`, `wit-bindgen-go` + `buf` wired; engine exports `execute-cli` with ConsoleIo + WASI FS only; **CLI host** (wasmtime) | `app create-record`/`list-records` write proto-JSON + read back on the CLI tier; golden vector defined |
| **2 — Index + events + reactivity** | SQLite-index capability (native + web), Events capability, lock-file write flow, `rebuild-index`; **Web + Desktop hosts** | create/list/delete round-trips on web + desktop; `data-changed` repaints; `rebuild-index` reproduces the index; per-host behavior tests |
| **3 — Display + embedded** | Display capability (describe + draw-list) with React host + **ESP32-S3 WAMR host**; RP2350 + RP2040(native) | shared notes/list renders on browser **and** ESP32-S3 TFT from one engine; `unavailable` file-scan fallback verified; RP2040 native parity |
| **4 — Pilot hardening** | Shared notes/list end-to-end on all six tiers; `make verify-all` | byte-identical engine across the five wasm tiers; golden parity on all six; verification matrix green |
| **5 — (deferred)** | File LWW sync; protobuf envelope + multi-handler routing; Input capability; Automerge upgrade path | — |

---

## 13. Pilot — shared notes/list (Decision 16)

A local-first notes-or-tasks app. Records are proto-schema'd JSON files; SQLite indexes `{id, title,
updated_at}`. Handlers: `create-record`, `list-records`, `open-record <id>`, `update-record <id> …`,
`delete-record <id>`, `rebuild-index`. UI: React on web/desktop; a scrollable list on the ESP32-S3 TFT via
the Display draw-list. Input: React events / Wails IPC / serial REPL / touch input-map. Sync: file LWW
(Phase 5). It exercises **every V1 capability** (WASI FS, SQLite-index, Events, Display, ConsoleIo) across
**every tier**, and is the concrete substrate for the §11 verification matrix.

---

## 14. Open questions / risks

1. **TinyGo + protobuf — largely resolved.** The old top risk (reflection-heavy official protobuf) is
   answered by **`protobuf-go-lite`** (reflection-free, TinyGo-built) + **`protobuf-es-lite`** for the web
   host (§4). Residual: confirm go-lite round-trips under `tinygo build -target=wasi` (Phase-0 spike 2),
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
