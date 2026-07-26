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
| CLI front-end + the universal `execute(method, request)` entry (§8) | *Optionally* a storage pattern (§7.1) |

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
| 2 | Runtime substrate | **Component Model** on web (jco) and as the parity/portability artifact; **CLI/desktop link the engine natively in-process** for bootstrap (Decision 26, not CM-under-wasmtime); **core WASM + WASI Preview 1 + native host functions** on WAMR embedded — WAMR has **no** CM / WASI-0.2 yet (tracked divergence, §5.3) |
| 3 | Artifact model | **Component tiers (web/desktop/CLI): TinyGo `-target=wasip2` → a WASM component directly** — one shared `engine.component.wasm` (validated by Spike 1; the `wasip1`+adapter path is abandoned — §5.3). **Embedded/WAMR** runs core WASM, not a component — its build is revisited when embedded lands; RP2040 links native. Per-platform host (the "two bits"). |
| 4 | Capability boundary | **Neutral WIT world** — polyglot-capable; Rust/C-via-WASI can interop |
| 5 | Embedded floor | **Go/WASM is the floor** (no Rust `no_std` tier); Rust-via-WASI still welcome |
| 6 | FileSystem | **WASI standard** via Go `os`; host `setPreopens` the root — *not* a custom capability |
| 7 | Storage model | **Split:** JSON files = source of truth; **SQLite = disposable derived index** |
| 8 | On-disk format | **Proto-schema'd canonical JSON** (protobuf governs shape/evolution; binary proto on the wire) |
| 9 | Sync | **Plain file LWW** (sync JSON docs, rebuild index locally); no CRDT-SQLite |
| 10 | Dispatch | **Local-only in V1** (no envelope/routing yet) |
| 11 | Async bridge (Rich/CM vs WAMR split) | **Rich/CM:** Spike 5 ✅ — stock jco **JSPI** (Node ≥24 + `--experimental-wasm-jspi` + async import/export) awaits Promise host imports; sync transpile cannot (negative control). **No ILC shims.** **Portable/WAMR:** Spike 5 ✅ — TinyGo wasip1 + blocking `wasmimport` host (WAMR native-fn shape). See [`WASI-UPGRADES.md`](./WASI-UPGRADES.md). |
| 12 | Capability set | WASI FS · SQLite-index · Events · Display · **Console** (WASI stdio, not a custom interface — §6.5) |
| 13 | Display | **Discovery (`describe`) + draw-command list + retained widget tree**; output-only |
| 14 | Input *(entry renamed by Decision 28)* | **One universal entry**; host maps native input → command (serial REPL / input-map on MCUs). Originally `execute-cli(argv)`; now `execute(method, request)` — the principle (one entry, host-built input) is unchanged. |
| 15 | Reactivity | Engine emits **events**; host subscribes; UI invalidates + re-fetches |
| 16 | Pilot | **Shared notes/list** local-first app |
| 17 | Tiers | **All six supported in V1** (Web · Desktop · CLI · ESP32-S3 · RP2350 · RP2040); an *app* picks a subset (§16.1) |
| 18 | Embedded exec | **Mixed by capacity:** ESP32-S3 / RP2350 → WAMR+wasm; RP2040 → native TinyGo |
| 19 | Compiler | **Retire** the custom Rust WIT→3-lang compiler; use `wit-bindgen-go` + `buf` |
| 20 | WASI standards reuse | Console → **WASI stdio** (drop custom `console-io`); CLI entry → **`wasi:cli/command`** + a custom persistent `execute-cli` export *(renamed `execute` by Decision 28)*; test host → **WASI Virt**; `sqlite-host` / `event-host` / `display-host` mirror **wasi:keyvalue** / **wasi:messaging** / **wasi-gfx** shapes (§6.6) |
| 21 | Filesystem export/import | **First-class platform primitive** (§7.3): app state = a portable FS bundle (`--format=bft\|zip\|proto`) for test setup/teardown, golden snapshots, backup/restore, bug repro, cross-tier migration, node bootstrap, and **BFT interchange when 2 apps/versions share a store**. Engine-side, tier-agnostic (uses only the filesystem cap). `dlc new` = importing a template bundle. |
| 22 | ~~CLI interpreter — in-engine~~ **⚠ SUPERSEDED by Decision 28** | *Retained for history.* Originally: the CLI parser lived **inside the engine** (host forwards argv to `execute-cli`), TinyGo-safe, ffcli default (Spike 4). **Reversed by Decision 28** — parsing is a host-side front-end; the engine takes a structured request. Spike 4's bake-off is re-scoped to the *host* parser (ffcli still the reference); its findings (kong panics, cobra `-name`, hand-rolled the leanest) stand as data. |
| 23 | Two-phase launch | Starting an app = **(1) launch the Environment** (host wires caps + mounts the FS root, optionally `import-fs`-seeded) then **(2) run the engine** (one-shot `execute-cli`→exit *(now `execute`, Decision 28)*, or persistent → many invocations). One command does both; splittable for test / persistent / dev (§5.5). |
| 24 | Project metadata | An **`dlc.toml`** (or proto-schema'd `dlc.json`) manifest is the app's config source of truth — capabilities, tiers, storage, UI, launch mode/seed, and the pinned `platform` version (§16.8). `dlc` commands read/write it; it drives scaffold / build / verify / host-add / launch and enables regenerate/upgrade. |
| 25 | ABI mode (WAMR toggle) | Targeting **WAMR/embedded** is a **setup-time** choice (`dlc new` / manifest `tiers`) that fixes the capability-boundary ABI: **on → portable byte ABI** (protobuf over `(ptr,len)`; also builds the wasip1 core; port-ready rules — §5.6); **off → rich Component-Model ABI** (rich WIT types, wasip2 only). Disk/wire stays protobuf either way. |
| 26 | Native host runs the engine in-process (Go); wasm is the parity contract | The CLI/desktop host **links the engine as a native Go package** and calls `execute-cli` *(now `execute`, Decision 28)* **in-process** — no wasm runtime in the run path, which sidesteps the `wasmtime-go` Component-Model gap and gives full-Go dev speed. This is a **build seam, not a fork** (§5.3/§5.4): one engine codebase behind the WIT-shaped interface, two host bindings — native in-process caps (`caps_native.go`) · wasm-component caps. The **wasm component stays the source of truth**: web requires it (B3) and it anchors the cross-tier identity guarantee, which moves from runtime to **CI / `dlc verify`** — run the CLI against the *wasm* engine and diff golden `command-result` vectors vs the native run. Scope: `dlc`-the-tool + non-embedded hosts; the capability-sandbox promise still binds the **apps `dlc` scaffolds** (web/embedded are wasm-mandatory). |
| 27 | Build/run units are **tiers** (`dlc build/run/verify <tier>`) | A project holds **one app** (its engine) built for a set of **tiers** (native/web/desktop/embedded/…). A tier is a **composition recipe** — the *shared* engine × a host/environment binding + ABI mode (Decision 25) + cap set — **not** a per-tier fork of the logic, preserving the two-bit invariant + Decision 26 parity. `dlc build <tier>` (orchestrates the toolchain), `dlc run <tier>` (two-phase launch, Decision 23), and `dlc verify <tier>` (§11 matrix; `--parity` = the Decision 26 native↔wasm check) are **host-side** — tier selection never enters engine logic (§5.4). Registry = `dlc.toml [tiers]` (§16.8); built-ins `native` + `web`. `dlc`-as-orchestrator is Backlog (§16.7): `make` bootstraps the first `dlc`, then these supersede it for scaffolded apps. Since one project = one app, no `(app × tier)` noun is needed — the tier *is* the unit. |
| 28 | CLI/argv parsing is a platform front-end; the engine takes a structured request (**supersedes Decision 22**) | The CLI is a *mechanism for constructing a request* for the shared business logic — so **parsing lives in the host, not the engine**. Each tier builds the request its own way (native: cobra/ffcli + `huh` menus; web: React form; embedded: REPL/input-map), and the engine exports **operations over a structured request** — `execute(method: u32, request: list<u8>) -> command-result` (scalar `method_id` selects the handler, proto-bytes payload; input now symmetric with the already-protobuf output; §8/Decision 10, promoted from backlog to the primary boundary). **Wins:** native front-end off the TinyGo leash (cobra/kong/`huh` all fine, Decision 26); menus/prompts fall out naturally; the boundary is typed + `buf breaking`-evolvable, not stringly-typed argv. **Cost:** each host builds the request (embedded reparses in its host lang, or calls an *optional* in-engine `parse(line)→request` helper) — drift bounded by the **shared proto schema**, not argv. Spike 4 **re-scoped** (informs the *host* parser; ffcli the reference; TinyGo-safety pressure relaxes). Bootstrap keeps `execute-cli(argv)` as a transitional shim until the envelope lands. |
| 29 | Self-describing command registry — service + `method_id`, direct request/response messages | The **app registers each command once** as a proto **`service` rpc** — `rpc New(NewRequest) returns (NewResponse) { option (method_id) = N; }` — plus a handler `func(*NewRequest) *NewResponse`. **Dispatch keys on the permanent `method_id`** (`map[u32]handler`; rename-safe — the *name* is cosmetic / the CLI verb; `buf breaking` + the plugin's id-lock guard the number). The wire passes the **request/response messages directly** (single-encode, flat — Spike 2-proven); no oneof or envelope for the command surface (the discriminator is a scalar param, Decision 31). **Introspection is host-side, standard protobuf:** the host embeds the `buf build` **FileDescriptorSet** and walks it with **protoreflect** (native Go) / `@bufbuild/protobuf` (web) to discover methods + `method_id` + request fields — field name→flag, type→flag type, **proto `enum`→menu choices**, with help/required/default from custom field options. The engine (TinyGo, no reflection) keeps **only** the `method_id→handler` map; a `describe()` export is **optional** — only for a *generic* host that doesn't embed the schema. **`protoc-gen-dlc-registry`** emits the engine registration + enforces `method_id` stability — reading the **`buf build` image**, since go-lite emits *no service stubs* (spike `options/`). (oneof retained for *response variants*, not command dispatch.) **Spike-confirmed (`spikes/options/`, all criteria green):** go-lite accepts `descriptor.proto` + `extend MethodOptions` and keeps `google.golang.org/protobuf` out of the guest graph; hosts read the options as **unknown fields** via `dynamicpb.NewExtensionType` + `Range` (not `HasExtension`). Options live in `proto/devalbo/options/v1/options.proto` (`method_id` = 50000; `help`/`required`/`default`/`short` = 50001-4). **Layout caveat:** *field* options must sit at the field's definition site, so a file holding request messages always imports the options package — the host-only/engine-only split can isolate the `service`, never the field metadata. |
| 30 | Command surface splits two ways — in-engine vs host-side | A `dlc` command is an **in-engine `execute` handler** if it only touches the **filesystem / app-data** (portable — runs in terminal *and* browser): `new`, `export-fs`, `import-fs`, app-domain commands. It's **host-side orchestration** if it must **spawn the dev toolchain or inspect the machine** (native-only, can't run in wasm/browser): `build`, `run`, `verify`, `doctor`, `gen`, `host add`, `proto`. The native `dlc` host **routes**: a toolchain verb → handle in-host; anything else → build the request and forward to the engine's `execute` (Decisions 28/29). Not a violation of the old 'host forwards argv' — host verbs are *dlc-the-tool's*, not the *app's* surface. Governs where each handler lives + how the native `main()` dispatches. |
| 31 | Single proto-bytes command boundary; `supported-abis()` negotiation; rich WIT reserved for CM-only caps | The guest exposes **one command boundary** — `execute(method: u32, request: list<u8>) -> command-result` (scalar `method_id` + proto-bytes payload; Decisions 28/29) — universal across wasip2 **and** WAMR (scalars **and** byte buffers both cross the byte ABI; only rich WIT records/variants/strings/resources need the Component Model WAMR lacks — Decision 25). **Rich typed DX is host-side:** the host's generated binding (es-lite/go-lite) presents typed calls and serializes to the one byte boundary — no second guest ABI. A tiny **`supported-abis() -> list<u8>`** export (byte-ABI, readable even on WAMR) lets the guest advertise its boundaries + versions so a host picks the richest it supports. A **second, rich WIT guest boundary is reserved for Component-Model-only capabilities** (streams, `resource` handles, `wasi:http`, CM async) with no byte equivalent — added *per-capability*, negotiated, degrading to byte/absent on WAMR — never for the general command surface. When both exist they're two bindings over one shared registry (the caps-seam pattern), not duplicated logic. |

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

`go.mod` · `wit/ilc.wit` · `proto/devalbo/{ilc,dlc,<app>}/v1/*.proto` + `buf.yaml` / `buf.gen.yaml` ·
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
├── proto/                          # versioned packages in matching dirs (idiomatic buf; STANDARD lint)
│   ├── devalbo/ilc/v1/common.proto # shared types + errors (IlcError)
│   ├── devalbo/dlc/v1/commands.proto  # DlcService: in-engine verbs + method_id (Decision 29/30)
│   ├── devalbo/options/v1/options.proto # method_id + field metadata (host-side only)
│   ├── devalbo/<app>/v1/…          # per-app domain, e.g. record.proto / display.proto (pilot)
│   └── buf.yaml / buf.gen.yaml      # local buf (lint + breaking=WIRE_JSON; gen go/ts)
├── engine/                         # THE SHARED ENGINE (Go, → wasm)  ── business logic only
│   ├── execute.go                  # ExecuteMethod(method, request): the one entry (Decision 28/31)
│   ├── registry.go                 # method_id → handler map + typed-handler adapter (Decision 29)
│   ├── commands.go                 # registers each command once (what protoc-gen-dlc-registry emits)
│   ├── records.go, index.go, ...   # handlers; use os + injected caps only
│   └── (build: TinyGo → engine.wasm; native TinyGo for RP2040)
├── cmd/
│   ├── engine-component/           # wasip2 component entrypoint: adapts the WIT exports to the engine
│   └── parity-runner/              # dev tool: golden method-vectors through the native engine (§11)
├── gen/                            # generated: wit-bindgen-go (caps) + buf (proto) for go & ts
├── hosts/
│   ├── native/                     # Go host: engine in-process (Decision 26) + os FS + modernc sqlite (desktop/CLI)
│   ├── desktop/                    # Wails wrapper over hosts/native + webview
│   ├── web/                        # TypeScript host: worker.ts (jco + OPFS + sqlite-wasm), api.ts, hooks/
│   └── embedded/                   # ESP32-S3/RP2350: C host (ESP-IDF/arduino-pico) embedding WAMR + platformio.ini;
│                                    #   RP2040: native TinyGo (Go) linking the engine
├── frontend/                       # React + Vite UI (web + desktop webview)
├── verify/parity/                  # golden argv- + method-vectors + the jco harness (Decision 26)
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
| Engine → wasm | **TinyGo `-target=wasip2`** → a WASM component in one shot (validated — Spike 1; TinyGo supplies `cabi_realloc` + wires `_initialize`). World `include`s `wasi:cli/imports`; WASI WIT deps vendored under `wit/deps/`. The `wasip1`+`wasm-tools` **adapter path is abandoned** (fragile — §5.3). |
| Web instantiation | **`@bytecodealliance/jco`** transpile + `preview2-shim` (WASI/OPFS) |
| Web SQLite | **`@sqlite.org/sqlite-wasm`** (OPFS) |
| Native/desktop/CLI runtime | **engine linked in-process** (native Go, Decision 26) + `modernc.org/sqlite` + **Wails v2**; wasmtime kept for the wasm parity build |
| Embedded runtime | **WAMR** on ESP32-S3 / RP2350 (official Espressif WAMR ESP-IDF component); **TinyGo native** (RP2040) |
| Embedded build/flash/monitor | **PlatformIO** — host firmware (ESP-IDF / arduino-pico), flashing, `pio device monitor` (the serial REPL); TFT libs (TFT_eSPI/LovyanGFX) for the Display host. TinyGo flash for RP2040. |
| Async | Spike 5: Rich/CM ✅ (jco JSPI on Node ≥24); Portable/WAMR-shaped ✅ (wasip1 + blocking host). No ILC shims. |
| Test host (wasip2) | **WASI Virt** — compose a virtual FS + captured stdio onto the component (standardized test injection) |
| CLI parser (host-side, Decision 28) | Parses argv **in the host** into a structured request; native is off the TinyGo leash (cobra/ffcli/`huh`). The engine takes a proto `Command`, not argv. ffcli = reference; Spike 4 informs the host choice. Supersedes in-engine parsing (Decision 22). |
| Reproducible dev env | **`devbox`** (Jetify) — Nix-backed, pins the whole toolchain via `devbox.json`; PlatformIO owns the board toolchains (§4.1); the Docker alternative |

**Why the lite generators (resolves the old #1 risk):** the official `google.golang.org/protobuf` is
reflection-heavy and does **not** work under TinyGo (TinyGo implements only a subset of `reflect` —
tinygo issues #2667, #3376). `protobuf-go-lite` + `protobuf-es-lite` are reflection-free static codegen
built for exactly this; go-lite's bundled JSON output *also* satisfies the §7.2
canonical-JSON-on-disk decision from a single generator. **Constraint to respect:** go-lite drops
protobuf **fieldmasks and wire-message extensions** — V1's message surface needs neither. **Descriptor
custom options are fine** (Decision 29 `method_id` / field metadata): `spikes/options/` ✅ proved
(1) go-lite generates through `descriptor.proto`+`extend`, (2) TinyGo guest import graph has no
`google.golang.org/protobuf`, (3) host reads `method_id` from a FileDescriptorSet on a service rpc.
go-lite emits **no service stubs** — registry codegen reads the descriptor set. Prefer keeping the
`service`+options file out of the engine's import graph if go-lite's blank `descriptorpb` import
ever shows up in size budgets.

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
                    │  exports: execute(method: u32, request: list<u8>) -> command-result                    │
                    └───────────────────────────────────────────────────────────────────────────────────────┘
   web        desktop         cli            esp32-s3         rp2350           rp2040
   jco        native          native         WAMR             WAMR             (native TinyGo — same Go source,
   + TS host  + Go host       + Go host      + C host         + C/Go host       no wasm; engine linked in)
              (engine linked  (engine linked
               in-process)     in-process)   ← Decision 26; wasm still built for web + parity
```

- **The engine imports only the ILC world + standard WASI, and builds to a WASM component directly with
  TinyGo `-target=wasip2`** (validated — Spike 1). **Web** runs that `engine.component.wasm` under jco;
  **desktop/CLI link the same engine natively in-process** (Decision 26), keeping the wasm as the
  parity/portability artifact. **WAMR** (ESP32-S3 / RP2350) can't run a component — it runs core WASM, so
  capabilities bind as WAMR native functions and its build is revisited when embedded lands. What we build
  per tier and where interfaces differ is tracked in **§5.3**.
- **RP2040 is the one exception (Decision 18):** 264 KB RAM is too tight for a comfortable WASM runtime,
  so the *same Go engine source* is compiled **natively** with TinyGo and linked directly against a native
  host. One source, per-tier build — L3 everywhere it fits, native fallback where it doesn't.
- **The host is the only per-platform code**, and it is the injected `Environment`.

### 5.2 Why WASI makes FileSystem free

The engine writes files with plain Go `os` (`os.WriteFile`, `filepath.Join`, …). TinyGo lowers these to
WASI calls. Each host binds the WASI root:

| Host | WASI root preopen |
| --- | --- |
| Web | **OPFS** (`navigator.storage.getDirectory()`) **or** an **FSA-granted directory** (`showDirectoryPicker`, Chromium). Current `@bytecodealliance/preview2-shim` browser FS is an **in-memory FileData tree** (`_setFileData` / `_setPreopens({"/": tree})`), **not** a raw `FileSystemDirectoryHandle` — hydrate/flush OPFS ↔ FileData in the host (Spike 3, `spikes/opfs/opfs-bridge.js`). Patch or replace upstream browser `write`/`read` bigint handling before relying on stock shim. |
| Desktop / CLI | native user-data dir (`~/.config/<app>`) via the host's `os` FS (native seam, Decision 26) |
| ESP32-S3 / RP2350 | on-chip flash / littlefs, preopened by the WAMR host |
| RP2040 (native) | littlefs mounted directly; `os` shim maps to it |

**WASI preopens replace the old ILC mount-table / `VirtualPath` scheme entirely** — the runtime already
provides a bounded, host-selected filesystem root. That is the entire FileSystem capability.

### 5.3 What we build per tier (tracked)

**Build model (validated by Spike 1, 2026-07-25 — see `spikes/README.md`).** Component tiers
build with TinyGo **`-target=wasip2` directly** — TinyGo emits the component in one shot (supplying
`cabi_realloc`, wiring `_initialize`). The originally-planned `wasip1` core module + `wasm-tools`
**preview1 adapter** path is **abandoned**: it's fragile with current TinyGo/wasm-tools (missing
`cabi_realloc`, a `cm` module-path bug, an init-ordering crash — all documented in the spike README).
**WAMR reality:** WAMR supports **core WASM + WASI Preview 1 only** (no Component Model), so embedded can't
run the component — it needs a *separate* core-wasm build, and the "one artifact across *every* tier"
question is **deferred to when embedded lands**. For the bootstrap the shared unit is the **engine
codebase**: **web** loads it as the wasip2 `engine.component.wasm`; **CLI/desktop** link it natively
in-process, with that same wasm built for the parity check (Decision 26).

| Tier | Artifact built | Build pipeline | Capability binding | Divergence from the wasip2 baseline |
| --- | --- | --- | --- | --- |
| **Web** | `engine.component.wasm` | TinyGo **`-target=wasip2`** → **jco** | WIT imports (Component Model) | baseline |
| **Desktop** | native binary; also `engine.component.wasm` | `go build` links engine + TinyGo **`-target=wasip2`** (parity/web reuse) | **direct Go linkage** (`caps_native`); wasm build uses WIT imports | engine runs **in-process** (Decision 26); wasm anchors identity |
| **CLI** | native `dlc` binary; also `engine.component.wasm` | `go build` links engine + TinyGo **`-target=wasip2`** (parity) | **direct Go linkage** (`caps_native`); wasm may expose `wasi:cli/command` `run()` | engine runs **in-process** (Decision 26); wasm = CI parity |
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
| **native** (in-process Go engine) | `main()` — argv → host parser → `(method_id, request)` → native Environment → `execute` in-process → exit code | shipping the CLI binary |
| **desktop** (Wails) | Wails startup hook → native Environment + webview | building the desktop app |
| **web** (jco) | `worker.ts` boot → jco instantiate + browser Environment | serving the web app |
| **embedded** (WAMR / native) | firmware boot → board Environment (peripherals) | flashing the firmware |

**Native links the engine directly; wasm is the parity contract (Decision 26).** The CLI/`dlc` native
binary **imports the engine as a Go package** and calls `execute` **in-process** — no wasm runtime in
the run path, so nothing depends on a Component-Model API for `wasmtime-go`. The engine stays **one
codebase behind the WIT-shaped interface**: the native host supplies capabilities through a native seam
(`caps_native.go`), while web supplies them through the wasm/jco boundary. The **wasm component remains the
source of truth** — web requires it (B3), and a CI / `dlc verify` step runs the CLI *against the wasm
engine* and diffs golden `command-result` vectors vs the native run, so the cross-tier identity guarantee
holds (moved from runtime to CI). Keep the native engine binding behind a small package boundary under
`hosts/native/` so a wasm-runtime host can be swapped in later (when `wasmtime-go` grows a CM API, or via
the wasmtime C API) without rewriting argv → request → Environment → `execute`.

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
   RP2040) and run it against the environment — either **one-shot** (`execute` once → exit code) or
   **persistent** (the environment stays up; `execute` is invoked many times — the reactive UI /
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

### 5.6 WAMR / embedded porting practices (and the ABI-mode toggle)

Whether an app targets **WAMR/embedded is a setup-time choice** (`dlc new` / the manifest's `tiers`),
because it fixes the **capability-boundary ABI** for the whole project:

| Mode | Chosen when | In-process capability boundary | Builds | Constraints |
| --- | --- | --- | --- | --- |
| **Portable (byte ABI)** | an **embedded/WAMR** tier is targeted | **protobuf bytes over `(ptr,len)`** — lowers to core wasm | wasip2 component **+ wasip1 core** | the full porting rules below |
| **Rich (Component-Model ABI)** | **no embedded** tier | rich WIT types (`string`/`list`/`record`/`result`), Canonical-ABI-marshaled | wasip2 only | relaxed — leverage the CM ABI directly |

Disk/wire serialization stays **protobuf either way** (§7.2); the toggle only affects the *in-process
capability boundary* and which targets build. **Choose portable mode if embedded is even plausible** —
retrofitting the byte boundary later is invasive; starting rich and staying rich is fine if you never need
WAMR.

Porting down to WAMR crosses three gaps at once — **ABI** (no Component Model), **resources** (an MCU),
**execution** (no OS/scheduler). Most of the discipline is the engine rules we *already* have; embedded is
their stress test.

**Adopt:**
- **Capability-mediated I/O only** — no direct platform / OS / syscall / cgo calls (the ILC thesis; the
  thing that makes porting possible at all).
- **Byte boundary (protobuf over `(ptr,len)`)** in portable mode — identical on the Component Model and
  core wasm; custom caps bind as **WAMR native functions**, and the host **bounds-checks** guest pointers.
- **Reflection-free / TinyGo-safe** — protobuf-go-lite, no `encoding/json`; the engine's *handlers* stay reflection-free. The CLI **parser is host-side** (Decision 28), so on native it may use any lib (cobra/kong/`huh`); TinyGo-safety applies to handlers, not the parser.
- **Handle `unavailable` for every capability** — SQLite→file-scan, network/RTC maybe-absent; degrade,
  never hard-fail.
- **Short, run-to-completion, non-blocking handlers** — no long-lived goroutines / busy-waits (watchdog +
  power budget).
- **Minimize allocations + module size**; assume **bounded data** (small files/datasets, no reliable RTC);
  **batch flash writes** (endurance). Consider WAMR **AOT** for speed + lower RAM.
- **CI-build every target (wasip2 + wasip1 core) and test on real hardware early** — the embedded build
  rots silently, and emulation hides RAM / flash / timing.
- **Thin build-tagged capability seam** — `caps_wasip1.go` (`//go:wasmimport`) vs `caps_wasip2.go` (WIT);
  identical handler logic above it (§5.3).

**Avoid:**
- Rich-typed host boundaries in portable mode (can't lower to core wasm); **reflection / `encoding/json` /
  `net/http` / large stdlib** (breaks or bloats TinyGo); **assuming a capability exists** (SQLite / network
  / RTC / a big POSIX FS); **long-running goroutines / busy-waits / preemptive-scheduling assumptions**;
  allocation-heavy hot paths / deep recursion / large stack frames; **high-frequency small flash writes**;
  any direct hardware poke "just for embedded" (kills portability the instant it lands).

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
  // `execute` is the custom PERSISTENT entry (callable many times on a live instance, for reactive
  // UI). A one-shot CLI can additionally use the standard wasi:cli/command `run` + get-arguments.
  // `method` is the permanent method_id from the app's command service; `request` is the flat
  // proto-encoded request message (Decisions 28/31). Scalars + byte buffers both cross the byte ABI,
  // so this shape survives on WAMR.
  export execute: func(method: u32, request: list<u8>) -> command-result;
  // Bootstrap shim, retired by host-side parsing: export execute-cli: func(args: list<string>) -> command-result;
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
| CLI entry (one-shot) | **`wasi:cli/command`** (`run` + `get-arguments` + `exit`) | **Adopt** for the CLI tier; keep the custom persistent `execute` for reactive/multi-call tiers | stable |
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
- **Recovery:** a `rebuild-index` handler (its own `method_id`) scans all JSON files and rebuilds the index
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

- **Platform handlers** (via `execute`, every tier): `export-fs [prefix] --format=<fmt> -> bundle`,
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

**Universal principle (Decision 14, refined by Decision 28): every environment drives the *same handlers* — but each host builds the request its own way.** The engine exports **operations over a structured request** (`execute(method: u32, request: list<u8>) -> command-result`; scalar `method_id` + proto-bytes payload); the **host front-end** turns native input into that request. Parsing, menus, and prompts are **host-side** (Decision 28), never in the engine.

| Tier | How input becomes a request |
| --- | --- |
| CLI | argv → host parser (cobra / ffcli, `huh` menus for missing values) → `Command` proto → `execute` |
| Desktop | Wails IPC / UI events → `Command` proto → `execute` |
| Web | React form / events → `Command` proto → `execute` over Comlink |
| ESP32-S3 / RP2350 / RP2040 | (a) UART REPL — host (or optional in-engine `parse(line)`) → `Command`; (b) input-map — touch/button → `Command` (tap row → `open-record{id}`) |

Every tier feeds the same `execute` entry, so **there is one dispatch path** — but the request is **structured**, not argv. The protobuf **envelope** + multi-handler routing (Decision 10) is therefore promoted from backlog to the *primary* boundary; local-only in V1.

**CLI parsing is a host front-end (Decision 28, superseding Decision 22).** The CLI is a *mechanism for constructing a request* for the shared business logic, so each host parses in the way that fits it: the native `dlc` uses a full-Go parser + `huh`/`survey` menus (unshackled from TinyGo — Decision 26); web builds the request from form state; the embedded REPL parses a line in its host language, or calls an *optional* in-engine `parse(line)→request` helper to reuse the Go parser where convenient. What keeps tiers aligned is the **shared protobuf request schema**, not a shared argv parser.

**What must compile under TinyGo is the engine's *handlers*, not the parser.** Handlers (business logic + go-lite decode) still build under TinyGo, so the reflection-free discipline stands for them. The **parser moved host-side**, so on native it may be cobra / kong / go-arg / `huh` freely. Spike 4's bake-off now informs that **host** parser choice (ffcli remains the reference); its "must be reflection-free" pressure comes off the critical path.

**The command registry ties it together (Decision 29).** The app registers each command once — a proto `service` rpc (`rpc New(NewRequest) returns (NewResponse)`, permanent `method_id`) + a handler. The engine **routes on `method_id`** (`map[u32]handler`); request/response messages cross **directly** (flat, single-encode). Hosts don't ask the engine what commands exist — they **embed the `buf build` FileDescriptorSet and introspect it with protoreflect** (native) / `@bufbuild/protobuf` (web): field→flag, `enum`→menu, `method_id` for the wire. So host and engine can't drift (one schema), the parser stays host-side, and a `describe()` export is optional (only for a generic host without the embedded schema).

**One byte boundary, negotiated (Decision 31).** The guest exposes a *single* command entry — `execute(method: u32, request: list<u8>) -> command-result` (scalar id + proto-bytes payload) — WAMR-portable (scalars **and** byte buffers both cross the byte ABI; only rich WIT records/variants/strings need the Component Model WAMR lacks). Rich typed DX is a **host-side** generated wrapper over the same proto, not a second guest ABI; a small `supported-abis()` export lets the guest advertise its boundaries, and a rich WIT boundary is reserved *per-capability* for CM-only features (streams / resources / `wasi:http`).

**Framing — service + `method_id`, messages passed directly.** Commands are authored as proto `service` rpcs with permanent `method_id`s; request/response messages cross the wire **directly** — no oneof or envelope for the command surface (the discriminator is a scalar `method: u32` param, WAMR-fine per Decision 31). RPC-idiomatic (explicit req→resp pairing) with a rename-safe numeric key. `protoc-gen-dlc-registry` emits the engine's `method_id→handler` registration **from the buf image** (go-lite generates no service stubs, so the descriptors are the only source); host introspection uses the standard descriptor set (above). The oneof stays for *response variants*, not command dispatch. The concrete surface is `proto/devalbo/dlc/v1/commands.proto` — `DlcService` with permanent ids for the **in-engine** verbs only (`Version`, `Echo`, `New`, `ExportFs`, `ImportFs`; toolchain verbs stay host-side per Decision 30) — over the shared options package `proto/devalbo/options/v1/options.proto`. Failure rides the `command-result` envelope, so responses carry no error field.

**Reactivity loop:** the UI issues an `execute(list-records)` request to read; a write emits `data-changed`; the host's subscription (React `useEngineEvent` / Wails `EventsOn`) invalidates and re-fetches.

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
| **Desktop** | **native in-process** (wasm for parity) | Go (Wails) | `~/.config/<app>` | `modernc.org/sqlite` | Wails webview React | stdio / Wails IPC |
| **CLI** | **native in-process** (wasm for parity) | Go | cwd / config dir | `modernc.org/sqlite` | none (Console) | argv / stdin |
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
| **Cross-tier** | `make build-engine` | — | — | **`engine.component.wasm` (wasip2) builds byte-identical** (sha256); web runs it, CLI/desktop link the engine natively in-process (Decision 26). A shared **golden vector** (create→list→rebuild-index) produces identical `command-result` bytes across the native runs and the wasm parity run. (Embedded's core-wasm build + its identity check are deferred with the tier.) |
| **CLI** | `go build` (engine linked) + `tinygo`→wasm for parity | engine in-process (wasm loaded only for the parity check) | `app create-record …` / `list-records` / `rebuild-index` | JSON file written + `.lock` gone; index row present; `list` returns it; delete index file + `rebuild-index` reproduces it; native and wasm-parity outputs match |
| **Desktop** | `wails build` (engine linked in-process) | Wails boots the linked engine + webview | create/list/delete in UI | record renders; persisted under `~/.config/<app>`; `data-changed` event repaints; relaunch shows data |
| **Web** | `make build-wasm` (TinyGo→jco) | `worker.ts` sets OPFS preopen, boots jco, injects caps | create/list in browser | record persists in OPFS (survives reload); SQLite index file in OPFS; event repaints; DevTools shows no host-cap errors |
| **ESP32-S3** | `tinygo` builds `engine.core.wasm`; **`pio run -t upload`** flashes the WAMR host firmware (wasm embedded) | WAMR instantiates on boot | **`pio device monitor`**: `create-record …`; touch a row | JSON on littlefs; TFT renders the list via `draw`; `describe()` reports 320×240/rgb565; `execute-query`→`unavailable` path exercised (file-scan fallback) |
| **RP2350** | as ESP32-S3 via PlatformIO (`board=rpipico2`) **if** WAMR ports; else native TinyGo fallback | WAMR (or native) on boot | `pio device monitor` / buttons | same behavior from the *same* `engine.core.wasm` (WAMR) or same source (native); confirms the second embedded target |
| **RP2040** | `tinygo build` **native** (engine linked) + flash | firmware boot | serial REPL / buttons | same behavior from the **native** build — proves one-source parity where wasm doesn't fit |
| **Scaffolder** | `dlc new tmp --caps=… --tiers=…` | — | `devbox run verify` on the *generated* project | the scaffold **builds + passes its verify matrix** — templates can't silently rot (§16.6) |

**Phase-0 spikes.** (1) ✅ **DONE (2026-07-25)** — TinyGo **`-target=wasip2`** → component → jco round-trip
of a trivial `execute-cli` (`spikes/component/`); this **reshaped the plan to wasip2-direct** (the
wasip1+adapter path was abandoned). (2) ✅ **DONE (2026-07-25)** — `protobuf-go-lite` binary **and**
canonical-JSON round-trip under `tinygo build -target=wasip2` (+ `protobuf-es-lite` decodes the same bytes
in the web host) (`spikes/proto/`); (3) **deferred with the embedded tier** — WAMR
running a TinyGo core module on ESP32-S3 with one host import; (4) ✅ **DONE (2026-07-25)** — OPFS preopen
letting `os.WriteFile` persist across reload (`spikes/opfs/`); (5) ✅ **DONE** — `devbox` builds the core
(non-embedded) toolchain reproducibly; (6) ✅ **DONE (2026-07-25)** — in-engine CLI bake-off (`spikes/cli/`); **default ffcli** (Decision 22 → re-scoped by 28 + 25, §8); (7) ✅ **DONE (2026-07-25)** — Spike 5 Rich ✅ / Portable ✅ (`spikes/async/`); jco JSPI (Node ≥24) vs wasip1 blocking host ([`WASI-UPGRADES.md`](./WASI-UPGRADES.md)). **Each spike
records its findings in `spikes/<name>/README.md`; any finding that contradicts the plan updates the plan**
(as Spike 1 did). Any red spike reshapes the plan before the pilot.

---

## 12. Implementation phases

| Phase | Scope | Exit criterion |
| --- | --- | --- |
| **0a — Repo migration** | Tag + remove the retired tri-language Phase-1 code; scaffold the Go layout (**§2**) | working tree matches §3; `compiler`/`packages`/root Cargo removed (tagged); `go.mod` + `devbox.json` in place |
| **0b — Spikes** | The five §11 spikes | All green (or plan adjusted to the reality) |
| **1 — `dlc` app on CLI (App #1)** | Build **App #1 = `dlc`** (self-hosting scaffolder, §16.4): `wit/ilc.wit`, `proto/*`, `wit-bindgen-go` + `buf` wired; engine uses **console (WASI stdio) + WASI FS** only; **CLI host** (native, engine in-process — Decision 26); `dlc new` emits a minimal self-shaped skeleton; `dlc doctor` (readiness as a command, §16.7); `export-fs`/`import-fs` (§7.3) | `dlc new myapp` scaffolds + runs a working CLI project; `dlc doctor` reports readiness; golden **FS snapshot** defined |
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
   host (§4). Round-trips confirmed under `-target=wasip2` (Spike 2; the wasip1 path is abandoned).
   Residual: dropping fieldmasks/wire-message extensions must stay acceptable — descriptor custom options
   are a *different* category and are fine (`spikes/options/` ✅). Watch item: option-bearing message
   packages blank-import go-lite's own `descriptorpb` (~500 KiB of Go source); no `google.golang.org/protobuf`
   in the guest graph, but measure the wasm-size effect if it bites. No longer the highest risk.
2. **WAMR on RP2350 — footprint *and* port maturity.** 520 KB is comfortable-ish but TinyGo-runtime-in-wasm
   + WAMR + linear memory needs measuring; separately, **official WAMR is ESP-centric**, so RP2350 needs
   WAMR ported onto `arduino-pico`/Pico SDK (unproven). If either is too costly, RP2350 falls back to a
   native TinyGo build like RP2040. ESP32-S3 (official WAMR ESP-IDF component + PlatformIO) is the reference
   embedded tier; RP2350 is the stretch.
3. **"All six tiers in V1" is aggressive.** The phase order builds a reference tier first (CLI → web/desktop
   → embedded) so slippage degrades to "fewer tiers shipped," not "nothing works." Consider gating V1 on
   Phase 4 for CLI+web+desktop+ESP32-S3, with RP2350/RP2040 as fast-follows.
4. **Desktop/CLI: wasm-under-wasmtime vs native link — RESOLVED (Decision 26).** `wasmtime-go` lacks
   Component Model bindings (Spike 5 review), so rather than take on the C API for bootstrap, the CLI/desktop
   host **links the engine as a native Go package and runs it in-process** (like RP2040). The wasip2 wasm
   stays the source of truth for web + the cross-tier identity guarantee, now verified by a CI / `dlc verify`
   parity check (§5.4). A wasm-runtime host (wasmtime C API, or `wasmtime-go` once it has CM) can be swapped
   in later behind the same `hosts/native/` package boundary — no longer a blocking risk.
5. **Display input on embedded** beyond the input-map (Decision 14) — a symmetric **Input capability** is
   the Phase-5 portable answer if host-side input proves limiting.
6. **Component Model async maturity** — Spike 5 greens Rich async via **jco JSPI** on Node ≥24 (no ILC shim). WASI 0.3 remains the longer-term native CM async destination; gates in [`WASI-UPGRADES.md`](./WASI-UPGRADES.md). Browser JSPI re-check is a follow-up.
7. **Two build targets (component vs core-wasm); the adapter path is abandoned.** Component tiers build
   `-target=wasip2` directly (Spike 1). **WAMR** has no Component Model, so embedded needs a *separate*
   core-wasm build — a build seam behind build tags (§5.3), reconciled when embedded lands. The originally
   planned `wasip1`+`wasm-tools` **adapter** bridge is **abandoned** (fragile; see
   `spikes/README.md`). Risk: drift between the component and (future) core-wasm binding layers;
   mitigated by identical capability *shapes* + §11 behavior tests.

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
  enable in the build/verify matrix; the engine source is unchanged. **Including an embedded/WAMR tier
  flips the project to the *portable byte ABI* (§5.6, Decision 25); a CLI+web+desktop-only app can leverage
  the richer Component-Model ABI.**

### 16.2 The progression (CLI-first, then add tiers)

| Stage | Do | Result | Engine change |
| --- | --- | --- | --- |
| 0 — scaffold | `dlc new myapp` → engine skeleton + CLI host + Devbox + wit/proto | working CLI (host parser → `execute`) | — |
| 1 — logic | write handlers over the capabilities you need | CLI does real work | write |
| 2 — + UI | enable the web/desktop host; add a UI that builds requests and calls `execute` | same app in browser/window | **none** |
| 3 — + embedded | enable the WAMR host + native cap bindings; flash | same app on device | **none** (rebuilt to core-wasm) |

The engine is stable from Stage 1; later stages add **hosts**, not logic. Graceful `unavailable`
degradation means a cap added for one tier (e.g. Display) never breaks the tiers without it.

### 16.3 What each app writes vs inherits

- **Writes:** `engine/` handlers, `proto/<app>.proto`, the capability + tier selection, the UI (if any),
  optionally the split-storage pattern (§7.1).
- **Inherits unchanged:** host adapters, the `caps_*.go` build seam, the core-module + adapter pipeline,
  the Devbox/buf toolchain, the per-tier verification harness, and the host CLI front-end + `execute` entry.

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
├── templates/    # skeletons (+ later: per-skeleton git submodules) + local fragment overlays
│   ├── component-model/  # full dlc-shaped CM skeleton (CLI + browser); bootstrap — in-tree first
│   ├── wamr/             # WAMR skeleton; after embedded verify exists
│   └── fragments/        # in-tree overlay packs (--caps / --tiers / …); not whole-project submodules
└── apps/
    ├── dlc/      # App #1 — the scaffolder; go:embed's resolved /templates at build
    └── notes/    # App #2 — breadth pilot
```

#### Bootstrap sequencing (locked 2026-07-25)

| # | Choice |
| --- | --- |
| **1 — Authorship** | **Author skeletons in-tree** under `templates/<name>/`. Lift each to its own git submodule **later** (when contribution/CI isolation pays for itself) — do not block B2 on repo/submodule choreography. |
| **2 — `ilc-platform` depend** | **Defer** the versioned `ilc-platform` `go.mod` dependency until skeletons graduate to submodules (§16.4 extract). Until then the skeleton is a full tree in this repo; depend-on/never-inline remains the *target* rule, not a B2 prerequisite. |
| **3 — Skeleton completeness** | Bootstrap `component-model/` is a **full `dlc`-shaped** project (engine + CLI host stubs + web host stubs + go.mod + devbox + wit + proto) — not a thin hello-world. B2 proves terminal; B3 completes browser on the same shape. |
| **4 — Engine in the host** | Native `dlc` **embeds** `engine.component.wasm` in the host binary (§5.4); keep embed/load **lift-ready** (small package boundary). Templates for `dlc new` are separately `go:embed`’d into the engine so scaffolding works offline + in-browser. |

#### Standing rules

- **Depends-on, never inlines (destination).** After submodule + `ilc-platform` extract, a template’s
  `go.mod` depends on `ilc-platform` as a **versioned module** + a thin `main`; it never copies framework
  internals. Bootstrap may ship a fuller in-tree tree until that extract lands (see sequencing #2).
- **Two skeleton families by ABI mode (Decision 25).** Rich/CM vs Portable/WAMR are different *project*
  shapes (guest target, host stub, capability ABI, build). Each is its **own skeleton directory** (and
  later its own submodule), not a fragment toggle:

  | Skeleton | Track | Emits | When |
  | --- | --- | --- | --- |
  | `templates/component-model/` | Rich / Component Model | wasip2 component; native in-process (CLI, Decision 26) + jco (browser); rich WIT | **Bootstrap first** — full terminal + browser `dlc` shape |
  | `templates/wamr/` | Portable / WAMR | wasip1 core; native-fn caps; byte ABI | **After** embedded/WAMR can `verify` — do not ship an unverifiable stub |

  Directory names follow the **substrate** (`component-model`, `wamr`); track prose stays Rich/CM vs
  Portable/WAMR (Decision 25, [`WASI-UPGRADES.md`](./WASI-UPGRADES.md)). `dlc new` / manifest `tiers`
  selects the family. Shared bits may later move into `templates/fragments/` once both skeletons exist.
- **Submodules are a graduation step, not day-one.** Target: one git submodule per project skeleton under
  `templates/<name>/` for independent build/verify/PRs. **Bootstrap authors in-tree**; lift when ready.
  Only add / graduate a skeleton when CI can validate it.
- **Bootstrap starts with `component-model/`.** Phase B2/B3 ship that skeleton only — one engine for
  terminal and browser (Decision 3). `wamr/` is Backlog until the WAMR track is real.
- **Fragments stay local (not submodules).** `--caps` / `--tiers` / `--ui` / `--storage` overlays are
  small packs under `templates/fragments/` (or equivalent), composed onto a skeleton via `import-fs`
  (§7.3). ABI-mode is **not** a fragment — it picks the skeleton.
- **Anti-drift by validation, not derivation.** Drift is caught by CI — the §11 **Scaffolder** row
  (`dlc new … → devbox run verify`) must pass.
- **Embedded, not cloned at runtime.** At `dlc` build time the resolved `templates/` tree is
  **`go:embed`’d** into the engine, so `dlc new` stays self-contained — offline and on the **browser
  tier**. Never runtime-`git clone` a template into the user’s project. (Submodule checkout, when used,
  is a *dev* boundary only.)
- **Composition + parameterization.** `dlc new` selects a **skeleton tree + fragment overlays**; a token
  pass substitutes `{{.Module}}` / `{{.AppName}}` / selected capability imports. Bundles are BFT/zip so
  they stay diffable.
- **ABI-mode-aware defaults.** Skeleton choice follows ABI mode (Decision 25). **CLI scaffolder default is
  ffcli** in both families; Spike 4 measured hand-rolled (~497 KiB wasip1) / go-arg as fall-backs if we
  split later.

**Prior art — Qroma** (`qromatech/qroma-project-generator`, `qroma.dev`). The author's earlier
embedded-Python generator validates this shape: templates lived in a **separate `qroma-project-template`
repo (submodule)**, `qroma new` substituted project identifiers, targeting firmware + protobuf + site +
optional app. ILC borrows the **separated-template model** (destination: **one submodule per skeleton**,
after in-tree bootstrap) and the **concern-scoped CLI** (§16.7), and independently confirms two Qroma
choices — **protobuf as the device↔app messaging layer** and **PlatformIO** for firmware. ILC's deltas:
**one WASM engine across every tier** (vs separate firmware/app/site artifacts), **`dlc` is itself an ILC
app** (self-hosting), and **`go:embed` not runtime-clone** (offline + browser).

### 16.7 The `dlc` command surface

Concern-scoped subcommands (Qroma-style `qroma new/build/pb/firmware/site`), parsed by the **host front-end** into a structured request (Decision 28, superseding Decision 22 — parsing is per-tier; native may use cobra/ffcli/`huh`), then dispatched to the engine's operations:

| Command | Does |
| --- | --- |
| `dlc new <app> --caps=… --tiers=… --ui=… --storage=…` | scaffold (import base + fragments, §16.6); `--build --git --run` for one-step setup |
| `dlc doctor` | assess readiness — system prereqs + per-tier toolchain/host; the **command form** of `scripts/preflight.sh` (Layer 1; the pure-bash script stays as the pre-toolchain Layer 0 gate — see prereqs doc) |
| `dlc build <tier>` / `dlc run <tier>` | build a tier (native `go build` · web tinygo→jco · embedded WAMR/PlatformIO) / launch it (two-phase, Decision 23). Tiers are composition recipes (Decision 27), not per-tier forks; `--all` builds the matrix |
| `dlc verify [<tier>]` | run the §11 per-tier build→load→run matrix; `dlc verify --parity` = the Decision 26 native↔wasm golden-vector check (`scripts/verify-parity.sh`) |
| `dlc gen` | run `buf generate` over the app's `commands.proto` → go-lite (engine) + es-lite (web) + **`protoc-gen-dlc-registry`** (dispatch map + `describe()` metadata, Decision 29). Host-side; supersedes `make gen` for scaffolded apps |
| `dlc proto …` | edit the app's `.proto` (add a command/field); `dlc gen` regenerates |
| `dlc host add <web\|desktop\|embedded>` | overlay a tier's host into an existing app (import a fragment) — adds a tier to `dlc.toml` |
| `dlc export-fs` / `dlc import-fs` | the §7.3 filesystem primitive, surfaced as commands |

`dlc` is a CLI+web ILC app (App #1); these are its registered `execute` handlers — **not bespoke tooling, the
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
tiers        = ["cli", "web", "desktop", "esp32-s3"]   # an embedded tier ⇒ portable byte ABI (§5.6)
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
- **ABI mode follows the tiers.** Any **embedded/WAMR** tier ⇒ **portable byte ABI** (protobuf over
  `(ptr,len)`, wasip1 core built, port-ready rules); otherwise the **rich Component-Model ABI** (wasip2
  only). `dlc new --wamr` forces portable mode even without an embedded tier (future-proofing). See §5.6 /
  Decision 25.
- **Regenerate / upgrade.** Because it pins `platform` + records the config, `dlc` can re-apply templates
  against a newer platform version (a scaffold *upgrade*), not just first-time generation.
- **Dogfood option:** define the manifest as a proto `Project` message → canonical-JSON `dlc.json`
  (validated + `buf breaking`-evolvable + diffable, §7.2). **TOML** is the hand-edit-friendlier alternative.
- **Prior art:** Qroma's "one source of truth and bill of materials" — the same idea.
