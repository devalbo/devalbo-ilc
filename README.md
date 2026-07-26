# devalbo-ilc

**Write your business logic once, in Go — run it in the terminal, the browser, on the desktop, and on
microcontrollers.** ILC (Inverted Line of Command) inverts the usual CLI: instead of a program reaching
into its environment, the environment's capabilities are *injected* into a portable engine. `dlc`
(**D**evalbo **L**ine of **C**ommand) is the CLI that scaffolds and drives ILC apps.

> **Naming:** **ILC** is the framework/concept; **`dlc`** is the tool. Framework artifacts keep the `ilc`
> name (`wit/ilc.wit`, the `devalbo:ilc` package, the `ilc-platform` module); the binary and its commands
> are `dlc`.

> ### 🚧 Bootstrapping — read the status tags
>
> This project is **mid-bootstrap**. Everything below is marked:
>
> | Tag | Meaning |
> | --- | --- |
> | ✅ | **Works today**, with an automated check that proves it |
> | 🚧 | **Partly built** — real, but with a stated gap |
> | 📋 | **Designed, not built** — the plan describes it; no code yet |
>
> The design is further along than the code. [`docs/DEVALBO-ILC-GO-PLAN.md`](docs/DEVALBO-ILC-GO-PLAN.md)
> is the authoritative *intent*; this file is the authoritative *state*.

## The idea — two bits

| Bit | What | Portability |
| --- | --- | --- |
| **Engine** (`engine/`) | all business logic, Go → WASM | one artifact, environment-independent |
| **Host** (`hosts/`) | provides the capabilities the engine imports; per-platform | Go / TypeScript / C |

The engine imports a capability world (console, filesystem, …) and never touches a platform API directly.
Each **host** wires those capabilities to its runtime and runs the *same* engine — so the logic is
identical whether it's a CLI process, a browser tab, or firmware. The WASM Component Model is the
injection substrate; **protobuf** is the one serialization story (disk + wire + capability boundary).

**✅ The one-engine claim is enforced, not asserted.** `make verify-parity` runs golden vectors through
the natively-linked engine *and* the wasip2 component and diffs both the results and the filesystems they
wrote. `make verify-parity-selftest` then injects a deliberate wasm-only divergence to prove that check
can actually fail.

## Quick start

Prerequisites are tiny — **Devbox + git + a browser** (Devbox auto-installs Nix); Devbox provisions the
rest (Go, TinyGo, buf, wasmtime, jco…). See [`docs/DEVALBO-DLC-PREREQUISITES.md`](docs/DEVALBO-DLC-PREREQUISITES.md).

```bash
./scripts/preflight.sh                     # assess your machine (or: make doctor)
devbox shell                               # provision the pinned toolchain

make build-host                            # ✅ builds ./dlc
./dlc new myapp --platform-path "$PWD"     # ✅ scaffold a project (see the note below)
cd myapp && make gen && make verify        # ✅ it generates, builds, and runs

make dev-web                               # ✅ dlc itself in the browser (React UI, OPFS)
```

> **`--platform-path` is temporary.** A scaffolded app depends on the ILC platform as a Go module, but
> `ilc-platform` is not published yet — so `dlc new` writes a `replace` directive pointing at your local
> checkout. It is clearly marked in the generated `go.mod` and goes away when the module is tagged.

## Runs everywhere the same engine fits

| Tier | Runtime | Host | State |
| --- | --- | --- | --- |
| **CLI** | engine linked in-process | Go | ✅ `dlc` runs; scaffolded apps build + run |
| **Web** | jco | TypeScript + React | 🚧 `dlc` runs in the browser (scaffold → OPFS → survives reload). Scaffolded apps have **no web host yet** |
| **Desktop** | — | Go (Wails) | 📋 |
| **Embedded** (ESP32-S3 / RP2350 / RP2040) | WAMR / native TinyGo | C / Go | 📋 — WAMR spike deferred |

📋 A capability a tier lacks returns `unavailable` and the engine degrades gracefully. The graceful-
degradation path is designed but unexercised: only Console + Filesystem exist, and both tiers have them.

> **The CLI does not use a wasm runtime** (Decision 26). It links the engine as a native Go package, which
> sidesteps `wasmtime-go`'s missing Component Model API and keeps dev fast. The wasm component remains the
> contract — CI diffs the two.

## What works today

