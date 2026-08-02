# DEVALBO-DLC — Implementation Tasks

Task breakdown derived from [`DEVALBO-ILC-GO-PLAN.md`](./DEVALBO-ILC-GO-PLAN.md) (authoritative). Naming:
**ILC** = the framework; **`dlc`** = the CLI tool (Devalbo Line of Command).

**The bootstrap is MET** (see the roll-up below) — `dlc` runs in the terminal and the browser from one
engine, and App #2 (`notes`) and the Events capability landed on top of it.

**The HOST LAYER is DONE** (Decision 34, [`HOST-LAYER-PLAN.md`](./HOST-LAYER-PLAN.md), all six phases).
Per-app, per-tier host code is a named, contracted, scaffolded thing; **Display dropped to optional**, with
tic-tac-toe as the worked example of the third render path — one engine, a DOM board and an ASCII board,
sharing only the schema. Three web routes (terminal, files, commands inspector) and the generated CLI
surface landed alongside it.

**The ENVIRONMENT MANIFEST is DONE** (Decision 32, [`ENVIRONMENT-PLAN.md`](./ENVIRONMENT-PLAN.md), all
four phases). A host states what it can DO; capability verbs register from that, so a command this host
cannot serve is marked unavailable instead of failing as `unknown method_id`, and a capability can go away
mid-session and come back. `platform.Boot` owns the startup order. Parity now compares the registered
command SURFACE as well as results — which it had to, because twice during the build parity was green
while every filesystem verb on both tiers was broken.

**The PLATFORM IS EXTRACTED** (§16.4). `dlc-platform/` is a real Go module —
`github.com/devalbo/devalbo-ilc/dlc-platform`, a path that matches its directory so Go can actually fetch it
(tag: `dlc-platform/vX.Y.Z`); `replace` still resolves it inside this repo — carrying the Go platform, the protos with **committed** generated code, the WIT world,
`protoc-gen-dlc-registry`, and the TS runtime (`@devalbo/dlc-web`). **A scaffolded app's module graph now
contains the platform and not `dlc`** — checked with `go list -m all` plus an offline build, not asserted.
`dlc`'s web slot moved `frontend/` → `hosts/web/`, so dlc finally has the layout it scaffolds.

**Current focus: nothing is claimed.** The candidates, roughly ordered: the **SQLite index** (§6.2, now
unblocked — the manifest makes `unavailable` expressible, and it is the second consumer that will catch a
wrong manifest schema while changing it is still cheap), **publishing `dlc-platform`** (extracted but not
tagged — deliberately deferred), and the **embedded tier**, which is where "no filesystem" and "no console"
stop being edge cases and become the normal shape.

**Note on what the extraction did NOT unblock:** dlc's hardcoded version. That cycle is `dlcconfig` being
written by the dlc binary built from the package that would import it, with the manifest parser still in
`hosts/native` (package main) — untouched by moving the platform out. It was listed as a beneficiary when
the extraction was proposed; that was wrong.

_Created 2026-07-25. Re-anchored 2026-07-27 — several boxes below were stale, ticked against what is
actually in the tree._

---

## 🎯 Bootstrap milestone (definition of done)

