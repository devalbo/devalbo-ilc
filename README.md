# devalbo-ilc

**Write your business logic once, in Go — run it in the terminal, the browser, on the desktop, and on
microcontrollers.** ILC (Inverted Line of Command) inverts the usual CLI: instead of a program reaching
into its environment, the environment's capabilities are *injected* into a portable engine. `dlc`
(**D**evalbo **L**ine of **C**ommand) is the CLI that scaffolds and drives ILC apps.

> **Naming:** **ILC** is the framework/concept; **`dlc`** is the tool. Framework artifacts keep the `ilc`
> name (`wit/ilc.wit`, the `devalbo:ilc` package, the `dlc-platform` module); the binary and its commands
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
./dlc new myapp --platform-path "$PWD"     # ✅ scaffold a project (see the notes below)

cd myapp
make gen && go mod tidy                    # ✅ codegen BEFORE tidy — engine imports generated code
make verify                                # ✅ it builds and runs in the terminal
make build-web && make dev-web             # ✅ …and in a browser, on :5173
cd frontend && npm test                    # ✅ its own shipped browser test

# and dlc itself, which is just another ILC app:
make dev-web                               # ✅ dlc in the browser (React UI, OPFS)
```

> **`--platform-path` is temporary.** A scaffolded app depends on the ILC platform as a Go module (and an
> npm package), but neither is published yet — so `dlc new` writes a `replace` directive and a `file:`
> dependency pointing at your local checkout. Both are clearly marked in the generated project and go away
> when the packages are released.

> **Scaffolding is offline; building the result is not.** `dlc new` needs no network — the templates are
> compiled into the binary, and nothing is ever cloned at runtime (§16.6). But the *generated project* is a
> fresh module: its first `make gen && make build` resolves Go dependencies through `GOPROXY` and npm
> packages from the registry. With warm caches it works offline; from scratch it does not.

## Runs everywhere the same engine fits

| Tier | Runtime | Host | State |
| --- | --- | --- | --- |
| **CLI** | engine linked in-process | Go | ✅ `dlc` runs; scaffolded apps build + run |
| **Web** | jco | TypeScript | ✅ `dlc` runs in the browser (scaffold → OPFS → survives reload) **and so do scaffolded apps**, each with its own shipped browser test |
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
| **Scaffolding** — `dlc new` emits an embedded template; the output generates, tests, builds, and runs **on both tiers** | ✅ |
| **`dlc build web`** — supplies the WIT world from `dlc` itself, so apps carry none and cannot be stranded on a stale one | ✅ |
| **Console** — stdio natively; browser stdio → `console.*` | 🚧 native works; the web wiring is not done |
| **Host-side arg parsing** (Decision 28) | ✅ every tier builds requests — argv in `hosts/native`, a form on the web. The engine has **one** entry, `execute(method, request)` |
| **Events** (§6.3) — the engine's first custom capability *import*: `platform.Emit(topic, payload)` → host subscription → UI re-reads | ✅ same code both tiers; parity compares the emitted stream; the browser repaints for a write no UI handler made |
| **Environment manifest** (§6.4a) — the host states what it can DO; capability verbs register from it, so a command a host cannot serve is marked unavailable rather than failing as `unknown method_id` | ✅ `platform.Boot` sends it; parity compares the manifest **and** the registered command surface; a capability can go away mid-session and come back |
| SQLite index · Display · Network · sync | 📋 the index is next — the manifest is what makes its `unavailable` expressible |

## Repository layout

```
engine/            ✅ dlc's own business logic (Go → wasm) — reflection-free / TinyGo-safe
dlc-platform/      ✅ SEPARATE MODULE — what every app INHERITS: dispatch, fs root seam,
                      path containment, BFT, the WIT world, codegen, and web/ (@devalbo/dlc-web)
