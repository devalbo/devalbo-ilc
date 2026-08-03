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

> **`--platform-path` is half-retired.** A scaffolded app depends on the ILC platform twice — as a Go
> module and as an npm package — and the two halves are now distributed differently:
>
> | | How a scaffold resolves it | Needs `--platform-path`? |
> | --- | --- | --- |
> | **`@devalbo/dlc-web`** (npm) | ✅ a **git ref** — `github:devalbo/devalbo-ilc#dlc-web-v0.1.0` | **no.** The flag still overrides it with a `file:` dependency, which is what this repo's own checks use |
> | **`dlc-platform`** (Go) | a `replace` directive pointing at your checkout | **yes**, still |
>
> **Neither package goes to a registry.** npm cannot install from a subdirectory of a git repo — the
> `#fragment` is a committish, not a path — so `dlc-platform/web` is released as a branch whose *root* is the
> package, via the `git subtree split` in [`scripts/release-dlc-web.sh`](scripts/release-dlc-web.sh). That
> the pinned ref resolves **and is complete** is checked nightly by `make verify-platform-ref`: every other
> check passes `--platform-path`, so none of them would notice a bumped constant with no pushed tag.
>
> **The Go half is now fetchable too** — its module path was renamed to match the directory it lives in
> (`github.com/devalbo/devalbo-ilc/dlc-platform`), which is what Go requires of a module inside a repo. What
> is left is a tag and a pinned version in the template; until then the scaffold still writes a `replace`.

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
| **Embedded** (RP2350 badge; ESP32 **RISC-V only**) | Wasmtime + **Pulley** (`no_std` interpreter) | Rust | 📋 [`EMBEDDED-PLAN.md`](docs/EMBEDDED-PLAN.md) — reverses the WAMR choice, because Pulley runs *components* and WAMR cannot |

📋 A capability a tier lacks returns `unavailable` and the engine degrades gracefully. The graceful-
degradation path is designed but unexercised: only Console + Filesystem exist, and both tiers have them.

> **"The same wasm everywhere" means the browser and the badge — not the CLI, and not byte-identical.**
> Worth stating plainly before the claim drifts:
>
> - **Browser and embedded** run the *same component*. The badge runs a `.cwasm` that
>   `wasmtime compile --target pulley32` produces from it — one engine build, one component, mechanically
>   retargeted. Strong unity, but **not the same bytes**, and the AOT step is real.
> - **The CLI links the engine natively** (Decision 26) and does not run wasm at all. Re-probed against
>   `wasmtime-go` v47 on 2026-08-02 (`spikes/wasmtime-go-cm/`): it can load and introspect a component but
>   cannot define imports or call exports, so this is not a preference — it is the only option today.
>
> What makes the tiers agree is not one binary; it is **one engine source plus checks that diff the
> results** — which is what `verify-parity` has always actually been.

### Windows: apps SHIP there; you BUILD them elsewhere

Two different questions, and only one of them is yes today.