**`dlc` (App #1) runs in the terminal *and* the browser, from one shared engine, using only Console +
Filesystem, with a React UI on the web tier.**

Concretely:
- **Terminal:** `dlc new myapp` scaffolds a working project to disk; `dlc` output goes to stdio.
- **Browser:** `dlc` runs under jco with a **React UI**, scaffolds into **OPFS**, survives reload, and can
  download the result.
- **One engine codebase:** web loads it as the wasip2 component (jco); the CLI links it natively in-process, with the same wasm built for a CI parity check (Decision 26).
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
- [x] **Spike 4 — in-engine CLI interpreter:** parser lives **inside** the TinyGo engine; host forwards argv → `execute-cli`. Bake-off measured (see [`spikes/README.md`](../spikes/README.md)): flag / ffcli / hand / go-arg matrix-green; cobra almost (fails `-name`); kong panics (`MethodByName`); subcommands unusable (hardcodes `os.Args`). **Default: ffcli**; wasip1 sizes show hand (~497 KiB) ≪ ffcli (~1.23 MiB) if portable splits later. `spikes/cli/`, `make spike-cli` / `make test-b1`. Decision 22 (→ **re-scoped by Decision 28**: parsing is host-side; this now informs the *host* parser choice) + 25.
- [x] **Spike 5 — dual-track async probe:** **Rich ✅** (jco JSPI on Node ≥24 + `--async-exports execute-cli`; sync transpile remains negative control) · **Portable ✅** (TinyGo wasip1 + blocking `env.host_delay`; wazero stand-in for WAMR native fns). No ILC async shims. Pin `nodejs@24`. Findings → [`spikes/README.md`](../spikes/README.md) + [`WASI-UPGRADES.md`](./WASI-UPGRADES.md). `spikes/async/`, `make spike-async`. Decision 11 + 25.

**Exit:** Spikes 1–5 green; **each with a findings section in `spikes/README.md`**; plan-contradicting findings folded back (as Spike 1 did). **Retired 2026-07-26:** spikes 1–4, oneof, and options were deleted once product code covered their claims (see `spikes/README.md` for what covers what). Spike 5 stays — nothing else exercises async. The findings are unchanged.

---

## Phase B2 — `dlc` engine + terminal (CLI host)

### Contract & codegen
- [x] `wit/ilc.wit` — bootstrap world (Console via WASI stdio + Filesystem via WASI, provided by the target/host); exports **`execute(method: u32, request: list<u8>) -> command-result`** (the real boundary, Decisions 28/31) plus the transitional `execute-cli(args)` shim (§6)
- [x] `proto/devalbo/ilc/v1/common.proto` — `IlcError` taxonomy; `proto/devalbo/dlc/v1/dlc.proto` — `new` / `export-fs` / `import-fs` messages (versioned packages, idiomatic buf layout). **Removed:** `dlc.proto` was absorbed by `commands.proto` (below) — same package, same message names — and deleted along with its generated output; nothing outside `gen/` referenced it.
- [x] `proto/buf.yaml` + `proto/buf.gen.yaml` (go-lite → `gen/go`, es-lite → `gen/ts`); `make gen` wires wit-bindgen-go + buf
- [x] **Validate under devbox:** `make gen` runs clean (`buf lint` + generate); shakes out `buf.gen.yaml` opts + go_package (Spike 2) — verified: TinyGo 0.41.1, buf `STANDARD` lint green with the versioned layout, go-lite + es-lite output in distinct `devalbo/*/v1` dirs
- [x] `wit-bindgen-go generate` produces the capability bindings in `gen/go` (Spike 1) — `gen/go/devalbo/ilc/{engine,types}/`

### Engine (Go → wasm; business logic only)
- [x] **Command schema:** `proto/devalbo/dlc/v1/commands.proto` — a proto **`service`** with one **rpc per command** (`rpc New(NewRequest) returns (NewResponse) { option (method_id) = N; }`; framing settled — Decision 29 / §8). Request/response messages cross **directly** (flat; Spike 2-proven) — no oneof/envelope for command dispatch. Arg metadata via custom **field options** (required/default/help/short); enum choices come from proto `enum` fields natively. One `.proto` shared by go-lite (engine) · es-lite (web) · nanopb (embedded). **Dispatch keys on `method_id`** (rename-safe; the rpc *name* is cosmetic). Guard `method_id` + wire compat with `buf breaking`. Options package: `proto/devalbo/options/v1/options.proto` (spike-proven). **Status — ✅ landed, lint + codegen green:** `DlcService` with permanent ids `Version`=1, `Echo`=2, `New`=3, `ExportFs`=4, `ImportFs`=5 — **in-engine verbs only** (Decision 30; toolchain verbs absent by design). Enums `UiKind`/`StorageKind`/`BundleFormat` double as host-side menu choices. Errors ride the `command-result` envelope, so **no response carries an error field** (drops the old `NewResult.error`). The superseded `dlc.proto` (duplicate `NewRequest`/`ExportFs*`/`ImportFs*`/`BundleFormat` in the same package) is **removed** — nothing outside `gen/` referenced it; `buf lint` + `buf generate` pass.
- [x] **De-risk spike — go-lite oneof under TinyGo (✅ GREEN):** a 2-arm `Command` oneof → build + `MarshalVT`/`UnmarshalVT` + `MarshalJSON`/`UnmarshalJSON` round-trips + **switch-free `map[tag]handler` dispatch** under `-target=wasip2` — all pass (`make spike-oneof`). go-lite emits **no `WhichX()`**; the discriminator comes from a 1-line-per-arm type-switch (exactly what `protoc-gen-dlc-registry` will emit — the app never writes it). Registry is viable reflection-free. `spikes/oneof/`; findings → [`spikes/README.md`](../spikes/README.md). **Note:** command dispatch re-settled to flat messages + `method_id` (Spike 2 covers flat), so this oneof proof now de-risks *response variants* (e.g. `MathResponse`) — still GREEN, still useful.
- [x] **De-risk spike — go-lite + custom options (✅ GREEN):** three pass criteria all green — (1) `descriptor.proto` + `extend MethodOptions` → `buf lint`/`generate` via go-lite; (2) TinyGo wasip2 guest has **no** `google.golang.org/protobuf` in import graph; (3) host reads `method_id` off a **service rpc** from `buf build` FileDescriptorSet. go-lite emits **no service stubs** → `protoc-gen-dlc-registry` must consume the descriptor set. `make spike-options` / `./scripts/check-options-criteria.sh`. Findings → [`spikes/README.md`](../spikes/README.md). **Gates the registry schema** (Decision 29).
- [x] **Command registry (Decision 29):** `engine/registry.go` — `Register(method_id, handler)` into a **`map[u32]handler`**; the app registers each command once (handler `func(*FooRequest) *FooResponse`). Reflection-free. **Landed:** `Register` panics on a duplicate id; `typedHandler[Req, PReq, Resp]` adapts typed command funcs to the byte-level `Handler` using **generics, not reflection** (TinyGo-compiled clean under `-target=wasip2`). Registration + the `Method*` id constants live in `engine/commands.go` — hand-written on purpose, as the shape `protoc-gen-dlc-registry` must emit.
- [x] `engine/execute.go` — `execute(method: u32, request: list<u8>)` → `map[method]` lookup → handler decodes `request` as its `FooRequest` (flat, single-encode) → returns `FooResponse` bytes in `command-result` (Decision 28). **Landed** as `ExecuteMethod`; `version`/`echo`/`new` registered, `export-fs`/`import-fs` wait on the filesystem cap seam and report as unregistered (error result, never a panic). Shared scaffold logic moved to `engine/scaffold.go` so the argv shim and the `new` handler cannot drift. Covered by `engine/execute_test.go`. The `switch args[0]` walking skeleton survives only inside the argv shim, which retires with host-side parsing.
- [x] **Host introspection (Decision 29) — GENERATED, not reflected at runtime.** The plugin emits a `clispec` command surface (rpc→subcommand, request field→flag, `help`/`required`/`default`/`short`/`cli_name`/`cli_flag`/`cli_source`, and the rpc's doc comment as `-h` summary); `engine/platform/cli` turns it into a parser with `ffcli` + stdlib `flag`, and encodes with `protowire`. **Amends the original plan**, which said the host would embed the FileDescriptorSet and walk it with protoreflect: the plugin already reads that descriptor set and already reads custom options, and go-lite messages carry no protoreflect, so runtime walking would additionally need `dynamicpb` plus the unknown-field dance the options spike measured as unreliable (`HasExtension` differs across dynamicpb type identities). Generating plain data makes a schema change a compile error instead of a runtime surprise; wasm cost measured at **1.6 KB**. Engine `describe()` stays **optional** — only for a *generic* host that doesn't embed the schema. **Bootstrap shim retired (Decision 28 complete):** `execute-cli` is gone from the world, `engine.Execute`/ffcli are gone from the *engine* (component **1.91 MB → 1.52 MB**, ~20% smaller), and the argv parity stream retired — the 19 method vectors cover it.
  - [ ] follow-up: the **web** tier's equivalent — a generated *form* description rather than flags. The TS side currently gets ids only.
  - [ ] follow-up: `huh` menus for missing enum args (the surface already carries `EnumValues`)
- [x] **WIT boundary migration (Decisions 28/31):** `execute-cli(args: list<string>)` → **`execute(method: u32, request: list<u8>) -> command-result`** — scalar id + proto-bytes payload (WAMR-portable; only rich WIT records/variants need the Component Model). Keep the string-args shim until callers move. **Landed:** both exports declared on `world engine`; `cmd/engine-component` wires each through a shared `toCommandResult`. `make build-engine` (TinyGo wasip2) green, and `make verify-parity` now covers **both** boundaries — 9 argv vectors + 10 `execute(method, request)` byte-vectors (`verify/parity/method-vectors.json`, hex requests derived from typed fixtures via `make parity-vectors`; native side is `cmd/parity-runner` until hosts/native builds requests). The method diff includes the **error string**, so TinyGo and native Go must agree on envelope errors, unregistered ids, and decode failures too.
- [ ] `supported-abis() -> list<u8>` export (byte-ABI, Decision 31) — the guest advertises its boundaries + versions (`["bytes/1"]` today) so hosts pick the richest supported. Cheap hook now; enables a per-capability rich WIT boundary later without breaking the byte path.
- [x] **`protoc-gen-dlc-registry` plugin** — reads the `service` + `method_id` options **from the `buf build` image / CodeGeneratorRequest descriptors** (go-lite emits no service stubs, so generated Go is not a source — spike-measured) → emits the engine's `method_id → handler` registration (the reflection-free part) and **enforces `method_id` stability** against a committed lock. Host-side introspection uses the standard descriptor set (no custom host config to generate). Runs under `dlc gen` / `buf generate` (Decision 29).
- [x] `engine/caps_native.go` / `caps_wasip2.go` build seam for capability imports (§5.3) — native seam lets the CLI host link the engine in-process (Decision 26). **Landed with Events** as `engine/platform/caps_{native,wasip2}.go`; `caps_wasip1.go` stays unbuilt because there is no WAMR tier to run it
- [x] `export-fs` / `import-fs` handlers over the WASI filesystem (§7.3) — needed because scaffolding = `import-fs`. **Landed** as method ids 4/5 in **BFT** (the real spec: recursive `directory`/`text`/`binary` nodes, alphabetical entries, base64 for binary). Hand-written encoder **and** parser in `engine/bft.go` — `encoding/json` is reflection-heavy and banned in the engine, so the parser accepts only the BFT subset (objects + strings). Bundles are byte-stable (sorted) and text-vs-binary is chosen by content, so a scaffold bundle is readable and diffable. Untrusted-input safe: every path goes through `safeJoin`. Non-regular files (symlinks) are **skipped** — BFT cannot represent them and reading one errors.
- [x] Scaffolding handler (`new`): `import-fs` a template bundle → write tree → token-substitute (`{{.Module}}`, `{{.ProjectName}}`) — `engine/scaffold.go`, checked end-to-end by `make verify-scaffold` (new → gen → test → build → run) and `make verify-scaffold-web`

### Templates (its own area, §16.6 — bootstrap sequencing locked)
- [x] Author **`templates/component-model/` in-tree** — **full `dlc`-shaped** skeleton (engine + CLI/web host stubs + go.mod + devbox + proto; the WIT world is supplied by `dlc build`, so a scaffolded app carries none and cannot be stranded on a stale one). **Do not** create the skeleton git submodule yet (lift later). **Note for the host layer:** the skeleton currently ships `frontend/` *and* `hosts/native/` — two names for one layer, which `HOST-LAYER-PLAN.md` Phase 1/5 unifies.
- [ ] **Defer** versioned `dlc-platform` `go.mod` depend until submodule graduation (§16.4 / §16.6 #2)
- [ ] `templates/fragments/` in-tree for overlay packs (`--caps` / `--tiers` / …) — ABI mode picks the skeleton, not a fragment
- [x] `go:embed` the resolved `templates/` tree into the **engine** so `dlc new` is offline + browser-capable. Never runtime-clone templates — `templates/templates.go`, `//go:embed all:component-model`
- [ ] **`templates/wamr/`** — Backlog until embedded verify exists; do not add an unverifiable stub in B2
- [ ] **`reset-fs` / import modes in the UI** — `reset-fs` (id 12) and `ImportMode.REPLACE` exist and are tested; the browser uses REPLACE for imports but has no reset button and no per-file editing (§7.3 file verbs remain open — see the notes-app question in §13)

### Build pipeline
- [x] Makefile: `build-engine` (TinyGo **`-target=wasip2`** → `engine.component.wasm`; wasip2-direct per Spike 1) — plus `verify-parity` (both boundaries) and `parity-vectors` (regenerate the method goldens)

### CLI host (native Go, engine linked in-process — Decision 26)
- [x] `hosts/native/` — `main()`: **front-end that builds the request** (Decision 28) — `os.Args` → host parser (populated from the engine's `describe()`, Decision 29; `huh` menus for missing enum args) → (`method_id`, request bytes) → construct native Environment → `execute(method, request)` **in-process** (engine imported via the `caps_native` seam) → `command-result` → exit code. No wasm runtime in the run path (sidesteps the `wasmtime-go` CM gap); parsing is host-side, native may use any parser lib.
- [ ] Keep the in-process engine binding behind a small **lift-ready package** under `hosts/native/` (not open-coded in `main`) so a wasm-runtime host can be swapped in later (wasmtime C API, or `wasmtime-go` once it has a CM API) without touching argv → request → Environment → `execute`
- [ ] Two-phase launch (§5.5): construct the native Environment (FS root = cwd/config dir, stdio) → invoke the engine
- [ ] Wire `dlc new <app> [--module …]` end-to-end
- [ ] `dlc doctor` — the **command form** of preflight (§16.7): assess system prereqs + per-tier toolchain/host readiness, exit non-zero if a prereq is missing. Layer 1 (assumes a `dlc` binary); `scripts/preflight.sh` stays as the pre-toolchain **Layer 0** bootstrap gate that gets you a first `dlc` ([`DEVALBO-DLC-PREREQUISITES.md`](./DEVALBO-DLC-PREREQUISITES.md))
- [ ] `dlc gen` (host-side orchestration) — wraps `buf generate` (go-lite + es-lite + `protoc-gen-dlc-registry`) over the app's `commands.proto`; supersedes `make gen` for scaffolded apps (Decision 29 / §16.7)

### Verify (terminal)
- [x] `dlc new myapp` produces a buildable project tree on disk — `make verify-scaffold`, in `test-b2` and therefore in `./scripts/ci.sh full`
- [x] Define the **golden FS snapshot** for a known `dlc new` invocation (§11 Scaffolder row) — `verify/scaffold/golden.txt`: path + size + digest, one line per file, produced by scaffolding a fixed invocation and running it through `export-fs`. A **manifest rather than the BFT bundle itself**: BFT stores contents as JSON-escaped strings, so a 3 KB file becomes one unreadable line and the review diff that justified it disappears. Re-bless with `make scaffold-golden`; checked by `make verify-scaffold-golden` in `test-b2`.
- [x] **wasm-parity check (Decision 26):** also build the engine as `engine.component.wasm` and run the same golden `command-result` vectors through the wasm engine; assert byte-identical output vs the in-process native run. This is the CI form of the cross-tier identity guarantee (native is convenience; wasm is the contract). **Landed:** `make verify-parity` covers **both** boundaries — argv (9 vectors, native `dlc` binary vs `execute-cli`) and method (10 vectors, `cmd/parity-runner` vs `execute`), the latter diffing the **error string** as well as the output. Vectors are regenerated from typed fixtures via `make parity-vectors`; the diff is falsification-tested (a tampered vector does produce a mismatch). **Falsification-tested:** `make verify-parity-selftest` injects a `//go:build tinygo`-only divergence and asserts both boundaries report a mismatch, so a silently-broken parity check can't pass as green (T-B2.2). The whole layer runs as `make test-b2` (unit → parity → self-test), wired into `make test`. **Still open:** vectors cover only in-memory commands — extend once `new` writes through the filesystem capability, and run `make test` in CI.

**Exit:** `dlc new myapp` scaffolds + runs a working CLI project from the terminal; golden FS snapshot passes.

---

## Phase B3 — `dlc` in the browser + React UI (web host)

### Web host (TypeScript — glue + UI only, no business logic)
- [x] `hosts/web/worker.ts` — jco instantiate the component + `preview2-shim`; OPFS-backed WASI root. **Landed:** hydrate OPFS → FileData tree **before** instantiating (the guest snapshots its preopen; a later `_setFileData` is invisible), flush back after every `execute`, exposed over Comlink. `reboot()`/`reset()` drop the instance when OPFS changes out of band.
- [x] Inject capabilities: filesystem → OPFS via `hosts/web/opfs.ts` (hydrate/flush/list/clear, lifted from Spike 3) + a **pinned** patched `preview2-shim` filesystem in `hosts/web/shim/` (stock breaks TinyGo writes on bigint offsets). **Still open:** WASI stdio → `console.*`.
- [x] Expose the engine to the main thread via **Comlink**
- [x] `hosts/web/api.ts` — environment-agnostic adapter (`execute(method, request)`; the UI builds the proto request — Decision 28). Sole door from UI to engine; carries the mirrored `Method` ids.

### React UI (the UI capability)
- [x] `frontend/` — React + Vite app that drives `dlc` via the adapter. `vite.config.ts` carries the three non-obvious bits: browser export conditions, the shim pin, and `worker.format: "es"` (the worker dynamic-imports). Bare specifiers imported from `hosts/web/` must be aliased — that dir sits outside the Vite root on purpose.
- [x] A scaffolding UI: run `dlc new`, browse the generated tree in OPFS, show a log. The **form is this tier's parser** (Decision 28) — it encodes a `NewRequest` with es-lite and hands over bytes.
- [x] **Download the result** — `export-fs` the OPFS tree → BFT → browser download, and the inverse (import a bundle into OPFS). The UI exports the **whole root** so download/import are exact inverses; `--prefix` subtrees stay available on the CLI.

### Async + build
- [ ] jco JSPI path for any async custom caps (from Spike 5: Node ≥24 + `--experimental-wasm-jspi` + async import/export — no ILC shim)
- [x] Makefile: `build-wasm` (TinyGo → jco transpile → `frontend/src/wasm/`) + `gen-web` (es-lite → `frontend/src/gen/`), `dev-web` (Vite), `verify-web` / `verify-web-watch` (Playwright)

### Verify (browser + cross-tier)
- [x] `dlc new` runs in the browser; project persists in OPFS across reload — `make verify-web` (T-B3.1), 3 tests: scaffold+reload, `--module`, refusal to overwrite. Reload assertion reads `go.mod` back **directly via the OPFS API**, bypassing the engine. Falsification-checked: disabling the flush turns 2 of 3 red.
- [x] React UI renders and drives the engine (headless Chromium, no host-capability errors). **Still open:** a `make verify-web-watch` eyeball pass in a real browser window.
- [ ] **Cross-tier identity (Decision 26):** web runs `engine.component.wasm`; the CLI runs the same engine codebase natively in-process. A shared golden vector produces byte-identical `command-result` across the native CLI run, the CI wasm-parity run (B2), and the browser — the wasm the browser loads is the same artifact the parity check exercises (sha256)

**Exit:** `dlc` runs in terminal **and** browser (React UI) from one engine — the bootstrap milestone is met.

---

## Bootstrap exit criteria (roll-up)

**Met 2026-07-27.** Ticked against the tree, not from memory — every line below names the check that proves it.

- [x] One engine **codebase** drives CLI (native in-process) and web (`engine.component.wasm` via jco); golden `command-result` vectors byte-identical across native and the CI wasm-parity run (Decision 26) — `make verify-parity`, falsified by `make verify-parity-selftest`. **Partial:** the sha256 tie from the parity artifact to the *browser's* bytes is still open (see the cross-tier identity row above)
- [x] `dlc new myapp` works from the terminal (scaffold to disk) and the browser (scaffold to OPFS + download) — `make verify-scaffold`, `make verify-web`, `make verify-bundle-xtier`
- [x] React UI on the web tier drives the engine — `frontend/`, `make verify-web`
- [x] Capabilities limited to Console + Filesystem; graceful behavior where a cap is absent. **Superseded in practice:** Events landed on top (Decision 33), which is beyond the bootstrap subset by design
- [x] `devbox run verify` green for CLI + web; golden FS snapshot stable — `./scripts/ci.sh full`, `make verify-scaffold-golden`

**The one bootstrap row still genuinely open** is the sha256 leg of cross-tier identity. It is a
verification tightening, not a capability, so it did not hold the milestone.

---

## Backlog / Next implementation

Deferred until after the bootstrap. Grouped; roughly priority-ordered within each group.

### 🎯 CURRENT — the derived index (§6.2, §7.1) — [`INDEX-PLAN.md`](./INDEX-PLAN.md)

**Re-scoped 2026-07-29 — it is no longer SQLite.** A **projection index the engine owns**, stored behind a
**`wasi:keyvalue`-shaped seam** that is file-backed today. The plan doc's §0 carries the full argument; the
short version:

- **`ORDER BY` was the entire case for SQL** (§6.2 rejects `wasi:keyvalue` in one sentence for it). A KV
  store cannot order, so the sort moves into Go — where it is the *same sort the fallback already uses*.
  Under SQLite the two paths were SQL collation vs `sort.Slice`, two implementations of ordering that had
  to agree forever, and the check to catch that divergence was self-inflicted work.
- **With Go doing the querying, the index is a cache of projections — which is a file.** That was the
  rejected alternative, rejected on the grounds that it would be a third *query engine*. It is not a query
  engine, so the rejection was aimed at the wrong target.
- **The app-facing branch disappears entirely.** The index is always present (its floor is a file), so
  `list-records` has ONE implementation, no `HasIndex()`, and no "the fallback must return identical
  results" rule — there is no second path. The scan moves into `rebuild-index`, where it belongs.
- **`wasi:keyvalue` earns its place at the STORAGE layer**, not the query layer: a whole-file rewrite per
  write is fine on web and native and is what §5.6 tells you not to do to flash, so the seam mirrors the
  standard's shape and a host binds a real store later with no app or query code changing.

**Dropped:** `sqlite-host` from Decision 12, `modernc.org/sqlite`, `@sqlite.org/sqlite-wasm`, the
per-tier capability binding, and the index's permanent absence on embedded — which now gets the same index
as everything else.

- [x] **Phase 0 — the synchronous-answer spike** ✅ 🟢 GREEN (2026-07-29) — `spikes/sqlite-sync/`, `make spike-sqlite-sync`. Ran against sqlite-wasm because that was the plan at the time; what it established outlives it. A browser **can** answer a host capability synchronously (`opfs-sahpool`, no microtask ran, no COOP/COEP needed — falsified with one injected `await`), and **anything storing files in the OPFS root collides with the engine's bridge**: hydrate pulls them into the engine's tree (so they would ride along in `export-fs` bundles) and the flush then throws `NoModificationAllowedError` on every command. Both are now prerequisites for the deferred host-store phase (plan D9), not current work
- [x] **Phase 1 — the manifest field** ✅ landed, **SUPERSEDED**, then **REVERTED** (field 3 is `reserved`, `HasIndex()`/`BootOptions.Index` gone) — `Index` + `Environment.index` + `HasIndex()` + `BootOptions.Index`, five tests, three falsifications. Reverted per plan D8: the index is always present under the revised design, so the field has nothing to say, and a field nothing sets is what `ENVIRONMENT-PLAN.md` D6 exists to prevent. The finding worth keeping: `Boot` states availability in both directions so `UNSPECIFIED` can keep meaning "no manifest yet", and the **TS encoder had to move with it** — two host runtimes would otherwise disagree in bytes with no check able to see it, since the parity vectors are hand-built requests rather than host-generated ones. That asymmetry recurs for every manifest field
- [x] **Phase 2 — the seam and the file backend** ✅ (2026-08-02) — `Store` (`Put`/`Delete`/`Scan`/`Clear`, mirroring `wasi:keyvalue`), the file-backed implementation, `rebuild-index` at id 200, `SetIndexRebuilder` (same shape as `SetVersion`: platform owns the verb, app owns the knowledge), and the export exclusion (`platform.IndexDir`, matched by full path so a user's own directory of that name survives). Five falsifications, each watched going red. **Registration is an APP fact:** an app with no collection — dlc, tictactoe — must not be handed a verb that could only fail, so `SetIndexRebuilder` is what registers the block, in either order relative to `RegisterAll`. **Findings:** (1) `dlc-platform`'s own tests had NEVER run in CI — separate module, so `./...` from the root missed six test files covering dispatch, containment, BFT and the manifest; now T-B2.0b and a `ci.sh` step. (2) One inherited verb costs a renderer in **seven** host sites (3 apps × 2 tiers + the template) — the missing-renderer rule working as designed, but a cost linear in apps, and worth a default renderer before the next capability. (3) `unavailable`'s message assumes the cause is the host; for the index it is the app, so it needs a second case (Phase 5). (4) One line was written and then deleted for being unfalsifiable. (5) A CLI test asserted more than its name and now asserts about the command it is named for
- [x] **Phase 3 — notes uses it** ✅ (2026-08-02) — create/delete maintain the index; `list-records` queries it **with no branch**; the scan is now `rebuildIndex`, reached through the inherited verb via `SetIndexRebuilder`. Falsified from both sides: dropping the index write from create gives `maintained [], rebuilt [apple zebra]`, dropping the delete gives `maintained [apple zebra], rebuilt [apple]` — in notes' own tests, no new harness. **`ListRecordsResponse` now returns the PROJECTION, not records**, which makes D6 structural rather than a rule: there is no body field to serve stale, and `open` reads the record's file. The preview is capped in the engine (storage) and truncated in the slot (presentation). **notes' reserved id 10005 was deleted** — an inherited verb costs an app no id at all. The index is visible in the web file browser and stays visible: hiding it would make the one view whose job is "the files are the truth" tidier than the disk
- [x] **Phase 4 — the checks worth a script** ✅ (2026-08-02), and **smaller than planned**. The browser test landed in Phase 3 (`files.spec.ts`: the index is on disk and absent from a bundle). **The index vectors were dropped deliberately** — the vectors run against dlc's engine, which keeps no collection, and they carry requests only, comparing native against wasm with no expected output. So a vector can catch divergence but can never pin "this verb should be absent", which is the Phase 2 decision worth pinning. `engine/execute_test.go`'s `TestDlcOffersNoIndexVerb` does that instead, falsified by giving dlc a rebuilder. Still **no `verify-index-parity.sh`**: rebuild-equivalence replaces the SQL-vs-Go comparison entirely, and parity's filesystem diff will compare index bytes for free the moment an app in the harness writes one
- [x] **Phase 5 — write it down + dogfood** ✅ (2026-08-02) — `AGENTS.md` **§3·7** carries never-authoritative (D6), file→index→event (D7) and "the index never travels" (D5), plus the rebuild invariant as a standing instruction and a warning not to add a manifest field. `DEVALBO-ILC-GO-PLAN.md` §6.2 is now the derived index, §7.1 carries the write order and drops the lock file with its reason, §6.6's row says outright that "needs `ORDER BY`" was the sentence this overturned; Decisions 7/9/12/20 updated and `sqlite-host` swept out of the WIT sketch, the world imports, the component diagram, the environment matrix and the `dlc.toml` example. `dlc` does not adopt it, recorded **and pinned by a test**. **Decision 33's strict/lenient knob is now unowned** — it was parked on "a capability that can genuinely be missing at runtime (the SQLite index)", and the index is never absent, so D33 hands the question to the next capability that can actually go missing
- [ ] **Phase 6 (deferred, unscheduled) — a host-provided KV store** — embedded first, because whole-file rewrites are a flash-endurance problem before they are a speed one. **The app and its queries must not change**, which is the test of whether this design was right

**The one invariant that replaces most of the old plan's machinery:** the maintained index equals a rebuilt
one. It catches a create that forgets to index, a delete that leaves a row, and a projection that drifts —
with no second tier, no second backend and no golden file.

### The host layer (Decision 34) — COMPLETE — [`HOST-LAYER-PLAN.md`](./HOST-LAYER-PLAN.md)

Per-app, per-tier host code gets a name, a contract, and a slot. The line `engine/` vs `engine/platform/`
already draws is missing one directory over, and `hosts/native/` currently mixes runtime with `dlc`'s own
code while notes ships the same layer twice under two names (`hosts/native/` and `frontend/`).

- [x] **Phase 1 — draw the line in `hosts/`** ✅ runtime vs *tier slot*; notes **and the template** adopt `hosts/<tier>/` uniformly (the template came early on purpose — D7); every tier in `dlc.toml` names its slot root and a missing slot fails the build, which is the first field `dlc.toml` has ever gated. Golden re-blessed; falsified by disabling the gate. Turned up a latent bug — `platformPathFrontend` ignored its own subdir argument — and a CI gap: `hosts/` was vetted but never tested. **Deferred:** splitting the runtime out of `hosts/native/` (separate "lift-ready package" task; named as a debt in `hosts/README.md`)
**Phases 2–6 were reordered after Phase 1** (see the plan for the reasoning): the pilot moved ahead of the
typed interface, because D2's case for locking event schemas is "a host renders from the payload" and no
host does that yet — locking a format before its first consumer exists is how the shape turns out wrong.
Host parity split off from the harness for the same class of reason: notes cannot exercise it, since its
native slot subscribes to nothing.

- [x] **Phase 2 — a slot renders with no engine** ✅ `hosts/web/port.ts` (`EnginePort`) + `hosts/web/testing.ts` (`createFakePort`); notes' slot split into `view.ts` (takes a port, exports `projection()`) and a thin `main.ts`. Six tests, falsified. **The seam is the whole port, not the event stream** — notes re-lists on an event, so a faked stream alone still calls a missing engine. Reached three things the live test cannot: a failed `list-records`, a foreign topic costing zero commands, and unmount actually unsubscribing
- [x] **Phase 3 — tic-tac-toe (App #3)** ✅ one engine, DOM and ASCII slots, semantic events the host renders. **Scaffolded with `dlc new`** — the first app the tool actually produced. Falsified: disabling win detection makes both slots wrong *identically* and neither invents a winner. Finding: dropping `platform.RegisterAll()` fails at run time (`unknown method_id 1`), not at compile time
- [x] **Phase 4 — host parity** ✅ four states, two languages, no shared code: `hosts/native/projection_test.go` + `hosts/web/test/parity.spec.ts`. Caught a real mismatch on its first run (both slots mis-indented rows relative to their separators, and disagreed about it) — precisely the class parity cannot see. D3 now has mechanical enforcement rather than only a sentence
- [x] **Phase 5 — the published interface** ✅: event schemas declared in proto, **locked** like `method_id`, generating both emit and subscribe sides. Deletes the four hand-mirrored topic literals that `AGENTS.md` §1 already bans for ids. Costs one rewrite of tic-tac-toe's subscriber wiring — accepted, so the codegen's shape is decided by a real consumer
- [x] **Phase 6 — scaffold the slot** ✅: `dlc new` emits slots + their tests, and **tier selection becomes a setup question** (host-side prompt when `--tiers` is absent — a tier is now a directory of host code plus a checked `dlc.toml` entry, so it is worth asking rather than defaulting silently). The documentation half of this phase already landed with Phase 1

**Generated apps are disposable for now** — re-scaffold rather than migrate, so template layout changes
cost a re-bless (`make scaffold-golden`) and not a migration story.

### App #4 — DoneBlock, a task DAG — [`DONEBLOCK-EXAMPLE-PLAN.md`](./DONEBLOCK-EXAMPLE-PLAN.md)

A CLI version of doneblock.com: tasks are nodes, an edge means one task blocks another, and the question the
tool answers is *what can I work on now*. Written as a spec **a coding agent can execute**, and built to
produce framework feedback rather than to demonstrate a capability.

Why this app, when notes and tictactoe exist: **its queries are traversals**, where every previous app has
been a flat collection. That makes it the first honest consumer for the derived index (a reverse lookup —
"what does this block"), the first genuinely tempting case for a host to compute something the engine should
own (readiness, in a browser with a graph library), and the first app with two collections and referential
integrity to maintain by hand.

**Scope:** multiple projects at once (each with its own metadata and its own DAG), tasks with a status,
edges that carry a **blocking reason** plus reason-specific arguments, and a `check` command that verifies
twelve integrity invariants over the stored files — the app-level answer to "the files are the truth, so
what happens when the files are wrong?"

- [ ] Phases 0–7 live in the plan doc, with a full command reference (§4.2) an agent can build from. Phase 0 has been **dry-run**; see below
- [ ] `example-apps/doneblock/FEEDBACK.md` — the friction log, written *while* building

**Three framework bugs already found, before any app code existed** (plan §11) — all reproducible, none
caught by any existing check *at the time*. All three are now closed; the third was the one that let the
other two exist, so it is the only one that needed a new check rather than a fix:

- [x] **`dlc new --help` prints an invocation that fails.** Usage says `dlc new <name> --tiers <tiers>`, but
      the parser stops at the first non-flag argument, so `dlc new myapp --module x` dies with
      `unexpected argument "--module"`. Either accept flags after positionals or stop advertising that order
      — **fixed**: flags are accepted after positionals (`dlc-platform/cli/run.go`, locked by
      `cli_test.go`'s `t go myapp --title x` case)
- [x] **A scaffolded project cannot run `make gen` in its own devbox environment.** The template's
      `devbox.json` installs `protoc-gen-go-lite` but not `protoc-gen-es-lite`, which the generated
      `buf.gen.yaml` needs the moment a `web` tier exists. The first command `dlc new` tells you to run is
      the one that fails — **fixed** in `templates/component-model/devbox.json.tmpl`, with
      `templates/templates_test.go` asserting the declaration
- [x] **Nothing verifies a scaffold in its own declared environment — which is why the above survives.**
      `verify-scaffold.sh` runs the scaffold's `make gen` inside the REPO's devbox shell, where the missing
      plugin is already on PATH. Same class of blind spot as the two `verify-platform-gen.sh` exists for:
      only broken from outside, therefore invisible from inside
      — ✅ **`scripts/verify-scaffold-env.sh`** (`make verify-scaffold-env`, nightly via `ci.sh all`).
      Scaffolds, strips this repo off PATH, and runs `make gen` + `go build` through the **scaffold's own**
      `devbox run`. Falsified: removing the `protoc-gen-es-lite` line from the template turns it red while
      `verify-scaffold.sh` stays green — the blind spot, demonstrated rather than argued.
      **Two findings worth keeping.** (1) `devbox run` REBUILDS PATH rather than inheriting it, so a scrub
      performed outside does not survive into the run — the check does it inside, and prepends the `dlc`
      binary there too (the first version failed with `dlc: No such file or directory` while claiming to
      prove something about protoc plugins). (2) The nesting is the whole reason the leak is subtle: CI
      reaches this script through the **repo's** `devbox run`, which re-adds the repo's nix profile *after*
      the scaffold's own entries — so the scaffold's declared tools win and a tool it FORGOT is silently
      found further down the list. Also: `devbox run -- sh -c '…'` is unusable, since devbox re-joins and
      pre-expands its arguments (variables came out empty, newlines literal); the inner half must be a file

### Second app — notes/list (App #2, the breadth pilot) — §13
- [x] Scaffold notes/list — `example-apps/notes/`, built on the platform via `dlc.toml` `[platform] path`. **Not** scaffolded by `dlc new` in the end; it predates the template being complete, and Phase 1 above brings its layout back in line
- [x] Handlers: `create` / `list` / `delete-record` over the filesystem, plus its own `notes.record-changed` event (the first app-defined topic)
- [x] `open` / `rebuild-index` / `update` ✅ — `open` reads the record's own file (never the index); `rebuild-index` is inherited from the platform; **`update` (10004) is the first command that can leave a projection STALE rather than absent**, which is what D6's "the index is a cache" was written for. Falsified: dropping its `indexRecord` call makes D4 report `maintained {title: "Buy milk"}, rebuilt {title: "Buy oat milk"}`. Three decisions worth keeping: **absent means unchanged** (proto3 cannot tell "" from unset for a bare string, so emptying a body takes an explicit flag — otherwise fixing a typo in a title erases the body); **the id does not follow the title**, because re-slugging turns an edit into a move (new file, stale file, index key to migrate, links broken); and **a no-op writes and announces nothing**, since an event would make every subscriber re-list for a stray keystroke. **Friction found:** promoting a reserved id to a real rpc requires hand-editing `method-ids.lock` — the guard refuses an rpc claiming a reservation, and reserving ids for planned commands therefore has a cost at the moment you spend one
- [x] Drove out Events and the `caps_*` seam; now driving out the host layer

### In-page terminal (web tier) — the CLI surface, in the browser

A terminal widget in the page: type `create --title "Buy milk"`, see it run against the same engine the
buttons drive. **Scaffolded on a route by default** so every app has one without wiring it up.

**Why this is more than a convenience.**
- **It is the second consumer of `clispec`.** The generated surface claims to be tier-neutral, and today
  exactly one host (Go) reads it. A TypeScript consumer is what turns that claim into something checked.
- **It makes the divergence hazard concrete and testable.** The moment two parsers exist, `argv → request
  bytes` stops being a thought experiment: the **parse vectors** item under the CLI-location task becomes
  runnable — same vectors, Go runner and TS runner, diff the bytes. Right now nothing forces that.
- **It rehearses the serial REPL.** The embedded tier's REPL is the same shape — a line of text in, a
  rendered response out — and iterating on it in a browser is far cheaper than on an ESP32.
- **It is a real debugging surface.** `window.app` reaches the *slot's* operations; a terminal reaches the
  *command surface*, including inherited verbs a UI never exposes (`export-fs`, `reset-fs`).

**Shape — mirror `cli.App`, don't invent a second model.** `hosts/web/terminal.ts` in the RUNTIME (every app
inherits it, like `subscribe`), taking the same three things the Go runner takes: an `EnginePort`, a
`clispec` surface, and per-method renderers. The app supplies the surface and the renderers; the runtime
owns parsing and history. A slot renders, it never decides (Decision 34).

- [x] **Phase 1 — a TS `clispec` + encoder.** ✅ Plugin emits the surface for `lang=ts` (it currently emits ids
      only — this is the concrete driver for the Decision 29 web follow-up above). Encode with es-lite's
      `writeScalar(writer, type, fieldNo, value)` from `@aptre/protobuf-es-lite/binary` — **checked: it is
      exported**, so no hand-rolled wire encoder. Falsify against the Go runner: same command line, same
      bytes.
- [x] **Phase 2 — the terminal itself.** ✅ Plain `<pre>` + input line, not xterm.js: the value is the command
      surface, not terminal emulation, and a dependency that renders ANSI buys nothing here yet. Parse a
      line into argv (quoting is the only fiddly bit), dispatch, render.
- [x] **Phase 3 — on a route by default.** ✅ A separate Vite entry (`terminal.html`) rather than a router
      dependency — Vite does multi-page natively, and the scaffold's UI is vanilla TS with no router today.
      If a real path (`/terminal`, no `.html`) is wanted, that is a dev-server rewrite plus a build config,
      not a library.
- [x] **Phase 4 — scaffold it** ✅ template ships `terminal.html`, `src/terminal.ts` and a shipped browser test; notes has one too. Golden re-blessed.
- [x] **Phase 5 — tab completion from the spec.** ✅ Cheap, and the payoff for having the surface as DATA:
      complete subcommands, then flags, then enum values, with no extra source of truth. Do it last — it is
      the reward, not the point.

**Framework: a plain core, with an optional React wrapper — not React in the runtime.** It is tempting to
say "we already use React", but that is only true of **`dlc`'s own `frontend/`**: the template and notes are
vanilla TS, and `dlc new --ui REACT` is unimplemented (the engine rejects `UI_KIND_REACT` — the web tier
scaffolds vanilla TS). Putting React in `@devalbo/dlc-web` would push it onto every scaffolded app that
deliberately has none, and make the runtime's dependency set depend on a UI choice apps have not made.

So: the terminal core is framework-free — it is parse → dispatch → render text, which needs no framework —
and `hosts/web/terminal-react.tsx` is a thin optional wrapper for hosts that already have React, `dlc`'s own
UI being the first consumer. That way dlc's React frontend gets a component, a scaffolded vanilla-TS app
gets a mount function, and neither pays for the other. If **`UI_KIND_REACT` scaffolding** lands later, the
wrapper is already there and the terminal is not what forced the decision.

**The wrinkle worth deciding early: `cli_source` FILE and STDIN are native concepts.** A browser has no cwd
and no stdin, so `import-fs backup.json` needs a meaning. The good answer is probably **OPFS**: the engine's
filesystem on this tier *is* OPFS, so a path resolves there and `import-fs` works the way it reads. Stdin
has no analogue and should be a named refusal, not a hang. Decide it in Phase 1 — it changes what the TS
runner does with a resolved value, and getting it wrong means the same command line means different things
per tier, which is the whole hazard.

**Explicitly out of scope:** ANSI/colour, curses, job control, piping between commands, and a shell
language. If any of those start to feel necessary, the terminal has stopped being a command surface and
become an emulator.

**Landed 2026-07-28.** `hosts/web/{clispec,encode,terminal,terminal-ui}.ts`, the plugin emitting
`.cli.pb.ts`, and a `terminal.html` route in notes and the template. 26 browser tests in notes.

**The encoder imports NOTHING, and that was a correction.** It first borrowed es-lite's wire writer, which
meant a bare specifier — and this package is consumed by `file:` symlink with no `node_modules` of its own,
so Vite resolved from the real path and failed. The first fix was a Vite alias onto `dist/binary.js`, which
reaches past the package's exports map into internals we do not control and would break on a version bump
with "file not found" nowhere near the cause. Hand-writing the ~40 lines of varint and length-delimited
encoding removes the whole problem class: the runtime package needs nothing from the app's `node_modules`.
The argument for borrowing it ("don't implement a varint twice in two languages") was circular anyway —
**the parse vectors are that argument's answer.**

**The parse vectors are real and they work.** `hosts/native/parsevector_test.go` and
`test/terminal.spec.ts` assert the same five command lines produce the same bytes, from two independently
written encoders (Go via `protowire`, TypeScript by hand). The duplication is deliberate: generating both
sides from one source would prove only that the generator agrees with itself. The vector that earns its
keep is **negative int64** — signed ints are 64-bit two's complement varints, so `-1` is *ten* bytes and an
int32 is sign-extended first, which is the one genuinely non-obvious rule in protobuf scalar encoding.

**Two hazards found while building:**
- **Vite's dev server serves any `.html`, but `vite build` only builds declared entries** — so a missing
  entry breaks nothing until production, then silently omits the route. Green tests, missing page. The
  preset now DISCOVERS `*.html` at the root, so adding a route is adding a file.
- **The prompt is disabled while a command runs** (one engine instance; a second command would interleave
  its output). Correct, and it made a test that pressed Up mid-command fail against nothing.

**Left deliberately:** `stdin` is refused by name in the browser rather than hanging, and `file` sources
read from OPFS — the right answer on this tier, since the engine's filesystem *is* OPFS, so
`import-fs backup.json` resolves against the tree the command writes into.

### "The files are the truth" is weaker on the web tier than §7.1 claims

**Found while building the OPFS browser (2026-07-28), and it is a real cross-tier divergence in a core
claim, not a bug in a page.**

Natively the engine reads and writes the actual filesystem (`Root()` is the process cwd), so an external
edit is seen on the next read and §7.1 holds literally: the files *are* the truth, and anything may write
them.

On the web tier they are not. `worker.ts` hydrates the whole OPFS tree into an in-memory `FileData`
structure **before** instantiating the component — the guest snapshots its preopen, so a later
`_setFileData` is invisible — and flushes that tree back after **every** `execute`. `writeDir` prunes as it
flushes: anything OPFS holds that the in-memory tree lacks is removed. So on this tier:

- the in-memory tree is the truth between flushes; **OPFS is a snapshot the engine overwrites**
- a write made directly to OPFS is invisible to the running engine, **and is deleted by the next command** —
  any command, including a read like `list`, because the flush is unconditional

The prune is correct and load-bearing (`import-fs --replace` and `reset-fs` must be able to delete), so
this is not fixed by merging instead.

**Consequences already absorbed:** the OPFS browser is read-only for this reason, and `api.ts`'s `reset()`
reloads the page rather than merely clearing storage.

**What would close the gap, roughly in order of cost:**
- [ ] **Granular file verbs through the engine** (§7.3 already lists these as open). A `write-file` command
      makes the in-memory tree the thing that changes, the flush persists it, and the write announces itself
      like any other. This is the honest answer, and it makes an editable file browser possible.
- [ ] **Flush only when the engine wrote.** `worker.ts` says it flushes always because "the engine cannot
      yet signal that it wrote anything — revisit when Events can report a write." Events now can. This
      would not fix external writes, but it shrinks the window in which one gets clobbered.
- [ ] **Re-hydrate on external change.** A `BroadcastChannel` or storage observer could reboot the engine
      when OPFS moves underneath it — this is the same machinery the cross-tab Events follow-up needs, so
      the two should be designed together.

**Until then, say it plainly in the docs rather than letting §7.1 imply something the web tier does not
do.** The store is inspectable on both tiers; it is externally *writable* only on native.

### Commands inspector (web tier) — the surface, explorable

A route that shows every command the app has: subcommands, their flags, types, which are required, defaults,
enum choices, positions — with a form per command that builds and runs the request. Third route alongside
the terminal and the file browser.

**Why it is more than a nicer terminal.** The terminal makes the surface *usable*; this makes it *visible*.
Three things follow from that:

- **It is the app's API documentation, generated rather than written.** Every fact on the page comes from
  `commands.proto` — `help`, `required`, `default`, `short`, `cli_name`, `cli_flag`, `cli_source`,
  `cli_positional`, enum values, and the rpc's doc comment. Documentation that cannot go stale, because
  there is nothing to keep in sync.
- **It shows what a command line CANNOT express.** `Unsupported` is already carried per command (nested
  messages, maps, floats). A terminal can only mention it; a form can render those fields as "not settable
  here" and make the gap in the CLI surface obvious rather than folkloric.
- **A FORM is the honest web front end** (Decision 28: each tier builds requests its own way). The terminal
  borrows the CLI's idiom because it is a terminal; a browser's native idiom is a form. Building one from
  the same `clispec` is the strongest evidence yet that the surface is genuinely tier-neutral — flags on
  one tier, fields on another, one schema.

- [ ] `hosts/web/inspector.ts` — render `clispec` as a browsable list plus a per-command form; reuse
      `encode.ts` so a submitted form and a typed command line produce the **same bytes**
- [ ] `commands.html` route in the template and notes
- [ ] Show `method_id`, request message name, and the `Unsupported` list per command — the things a
      developer debugging a request actually wants
- [ ] Enum fields become `<select>`s from `enumValues`; required marked; defaults pre-filled — all already
      in the surface, none of it hand-written
- [ ] "Copy as command line" — turn the filled form into the equivalent terminal line, which makes the two
      front ends visibly the same surface
- [ ] Extend the **parse vectors** to a third front end: form input → request bytes must equal what the CLI
      and the terminal build. That is the check that keeps three parsers honest, and it costs one more
      assertion per vector

**Careful about:** `cli_source` file/stdin fields (a form should offer a file picker or an OPFS path, not a
text box pretending to be one), and running a mutating command by accident — a form that submits on Enter
next to a `reset-fs` button deserves a confirmation.

### OPFS file browser (web tier) — a route that shows the store

A page listing what is actually in OPFS, with file contents viewable. Same reasoning as the terminal, and
the same shape: a route the scaffold ships, `hosts/web/` runtime code an app mounts.

**Why it belongs in the platform rather than in one app.** §7.1's whole claim is *the files are the truth* —
one JSON file per record, readable without the app. On the terminal tier you can `cat` them; on the web
tier that claim has been **unverifiable by eye** so far, because OPFS has no Finder. A browser makes the
architecture's central promise inspectable, which is worth more here than it would be in a normal app.

It also covers the debugging case the terminal does not: after `import-fs --replace`, "what is in there
now?" is a filesystem question, not a command question.

- [ ] `hosts/web/files.ts` — mount a tree from `listOPFS`, read with `readOPFSText`; both already exist in
      `hosts/web/opfs.ts`, so this is presentation over an existing API
- [ ] `files.html` route in the template and notes; the preset already discovers `*.html`, so a route is a
      file
- [ ] Read-only first. **Editing is a trap worth naming:** the engine holds a preopen captured at
      instantiation and OPFS changes made behind its back are invisible to it until a reboot — which is why
      `api.ts`'s `reset()` reloads the page rather than just clearing storage. A file editor would be a
      second writer to the engine's own filesystem, and the events work has already established that a
      second writer must announce itself. If editing is wanted, it should go through `import-fs` (which
      emits `ilc.data-changed`) rather than writing OPFS directly.
- [ ] Show byte sizes and let a file be downloaded — the inverse of the existing `export-fs` bundle path
- [ ] A browser test asserting the tree matches what a command just wrote

**Not a filesystem editor, and not a replacement for `export-fs`.** A bundle is the portable artifact; this
is a window.

### App root: every tier should be GRANTED a root, the way WASI grants a preopen

**The seam exists and one side does not use it.** `Root()` is a build-tag constant — `"."` natively, `"/"`
under WASI — and every path goes through `SafeJoin(Root(), path)`. Under WASI the host *installs* a preopen
before instantiation and the guest cannot name anything outside it. Natively nothing is granted: the root is
wherever the user happened to be standing.

**Three problems, in descending order of severity.**

1. **`reset-fs` deletes the working directory.** It is an INHERITED verb — every ILC app has it — and
   natively it recursively removes the contents of wherever you ran it. This already bit us during
   development: it deleted an exported bundle sitting in the same directory. In a real app,
   `notes reset-fs` in the wrong terminal is data loss, from a verb the app author never wrote.
2. **No confinement.** `SafeJoin` stops `../` escapes *relative to the root*, but the root itself moves with
   the shell. The WASI guarantee — "this is your filesystem, there is nothing else" — has no native
   equivalent, so the tiers differ in what an app can even reach.
3. **Paths and errors diverge** (see the item below): native errors carry cwd-relative paths, wasm errors
   carry `/`-rooted ones. Granting a root does not by itself fix the errno wording, but it is what makes a
   single reported path shape possible.

**The shape: the host grants, the engine consumes.** `Root()` becomes host-set state rather than a
constant — set during phase 1 of the two-phase launch (§5.5 already says the native host constructs the
Environment with an FS root). Wasm keeps `/`, its preopen. The engine is unchanged: it already only ever
joins against `Root()`.

**THE OPEN QUESTION, and it is a real tension rather than a detail: what is the native root?**

| option | good | bad |
| --- | --- | --- |
| cwd (today, made explicit) | familiar, git-like, project-local | keeps the `reset-fs` hazard entirely |
| per-app data dir (XDG / Application Support) | one store per app, `reset-fs` is safe | surprising for a tool meant to act on the current directory |
| `./.<app>/` under cwd | project-local AND confined; `reset-fs` can only clear that subtree | a hidden directory per project |
| explicit flag, no default | maximally explicit | every invocation carries it |

**`dlc` itself is the case that rules out a blanket answer.** Its data *is* the user's working directory —
`dlc new myapp` must scaffold into cwd, so giving dlc a private data dir would break the tool. Meanwhile
`notes` probably wants one store regardless of where you are. So the root is **per-app**, which points at
`dlc.toml` declaring it (a launch fact, next to tiers and slots) with the host resolving it.

- [ ] Decide the per-app default and the `dlc.toml` spelling (`[storage] root = …`?)
- [ ] `platform.SetRoot` + host-side resolution; keep `Root()` as the single accessor so the engine is
      untouched
- [ ] Make `reset-fs` safe by construction, and test that it cannot reach outside the grant
- [ ] Report paths RELATIVE to the root in errors, which is what would let the parity check guard the
      missing-file path instead of avoiding it
- [ ] Update the parity harness and verify scripts, which currently rely on chdir-as-root

### Native and wasm word OS errors differently — a latent parity landmine

**Observed 2026-07-28**, when a parity vector accidentally reached a missing-file path:

```
native     export-fs: open app-default: no such file or directory
component  export-fs: open /app-default: file does not exist
```

Two divergences in one line. The **path** differs because `Root()` is `.` natively and `/` under WASI, and
the joined path lands in the error. The **errno text** differs because Go's `os` and TinyGo's WASI runtime
phrase the same condition differently.

**Why this matters more than it looks.** The parity check diffs the error STRING deliberately — that is
what makes "TinyGo and native Go agree on envelope errors" a checked claim rather than a hope. So any
command whose error wraps an OS error is un-parity-able, and today the vectors stay green only because
none of them happen to hit such a path. The one that did hit it was an accident (a `new` vector stopped
scaffolding, so a later `export-fs` found nothing), and it failed instantly — which is the check working,
but it means this is a trip-wire rather than a guarded boundary.

It is also user-visible: `notes open nonexistent` prints different text in a terminal and in a browser.

- [x] **DONE 2026-07-28.** `platform.FSError(op, rel, err)` words filesystem failures itself, from the
      path the CALLER named rather than the joined absolute one:
      `export-fs: no-such-tree: does not exist` on both tiers. Wired into export-fs, import-fs and
      reset-fs — the three that wrapped an OS error.
- [x] **The boundary is guarded, not merely unexercised.** A parity vector now hits a missing prefix on
      purpose (21 vectors, native == component), and it was falsified: restoring the raw wrapping reddens
      exactly that vector with both divergences visible —
      `open no-such-tree: no such file or directory` against `open /no-such-tree: file does not exist`.
- [x] Three unit tests pin the rules, including that the runtime's phrasing and the absolute path cannot
      leak back in.

**One honest imprecision, documented in `FSError` rather than hidden:** a permission failure is reported as
"does not exist", because the two cannot be told apart portably — `AGENTS.md` §2 records that
`os.IsNotExist` and `errors.Is(err, fs.ErrNotExist)` do not match TinyGo's WASI errno, so classifying by
inspecting the error is the trap this package already paid for once. Probing with `Stat` and parsing nothing
is the portable option. Being occasionally coarse beats being tier-dependent: a coarse message is a
usability cost, a tier-dependent one is a parity failure.

### Users are a HOST concern — identity, per-user resources, and host-owned storage (candidate platform work)

**Raised 2026-07-29 while planning DoneBlock** ([`DONEBLOCK-EXAMPLE-PLAN.md`](./DONEBLOCK-EXAMPLE-PLAN.md)
D11). It went through three shapes in one sitting — a manifest field, then WASI env vars, then injected
resources — before landing on the one that needs the least: **the engine has no user concept at all.**

> **The philosophy, in one line: users are infrastructure, coordinated by host and tier. An app should not
> know what a user is.**

That is a capability-model statement, not a feature. The engine gets a granted root and does its work; who
that root belongs to, and what they prefer, is resolved before the engine is ever called.

#### The app-facing concept is BINARY: `single` vs `multi`

Declared in `dlc.toml` (`[project] users = "single" | "multi"`), with **no default** — the tiers precedent:
worth asking rather than falling into.

The three host arrangements collapse to two app-facing modes, and the collapse is the point:

| Host arrangement | What the app sees | Why |
| --- | --- | --- |
| no user concept anywhere | **`single`** | there is one store and one writer |
| **`partitioned`** — host grants a per-user root | **`single`** | each partition IS a single-user store; the app cannot tell, and should not |
| **`shared`** — one store, several people | **`multi`** | "who" becomes part of the data model |

Two arrangements presenting as one mode is what makes this a real abstraction rather than a relabelling: an
app written for `single` runs unchanged on a partitioned multi-user host, because partitioning was never
its business.

**"Host-managed users" is not one of the modes** — it is true in *both*. Identity is always resolved by the
host (the philosophy above). The mode says only whether the app's **data model has a "who" in it**.

#### What `multi` actually obliges an app to do

If it obliges nothing, it is a comment rather than a mode — Decision 33's test. Concretely, `multi` means:

1. **Records may carry provenance** — `created_by`, an opaque host-supplied string, exactly as `created_at`
   already works. In `single` there is no such field to populate and none should exist.
2. **The app may not assume it is the only writer.** This is where the deferred lock file and §9's
   accepted last-writer-wins loss stop being theoretical, because two people can now write one store.
3. **Output that names an actor is meaningful**, so there is something for a host to render.

`single` is the promise that none of that applies, which is why it is worth declaring rather than assuming.

**Nothing needs `multi` yet, and that should be said out loud.** DoneBlock is `single`. Building `multi` now
would be a mode nothing exercises — the same trap as a manifest field nobody sets. Design it, declare the
field, and let the first app that genuinely shares a store drive the rest.

#### What that means per layer

| Concern | Owner | Notes |
| --- | --- | --- |
| who is invoking | **host** | OS user, browser profile, device — the host already knows, natively |
| per-user preferences | **host** | e.g. "alice's default project", filled into the request (Decision 28) |
| per-user data partitioning | **host** | binds a per-user directory as the granted root — the engine cannot tell |
| host-owned storage | **infrastructure** | must live OUTSIDE the app's granted area — see below |
| app data | **engine** | knows nothing about any of the above |

**Why this is cheaper than every earlier proposal:** no manifest field, no env var read by the engine, no
`user` field on request messages, no platform identity API. All three earlier designs put something in the
engine's path; this one deletes the requirement instead.

**The rule that keeps it checkable:** an app may **store** what the host tells it — `created_by`, the way
`created_at` already works — but may never **resolve, enumerate, or reason about** users. Provenance is a
string; a user model is not.

#### Yes: the host resolves the user and grants that user's subdirectory as the app's root

The mechanism, concretely — and **most of it already works**:

```
host resolves who            ->  "alice"
host grants a narrower root  ->  platform.Boot({Root: ".doneblock/users/alice", ...})
engine writes                ->  tasks/t1.json    (lands in alice's subtree; the engine cannot tell)
```

**Natively this needs nothing new.** `BootOptions.Root` is already an arbitrary host-chosen path and
`AppRoot(name)` is only a convention helper (`"." + name`) — a host may pass
`.doneblock/users/alice` today and the platform is satisfied. Containment then comes from the same
preopen/`SafeJoin` machinery everything else uses: the app cannot reach a sibling's directory whether or
not it tries.

**On the web it is nearly free too, and the reason is a detail already in the code:**
`loadTreeFromOPFS(root?)`, `flushTreeToOPFS(tree, root?)` and `clearOPFS(root?)` **all take an optional
directory handle** and only default to the OPFS root. The worker currently calls them with no argument;
passing a per-user subdirectory handle is a change to the *caller*, not to the bridge. (Note that a browser
origin is already a partition, so per-user subdirectories are mostly a native and server concern.)

**What falls out for free, and is worth stating because it is the payoff:**

- `reset-fs` wipes **only that user's** data — it resolves under the granted root
- `export-fs` produces a **per-user bundle**, which is also what makes bundles mergeable under §9's LWW sync
- two users never write the same file, so partitioned data has **no concurrent-write loss by construction**

**What was actually done (2026-07-29), and what was deliberately not:**

- [x] **`AGENTS.md` §3·5 now says partitioning is exactly as trustworthy as the host** — a host granting the
      wrong person's directory is undetectable from inside the engine, and no engine-side check is possible
      even in principle. Consistent with the capability model, and worth stating rather than letting "the
      filesystem enforces it" sound stronger than it is. The same section now also says a host may keep its
      own state and must keep it outside the granted root.
- [x] **Host-owned OPFS storage** — `dlc-platform/web/opfs.ts` reserves the top-level `.ilc-host` prefix and
      skips it on hydrate **and** flush. The flush is the half that matters: `writeDir` mirrors, so a
      directory the engine never hydrated would be deleted by the next command that writes anything.
      Falsified both ways — removing the flush guard deletes the host's state, removing the read guard puts
      it in an `export-fs` bundle. Test: `hosts/web/test/web.spec.ts`, "a host-reserved OPFS directory
      survives the engine and stays out of bundles". **This also serves [`INDEX-PLAN.md`](./INDEX-PLAN.md)
      D9**, which wanted the same mechanism for a different reason.
- [x] **Isolation is reported in the manifest** — `Filesystem.isolation`
      (`UNSPECIFIED | SHARED | PER_USER`) + `platform.Isolated()` + `BootOptions.Isolation`, in Go and in
      the TS encoder, with a parity vector so the field crosses both tiers rather than existing only in Go.
      Six tests; falsified by making silence read as isolated, which two tests caught.

      **Reversal, recorded because the first answer was wrong.** This was declined hours earlier on D6
      grounds — no consumer, and a "describe the root honestly" justification that was descriptive rather
      than functional. The consumer is not any app in this repo: it is **an app holding private data that
      needs to know whether privacy is its own problem** (a game host with per-player hidden state was the
      example that made it concrete). D6 exists to stop speculative *convenience* fields; it should not be
      read as forbidding a fact whose absence is discoverable only by leaking someone's data. **An unused
      field costs one field; a missing one costs data.**

      **Not a `FilesystemKind` value**, which is what was originally proposed: `kind` already conflates
      WHERE the root is (`CWD`, `APP_DIR`) with WHAT backs it (`OPFS`), and isolation is orthogonal to both
      — a per-user OPFS subtree is genuinely both. Three states rather than a bool, matching `Availability`:
      "nobody said" is not a claim that the store is shared.

      **Unset is SAFE, which is why no existing host had to change** — silence reads as not-isolated, so an
      app requiring privacy refuses rather than assumes, and a forgetful host fails loudly instead of
      leaking. That is the opposite of `FilesystemKind`, where `Boot` must refuse an unset value because
      the wrong guess points `reset-fs` at a user's directory. `AGENTS.md` §3·5 carries both, plus the
      caveat that this is the host's word and not a boundary.
- [ ] ~~the web worker passing a subdirectory handle~~ — **deferred, same reason.** The bridge already
      accepts one (`loadTreeFromOPFS(root?)`), so this is a caller change on the day a host wants
      partitioning. Nothing wants it: a browser origin is already a partition, and DoneBlock is single-user.
      Building it now would be a parameter nothing passes.

#### The one thing that DOES need platform work: host storage outside the app area

If the host owns user info, it needs somewhere to put it that the app cannot reach. Natively that is free
(`~/.config/<app>/` — a host is an ordinary program). **The web tier is where infrastructure is needed:**
the OPFS bridge hydrates everything under the root into the engine's FileData tree, so a host-owned subtree
would end up visible to the engine and inside `export-fs` bundles.

**That is the same mechanism [`INDEX-PLAN.md`](./INDEX-PLAN.md) D9 already wants**, for a different reason
(a SQLite pool directory, if a host-provided store ever lands). Two features now want "a place in OPFS that
is not part of the engine's tree", which is the point at which it is worth building once, properly, rather
than twice as exclusions.

- [ ] `dlc-platform/web/opfs.ts`: a reserved host-owned prefix, skipped by both hydrate and flush
- [ ] Assert it: a browser test that an `export-fs` bundle contains nothing from the host's area

#### Per-user resources: areas, if more than one is ever needed

The richer version — the host injects a *bundle* of resources, chiefly storage areas — is recorded because
it dissolves the mixed case (some data shared, some per-user) that a single root cannot express:

- **WASI already supports it.** `preopens.get-directories()` returns a LIST; today the web host does
  `_setPreopens({"/": tree})` and the platform exposes one `Root()`. The preopen path *is* the area name, so
  enumeration needs no schema.
- **The cost is concentrated in the inherited verbs**: `export-fs`/`import-fs`/`reset-fs` (100–102) resolve a
  prefix under `Root()` and would need to name an area — a platform schema change, a re-blessed id lock, and
  every host updated. `Root()`'s deliberate panic (`ENVIRONMENT-PLAN.md` D8) gets more complicated, and
  parity compares one tree where it would then compare several.

**Do not build areas until something binds two of them.** Nothing does. DoneBlock is single-area, and a
second area nothing uses would be a branch nothing tests.

#### Still true regardless

- **A clock does not fit any of this** and needs its own answer; it changes continuously, where a granted
  root and a remembered preference do not.
- **Identity is not authentication.** The host asserts it and nothing verifies it. It selects data and keys
  preferences. If it ever gates what a command may *do*, that is a security bug — and `AGENTS.md` should say
  so the day any of this lands.
- **No scaffolded app has ever had a host-side verb.** `dlc` has several; notes and tictactoe are pure engine
  surfaces. DoneBlock's `use` will be the first, and whether the template and the generated runner
  accommodate one is unproven.

### Capabilities
- [ ] **Derived index** (§6.2) — **re-scoped away from SQLite 2026-07-29; see [`INDEX-PLAN.md`](./INDEX-PLAN.md) and the CURRENT section above. The phases live there, not here.** A projection index the engine owns, queried in Go, stored behind a `wasi:keyvalue`-shaped seam that is file-backed today. Present on every tier including embedded, so there is no `unavailable` branch in app code at all — the scan survives only inside `rebuild-index`. The old note here (native `modernc.org/sqlite`, web `@sqlite.org/sqlite-wasm`, a `platform.HasIndex()` fallback branch, and index verbs in the surface parity vectors) is superseded in every particular except one: adding a capability still means adding it to the surface vectors, because parity cannot see registration.
- [ ] **Split-storage** write flow + `rebuild-index` (§7.1): lock-file discipline, atomic writes
- [x] **Events** capability + reactivity loop (§6.3): `ilc.data-changed` / `notes.record-changed` → UI re-reads. Decision 33; plan + findings in `docs/EVENTS-PLAN.md`. Built the `caps_native`/`caps_wasip2` seam (§5.3) and the first custom WIT import. No `useEngineEvent` hook — `subscribe()` from `@devalbo/dlc-web/api` was enough, and notes' UI is not React
  - [x] follow-up: cross-tab (`BroadcastChannel`) ✅ (2026-08-02) — and it turned out to be a **data-loss fix**, not a notification feature. The web tier mirrors a whole-tree snapshot back to OPFS and PRUNES, so a tab that hydrated before another tab's write deletes that write on its next write. Measured with the guard off: two tabs, one note each, **one note on disk**. Now the worker broadcasts after a write, a tab that hears it refuses further commands, and notes reloads. **Two design corrections came out of testing:** (1) the signal must ride the **flush, not events** — the first version relayed engine events and `dlc new` wrote 35 files while emitting nothing, so the other tab stayed happily stale; (2) the host must **flush only when the tree changed**, or a second tab's list-on-load invalidates the first tab for reading — `treeFingerprint` in `opfs.ts`. dlc's own tabs are unaffected by the hazard because it boots the engine lazily, which is why this sat unnoticed
  - [ ] follow-up: no desktop tier to wire `runtime.EventsEmit` into yet (§6.3)
- [x] **An app cannot ask whether it HAS a filesystem, and absence is not survivable.** *CLOSED 2026-07-28 by the manifest.* `platform.HasFilesystem()` answers it, `Availability` distinguishes "nobody said" from "there is none", and an app on `RegisterDiscovered` never registers verbs it cannot serve. §6.5's promise is now partly kept and partly reframed: the platform REPORTS and the app DECIDES (plan D10), so there is no inherited degradation mechanism — only an inherited way to ask. Original note: §6.5 promises graceful degradation when a capability is missing, and for the filesystem there is no degradation path at all: `engine/platform` exposes no availability API — no `Available()`, no `unavailable` — so an app calls `WriteTree` and either it works or it returns an error it had no way to anticipate. `dlc.toml`'s `capabilities = ["console", "filesystem"]` does not help; it has one writer and zero readers. Today apps *assume*. The **query/verify** half is exactly what the manifest below is for, and it is the first concrete demand on it that is not about Display — which matters, since Decision 34 removed the Display argument for building it.
- [x] **Environment manifest** (§6.4a, Decision 32) — *COMPLETE 2026-07-28; [`docs/ENVIRONMENT-PLAN.md`](./ENVIRONMENT-PLAN.md).* `SetEnvironment` platform command (core block, id 2 reserved): the host pushes capability facts at launch and re-sends on change; how `unavailable` stops being a linking problem and becomes a data one. **Re-justified by Decision 34:** its original headline reason was so a handler could branch on display facts, and a host-rendered app never learns there is a screen. What remains load-bearing is the non-display half — is there an index, what kind of FS root — which is also where the strict/lenient knob was already headed (`EVENTS-PLAN.md` §3, Phase 5)
  - [x] **decided (2026-07-28): standalone, with a real consumer.** The objection was that nothing today can be absent, making the "capability missing" branch decoration — but **OPFS can fail today** (storage denied, private browsing, older Safari) and the web host has no answer for it, so absence is a shipping gap on a tier we already ship. Also settled: `Root()` keeps panicking (D8), inherited FS verbs are registered by app choice through a **two-phase registry** (D7 — core verbs at init, capability verbs when the manifest lands), unregistered commands stay **visible and marked unsupported** rather than filtered (D9), and the platform reports while the app decides degradation (D10). Also settled: a **required non-zero revision** (D11 — its reader is D7, letting an unchanged manifest skip re-registration), `platform.Boot` owns the startup sequence so the template calls one function instead of copying five ordered steps (§2.5a), and `ilc.environment-stale` is adopted **provisionally** as the pull-shaped escape hatch over existing boundaries (D4)
  - [ ] **follow-up: the OPFS probe has no end-to-end test.** The absent BRANCH is watched running in a browser (a capability drops, verbs unregister, the inspector re-marks, it comes back), but the probe that would detect a real denial is not: it runs in the WORKER, and a Playwright stub of `navigator.storage.getDirectory` cannot reach a worker's global scope. Options are moving the probe to the main thread (stubbable, but the probing thread is then not the one using the filesystem) or a worker-visible `?ilc-no-fs=1` switch (a production test seam, declined). Left open deliberately — the residual risk is one `try/catch` around one API call
  - [ ] **follow-up: `ilc.environment-stale` designed, not built.** The pull-shaped escape hatch (engine asks, host re-sends). Nothing needs it yet, and an event with no emitter is the "field nobody sets" trap; build it when something asks
  - [ ] **follow-up: nothing triggers a re-send automatically.** The browser has no event for a filesystem appearing or disappearing, so `window.host.setEnvironment` is the only trigger today. The path is kept exercised so it can be trusted when a real trigger exists
- [ ] **Display** capability (§6.4) — **now OPTIONAL, and the app author's call** (Decision 34).

  **The governing principle, stated 2026-07-29 and stronger than what §6.4 currently argues:**

  > **Render decisions belong to the TIER, because it knows its timing and constraints better than the engine
  > ever can** — refresh rate, whether partial updates are cheap, how much RAM a framebuffer may take, whether
  > it is on battery, what is already on screen. An engine emitting draw commands knows none of it, and telling
  > it would take a manifest field per constraint, a list with no end.

  §6.4 argues for the semantic path from **dissimilarity of output** ("DOM, a TFT grid and terminal ASCII share
  no structure"). This argument is better, and it survives a case the other one does not: two tiers on ONE
  screen (`badge-native` / `badge-wamr`) look like the case where a shared draw list should win — until you
  notice **one of them is running an interpreter and the other is not**, so their envelopes differ even though
  their pixels are identical. **That retires the "the rule depends on which pair of tiers" finding** recorded
  here earlier: it does not depend on the pair, it depends on who knows the cost.

  **If a shared render is ever built, three parts — and they let a REQUIRED Display coexist with tier
  authority:**

  | What | Whose | Consequence |
  | --- | --- | --- |
  | **timing** — when to repaint | the tier | **pull, never push**: a command the host calls, not an import the engine drives |
  | **whether to use a shared render at all** | the tier | a constrained tier may decline the draw list and render from semantic state |
  | **content, when asked** | the engine | so two tiers that do use it cannot disagree about what is true |

  So the shape is `render(state) -> draw-list` as the app's own **command** (`method_id`, app band): an export
  the host pulls. No new WIT, no new import, no stub on a screenless tier, and the tier keeps timing for free.
  **A required Display is then harmless** — nothing is obliged to call it — which is what makes the
  optional-vs-required question the wrong one. It is push-vs-pull.

  **The draw vocabulary stays the app author's to keep straight** between engine and slots. No framework
  enforcement, and no cross-tier check that a rasterizer honours it: host parity compares renderings, not
  intentions.

  **Recorded as Decision 35** (2026-07-29) in [`DEVALBO-ILC-GO-PLAN.md`](./DEVALBO-ILC-GO-PLAN.md), with
  pointers added to Decision 34 (its optional-vs-required axis is superseded) and to §6.4 (the first two paths
  are sketched as an import and should be a pulled export; the reasoning is unchanged, the mechanism is not).

- [ ] **Network** (deferred): `wasi:http` when needed

### Tiers / hosts

**Tiers are declared in `engine/tiers.go`** — constants plus a `TierLandscape` table, cross-checked against
the template's `hosts/*` slots (see the landscape entry at the end of this section). `native` and `web` are
built. Every other row in that table is a task below, named by its constant, so the roadmap and the code use
one vocabulary.

**A tier is a composition recipe** (Decision 27): engine × host binding × ABI mode × capability set. Not a
board — **one board can be two tiers** (`badge-native` and `badge-wamr` are the same hardware) and two boards
can share one tier. Adding a tier is a table row plus a `templates/component-model/hosts/<tier>/` skeleton.

#### Shared prerequisites — nothing embedded starts before these

- [ ] **`platform.Boot` cannot report an absent filesystem, and its error message says it can.** Boot refuses
      an empty `Root` with *"grant one (see platform.AppRoot) or say so explicitly"* — and there is no way to
      say so: it then sends `Filesystem{Availability: PRESENT}` unconditionally. **Why nobody noticed:** the
      web host does not use `Boot`; `worker.ts` hand-builds the manifest and already handles the absent case,
      which is why that branch is tested at all. So the absent-filesystem path **exists in TypeScript and is
      unreachable from Go**, behind a correct-looking error. Needs an explicit "no filesystem" option that
      sends `AVAILABILITY_ABSENT` and skips `SetRoot`, keeping the refusal for a host that says *nothing*.
      **Blocks every embedded tier** — a board with no WASI has no filesystem to grant
      ([`example-plans/TIC-TAC-TOE-PLAN.md`](./example-plans/TIC-TAC-TOE-PLAN.md) §10.1d).
- [ ] **The caps seam has two of its three files, and the tags cannot express bare metal.** §5.3 and §5.6
      both specify `caps_wasip2.go` / `caps_wasip1.go` / `caps_native.go`; the tree has `caps_native.go`
      (`//go:build !tinygo`) and `caps_wasip2.go` (`//go:build tinygo`). Those tags conflate **"TinyGo →
      wasm"** with **"TinyGo → microcontroller"**, so a natively-linked embedded build selects the WIT-import
      file and fails on an import that is meaningless on the device. Needs a finer discriminator — TinyGo's
      `baremetal` tag is the likely candidate, unverified — plus the third file. **Blocks the first native
      embedded tier**, and found by reading the code rather than by building
      ([`example-plans/TIC-TAC-TOE-PLAN.md`](./example-plans/TIC-TAC-TOE-PLAN.md) §10.1a).
- [ ] **No wasip1 core-wasm build target exists.** `make build-wasm` emits the wasip2 component; WAMR needs
      `engine.core.wasm` from `tinygo -target=wasip1`, and nothing produces one. Portable mode's other half
      (Decision 25). **Blocks `badge-wamr` and `esp32-wamr`** but not the native embedded tiers, which use no
      wasm at all.
- [ ] **The deferred WAMR spike** — Phase-0 spike (3) of the main plan, the only one never run: *"WAMR running
      a TinyGo core module on ESP32-S3 with one host import."* **WAMR tiers only** (`badge-wamr`,
      `esp32-wamr`); the native embedded tiers do not need it. Worth noting what "one host import" now means
      concretely: it is `emit`, whose lowering to `//go:wasmimport` `ilc.wit` still marks **UNVERIFIED**. So
      the spike and the first real test of Decision 33's flat-scalars-and-bytes shape are the same piece of
      work. Findings go in `spikes/<name>/README.md`, and a red result reshapes the plan (§11).
- [ ] **A skeleton per embedded shape.** There are **three** shapes to template eventually — a Go host with a
      screen, a Go host without one, and a C++ host embedding WAMR — and `templates/wamr/` is deliberately
      unbuilt until a WAMR spike can `verify` (§16.6). Until a tier has a skeleton it stays `TierPlanned` and
      `dlc new` refuses it by name. **The hand-written build/flash target for each tier is the best input that
      template will ever get**; write each as though it were going to be scaffolded.

#### One task per planned tier

- [ ] **`desktop`** — Wails v2: webview over the **native** binding, so no new engine build and no new ABI
      (§5.4, §10). The cheapest remaining tier, and the only planned one with no embedded prerequisites.
- [ ] **`badge-native`** — RP2350B (Adafruit 6463, Badgeware Tufty): TinyGo compiled for the board and
      **linked directly**, Go host, 320×240 TFT, five buttons. **Build this before `badge-wamr`**: it has one
      unknown (does the pinned TinyGo target RP2350) where WAMR has several, so the badge *works* while WAMR
      is still a question, and the port gets a running reference instead of a blank screen. Needs the caps
      seam and the `Boot` fix above. Track N of
      [`example-plans/TIC-TAC-TOE-PLAN.md`](./example-plans/TIC-TAC-TOE-PLAN.md).
- [ ] **`badge-wamr`** — the same board, engine as **wasip1 core wasm under WAMR**, C++ host with Pimoroni
      display libraries, capabilities as WAMR native functions. §14 risk 2 calls RP2350-under-WAMR the
      least-proven combination in the project, and treating it as a *second tier* rather than an either/or
      spike **retires that framing as a gate** — a red result is a result, because Track N already shipped.
      **The prize for building both:** one engine source, core wasm vs native, on the same hardware — the
      first real check of the portable byte ABI, and the first verification that `emit` survives to core wasm
      (`ilc.wit` marks it UNVERIFIED today). **The cost:** two hosts in two languages, so the presentation is
      written twice. Track W of the same plan.
- [ ] **`keeb-native`** — RP2040 (Adafruit 5302, KB2040): TinyGo linked directly, and **no display at all**.
      264 KB SRAM makes native mandatory rather than chosen (Decision 18), so there is no WAMR variant of this
      tier. Renders over USB serial into someone else's terminal; input is a typed line or a **3×3 key matrix**,
      the purest input-map in the project (Decision 14). This is where "an app ships no presentation"
      (Decision 34) stops being a claim, and where `HasFilesystem() == false` is a fact about the hardware
      rather than a policy. Shares both prerequisites with `badge-native`. Track K of the same plan.
- [ ] **`esp32-wamr`** — ESP32-S3 under the official Espressif WAMR ESP-IDF component, PlatformIO C host, TFT
      display, serial REPL (§4, §5.3). The tier the WAMR toolchain is actually documented for, which makes it
      the *lower-risk* WAMR target even though no demo board for it is on the list yet
      (`demo-platforms.txt`) — worth considering before `badge-wamr` if the WAMR port fights back.

- [ ] **WAMR skeleton** (`templates/wamr/`) — wasip1 + native-fn caps; only after the WAMR spike can `verify` (§16.6, Decision 25); in-tree first, submodule later
- [ ] **Lift skeletons to git submodules** (`component-model`, then `wamr`) + introduce versioned `dlc-platform` depends (§16.6 sequencing #1–#2)

#### How the landscape came to be declared

- [x] **The tier landscape is declared, and the template says which entries are buildable** (2026-07-29) — `supportedTiers` is **derived** from
      `templates/component-model/hosts/*` instead of being a hard-coded `{native, web}`, and `dlc.toml`'s
      `[tiers.*]` writer defaults to `root = "hosts/<tier>"` for anything that is not web. `tierOf` has
      claimed since the host-layer work that "a new tier needs a directory here and nothing else"; that was
      false — it also needed an edit to the list and a case in the switch, and a contributor would have found
      the first by having their tier refused as "not supported yet". Two invariant tests (the offered set
      equals the template's slots; an unslotted tier is refused by name), falsified by re-hardcoding the
      list. Scaffold golden re-blessed: the native `[tiers.native]` comment is now the generic one.
      **Then corrected the same day:** derivation alone left no place that NAMES a tier — a typo'd
      `hosts/webb/` would silently become one, nothing tied the scaffolder's vocabulary to `dlc.toml`'s or the
      docs', and there was no constant to reference. So `engine/tiers.go` now declares the whole landscape as
      constants plus a `TierLandscape` table (`native`, `web` available; `desktop`, `badge-native`,
      `badge-wamr`, `keeb-native`, `esp32-wamr` planned), and the table drives both the offered set and the
      `[tiers.*]` sections. The two check **each other**: a slot with no row is a template that outgrew its
      vocabulary, a row claiming availability with no slot is a lie. Same shape as `reserved_method_id` —
      claim the name without pretending it works. A declared-but-unbuilt tier is refused **differently** from
      a nonexistent one, because those are different mistakes.

      **Falsification caught a tautology in my own test:** the first version asserted every *offered* tier had
      a slot, but the offered set is already filtered by slots, so it could not fail — marking `desktop`
      available left it green. Now it iterates the table's rows. Both directions watched failing.

      **Expect a refactor:** a proto enum is the likelier long-term home (constants in Go *and* TypeScript, and
      Decision 29 turns an enum into host-side menu choices), but that changes `NewRequest.tiers` from
      `repeated string` and is a wire change to make deliberately.

      **So adding an embedded tier to `dlc new` is now a row plus a skeleton** — and the skeleton still waits
      on the WAMR-vs-native answer below, since the two shapes differ.
### Filesystem export/import (§7.3)
- [ ] `--format=zip` and `--format=proto` (BFT is bootstrap; these are additive) — declared in `BundleFormat` and **explicitly refused** today rather than silently returning BFT
- [ ] BFT **deflate** variant (size)
- [ ] Two-apps/versions **BFT interchange** workflow (diff/migrate/merge). **Foundation landed:** `make verify-bundle-xtier` proves a bundle exported in the **browser** imports in the **terminal** and rebuilds a byte-identical tree; the diff/migrate/merge workflows build on that.

### Platform & tooling
- [x] Extract **`dlc-platform`** module once App #2 shares it (§16.4) — **DONE 2026-07-28.** A real nested Go module at `dlc-platform/`, resolved by `replace` inside this repo. **Renamed 2026-08-02** from `github.com/devalbo/dlc-platform` — a path naming a repo that does not exist, which Go can never fetch — to `github.com/devalbo/devalbo-ilc/dlc-platform`, which it can, via a `dlc-platform/vX.Y.Z` tag. Carries the Go platform, the protos + **committed** generated code (consumers cannot run `buf`), the WIT world, `protoc-gen-dlc-registry`, and the TS runtime `dlc-platform/web` (`@devalbo/dlc-web`). **A scaffolded app now depends on the platform and not on `dlc` at all.** Two id locks, one per band. `dlc`'s web slot moved `frontend/` → `hosts/web/`, so dlc finally has the layout it scaffolds. Original note: templates depend-on, never inline. **Package boundary landed early** as `engine/platform/` (now `dlc-platform/`) (a later module extraction is a directory move): dispatch, fs root seam, `SafeJoin`/`WriteTree`, BFT, and the inherited verbs. Driven by the template work — a template built against the wrong boundary would teach it to every scaffolded app. **Method id bands reserved: 1–9999 ILC (capability sub-blocks, 600–9999 held for capabilities not yet shipped), 10000+ the app** (`platform.AppMethodBase`); settled before the id-lock existed, when renumbering was still free. **`dlc` claims no reserved block — it is an app like any other** (`New`=10000): it never shares a registry with a scaffolded app, so a block would be signalling not protection, and keeping it in the app band is the dogfooding (plan §8). **Both claims are now checked, not asserted** (2026-07-28): `verify-scaffold.sh` reads the scaffold's `go list -m all` and builds it with `GOPROXY=off`, and `verify-platform-gen.sh` (T-B2.6) catches committed generated code going stale or never being added — the two failures invisible from inside this repo because we always regenerate.
  - [ ] **follow-up: publish it.** Deliberately deferred (no new repo yet). The remaining work is a
        directory move plus a tag — the module path is ALREADY `github.com/devalbo/devalbo-ilc/dlc-platform`, so no
        consumer re-imports anything. What must move with it: `dlc-platform/proto` currently sits in this
        repo's buf WORKSPACE so that dlc's `commands.proto` can import `devalbo/options/v1` from two
        directories away. Split the repo and that import has to resolve some other way:
        **vendor a fourth copy of `options.proto` into `dlc`** (what the template and both example apps
        already do, kept honest by `TestTemplateOptionsProtoInSync` + `make sync-template-proto`), or
        publish the platform's protos to the Buf Schema Registry and add a `deps:` entry the way
        `buf.build/protocolbuffers/wellknowntypes` already resolves `descriptor.proto`.
        **Vendoring is probably right:** the machinery exists and is tested, `dlc` would be one more copy
        of a file that already has three, and a BSR dep adds a hosted service, an account, and network
        access at codegen time for a repo that currently generates offline. Also drop the `replace`
        directives from the root, both example apps, and the scaffold template.
  - [ ] **follow-up: `@devalbo/dlc-web` is unpublished too**, consumed by `file:` paths. Same shape: an npm
        publish plus removing three `file:` deps and the template's `PlatformPathFrontend`.
- [ ] **Periodic dogfood review — where is `dlc` not using its own framework?** `AGENTS.md` §3 already says "`dlc` is an app like any other; if `dlc` needs special treatment, the template is teaching something `dlc` does not do." Nothing enforces it, and drift is **one-directional and invisible**: a capability gets built, notes and the template adopt it, and `dlc` keeps the old shape — so the tool that teaches the pattern is the one app not following it, and nobody notices because everything is green.

  **Cadence: when a capability or plan phase LANDS**, not on a calendar. That is when drift is created, and the diff is still fresh enough to fix cheaply.

  **The checklist** — for each thing the platform now offers, does `dlc` use it?
  - the generated CLI surface (`clispec` + `platform/cli`), or a hand-written `switch`?
  - a `dlc.toml` with declared tiers and slots — is `dlc` subject to the gate it enforces on apps?
  - `hosts/<tier>/` slot layout (Decision 34)
  - a view/port seam, so the slot is testable with **no engine**
  - `window.app` on the web tier
  - the inherited platform verbs, rather than re-implementing them

  **First review run, 2026-07-28 — most of the list is now closed.** dlc gained the three text routes
  (terminal, files, commands inspector), an `EnginePort` seam on its React slot, `window.app`, and a
  `dlc.toml`. Four browser tests cover them. What that leaves:
  - [ ] **dlc's version is hardcoded** while every app reads `dlcconfig.Display()` from its manifest. A real
        cycle, not neglect: dlcconfig is written by the dlc binary, which is built from the package that
        would import it. Needs a standalone generator, which needs the manifest parser out of
        `hosts/native` (package main). Until then the version lives in two places and this review is what
        catches them diverging.
  - [x] **`hosts/native/` was both the slot and the un-extracted runtime, and `frontend/` was dlc's web
        slot rather than `hosts/web/`** — both halves CLOSED 2026-07-28 by the §16.4 extraction. The
        runtime is `dlc-platform/` (Go) and `dlc-platform/web` (TS); `hosts/` now holds dlc's own slots and
        nothing else, and dlc's web slot is `hosts/web/` like every app's. What remains in `hosts/native/`
        is dlc's slot plus dlc's toolchain, which is ordinary.
  - [ ] **still open: dlc's hardcoded version.** The extraction did NOT unblock this — the cycle is
        `dlcconfig` being written by the dlc binary that is built from the package that would import it,
        and the manifest parser is still in `hosts/native` (package main). Unchanged by moving the
        platform out.

  **Second review run, 2026-07-28 — after the environment manifest (all four phases).** The two open items
  above are unchanged. What this capability created and the review caught:
  - [x] **drift in the OTHER direction**: `window.host` landed on `dlc`'s commands page only, so for a few
        minutes `dlc` had a handle the template did not teach. Fixed — notes, tictactoe and the template
        all expose it. Worth noting that the checklist is usually read as "is `dlc` behind?", and this was
        `dlc` ahead; both are drift.
  - [x] `dlc` uses the capability it built: it is the app on `RegisterDiscovered`, and its own web tier is
        the one that can genuinely lose a filesystem. notes and tictactoe stay eager **deliberately**, so
        one app exercises each policy.
  - [x] every native host, `dlc`'s included, boots through `platform.Boot` rather than hand-ordering the
        sequence — including the parity runner and the golden-tree generator, which are hosts too.

  **Third review run, 2026-07-28 — after the platform extraction (§16.4).** The biggest dogfood gap in the
  repo is now closed: `dlc` has the layout it scaffolds. `hosts/` holds only its own tier slots
  (`hosts/native/`, `hosts/web/`), the same shape `example-apps/notes/` has, and the inherited runtime it
  used to be tangled with lives in a separate module.
  - [x] **`frontend/` is gone.** dlc's web slot is `hosts/web/`, and the `dlc.toml` comment that used to
        explain the exception now explains that there isn't one.
  - [x] **dlc consumes the platform exactly as a scaffolded app does** — same module, same `replace`, same
        codegen plugin. It is no longer the app with a shortcut.
  - [ ] **still open: dlc's hardcoded version** (see the header note — the extraction did not touch it).
  - [ ] **new gap, small: dlc's `dlc.toml` has no `[platform] path`** the way scaffolded apps do; dlc finds
        the platform by being it. Harmless today, and worth a look when `dlc-platform` is published, since
        that is the moment the two stop being the same tree.

  **Original gap list (2026-07-28), kept for the record:**
  - `hosts/native/commands.go` still hand-writes argv parsing while notes and the template use the generated surface — `dlc` is now the *only* app not eating this
  - the repo root has **no `dlc.toml`**, so `dlc` alone escapes the slot gate
  - `dlc`'s web slot is `frontend/`, not `hosts/web/` — *deliberate*, blocked on the §16.4 runtime extraction, and already recorded as debt in `hosts/README.md`
  - `frontend/src/App.tsx` imports the api directly, with no `EnginePort` seam, so it cannot be slot-tested with no engine the way notes can
  - no `window.app` on `dlc`'s own web tier
  - `engine/commands.go` validates `--caps` and then writes a hardcoded `capabilities = [...]` regardless (noted in `EVENTS-PLAN.md` Phase 5, left alone)

  **Mostly a review, partly automatable** — and worth being honest about which: "does `dlc` use capability X" is a judgement call, but a few pieces are greppable invariants (a `dlc.toml` exists; no `switch args[0]` under `hosts/`; every tier directory matches a declared slot). Add those as checks so the manual pass shrinks over time rather than growing.

- [x] **`dlc` moved out of the app band into 9000–9999** (2026-07-29) — reversing Decision 29's "claims no
      privileged block", with the original reasoning kept and marked rather than edited away.

      **The safety argument was always sound and still is:** `dlc` and a scaffolded app never share a registry,
      so collision was impossible either way. **What moved is legibility** — `10000` meant "some app's first
      command, *or* dlc's `New`", and neither `method-ids.lock` nor a wire trace distinguished them. 10000+ is
      now the app's alone.

      **What made the block affordable, and it is the more interesting finding:** 600–9999 was reserved for
      capability verbs, and **capabilities turned out not to be command-shaped.** Events is an import
      (Decision 33), a shared render is a pulled *app* command (Decision 35), network is `wasi:http` — none
      consume method ids. Blocks 200–599 are empty and **seven framework ids exist in total**, so 9,400
      reserved ids were over-provisioned by three orders of magnitude. The band structure was designed when
      capabilities were expected to arrive as commands; three later decisions moved them off the command
      surface entirely. The realistic future claimants are more *inherited* verbs (§7.3's file verbs at 103+),
      which already have a home.

      New layout: **1–599** inherited verbs · **600–8999** future capability verbs · **9000–9099** dlc
      engine-served · **9100–9999** dlc host-local · **10000+** the app. This also retires the ad-hoc `10100+`
      band invented a day earlier purely to avoid colliding with dlc's own 10000/10001 inside one surface.

      **`TestMethodIDsRespectRanges` rewritten** to police four bands instead of two, with every boundary
      quoted from `engine.DlcMethodBase` / `engine.DlcHostLocalBase` / `platform.AppMethodBase` and never
      retyped — the failure mode that test's own comment records. Falsified both new edges. Updated:
      `AGENTS.md` §1, the plan's §8 table and Decision 29's paragraph, `DLC-COMMANDS.md`, and the id lock and
      parity vectors were regenerated (8 vectors pinned the old ids).
- [x] **`dlc run <tier>`, and the toolchain verbs' flags moved into the schema** (2026-07-29) — `run` claims
      10102 (the id lock refused the dropped reservation until re-blessed, which is the guard working).
      `native` builds then execs, forwarding args (`dlc run native list`); `web` resolves the same defaults
      `build web` does, npm-installs, opens a browser, serves. **Every other tier refuses by name** — `dlc`
      can exec a binary and serve a directory, not flash a board — and `run web` refuses stray program args
      rather than dropping them.

      **`BuildRequest`/`RunRequest` declare their flags**, so parsing, help, defaults and required-ness are
      generated; the hand-rolled flag loops are gone. `out`/`web_out` deliberately carry **no** schema default,
      because precedence is built-in → `dlc.toml` → flag and the host must distinguish "not given" from
      "given" — a declared default would make every invocation look explicit and silently clobber the manifest.

      **DX fix in the same pass: a boolean is a switch.** `--no-open`, not `--no-open true`. Bools now register
      via `flag.BoolFunc`, and the argv permutation stopped assuming every known flag consumes the next token
      (`--no-open web` would have eaten the tier). One existing test asserted the old ceremony and was updated
      rather than preserved.

      **The web surface omits host-local verbs entirely** rather than marking them: a browser cannot spawn a
      toolchain, and a listed command that cannot run is worse than an absent one.

      **Falsification caught a weak test, again.** The first two bool tests stayed green with the permute guard
      removed — a swallowed token still ended up trailing where the positional parser found it. Only
      `--force myapp --title x` (a flag *after* the positional) exposes it. Same lesson as the tier-landscape
      tautology earlier: the assertion has to be able to fail.

      **Left alone:** ffcli renders the switch as `-no-open=...` in help. Cosmetic, comes from ffcli's usage
      formatter rather than the schema, and worth a separate look.
- [x] **The toolchain verbs are declared in proto, and `dlc --help` finally lists them** (2026-07-29) —
      `proto/devalbo/dlc/v1/toolchain.proto` + a new `host_local` method option (50012). **The bug:** `gen` and
      `build` were a hand-written `map[string]func` consulted *before* the CLI ran, so they never appeared in
      `dlc --help` — the two commands every tutorial tells you to run were missing from the tool's own command
      list, and a reader would fairly conclude they did not exist. There was no single place listing the
      surface at all; it was split across `--help`, two id locks, a Go map, and Decision 30.
      **Now:** name/summary/help come from the schema like everything else, the generator emits **no dispatch
      map** for a service with no engine-served rpc (so an engine cannot serve one by accident), and the runner
      refuses a declared local verb with no handler — the same stance as a missing renderer.
      **Ids here are not wire ids** and are excluded from `method-ids.lock`; reservations still lock, because
      the lock is also the record of "this number is claimed". Band: **10100+**.
      **Two bugs found by running it**, both fixed: host-local verbs were marked `(unavailable on this host)`
      because the live-surface check asked the engine about commands it will never register; and the generated
      summary was cut mid-clause because only an rpc comment's first line becomes the summary.
      Falsified three ways — no handler → build error naming the command; drop the live-surface guard → both
      verbs go unavailable; and `ToolchainServiceHandlers` exists nowhere in the generated output.
      Documented in [`DLC-COMMANDS.md`](./DLC-COMMANDS.md). Follow-up: move these verbs' flags into the
      `.proto` so their parsing is generated too — today they still parse their own argv, which is why the
      `Local` handler takes `[]string`.
- [ ] `dlc` command surface: **tier-scoped** `dlc build <tier>` / `dlc run <tier>` / `dlc verify [<tier>] [--parity]` (Decision 27 — tiers are composition recipes, not forks), plus `dlc proto`, `dlc host add <tier>` (§16.7). Supersedes the bootstrap `make build-host` / `build-engine` / `verify-parity` for scaffolded apps
- [ ] **Project manifest** `dlc.toml` (§16.8) — capabilities/tiers/storage/ui/launch/platform pin; drives build/verify/host-add/launch
- [ ] Regenerate / upgrade (re-apply templates against a newer `platform` pin)
- [x] Scaffolder verify in CI: `dlc new … → devbox run verify` (§11 Scaffolder row) — `make verify-scaffold` (new → gen → test → build → run, hyphenated app name so the derived-identifier path is exercised), inside `test-b2` and therefore inside `./scripts/ci.sh full`
- [ ] `--tiers` / `--caps` / `--ui` / `--storage` flag expansion + fragment overlays (§16.6)
- [ ] FSA-granted directory backend for the web host (write to a user-picked local folder — Chromium) — §5.2
- [ ] **Setup parameter: where the command line lives** — engine-embedded vs host-side. Today parsing is host-side by Decision 28/29: the engine takes `execute(method, request)` and the surface is generated per host.

  **The case for engine-embedded is EMBEDDED, and it is strong.** On an ESP32 the "host" is C firmware under WAMR. Host-side parsing asks that firmware to know the command surface *and* encode protobuf requests in C (nanopb), per app. If the guest parses instead, the firmware is a dumb pipe — read a serial line, pass the string in, print what comes back — which is the difference between writing a parser per app in C and writing none. A serial REPL also maps cleanly onto the engine's *persistent* `execute` entry, which is already callable many times on a live instance. Also pairs with the deferred `wasi:cli/command` `run()` entry below, for a one-shot binary run straight under a wasm runtime with no host at all.

  **The tension: embedded is also where size hurts most.** Spike 4 measured in-engine ffcli at ~1.23 MB wasip1 against ~497 KiB hand-rolled, and retiring the argv shim shrank the component 1.91 → 1.52 MB. So the embedded answer is almost certainly **not** ffcli.

  **What makes this cheap later is a property of the choice already made.** Generating the surface as `clispec` DATA (rather than reflecting at runtime or hand-writing a switch) means one source of truth can drive either consumer: `platform/cli` + ffcli where size is irrelevant, or a minimal generated parser walking the same spec where it is not. Had the surface been runtime protoreflect, the embedded tier could not have had it at all — go-lite carries no protoreflect and dynamicpb is far past the size budget.

  **The hazard this creates, and it is the serious one: per-platform parsing means per-platform BEHAVIOUR.** Two parsers can disagree about `-name` vs `--name` (Spike 4 measured exactly that, pflag vs stdlib `flag`), about whether `required` is checked before or after defaults, about resolving an enum by name, about whether `-` means stdin. And **parity cannot see any of it**: parsing happens *before* the boundary parity guards, so each tier would be internally consistent while building a different request from the same input, with every existing check green. Same structural blind spot as slot rendering (Decision 34 D3), one layer earlier.

  Two mitigations, both of which should land WITH the choice rather than after it:

  1. **The invariant is `argv → request bytes`, not "one parser".** Heterogeneity already exists and is fine — the web tier parses no argv at all, it builds requests from a form. So what must be pinned is the mapping, not the implementation: a set of **parse vectors** (argv in, request bytes out), in the same shape as the existing parity vectors, that every CLI-bearing tier must satisfy in CI. That is the check that would catch a `-name` disagreement; nothing else can.
  2. **Keep the semantics portable and the platform-specific part tiny.** `platform/cli` already splits this way by accident and should on purpose: `encode.go` (required checks, defaults, enum names, source resolution, wire encoding) is pure and TinyGo-safe; only `run.go` pulls in ffcli for subcommand routing and help. If an in-engine parser reuses the portable half, what can differ shrinks to argv *tokenizing*.

  Decided at `dlc new` alongside `--tiers`, and closer in kind to the ABI-mode toggle below than to a flag. Note it probably cannot be one project-wide answer — an ESP32 wants in-engine, a browser has no argv at all — so the conformance vectors are not optional extra credit, they are what makes the per-tier choice safe.
- [ ] **ABI-mode toggle at setup** (§5.6, Decision 25): `dlc new` derives portable byte-ABI vs rich Component-Model ABI from the `tiers` (`--wamr` forces portable); scaffold the matching capability-boundary + build targets (wasip1 core seam only when portable); a lint/check that keeps a portable-mode engine core-wasm-safe
- [→] `dlc doctor` — **promoted to Phase B2** (CLI host): the command form of preflight, assessing per-tier readiness — [`DEVALBO-DLC-PREREQUISITES.md`](./DEVALBO-DLC-PREREQUISITES.md)

- [x] **Shell lint** (`make lint-scripts`, in `ci.sh`) — the patterns that stay green locally and only fail once output grows: capturing a verify script's output into a variable (blows Linux `ARG_MAX`), piping into `grep -q` (SIGPIPE + `pipefail` reports a *successful* match as failure), and writing into the repo without a cleanup trap. Each rule is a bug that already reached CI; each is falsified in place before being trusted.
- [ ] **Local Linux CI emulation via Docker** — `docker/Dockerfile.ci` (Debian + devbox + Chromium system deps) and `make ci-docker` running `./scripts/ci.sh full`, so `ci.sh` gains a third caller alongside the shell and the workflow.
  **Why:** every cross-platform bug so far has been Linux-vs-macOS, not x86-vs-ARM — `ARG_MAX`/E2BIG, case-sensitive paths, GNU vs BSD coreutils, missing Chromium libs. One of them (a failed `rm` leaving a `//go:build tinygo` probe that emptied the template FS) took out two suites and was invisible on macOS.
  **Default `linux/arm64`**, not amd64: it runs at native speed on Apple Silicon and catches that whole class; keep `--platform linux/amd64` behind a flag for arch-specific work, where qemu makes it slow.
  **Honest limits:** multi-GB image (nix store + TinyGo + node + Chromium), slow first build, and Debian+devbox is not byte-identical to `ubuntu-latest` — it narrows divergence rather than eliminating it, and only helps if someone runs it. The shell lint above covers the same bug class unconditionally, which is why it came first.

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
