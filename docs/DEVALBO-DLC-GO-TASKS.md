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
- [ ] **Host introspection (Decision 29):** the host embeds the `buf build` **FileDescriptorSet** and walks it with **protoreflect** (native Go) / `@bufbuild/protobuf` (web) → methods + `method_id` + request fields (name→flag, type, proto `enum`→menu, option help/required/default). **Custom options arrive as unknown fields** on `MethodOptions`/`FieldOptions`: register via `dynamicpb.NewExtensionType`, re-unmarshal with `UnmarshalOptions{Resolver}`, read with `ProtoReflect().Range` — `HasExtension` is unreliable across dynamicpb type identities (spike-measured). Engine `describe()` is **optional** — only for a *generic* host that doesn't embed the schema. **Bootstrap shim retired (Decision 28 complete):** `hosts/native/commands.go` parses argv into requests; `execute-cli` is gone from the world, `engine.Execute`/ffcli are gone from the engine (component **1.91 MB → 1.52 MB**, ~20% smaller), and the argv parity stream retired — the 19 method vectors cover it.
- [x] **WIT boundary migration (Decisions 28/31):** `execute-cli(args: list<string>)` → **`execute(method: u32, request: list<u8>) -> command-result`** — scalar id + proto-bytes payload (WAMR-portable; only rich WIT records/variants need the Component Model). Keep the string-args shim until callers move. **Landed:** both exports declared on `world engine`; `cmd/engine-component` wires each through a shared `toCommandResult`. `make build-engine` (TinyGo wasip2) green, and `make verify-parity` now covers **both** boundaries — 9 argv vectors + 10 `execute(method, request)` byte-vectors (`verify/parity/method-vectors.json`, hex requests derived from typed fixtures via `make parity-vectors`; native side is `cmd/parity-runner` until hosts/native builds requests). The method diff includes the **error string**, so TinyGo and native Go must agree on envelope errors, unregistered ids, and decode failures too.
- [ ] `supported-abis() -> list<u8>` export (byte-ABI, Decision 31) — the guest advertises its boundaries + versions (`["bytes/1"]` today) so hosts pick the richest supported. Cheap hook now; enables a per-capability rich WIT boundary later without breaking the byte path.
- [ ] **`protoc-gen-dlc-registry` plugin** — reads the `service` + `method_id` options **from the `buf build` image / CodeGeneratorRequest descriptors** (go-lite emits no service stubs, so generated Go is not a source — spike-measured) → emits the engine's `method_id → handler` registration (the reflection-free part) and **enforces `method_id` stability** against a committed lock. Host-side introspection uses the standard descriptor set (no custom host config to generate). Runs under `dlc gen` / `buf generate` (Decision 29).
- [ ] `engine/caps_native.go` / `caps_wasip2.go` / `caps_wasip1.go` build seam for capability imports (§5.3) — native seam lets the CLI host link the engine in-process (Decision 26)
- [x] `export-fs` / `import-fs` handlers over the WASI filesystem (§7.3) — needed because scaffolding = `import-fs`. **Landed** as method ids 4/5 in **BFT** (the real spec: recursive `directory`/`text`/`binary` nodes, alphabetical entries, base64 for binary). Hand-written encoder **and** parser in `engine/bft.go` — `encoding/json` is reflection-heavy and banned in the engine, so the parser accepts only the BFT subset (objects + strings). Bundles are byte-stable (sorted) and text-vs-binary is chosen by content, so a scaffold bundle is readable and diffable. Untrusted-input safe: every path goes through `safeJoin`. Non-regular files (symlinks) are **skipped** — BFT cannot represent them and reading one errors.
- [ ] Scaffolding handler (`new`): `import-fs` a template bundle → write tree → token-substitute (`{{.Module}}`, `{{.ProjectName}}`)

### Templates (its own area, §16.6 — bootstrap sequencing locked)
- [ ] Author **`templates/component-model/` in-tree** — **full `dlc`-shaped** skeleton (engine + CLI/web host stubs + go.mod + devbox + wit + proto). B2 = terminal path; B3 completes browser. **Do not** create the skeleton git submodule yet (lift later).
- [ ] **Defer** versioned `ilc-platform` `go.mod` depend until submodule graduation (§16.4 / §16.6 #2)
- [ ] `templates/fragments/` in-tree for overlay packs (`--caps` / `--tiers` / …) — ABI mode picks the skeleton, not a fragment
- [ ] `go:embed` the resolved `templates/` tree into the **engine** so `dlc new` is offline + browser-capable. Never runtime-clone templates
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
- [ ] `dlc new myapp` produces a buildable project tree on disk
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

- [ ] One engine **codebase** drives CLI (native in-process) and web (`engine.component.wasm` via jco); golden `command-result` vectors byte-identical across native, CI wasm-parity, and browser (Decision 26)
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
- [ ] **Environment manifest** (§6.4a, Decision 32) — `SetEnvironment` platform command (core block, id 2 reserved): the host pushes capability facts at launch and re-sends on change. Prerequisite for Display; also how `unavailable` stops being a linking problem and becomes a data one
- [ ] **Display** capability (§6.4): draw-command list + retained widget tree, branching on the manifest
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
- [ ] `dlc` command surface: **tier-scoped** `dlc build <tier>` / `dlc run <tier>` / `dlc verify [<tier>] [--parity]` (Decision 27 — tiers are composition recipes, not forks), plus `dlc proto`, `dlc host add <tier>` (§16.7). Supersedes the bootstrap `make build-host` / `build-engine` / `verify-parity` for scaffolded apps
- [ ] **Project manifest** `dlc.toml` (§16.8) — capabilities/tiers/storage/ui/launch/platform pin; drives build/verify/host-add/launch
- [ ] Regenerate / upgrade (re-apply templates against a newer `platform` pin)
- [x] Scaffolder verify in CI: `dlc new … → devbox run verify` (§11 Scaffolder row) — `make verify-scaffold` (new → gen → test → build → run, hyphenated app name so the derived-identifier path is exercised), inside `test-b2` and therefore inside `./scripts/ci.sh full`
- [ ] `--tiers` / `--caps` / `--ui` / `--storage` flag expansion + fragment overlays (§16.6)
- [ ] FSA-granted directory backend for the web host (write to a user-picked local folder — Chromium) — §5.2
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