| | State |
| --- | --- |
| **A binary your app ships** runs natively on Windows | ✅ the CLI tier cross-builds for `GOOS=windows` on every push, and `dlc-platform`'s tests — dispatch, path containment, BFT, the index — run on a `windows-latest` runner |
| **Developing an app** (or this repo) on Windows | 🚧 **WSL2**, per [prerequisites](docs/DEVALBO-DLC-PREREQUISITES.md#5-supported-platforms). Devbox is nix-based, and a scaffolded project's `make gen` assumes a unix shell |

**This is a deliberate boundary, not an oversight.** Making `dlc new` produce something buildable on native
Windows means replacing `make` with `dlc` subcommands and finding a non-nix toolchain story — a real
project, and one nobody has asked for yet. It is worth doing when a Windows *developer* turns up; it is not
worth doing speculatively.

**What the ship-side promise actually covers**, since "runs on Windows" is easy to over-claim: paths above
the filesystem seam stay `/`-separated so a bundle exported on Windows matches one exported on Linux, and
anything an app turns into a filename is validated against Windows' rules — case-insensitivity, the illegal
characters, the reserved device names — **on every platform**, so a rule cannot pass on your machine and
fail on a user's. See `AGENTS.md` §3·9.

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
| **Derived index** (§6.2) — a projection the engine owns, stored behind a `wasi:keyvalue`-shaped seam that is file-backed today | ✅ `rebuild-index` is inherited; notes maintains one and `list` answers from it **with no branch on any tier**. Not SQLite: the sort is Go, so the backend can change a duration but never a result. The invariant is that a maintained index equals a rebuilt one |
| Display · Network · sync | 📋 |

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
| `make verify-scaffold-env` | a scaffolded project generates and builds in **its own** declared devbox — this repo's toolchain scrubbed off PATH, so a tool the template forgets to declare fails here instead of on a new user (nightly; needs the network) |
| `make ci` | what CI runs, identically — `fast` / `full` / `all` tiers |

✅ **CI runs them** — `./scripts/ci.sh full` on push, `all` (adding the B1 spikes and the scaffold's own
environment) nightly. The script is
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
| [`docs/EMBEDDED-PLAN.md`](docs/EMBEDDED-PLAN.md) | 📋 the embedded tier — one component on a badge via Wasmtime + Pulley |
| [`docs/WASI-UPGRADES.md`](docs/WASI-UPGRADES.md) | WASI / Component Model version gates per platform |
| [`docs/archives/`](docs/archives/) | Superseded planning history (incl. the tri-language design) |

## Status

**Bootstrap milestone: met.** `dlc` (App #1) runs in the **terminal and the browser** from one shared
engine using Console + Filesystem — and so do the projects it generates, each shipping its own browser
test. The cross-tier claim is checked rather than asserted: golden vectors diff the native and wasm
engines (results *and* the filesystems they write), and a BFT bundle exported in Chromium rebuilds an
identical tree through the CLI.

The honest gaps, in the order they matter:

1. 🚧 **The Go module still needs `--platform-path`, but only until it is tagged.** The structural blocker
   is gone. It used to declare `module github.com/devalbo/dlc-platform` — a repo that does not exist — and
   Go resolves a module path to its own repo, so it could never be fetched at all. It is now
   `github.com/devalbo/devalbo-ilc/dlc-platform`, matching the directory it lives in, which Go fetches from
   a `dlc-platform/vX.Y.Z` tag. Remaining: cut that tag, then pin the version in the template the way
   `PlatformWebRef` pins the npm side. **Splitting the platform into its own repo is no longer a
   prerequisite for anyone outside using it** — it stays available as an independence decision, worth taking
   when the platform has a consumer that is not `dlc`.
2. 📋 **Desktop, embedded, and the remaining capabilities** (sync) are designed and
   unbuilt — see the tasks doc. **The index landed** (`docs/INDEX-PLAN.md`) after being re-scoped away from
   SQLite: `ORDER BY` was the only argument for SQL, and moving the sort into Go makes the index a
   projection cache the engine owns on every tier — including embedded, which SQLite could never reach. It
   costs no host capability, so there is nothing for an app to branch on. What remains there is a real KV
   backend, which **embedded forces first** — a whole-file rewrite per write is a flash-endurance problem
   before it is a speed one. Events is the other capability
   that landed, and it built the `caps_native` / `caps_wasip2` seam the rest of them inherit. **Display is now optional** (Decision 34): an app either
   renders app-side through that capability, or emits a *semantic* event and lets each host draw it
   however that tier likes — the second costs no capability at all, so it goes first.
3. ✅ **Per-app, per-tier host code has a shape** (Decision 34, `docs/HOST-LAYER-PLAN.md`): inherited
   runtime in `dlc-platform/`, this app's presentation and input in `hosts/<tier>/`, one slot per tier
   declared in `dlc.toml`. Host code renders and never decides, enforced by host parity. What remains is
   that a slot is only checkable against another slot — a single-tier app's rendering is unverified.
4. 🚧 **Cross-tab is SAFE but coarse; cross-process is still open.** A second tab on the same OPFS origin
   now learns that the store moved (`BroadcastChannel`, keyed on the flush rather than on events, since a
   write that emits nothing still moves the store) and **stops**, because the web tier holds a whole-tree
   snapshot whose flush prunes — a stale tab's write used to delete the other tab's work, measured as two
   tabs creating one note each and leaving one note on disk. The response is a reload, which is coarse but
   honest: a running component cannot be rebound to a new filesystem root. What is NOT here is a stale tab
   catching up in place. **Cross-process is a different question and was checked rather than assumed:** the
   snapshot-clobber cannot cross a process, because OPFS is browser-private and the native tier holds no
   snapshot. What can is a lost index entry when two CLI processes write at once — repairable by
   `rebuild-index`, and the first real case for the lock §7.1 deferred.

## License

See [LICENSE](LICENSE).