| Capability | State |
| --- | --- |
| **Command boundary** — `execute(method: u32, request: list<u8>)`, dispatch on a permanent `method_id` | ✅ |
| **Codegen** — `protoc-gen-dlc-registry` emits ids + dispatch from the `.proto`; committed id lock fails the build on a renumber | ✅ |
| **Filesystem** — plain Go `os` over a host-bound root (process cwd natively, WASI preopen in wasm) | ✅ |
| **BFT bundles** — `export-fs` / `import-fs` / `reset-fs`; one human-readable JSON blob for a whole tree | ✅ browser → terminal interchange is checked in CI |
| **Scaffolding** — `dlc new` emits an embedded template; the output generates, tests, builds, and runs | ✅ terminal only |
| **Console** — stdio natively; browser stdio → `console.*` | 🚧 native works; the web wiring is not done |
| **Host-side arg parsing** (Decision 28) | 🚧 the web UI builds requests properly; the CLI still uses a transitional in-engine argv shim |
| SQLite index · Events · Display · Network · sync | 📋 |

## Repository layout

```
engine/            ✅ dlc's own business logic (Go → wasm) — reflection-free / TinyGo-safe
engine/platform/   ✅ what every app INHERITS: dispatch, fs root seam, path containment, BFT
cmd/               ✅ thin entrypoints: wasip2 component shim, codegen plugin, dev tools
hosts/native/      ✅ CLI host — engine linked in-process
hosts/web/         ✅ browser host — jco worker, OPFS, Comlink
wit/               ✅ the ILC capability world (framework)
proto/             ✅ message types + command services (go-lite + es-lite codegen)
frontend/          ✅ React + Vite web UI
templates/         ✅ what `dlc new` emits (go:embed'd) — depend-on, never inline
spikes/            ✅ de-risking proofs, kept as regression tests
verify/            ✅ cross-tier checks — golden vectors proving native == wasm
scripts/           ✅ preflight + the verify suites
docs/              plan, tasks, prerequisites, test steps
```

`engine/platform/` becomes the **`ilc-platform`** module (📋 not extracted or published yet). Method ids
are range-reserved today so that extraction is not a breaking change: **1–999 platform** (in
per-capability blocks), **1000+ the app**.

## Verification

Every claim above has a target behind it. `make test` runs all of them.

| Target | Proves |
| --- | --- |
| `make test-b0` | repo structure + toolchain integrity |
| `make test-b1` | the five de-risking spikes still pass |
| `make test-b2` | engine unit tests · native↔wasm parity · **the parity check can fail** · `dlc new` output builds and runs |
| `make test-b3` | `dlc` in headless Chromium: scaffold → OPFS → survives reload; BFT bundle crosses browser → terminal |

📋 **CI does not run these yet** — they are run locally.

## Documentation

| Doc | Purpose |
| --- | --- |
| [`docs/DEVALBO-ILC-GO-PLAN.md`](docs/DEVALBO-ILC-GO-PLAN.md) | **The authoritative plan** — architecture, decisions, capabilities |
| [`docs/DEVALBO-DLC-GO-TASKS.md`](docs/DEVALBO-DLC-GO-TASKS.md) | Implementation tasks (bootstrap + backlog) |
| [`docs/DEVALBO-DLC-PREREQUISITES.md`](docs/DEVALBO-DLC-PREREQUISITES.md) | Prerequisites & getting started |
| [`docs/DEVALBO-DLC-TEST-STEPS.md`](docs/DEVALBO-DLC-TEST-STEPS.md) | Regression test steps |
| [`docs/WASI-UPGRADES.md`](docs/WASI-UPGRADES.md) | WASI / Component Model version gates per platform |
| [`docs/archives/`](docs/archives/) | Superseded planning history (incl. the tri-language design) |

## Status

**Bootstrap milestone: mostly met.** `dlc` (App #1) runs in the **terminal and the browser** from one
shared engine, using Console + Filesystem, with a React UI on the web tier — and `dlc new` now emits a
project that actually builds and runs.

The honest gaps, in the order they matter:

1. 🚧 **Scaffolded apps are terminal-only.** The template has no web host, so "write once, run everywhere"
   is proven for `dlc` itself but not yet for what `dlc` generates.
2. 🚧 **`dlc`'s own CLI still parses argv inside the engine** — a transitional shim that Decision 28
   retires. The web UI already does it the right way.
3. 📋 **`ilc-platform` is not published**, so every scaffold needs `--platform-path`.
4. 📋 **No CI.** The suites exist and pass locally; nothing runs them on push.
5. 📋 **Desktop, embedded, and the richer capabilities** (SQLite index, events, display, sync) are
   designed and unbuilt — see the tasks doc.

## License

See [LICENSE](LICENSE).
