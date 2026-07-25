# devalbo-ilc

**Write your business logic once, in Go — run it in the terminal, the browser, on the desktop, and on
microcontrollers.** ILC (Inverted Line of Command) inverts the usual CLI: instead of a program reaching
into its environment, the environment's capabilities are *injected* into a portable engine. `dlc`
(**D**evalbo **L**ine of **C**ommand) is the CLI that scaffolds and drives ILC apps.

> **Naming:** **ILC** is the framework/concept; **`dlc`** is the tool. Framework artifacts keep the `ilc`
> name (`wit/ilc.wit`, the `devalbo:ilc` package, the `ilc-platform` module); the binary and its commands
> are `dlc`.

## The idea — two bits

| Bit | What | Portability |
| --- | --- | --- |
| **Engine** (`engine/`) | all business logic, Go → WASM | one artifact, environment-independent |
| **Host** (`hosts/`) | provides the capabilities the engine imports; per-platform | Go / TypeScript / C |

The engine imports a capability world (console, filesystem, …) and never touches a platform API directly.
Each **host** wires those capabilities to its runtime and runs the *same* engine — so the logic is
identical whether it's a CLI process, a browser tab, or firmware. The WASM Component Model is the
injection substrate; **protobuf** is the one serialization story (disk + wire + capability boundary).

## Quick start

Prerequisites are tiny — **Devbox + git + a browser** (Devbox auto-installs Nix); Devbox provisions the
rest (Go, TinyGo, buf, wasmtime, jco…). See [`docs/DEVALBO-DLC-PREREQUISITES.md`](docs/DEVALBO-DLC-PREREQUISITES.md).

```bash
./scripts/preflight.sh          # assess your machine (or: make doctor)
devbox shell                    # provision the pinned toolchain
dlc new myapp                   # scaffold a project (terminal)
make dev-web                    # run it in the browser (React UI)
```

## Runs everywhere the same engine fits

| Tier | Runtime | Host |
| --- | --- | --- |
| **CLI** | wasmtime | Go |
| **Web** | jco | TypeScript (React UI) |
| **Desktop** | wasmtime | Go (Wails) |
| **Embedded** (ESP32-S3 / RP2350 / RP2040) | WAMR / native TinyGo | C / Go |

A capability a tier lacks returns `unavailable` and the engine degrades gracefully — the handler never
changes across tiers.

## Repository layout

```
engine/       the shared business logic (Go → wasm) — reflection-free / TinyGo-safe
hosts/        per-platform capability providers (native, web) — no business logic
wit/          the ILC capability world (framework)
proto/        message types (protobuf; go-lite + es-lite codegen)
frontend/     React + Vite web UI
templates/    what `dlc new` emits (go:embed'd) — depend-on, never inline
spikes/       de-risking proofs, kept as regression tests
scripts/      pre-toolchain helpers (preflight)
docs/         plan, tasks, prerequisites, test steps
```

Each major folder has a thin README stating its one boundary rule.

## Documentation

| Doc | Purpose |
| --- | --- |
| [`docs/DEVALBO-ILC-GO-PLAN.md`](docs/DEVALBO-ILC-GO-PLAN.md) | **The authoritative plan** — architecture, decisions, capabilities |
| [`docs/DEVALBO-DLC-GO-TASKS.md`](docs/DEVALBO-DLC-GO-TASKS.md) | Implementation tasks (bootstrap + backlog) |
| [`docs/DEVALBO-DLC-PREREQUISITES.md`](docs/DEVALBO-DLC-PREREQUISITES.md) | Prerequisites & getting started |
| [`docs/DEVALBO-DLC-TEST-STEPS.md`](docs/DEVALBO-DLC-TEST-STEPS.md) | Regression test steps |
| [`docs/archives/`](docs/archives/) | Superseded planning history (incl. the tri-language design) |

## Status

**Bootstrapping.** The first milestone is `dlc` itself (App #1) running in the **terminal and the browser**
(Console + Filesystem, React UI) from one shared engine. Desktop, embedded, and the richer capabilities
(SQLite index, events, display, sync) follow — see the tasks doc.

## License

See [LICENSE](LICENSE).