cmd/               ✅ thin entrypoints: wasip2 component shim, codegen plugin, dev tools
hosts/native/      ✅ CLI host — engine linked in-process
hosts/web/         ✅ browser host, published as @devalbo/dlc-web — jco worker, OPFS, Comlink, Vite preset
wit/               ✅ the ILC capability world (framework)
proto/             ✅ message types + command services (go-lite + es-lite codegen)
frontend/          ✅ dlc's own React + Vite UI (a scaffolded app gets a vanilla-TS one)
templates/         ✅ what `dlc new` emits (go:embed'd) — depend-on, never inline
spikes/            ✅ de-risking FINDINGS (one live spike; the rest retired, their lessons kept)
verify/            ✅ cross-tier checks — golden vectors proving native == wasm
scripts/           ✅ preflight + the verify suites
docs/              plan, tasks, prerequisites, test steps
```

`dlc-platform/` **is** the module (✅ extracted; 📋 not published — resolved by `replace`). Method ids
are band-reserved today so that extraction is not a breaking change: **1–9999 ILC** (subdivided by
capability, with 600–9999 held for capabilities not yet shipped), **10000+ the app**. `dlc` claims no
reserved block — it is an app like any other.

## Verification

Every claim above has a target behind it. `make test` runs all of them.

| Target | Proves |
| --- | --- |
| `make test-b0` | repo structure + toolchain integrity |
| `make test-b1` | the surviving de-risking spike (async); the rest retired once product code covered them — findings kept in `spikes/README.md` |
| `make test-b2` | engine unit tests · native↔wasm parity · **the parity check can fail** · `dlc new` output builds and runs · the scaffold matches its golden snapshot · the example apps build and pass |
| `make test-b3` | `dlc` in headless Chromium (scaffold → OPFS → survives reload) · BFT bundle crosses browser → terminal · a scaffolded app runs in a browser via its own test · **the example apps run in a browser** |
| `make ci` | what CI runs, identically — `fast` / `full` / `all` tiers |

✅ **CI runs them** — `./scripts/ci.sh full` on push, `all` (adding the B1 spikes) nightly. The script is
provider-agnostic and is the same command locally; [`.github/workflows/ci.yml`](.github/workflows/ci.yml)
is a ~20-line adapter, not the logic.

## Documentation

| Doc | Purpose |
| --- | --- |
| [`AGENTS.md`](AGENTS.md) | **Rules for changing this repo** — the constraints that are not visible in the code |
| [`docs/DEVALBO-ILC-GO-PLAN.md`](docs/DEVALBO-ILC-GO-PLAN.md) | **The authoritative plan** — architecture, decisions, capabilities |
| [`docs/DEVALBO-DLC-GO-TASKS.md`](docs/DEVALBO-DLC-GO-TASKS.md) | Implementation tasks (bootstrap + backlog) |
| [`docs/DEVALBO-DLC-PREREQUISITES.md`](docs/DEVALBO-DLC-PREREQUISITES.md) | Prerequisites & getting started |
| [`docs/DEVALBO-DLC-TEST-STEPS.md`](docs/DEVALBO-DLC-TEST-STEPS.md) | Regression test steps |
| [`docs/WASI-UPGRADES.md`](docs/WASI-UPGRADES.md) | WASI / Component Model version gates per platform |
| [`docs/archives/`](docs/archives/) | Superseded planning history (incl. the tri-language design) |

## Status

**Bootstrap milestone: met.** `dlc` (App #1) runs in the **terminal and the browser** from one shared
engine using Console + Filesystem — and so do the projects it generates, each shipping its own browser
test. The cross-tier claim is checked rather than asserted: golden vectors diff the native and wasm
engines (results *and* the filesystems they write), and a BFT bundle exported in Chromium rebuilds an
identical tree through the CLI.

The honest gaps, in the order they matter:

1. 📋 **`dlc-platform` and `@devalbo/dlc-web` are not published**, so every scaffold needs
   `--platform-path`. Publishing the Go module additionally requires committing the platform's generated
   proto code, which `/gen/` currently ignores.
2. 📋 **Desktop, embedded, and the remaining capabilities** (SQLite index, sync) are designed and
   unbuilt — see the tasks doc. Events is the one that landed, and it built the `caps_native` /
   `caps_wasip2` seam the rest of them inherit. **Display is now optional** (Decision 34): an app either
   renders app-side through that capability, or emits a *semantic* event and lets each host draw it
   however that tier likes — the second costs no capability at all, so it goes first.
3. 📋 **Per-app, per-tier host code has no agreed shape yet.** `hosts/web/` is inherited runtime,
   `hosts/native/` mixes runtime with `dlc`'s own code, and `frontend/` is app code under a third name.
   The line `engine/` vs `dlc-platform/` already draws is missing one directory over — that is the
   current focus (`docs/HOST-LAYER-PLAN.md`).
4. 📋 **Events do not cross a tab or a process.** Same graph only — worker → main thread, or native
   in-process. A second tab on the same OPFS origin does not see this one's writes; that needs a
   `BroadcastChannel` and is the natural follow-up.

## License

See [LICENSE](LICENSE).
