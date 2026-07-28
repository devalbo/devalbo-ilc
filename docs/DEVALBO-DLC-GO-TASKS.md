# DEVALBO-DLC — Implementation Tasks

Task breakdown derived from [`DEVALBO-ILC-GO-PLAN.md`](./DEVALBO-ILC-GO-PLAN.md) (authoritative). Naming:
**ILC** = the framework; **`dlc`** = the CLI tool (Devalbo Line of Command).

**The bootstrap is MET** (see the roll-up below) — `dlc` runs in the terminal and the browser from one
engine, and App #2 (`notes`) and the Events capability landed on top of it.

**Current focus: the HOST LAYER** — [`HOST-LAYER-PLAN.md`](./HOST-LAYER-PLAN.md), would settle as Decision
34. Per-app, per-tier host code becomes a named, contracted, scaffolded thing, and **Display drops to
optional**: an app author chooses between app-side rendering (§6.4's draw-list / widget tree) and emitting a
semantic event the **host** renders however that tier likes.

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
- [ ] **Defer** versioned `ilc-platform` `go.mod` depend until submodule graduation (§16.4 / §16.6 #2)
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

### 🎯 CURRENT — the host layer (Decision 34) — [`HOST-LAYER-PLAN.md`](./HOST-LAYER-PLAN.md)

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
- [ ] **Phase 3 — tic-tac-toe (App #3)**: one engine, DOM and ASCII slots, semantic events the host renders. Built against the **current** string+bytes API on purpose
- [ ] **Phase 4 — host parity**: two slots, one synthetic stream, compare normalized renderings — the only mechanical check on a layer parity cannot reach, and it has no subject until Phase 3 exists
- [ ] **Phase 5 — the published interface**: event schemas declared in proto, **locked** like `method_id`, generating both emit and subscribe sides. Deletes the four hand-mirrored topic literals that `AGENTS.md` §1 already bans for ids. Costs one rewrite of tic-tac-toe's subscriber wiring — accepted, so the codegen's shape is decided by a real consumer
- [ ] **Phase 6 — scaffold the slot**: `dlc new` emits slots + their tests, and **tier selection becomes a setup question** (host-side prompt when `--tiers` is absent — a tier is now a directory of host code plus a checked `dlc.toml` entry, so it is worth asking rather than defaulting silently). The documentation half of this phase already landed with Phase 1

**Generated apps are disposable for now** — re-scaffold rather than migrate, so template layout changes
cost a re-bless (`make scaffold-golden`) and not a migration story.

### Second app — notes/list (App #2, the breadth pilot) — §13
- [x] Scaffold notes/list — `example-apps/notes/`, built on the platform via `dlc.toml` `[platform] path`. **Not** scaffolded by `dlc new` in the end; it predates the template being complete, and Phase 1 above brings its layout back in line
- [x] Handlers: `create` / `list` / `delete-record` over the filesystem, plus its own `notes.record-changed` event (the first app-defined topic)
- [ ] `open` / `update` / `rebuild-index` — wait on split-storage and the SQLite index
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
scaffolds vanilla TS). Putting React in `@devalbo/ilc-web` would push it onto every scaffolded app that
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

### Capabilities
- [ ] **SQLite-index** (§6.2): native `modernc.org/sqlite`; web `@sqlite.org/sqlite-wasm` (OPFS); `unavailable` fallback → file scan
- [ ] **Split-storage** write flow + `rebuild-index` (§7.1): lock-file discipline, atomic writes
- [x] **Events** capability + reactivity loop (§6.3): `ilc.data-changed` / `notes.record-changed` → UI re-reads. Decision 33; plan + findings in `docs/EVENTS-PLAN.md`. Built the `caps_native`/`caps_wasip2` seam (§5.3) and the first custom WIT import. No `useEngineEvent` hook — `subscribe()` from `@devalbo/ilc-web/api` was enough, and notes' UI is not React
  - [ ] follow-up: cross-tab delivery (`BroadcastChannel`) — a second tab does not see this one's writes
  - [ ] follow-up: no desktop tier to wire `runtime.EventsEmit` into yet (§6.3)
- [ ] **An app cannot ask whether it HAS a filesystem, and absence is not survivable.** §6.5 promises graceful degradation when a capability is missing, and for the filesystem there is no degradation path at all: `engine/platform` exposes no availability API — no `Available()`, no `unavailable` — so an app calls `WriteTree` and either it works or it returns an error it had no way to anticipate. `dlc.toml`'s `capabilities = ["console", "filesystem"]` does not help; it has one writer and zero readers. Today apps *assume*. The **query/verify** half is exactly what the manifest below is for, and it is the first concrete demand on it that is not about Display — which matters, since Decision 34 removed the Display argument for building it.
- [ ] **Environment manifest** (§6.4a, Decision 32) — `SetEnvironment` platform command (core block, id 2 reserved): the host pushes capability facts at launch and re-sends on change; how `unavailable` stops being a linking problem and becomes a data one. **Re-justified by Decision 34:** its original headline reason was so a handler could branch on display facts, and a host-rendered app never learns there is a screen. What remains load-bearing is the non-display half — is there an index, what kind of FS root — which is also where the strict/lenient knob was already headed (`EVENTS-PLAN.md` §3, Phase 5)
- [ ] **Display** capability (§6.4) — **now OPTIONAL, and the app author's call** (Decision 34). Three paths, chosen per app or per event: draw-command list · retained widget tree · **semantic events the host renders**. The first two put presentation in the app and are what this capability builds; the third costs one small tier slot and no capability at all, so it goes first. Build draw-list/widget-tree when an app genuinely wants to write presentation **once** and have it work everywhere — not before
- [ ] **Network** (deferred): `wasi:http` when needed

### Tiers / hosts
- [ ] **Desktop** host — Wails v2 (webview + native Environment) — §5.4, §10
- [ ] **ESP32-S3** — WAMR (official ESP-IDF component) + PlatformIO C host firmware; TFT Display; serial REPL — §4, §5.3
- [ ] **RP2350** — WAMR-via-arduino-pico *or* native TinyGo fallback (§14 risk 2)
- [ ] **RP2040** — native TinyGo build (no wasm) — §5.3
- [ ] WAMR embedded spike (the deferred §11 spike 3)
- [ ] **WAMR skeleton** (`templates/wamr/`) — wasip1 + native-fn caps; only after the WAMR spike can `verify` (§16.6, Decision 25); in-tree first, submodule later
- [ ] **Lift skeletons to git submodules** (`component-model`, then `wamr`) + introduce versioned `ilc-platform` depends (§16.6 sequencing #1–#2)

### Filesystem export/import (§7.3)
- [ ] `--format=zip` and `--format=proto` (BFT is bootstrap; these are additive) — declared in `BundleFormat` and **explicitly refused** today rather than silently returning BFT
- [ ] BFT **deflate** variant (size)
- [ ] Two-apps/versions **BFT interchange** workflow (diff/migrate/merge). **Foundation landed:** `make verify-bundle-xtier` proves a bundle exported in the **browser** imports in the **terminal** and rebuilds a byte-identical tree; the diff/migrate/merge workflows build on that.

### Platform & tooling
- [~] Extract **`ilc-platform`** module once App #2 shares it (§16.4) — templates depend-on, never inline. **Package boundary landed early** as `engine/platform/` (a later module extraction is a directory move): dispatch, fs root seam, `SafeJoin`/`WriteTree`, BFT, and the inherited verbs. Driven by the template work — a template built against the wrong boundary would teach it to every scaffolded app. **Method id bands reserved: 1–9999 ILC (capability sub-blocks, 600–9999 held for capabilities not yet shipped), 10000+ the app** (`platform.AppMethodBase`); settled before the id-lock existed, when renumbering was still free. **`dlc` claims no reserved block — it is an app like any other** (`New`=10000): it never shares a registry with a scaffolded app, so a block would be signalling not protection, and keeping it in the app band is the dogfooding (plan §8).
- [ ] **Periodic dogfood review — where is `dlc` not using its own framework?** `AGENTS.md` §3 already says "`dlc` is an app like any other; if `dlc` needs special treatment, the template is teaching something `dlc` does not do." Nothing enforces it, and drift is **one-directional and invisible**: a capability gets built, notes and the template adopt it, and `dlc` keeps the old shape — so the tool that teaches the pattern is the one app not following it, and nobody notices because everything is green.

  **Cadence: when a capability or plan phase LANDS**, not on a calendar. That is when drift is created, and the diff is still fresh enough to fix cheaply.

  **The checklist** — for each thing the platform now offers, does `dlc` use it?
  - the generated CLI surface (`clispec` + `platform/cli`), or a hand-written `switch`?
  - a `dlc.toml` with declared tiers and slots — is `dlc` subject to the gate it enforces on apps?
  - `hosts/<tier>/` slot layout (Decision 34)
  - a view/port seam, so the slot is testable with **no engine**
  - `window.app` on the web tier
  - the inherited platform verbs, rather than re-implementing them

  **Known gaps as of 2026-07-28** (the first review's starting list, all real today):
  - `hosts/native/commands.go` still hand-writes argv parsing while notes and the template use the generated surface — `dlc` is now the *only* app not eating this
  - the repo root has **no `dlc.toml`**, so `dlc` alone escapes the slot gate
  - `dlc`'s web slot is `frontend/`, not `hosts/web/` — *deliberate*, blocked on the §16.4 runtime extraction, and already recorded as debt in `hosts/README.md`
  - `frontend/src/App.tsx` imports the api directly, with no `EnginePort` seam, so it cannot be slot-tested with no engine the way notes can
  - no `window.app` on `dlc`'s own web tier
  - `engine/commands.go` validates `--caps` and then writes a hardcoded `capabilities = [...]` regardless (noted in `EVENTS-PLAN.md` Phase 5, left alone)

  **Mostly a review, partly automatable** — and worth being honest about which: "does `dlc` use capability X" is a judgement call, but a few pieces are greppable invariants (a `dlc.toml` exists; no `switch args[0]` under `hosts/`; every tier directory matches a declared slot). Add those as checks so the manual pass shrinks over time rather than growing.

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
