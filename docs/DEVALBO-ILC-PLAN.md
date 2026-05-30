# DEVALBO-ILC — Review & Implementation Plan

Companion to [DEVALBO-ILC.md](./DEVALBO-ILC.md). This document records feasibility/utility assessment, concerns, open questions, and a concrete path to V1.

> **⚠️ Reading order (2026-05-30):** The dated sections **Runtime decisions** → **Invocation & CLI
> binding** → **Handler I/O & serialization** → **Compiler architecture — dual front-end** →
> **Distributed state** are the **current, authoritative** direction. They supersede earlier passages
> where these differ. The pre-2026-05-30 review below (Executive summary, Feasibility/Utility,
> Recommended V1 scope, Questions, Concerns) is retained for history; **reconciliation notes** mark
> what changed. Net shifts: **Rust-lead** (not tri-language-parallel); **three Rust runtimes**
> (native / embedded-Embassy / browser-WASM); **two IDLs by concern** — WIT for capability interfaces,
> **protobuf** for message types (OBI/JSON-Schema dropped for I/O); CLI/dispatch **bind to existing
> libraries**; a **`Store`** capability for distributed state.

---

## Executive summary

> **Reconciled (2026-05-30):** "three-language contract pipeline first" held for Phase 1 (done), but
> Phases 2+ are now **Rust-lead** — Rust built deepest, TS/Python kept in lockstep via golden tests,
> their hosts deferred. See *Runtime decisions*.

**Verdict:** The problem is real and aligned with existing devalbo thinking (command decomposition, environment adapters, testability). V1 remains feasible if phases are ordered by **risk reduction**: prove the **three-language contract pipeline** first, then flesh out capabilities and hosts.

**Phase 1 priority:** TypeScript, Python, and Rust share generated **`ConsoleIo`** + minimal **`Environment`** (`consoleIo` only). Output is **logger-shaped** (`info` / `error`, void side-effects); **stdin stays** (`readLine`). Handlers never pick a host binding.

**Verdict on the full spec as written:** Still ambitious for full hosts, pilots, and registry publishing—but **front-loading WIT + codegen for `ConsoleIo` across three languages** validates the pipeline before `FileSystem` / `Network`.

**Recommended posture:** Phase 1 = **shared schema + compiler + golden tests** + per-language packages. Host factories (`createNodeEnvironment`, `createBrowserEnvironment`, …) wire logger sinks; callbacks are SDK internals, not handler choices.

---

## Review decisions (2026-05-27)

Resolved with the author after the [second-pass review](./DEVALBO-ILC-CLAUDE-REVIEW.md).
**These amend the phased plan below where they differ** — notably: FileSystem enters V1, the
contract is async-first, and the tagline changes.

- **Posture: matrix-first.** Validate cross-language parity *early* (the riskiest assumption)
  over a single working pilot. Tri-language (TS, Python, Rust) stays a hard V1 goal — real
  runtimes act as guardrails. (The reviewer's "TS-only first" recommendation is withdrawn.)
- **Name:** keep `ILC` (it is `CLI` reversed). Tagline → **"Inverted Line of Command"**, with
  "inversion of control" in the opening line. V1 is **capability injection**; handler
  registration/dispatch stays out of V1 scope (revisit later) — so the spec should stop
  implying a CLI framework.
- **IDL: keep WIT, but compile through an internal IR.** Emitters target the IR, never WIT
  types directly, so the IDL is swappable later (WASM-future stays open) at the cost of a new
  parser front-end only. → Phase-1 design rule. *(2026-05-30: this IR drives the **WIT/capability**
  front-end only; **message types** use a second, independent front-end — off-the-shelf `buf`/proto,
  not the IR. See *Compiler architecture — dual front-end*.)*
- **Async-first contract.** All capabilities are async; emit idiomatically (TS `Promise`,
  Rust `async fn`, Python awaitable). **Sync is non-portable host sugar only** — it cannot
  exist in the browser, so it must never enter the shared contract. `IlcResult` rides *inside*
  the future (`Promise<Result<T,E>>` / `async fn -> Result<T,E>`).
- **V1 capability scope:** `ConsoleIo` (hello world) + stdin + **basic, binary-capable
  FileSystem** (`read_bytes`/`write_bytes`, text as a convenience). Network + the BFT pilot
  are deferred.
- **Environment:** introspectable, per-host; handlers pull capabilities from it. Minimal core
  + a typed **escape hatch** to discover/reach host-specific capabilities.
- **Host selection:** auto-detect the runtime to pick a *default* host, but it must be
  **overridable** (tests inject explicitly). Drop the "default implementations" framing from
  DEVALBO-ILC.md — these are startup-selected adapters, not ambient defaults.
- **TS host may use Effect-TS** (`Effect<A, E, R>` covers async + Result + the Environment `R`
  channel) as a TS *implementation*, with the generated interface kept Effect-agnostic so
  Python/Rust stay first-class.
- **To spec (added below):** an error taxonomy (variants + host-error→`E` mapping) and a
  prior-art/positioning section.

---

## Runtime decisions (2026-05-30)

**Rust leads** as the first fully-built target. WIT stays source of truth; TS/Python keep
regenerating via golden tests so drift surfaces immediately (build out Rust hosts deepest;
defer TS/Python *hosts*, not *types*).

**Three Rust runtimes are an explicit goal, all conforming to the same generated Rust
contract:**

- **Native host (std + tokio)** — servers, CLIs, tests.
- **Embedded host (`no_std` + Embassy)** — bare-metal MCUs. The ultimate constrained host and
  the contract's portability stress test: if it maps here, it maps anywhere.
- **Browser-WASM host (`wasm32-unknown-unknown` + `wasm-bindgen-futures`)** — Rust compiled to
  WASM, served as a single-page app (`trunk`/`wasm-pack`; UI framework is the app author's
  choice — ILC governs only the capability boundary). This is the **Rust mirror of the TS
  browser host**: `info`/`error`→`console.log`/`console.error`, `readLine`→`window.prompt()`,
  `Network`→`fetch` (`web-sys`/`gloo-net`), `FileSystem`→OPFS/`memory://` or `unavailable`.
  Note: this is a plain Rust→WASM *app build*, **not** the WIT Component Model (still the
  deferred WASM-future endgame); the SPA target keeps that door open without committing to it.

Three executors (tokio / Embassy / `wasm-bindgen-futures`), one async contract — which is
precisely why async stays executor-agnostic.

For one contract to serve all three, these are **locked constraints on the *generated* Rust
code** (the hosts, not the contract, carry the runtime specifics):

- **`no_std`-able.** Rust emitter gains a `no_std` profile. Default `alloc`-on tier (owned
  `String`/`Vec`, Embassy executor); bare-metal no-alloc (`heapless`, `&'static str`) as a
  later stretch tier.
- **No mandated `Arc` / `Box` / `dyn`.** `Environment` and capability consumption use **static
  dispatch / generics**; a host may pass `&dyn` if it chooses, but the contract never
  *requires* boxing or atomics.
- **Async stays executor-agnostic.** Generated traits use `async fn` / `impl Future`; the
  **host** picks the executor (tokio / Embassy / `wasm-bindgen-futures`). No runtime type ever
  appears in generated code.
- **No `Send` / `Sync` bounds in the contract.** Browser-WASM futures are `!Send` (single
  thread, JS values aren't `Send`); tokio's `spawn` and `#[async_trait]`'s default *add*
  `Send`. So generated traits use **native `async fn` in traits (AFIT), not `#[async_trait]`'s
  `Send`-by-default**, and stay `Send`/`Sync`-agnostic — each host adds `Send` only where its
  executor needs it (tokio yes; Embassy / browser no).
- **Heavy deps live in hosts only.** Effect frameworks, allocators, runtimes — host-side,
  never the contract. `id_effect` (Effect-TS analog: `Effect<A, E, R>`, Layer/Service DI) is
  std+heap (tokio/futures/`im`/flume), so it is **host-implementation-only**, the Rust mirror
  of the Effect-TS decision, and excluded from embedded by construction.

**Resolved (2026-05-30):** the capability error is restructured to `{ kind: error-kind, code: u32,
message }` — `kind` + numeric `code` always present and zero-alloc; only `message` is tiered
(`String` / `&'static str` / `heapless::String<N>`). See *Error taxonomy*.

### Runtime × capability matrix

One generated contract; each host environment maps the capabilities to its substrate. Absent
capabilities never crash — they return the `unavailable` error variant.

| Capability | Terminal / shell — **native** (std, tokio) | Browser SPA — **WASM** (`wasm32-unknown-unknown`, `wasm-bindgen`) | Embedded boot — **host mode** (`no_std`, Embassy) |
| --- | --- | --- | --- |
| `ConsoleIo.info` / `.error` | stdout / stderr | `console.log` / `console.error` | UART TX / RTT (`defmt`) |
| `ConsoleIo.readLine` | stdin (line-buffered) | `window.prompt()` (cancel → `none`) | UART RX line, else `none` (no console) |
| `FileSystem.read_bytes` / `write_bytes` | `tokio::fs` over real FS | OPFS or `memory://` virtual paths; else `unavailable` | on-chip flash (`embedded-storage` / `littlefs`); else `unavailable` |
| `Network.fetch` | `reqwest` / `hyper` | `fetch` (`web-sys` / `gloo-net`), CORS-bound | `embassy-net` / `smoltcp` if a stack exists; else `unavailable` |
| **Executor** | tokio | `wasm-bindgen-futures` (browser event loop) | Embassy executor (sleeps core on idle) |
| **Entry point builds `Environment`** | `main()` parses argv → `NativeEnvironment` | `#[wasm_bindgen(start)]` → `BrowserEnvironment`, mounts UI | `#[embassy_executor::main]` at boot → `BootEnvironment` from board peripherals |

**On a terminal / shell (native, tokio).** The handler is the body of a CLI command. `main()`
reads argv, constructs the native `Environment` (real stdout/stderr/stdin, real FS, `reqwest`),
runs the handler to completion on a tokio runtime, and maps the final `IlcResult` to a process
exit code. Shell piping/redirection just works because `info`→stdout and `error`→stderr;
`readLine` reads real stdin (or `none` at EOF). This is the reference host.

**In a browser (WASM SPA).** A `#[wasm_bindgen(start)]` entry builds the browser `Environment`
and mounts the UI (Leptos/Yew/Dioxus — ILC stays UI-agnostic). The *same* handler source runs,
driven by `wasm-bindgen-futures` on the browser event loop — **no tokio**, and its `!Send`
futures are why the contract carries no `Send` bound. `ConsoleIo` writes to the devtools console
with `prompt()` for input; `Network` is `fetch` (CORS-bound); `FileSystem` is OPFS or an
in-memory `memory://` store, else `unavailable`. The mapping is identical to the TS browser
host — a built-in parity check across two source languages.

**On bootup in an embedded system (host mode, Embassy).** There is no OS — **the board itself
is the host.** `#[embassy_executor::main]` runs at boot, initializes peripherals (UART, flash,
optional network stack), and constructs a `BootEnvironment` binding `ConsoleIo`→UART/RTT,
`FileSystem`→on-chip flash (or `unavailable`), `Network`→`embassy-net` (or `unavailable`). The
handler — identical source to the CLI and browser builds — runs as an Embassy task; the executor
puts the core to sleep between `.await`s. Capabilities the board lacks surface as the
`unavailable` variant rather than a missing API, so the same handler degrades gracefully instead
of failing to link. "Host mode" is literal: the firmware boot path *is* the `Environment`
constructor, exactly as `main()` or the WASM start function is on the other two runtimes.

---

## Invocation & CLI binding (2026-05-30)

**Principle: ILC is invocation-agnostic — it binds to each runtime's preferred dispatch
front-end rather than supplanting it.** ILC defines only the handler + `Environment` boundary;
*how* a handler gets invoked is the runtime's concern: a CLI library on the terminal, UI events
in the browser, the boot/task scheduler on embedded. "Bind, don't supplant" is the entry-point
philosophy across all three runtimes — the CLI case is just its most concrete instance.

On the terminal specifically: using **Typer** (Python), **clap** (Rust), or
**commander/oclif** (TS) already *means* "this is a CLI." ILC does not re-implement argv parsing,
subcommands, help, or completion — those libraries do it well. ILC supplies a thin **shim** that
bridges the CLI library to an ILC handler + `Environment`.

**Three orthogonal layers:**

| Layer | Owner | Responsibility |
| --- | --- | --- |
| Dispatch / argv / subcommands / help | the CLI library (Typer / clap / commander) | parse argv, route to a command, produce typed input |
| Capability injection (`Environment`) | **ILC** | provide `ConsoleIo` / `FileSystem` / `Network` via the native host |
| Operation I/O contract (optional) | OpenBindings, or the CLI lib's typed args | the shape of a command's input/output |

The shim is thin: **parse (CLI lib) → build native `Environment` (host) → call
`handler(env, input)` → map `IlcResult` to exit code / CLI error.** It sits *above* the host
(capability provider) and is the entry-point wiring — now delegated to a real CLI framework
instead of a hand-rolled `main()`.

**Per-language binding — a *separate, optional* adapter package, kept out of ILC core so
WASM/embedded builds never pull CLI deps:**

| Language | Preferred CLI lib | Shim shape (sketch) | Adapter package |
| --- | --- | --- | --- |
| Python | **Typer** (Click under it) | `Depends`-style / decorator injection of `env` into a Typer command | `ilc-py-typer` |
| Rust | **clap** (derive) | `ilc_clap::run(handler)` builds the native `Environment`, passes the derived args struct as `input` | `ilc-rs-clap` (feature `clap`) |
| TypeScript | **Optique** (type-safe parser combinators) | parser → typed args → shim maps to proto `TInput` → handler. Optique is *purely a parser* (no execution/scaffolding) = the cleanest "bind, don't supplant" fit; commander/oclif also work | `ilc-ts-optique` |

These are binding *examples*; users can write their own for argparse / yargs / etc. ILC only
requires the shim end up calling `handler(env, input)`.

**The BFT pilot bootstraps these libraries (2026-05-30).** `ilc-rs-clap` / `ilc-py-typer` /
`ilc-ts-optique` are *born* from the pilot — the first concrete coupling of ILC to each CLI platform,
not throwaway glue. Disciplines:

- **Concrete-first, extract-after.** Write BFT's binding inline per platform, get it working across
  runtimes, *then* lift the reusable shim out. Keep the libraries **in-workspace / unpublished** until
  a **second** consumer validates the shape (per the "premature triple-SDK publication" concern).
- **One common contract, only step 1 differs:** `(1) parse argv` [platform-specific: clap / Typer /
  Optique] → `(2) map parsed → TInput` [thin per-platform adapter] → `(3) build Environment` →
  `(4) invoke the generated harness` (decode→handle→encode, route by `target`) → `(5) map IlcResult /
  CapabilityError → exit code`. Steps 3–5 are **shared/generated**; the binding library is essentially
  step 1 + the step-2 adapter + exit-code conventions. Resist leaking platform specifics into 3–5.

**Resolves open Question 2 (CLI ownership):** ILC owns *only* handler + `Environment`; the CLI
library owns argv/subcommands; a per-language shim bridges them. It also reframes the "handler
registration / dispatch" risk — **registration = registering your handler as a command in your
CLI lib of choice**, not a bespoke ILC mechanism.

**Input-schema ownership — resolved (2026-05-30):** `TInput`/`TOutput` are **WIT/OBI-defined and
serializable, always** (not CLI-native). See *Handler I/O & serialization* below.

---

## Handler I/O & serialization (2026-05-30; revised to protobuf)

**Two IDLs, split by concern (resolved): WIT owns capability *interfaces*; protobuf owns
*message types*.** They meet at the handler signature:
`handle(env /*WIT*/, input: TInput /*proto*/) -> Result<TOutput, IlcError> /*proto*/`. This
**drops OBI / JSON-Schema for handler I/O** — two sources of truth, each doing what it does best,
instead of three. (If OBI returns at Phase 5, it maps operations onto these proto messages; it no
longer types I/O.)

Rationale (supersedes the earlier JSON+CBOR+serde sketch): cross-runtime I/O must cross a wire —
**serial** (embedded↔host over UART/USB) and **network** (browser/distributed). Protobuf is
embedded-viable, compact, has mature polyglot codegen, and gives **field-number schema evolution**
(forward/backward compat) that JSON/CBOR don't.

**Message layering — three layers** (start decomposed: collapsing layers later is mechanical;
splitting a live wire format apart is breaking):

| Layer | `.proto` | Carries |
| --- | --- | --- |
| **Common / domain** | `ilc/common.proto` | shared value types + **`IlcError`** (the error taxonomy as a `oneof`); imported everywhere |
| **Payload** | `<handler>.proto` | per-handler `TInput` / `TOutput`, importing common |
| **Envelope / frame** | `ilc/envelope.proto` | `message_id`, `target` (handler route/address), `content_type`, `payload`; responses carry `TOutput` or `IlcError`. Transport-agnostic — same frame over serial / network / in-process |

The **envelope absorbs remote-dispatch routing** ("which handler does this frame target?" → the
`target` field) — the question parked last turn, now part of the message layer, not a bespoke
mechanism.

**Codegen / dependencies (protobuf, not serde):**

- **Rust native (std):** `prost` (+ `prost-build`/`buf` at build time).
- **Rust embedded (`no_std`):** `micropb` / `femtopb` (no-alloc, fixed-capacity) — keeps the
  embedded target alive. The `no-alloc string-payload` open item still applies (proto strings →
  fixed-capacity on embedded).
- **TS:** `ts-proto` (or protobuf-es). **Python:** `protobuf` / `betterproto`.
- **Protobuf is the encoding only — NOT gRPC.** Transports stay ILC's (serial frames, `fetch`,
  in-process). gRPC/tonic pulls tokio and runs on neither bare-metal nor in-browser-sans-grpc-web.

**WIT→proto mismatch to manage:** proto3 has `oneof` (no repeated/presence niceties), open
int-backed enums, zero-vs-unset ambiguity — weaker than WIT's `variant`/`option`/`result`. Since
proto owns messages, message shapes are constrained to proto's expressiveness (acceptable — it
forces wire-friendly shapes); the **`IlcError` taxonomy moves from a WIT `variant` to a proto
`oneof`** in `common.proto`.

**CLI shim stays a mapping layer:** clap/Typer parse structs → generated proto `TInput`; generated
types carry no clap/Typer attributes. Flow: *parse (CLI) → map to `TInput` → build `Environment` →
handler → encode result envelope.*

**Local and remote invocation unify:** `TInput` is always a proto message, so a handler is driven
identically by argv (CLI maps args → `TInput`) or a wire frame (transport decodes envelope →
`TInput`). CLI shim and serial-frame dispatcher are the same pattern.

**New test dimension:** golden tests add **cross-language round-trip parity** — a message encoded
by one language's protobuf codegen decodes identically in the others, and bytes match the wire
format. Type-shape snapshots no longer suffice.

### Validation, JSON & the parse/build model (2026-05-30)

**JSON ⇄ protobuf is free via the proto3 *canonical JSON mapping*** — no second schema. Every message
has a defined JSON form; codegen emits both:
- **Rust:** `prost` (binary) + **`pbjson`** (serde impls following canonical JSON); or **`buffa`**
  (all-in-one: proto + canonical JSON + zero-copy + WKTs).
- **TS:** `ts-proto` / protobuf-es `toJSON`/`fromJSON`. **Python:** `google.protobuf.json_format`.

JSON = **debug/inspection face**; binary = **wire** (esp. embedded). The envelope's `content_type`
selects which — same messages, same validation, either encoding. (Concretizes "encoding is
transport-chosen"; does *not* reintroduce JSON as a rival schema — it's a projection of the proto one.)

**Zod-like validation = `protovalidate`, sourced from proto (not a parallel schema lib).** Zod
conflates schema-definition + validation + type-inference; proto already owns schema + types, so take
only the *validation* half — from the single source, enforced identically everywhere. `protovalidate`
(buf, v1.0): constraint annotations (`buf.validate`) live **in the `.proto`**, CEL for custom rules,
identical across languages.
- Official: Go/Java/Python/C++/TS. **Rust matured (re-eval 2026-05-30):** a Rust impl now reports
  **full conformance** (2872/2872) vs the upstream harness. Two strategies: **static codegen**
  (`protocheck`, **`protovalidate-buffa`** — no reflection, embedded-friendlier) vs **runtime
  reflection** (`prost-protovalidate`, full CEL, std-only). `buffa` + `protovalidate-buffa` is a
  cohesive proto+JSON+validation stack (vs prost+pbjson+protocheck stitched from 3 authors); trade-off
  is buffa's newness vs prost's ubiquity.
- **Behind a `Validate` trait (ILC-owned), swappable.** Default leans **generate the *standard*
  constraints ourselves** (required/len/range/regex/enum — mechanical from the FileDescriptorSet, no
  third-party dep, `no_std`/no-alloc safe = the embedded-shared subset); reserve a third-party
  reflection impl (std-only) for **custom CEL**. High appetite to own this layer.
- **Do NOT** put contract validation in a standalone Rust lib (`garde`/`validator`) — re-creates a
  second source of truth, breaks parity. (Fine for purely-local, non-wire checks.)

**Parse/build model (LOCKED):** a uniform handler-boundary pipeline —
`decode (binary|JSON) → validate → handle(env, input) → validate → encode (binary|JSON)`. **"Parse,
don't validate":** the handler receives an *already-validated* typed value; violations return as
structured errors. Construction: prost structs + an ergonomic builder (`bon`/`typed-builder`) on Rust.
Delivers the Zod *experience* (define once → parse untrusted input → typed/validated value or rich
errors), sourced from proto.

**Embedded caveats:** the official CEL engine is interpreter + alloc — likely not bare-metal.
Mitigations: (a) **codegen'd** validators (protocheck-style → plain checks, friendlier than runtime
CEL); (b) restrict *embedded-shared* rules to standard constraints (required/len/range/format), reserve
custom CEL for std-side; (c) validate at the host/gateway fronting the device. JSON is std/debug-side;
embedded wire stays binary.

---

## Compiler architecture — dual front-end (2026-05-30)

**Issue that kept this open:** the fear of one unified IR ingesting *both* schemas — forcing ILC
to build/own a protobuf front-end *and* reconcile two type systems (WIT `variant`/`option`/`result`
⟷ proto `oneof`/`optional`) with cross-references in generated code. Large compiler surface,
permanent maintenance tax.

**Resolution (per 2026-05-30 steer — "apps need proto, not WIT awareness"):** WIT and proto have
different audiences and lifecycles → keep **two independent front-ends that never reference each
other's types.**

| | Schema | Audience | Authored by | Toolchain |
| --- | --- | --- | --- | --- |
| **Capabilities** (interfaces) | **WIT** | invisible (seen only as generated `env.*`) | ILC maintainers; stable, small | `ilc compile` (WIT→IR→TS/Py/Rust) — ILC-owned, the parity engine |
| **Messages + handler signatures** | **protobuf** | the app surface | app devs | off-the-shelf `buf`/`protoc` (+ prost/ts-proto/python) — ILC does **not** build this |

- **App devs live in proto + handler bodies; never touch WIT.** WIT is framework plumbing; the only
  WIT they feel is the generated idiomatic `env.*` capability API.
- **No type-system merge.** Capability boundaries pass app data as **opaque `list<u8>`** (the `Store`
  pattern, generalized) — a WIT interface never names a proto type, nor vice-versa. They meet *only*
  at the handler signature.
- **Delegate proto to the ecosystem** — app devs get `buf` lint + breaking-change detection + every
  language's codegen for free.

**The seam — generated handler harness:** ILC reads the proto **FileDescriptorSet** (from `buf`) and,
per proto **`service` method** (a *declaration only — not gRPC*), generates a thin per-language
harness: `decode(envelope.payload) → TInput → inject Environment → call handler → encode TOutput`.
The `service` method name *is* the envelope `target` route. The **only** cross-schema coupling, at
the descriptor level (names + types), not a type merge.

```
wit/      ──ilc compile──▶  capability SDK (Environment, traits, capability-error)   ┐
proto/    ──buf generate─▶  message types (TInput/TOutput, common, envelope, store)  ├─▶ handler harness (generated)
proto svc ──ilc harness──▶  handler ↔ (input, output, target) wiring                 ┘
```

`ilc build` orchestrates both toolchains (shells out to `buf`/`protoc`) so app devs run one command.
**Two pipelines, not one IR.**

**`buf` config (LOCKED 2026-05-30; local-only, no BSR):**

```yaml
# buf.yaml
version: v2
modules: [{ path: proto }]
deps: [buf.build/bufbuild/protovalidate]      # validation annotations
lint: { use: [STANDARD] }
breaking: { use: [WIRE_JSON] }                # both binary + canonical-JSON compatibility
```
```yaml
# buf.gen.yaml
version: v2
plugins:
  - { local: protoc-gen-prost,       out: gen/rust }   # Rust binary
  - { local: protoc-gen-prost-serde, out: gen/rust }   # + canonical JSON (pbjson-style)
  - { local: protoc-gen-es,          out: gen/ts }     # TypeScript
  - { local: protoc-gen-python,      out: gen/py }     # Python
# ILC harness input:  buf build -o ilc-image.binpb   (FileDescriptorSet incl. service descriptors)
```

**Sub-decisions:**

1. **Capability-error vs application-error domains — LOCKED (2026-05-30).** Two error domains:
   - **Capability error (WIT-native):** capability methods return `result<T, capability-error>` — the
     taxonomy (`unavailable`/`permission-denied`/`not-found`/`io`/`timeout`/…), the local host ABI.
   - **Application error (proto):** handlers return `Result<TOutput, AppError>` where `AppError` is a
     proto type. It **may embed a proto `CapabilityError`** (the wire mirror of the WIT taxonomy) so a
     *propagated* infrastructure failure can cross the wire; otherwise it carries the app's domain errors.
   - **Bridge:** the harness/transport maps WIT `capability-error` → proto `CapabilityError` **only when
     encoding a result that leaves the runtime**. In-process the handler consumes the WIT error directly —
     no proto mirror needed. The two front-ends stay independent locally; the wire is the only crossover.
2. **`service`-as-manifest — LOCKED (2026-05-30).** Handlers are declared as proto `service` blocks
   (no separate `ilc.toml`); the FileDescriptorSet drives harness + client-stub codegen. Proto is
   **hand-authored**, never generated from handler source (that would make a host language the schema
   owner). The wire **route is the `service` method name**, not the implementation's function name
   (which varies by language naming convention).

### Binding & registration (2026-05-30)

Two halves that compose — codegen gives types/routes, registration gives the live wiring:

- **Compile-time (codegen):** proto `service` → per-language typed handler **trait** + decode/encode
  harness + **route constants** + **client stubs** (so a caller in language A can invoke a handler in
  language B — protobuf carries the args across the barrier).
- **Runtime (explicit early registration):** at the **entry point**, alongside `Environment`
  construction, the app instantiates handlers (with their deps) and **registers** them into a
  dispatcher. The dispatcher routes `envelope.target` → handler → decode → `handle(env, input)` →
  encode. A frame to an unregistered route returns the `not-found`/`unavailable` capability error.

Why registration (beyond static codegen): (a) **dependency injection** — handlers instantiated with
runtime-constructed deps + the `Environment`; (b) **which subset is live** — proto declares all
possible handlers, registration declares those running in *this* process (embedded registers a few; a
server registers all); (c) **middleware seam** — logging/auth/the WIT→proto capability-error mapping
wrap handlers here.

- **Explicit, not magic (Concern #3):** the entry point explicitly lists what it registers — no
  decorator auto-registration or side-effect-import discovery (breaks tree-shaking + explicit graphs).
- **Tiered (registration is a harness concern, never the contract):** std/alloc (native, browser-WASM)
  → dynamic registry (`map<route, handler>`, `dyn`/`Box` fine); `no_std` no-alloc (embedded) → a
  generated **static dispatch table** (`match target { … }` / `&'static [(route, fn)]`), no alloc/`dyn`.

**Resolves the deferred "registration / dispatch" item:** declared in proto `service` → typed by
codegen → explicitly registered at the entry point → routed by `envelope.target`. Lands with
remote/multi-handler invocation (**Phase 4+**); Phases 1–3 don't need it.

---

## Distributed state (Phase 4+)

Once runtimes don't share memory, "shared state" is its own design problem — protobuf only
carries the bytes; it doesn't decide *who owns truth* or *how replicas converge*.

**Decision (2026-05-30): model state-sharing as a `Store` capability** in the `Environment`, so the
*coordination strategy is host-chosen* — the same shape as executor and codec. Written once against
`env.store.*`; the host picks RPC vs CRDT vs shadow. Underneath several models sits **logical time**
(Lamport / vector / **hybrid logical clocks**) — no global clock exists and embedded often has no
RTC — which surfaces in the sync messages as a hybrid timestamp. See the locked sketch below.

### Coordination models (choose per state-class, not globally)

| Model | Ships | Consistency | Best for | ILC fit |
| --- | --- | --- | --- | --- |
| Request/response RPC | queries | single-owner | always-connected, one authority | the envelope today; baseline |
| Event log / sourcing | commands | eventual | replayable/auditable | strong — handlers already *are* command processors; needs log compaction on embedded |
| **CRDT** | ops/deltas | eventual, multi-master | offline-first, concurrent edits, no coordinator | the "share state without shared memory" answer; tier by runtime |
| Device shadow / twin | desired+reported deltas | eventual | embedded ↔ host, intermittent link | best embedded↔cloud pattern |
| Pub/sub + retained | state changes | eventual | one-to-many telemetry, decoupling | MQTT is MCU-native; retained msg = last-known-state |
| Snapshot / delta repl. | state | last-wins | low-stakes telemetry | simplest that works |
| Strong consensus (Raft) | log entries | strong | multiple reliable peers | **ruled out** — infeasible on sleepy MCU / lossy serial |

### Open source to leverage (researched 2026-05-30)

| Project | License | What | Fit / caveat |
| --- | --- | --- | --- |
| **crdt-kit** | MIT/Apache-2.0 | `no_std`+alloc CRDTs (11 types), delta-sync, **built-in HLC**, optional serde + wasm; bare-metal/ESP32/RPi | **Linchpin: one lib spans all three Rust runtimes (native + embedded + WASM).** Caveats: pre-1.0 (v0.5.1); **Rust-only** (browser via its wasm feature; Python would need wire-compat or `uniffi`). |
| **Automerge 2.0** | MIT | Rust-core CRDT, JSON model, compact format + **sync protocol**, bindings JS/Python/Swift/C | Production-ready, **polyglot** — best browser↔server. **Not embedded** (metadata too heavy for MCU). |
| **Loro** | MIT | High-perf Rust CRDT, WASM/Swift | Promising but **experimental; authors advise against production**. Watch. |
| **yrs / Yjs** | MIT | Mature CRDT (esp. sequence/text), Rust + JS | If collaborative text/list editing becomes a use case. |
| **cr-sqlite** | MIT/Apache | SQLite extension: CRDT multi-writer replication | If state is relational / persisted rather than in-memory. |
| **Eclipse Ditto** | EPL-2.0 | Digital-twin framework: reported/desired/current, MQTT/AMQP/Kafka, HTTP/WS | Server-side twin option, but **heavy (JVM microservices)** — likely roll a lightweight shadow over MQTT instead. |
| **MQTT** (Mosquitto / `rumqtt`) | EPL / Apache | Pub/sub transport with retained messages | The pub/sub + "last-known-state on connect" transport; MCU-native. |
| **uniffi-rs** | MPL-2.0 | Rust→multi-language bindings generator | Bridge if a Rust `Store`/crdt-kit core must expose Python/Swift/Kotlin. |

### Recommended starting bet

- **`Store` capability in WIT**, proto-carried deltas/ops, host-chosen strategy per runtime.
- **crdt-kit as the Rust CRDT engine** across native/embedded/WASM, **behind an ILC-owned `Replica`
  trait** — clean license (vs `id_effect`), HLC included, *one* lib for all three runtimes (mixed
  engines can't sync). Re-eval 2026-05-30: still the only no_std+embedded+delta+HLC option; closest
  peer **crdt-lite** has a production user (Formabble) but requires alloc + is narrower; **Automerge**
  is production-grade but std-only. **Low appetite to hand-roll** — CRDT merge/causality is exactly
  what you don't want to own. Swap path via the trait: Automerge (std-only deploys), or a couple of
  minimal own CRDTs (PN-counter, LWW-register) only on bare-metal-no-alloc. Pin the version.
- **Automerge** where polyglot / rich-doc browser↔server collaboration is wanted (heavier, not embedded).
- **MQTT + retained** or a **lightweight device-shadow** for embedded↔host telemetry/config.
- **Don't force one model:** telemetry → snapshot/last-wins; config → shadow; collaborative → CRDT.

### `Store` capability — locked sketch (Phase 4+)

**WIT (interface only; all values/deltas/version-vectors are opaque `list<u8>`):**

```wit
// wit/store.wit — kebab-case source; emitted camelCase (env.store(), stateVector, deltaSince)
interface store {
  use ilc-result.{ilc-error};

  // current materialized value; none = absent
  get: func(key: string) -> result<option<list<u8>>, ilc-error>;

  // local write — host stamps a hybrid timestamp; valid for LWW/register-shaped keys
  set: func(key: string, value: list<u8>) -> result<_, ilc-error>;

  // apply a remote CRDT delta (crdt-kit delta bytes)
  merge: func(key: string, delta: list<u8>) -> result<_, ilc-error>;

  // efficient sync (Automerge/Yjs-style): exchange version vectors, compute the missing delta
  state-vector: func(key: string) -> result<list<u8>, ilc-error>;
  delta-since: func(key: string, peer-vector: list<u8>) -> result<list<u8>, ilc-error>;
}
// environment.wit gains: store: func() -> store;  →  emitted env.store()
```

All funcs are `async`, `Send`-agnostic, `no_std`-able (`list<u8>` → `Vec`/`heapless`). **Watch /
subscribe is deferred** — cross-runtime streaming is its own design. The per-key CRDT *kind* is
host/registration config, not in the hot-path interface.

**Protobuf (the bytes the WIT layer carries — a domain layer beside `common`):**

```proto
// ilc/store.proto
syntax = "proto3";
package ilc.store;

message HybridTimestamp { uint64 wall_ms = 1; uint32 counter = 2; uint64 node_id = 3; } // crdt-kit-style u64 node id
message VersionEntry   { uint64 node_id = 1; HybridTimestamp ts = 2; }
message StateVector    { string key = 1; repeated VersionEntry entries = 2; }

enum CrdtKind { LWW_REGISTER = 0; PN_COUNTER = 1; OR_SET = 2; LWW_MAP = 3; RGA = 4; } // ⊆ crdt-kit's 11 types

message Delta { string key = 1; CrdtKind kind = 2; HybridTimestamp ts = 3; bytes payload = 4; } // payload = crdt-kit delta bytes
message DeltaBatch { repeated Delta deltas = 1; }

message SyncFrame {                 // rides as the `payload` of envelope.proto; envelope.target routes to the store subsystem
  oneof body { StateVector request = 1; Delta delta = 2; DeltaBatch batch = 3; }
}
```

**How it composes:** `state-vector` → peer sends its `StateVector` in a `SyncFrame.request` →
other side computes the missing `Delta`/`DeltaBatch` via crdt-kit's `delta-since` → returned in a
`SyncFrame`, applied via `merge`. The transport `envelope` (serial / network / in-process) carries
`SyncFrame` as its payload; `HybridTimestamp` is the logical clock. crdt-kit provides the engine on
native/embedded/WASM; the encodings above are its on-the-wire form.

**Handler discipline:** eventual consistency means handlers must tolerate stale/conflicting reads
and write merge-friendly ops — an authoring rule, not just a host concern.

---

## Feasibility review

### What is strong

| Area | Assessment |
| --- | --- |
| **Problem framing** | Inversion of control for I/O and runtime deps is well understood; matches patterns already described in `docs/sw-principles` (commands, `CliContext`, adapters). |
| **V1 capability set** | `ConsoleIo`, `FileSystem` (text only), `Network` (`fetch`) is small enough to implement and mock. |
| **`IlcResult` parity** | Explicit result types for fallible capabilities (`FileSystem`, `Network`); `ConsoleIo` output stays void in Phase 1. |
| **Test harness direction** | In-memory FS + captured log lines + queued HTTP mocks is standard and implementable. |
| **WIT as schema-only** | Using WIT purely as IDL (via `wit-parser`) without targeting WASM is technically sound; avoids shipping a WASM runtime. |

### What is risky or underspecified

| Area | Risk | Notes |
| --- | --- | --- |
| **Handler registration / dispatch** | High | The vision describes “each function registers itself” but V1 spec only defines `Environment` injection—not routing, argv parsing, subcommands, or discovery. Without this, ILC is interfaces only, not a CLI framework. |
| **Three-language codegen day one** | High | Template maintenance for TS + Python + Rust triples cost; drift between emitters is likely without golden tests per language. |
| **Async `fetch` in WIT** | Medium | WIT and Rust want explicit async; Python/TS ergonomics differ. V1 says buffered bodies—good—but async model must be chosen early (sync-only V1 vs. async everywhere). |
| **Stdin in browser** | Low | Browser/DevTools hosts use `window.prompt()` for `readLine`; cancel/dismiss → `none`. Node uses real stdin; tests use a queued line buffer. |
| **Browser FileSystem** | Medium | OPFS / download / in-memory paths need a **path convention** (virtual roots, `memory://`, etc.). |
| **Python “browser runtime”** | Low utility | Pyjamas is obsolete; realistic browser Python is Pyodide/WASM—not ILC’s sweet spot. Defer or drop from V1 targets. |
| **Publishing** | Medium | NPM + PyPI + Crates.io implies versioning, CI, breaking-change policy before any consumer exists. |
| **OpenBindings overlap** | Low–medium | Complementary layers (see below), but teams may confuse “operation contract” (OBI) with “capability contract” (ILC). |

> **Reconciled (2026-05-30):** several of these are now resolved — **Handler registration/dispatch**
> is designed (proto `service` → codegen → explicit registration → `envelope.target`; see *Binding &
> registration*); **Three-language codegen day one** is reframed as **Rust-lead** with TS/Python in
> golden-test lockstep; **Async `fetch`** → async-first locked; **Browser FileSystem** path scheme
> resolved (mount table); **OpenBindings overlap** → OBI no longer types I/O (protobuf does). Risk that's *new*:
> the third-party Rust deps we lean on (crdt-kit, protocheck) are pre-1.0.

### Feasibility by component

> **Superseded (2026-05-30)** by the reordered *Implementation plan* below — phases are now Rust-lead
> and "ILC as CLI framework" is resolved (bind to clap/Typer/commander, don't build one). Kept for
> historical context.

```
Phase 1 (ConsoleIo WIT + emit ×3) → Feasible in Rust; 2–4 weeks for ConsoleIo + minimal Environment
Phase 2 (expand WIT + emit)       → FileSystem, Network; expand Environment bundle
Phase 3 (hosts ×3)                → Node / browser / DevTools / test hosts wire ConsoleIo sinks
Phase 4 (TestEnvironment ×3)  → After NativeHost; network queue is the heavy piece
“ILC as CLI framework”          → Not specified; add 2–6 weeks once routing model is chosen
```

---

## Utility review

### Why this is worth doing (for devalbo)

1. **Lab tools already straddle runtimes.** BFT has browser UI (`bft-codec.ts`) and documented Node CLI usage (`BFT-DECODING.md`); logic duplication and divergent error handling are likely without a shared boundary.
2. **TODO mentions `devalbo-cli`.** ILC could be the **implementation layer** under a thin argv/MCP/OpenBindings shell—if that repo is still planned.
3. **Principles doc already argues for commands + context.** ILC names and formalizes what `PRINCIPLES_AND_GOALS.md` describes; it reduces ad-hoc `fs` / `fetch` in handlers.
4. **Agent/test ergonomics.** Bounded `Environment` makes “run this handler with fake FS and one mock HTTP response” trivial—relevant for coding agents and Vitest.

### When utility is low

- **Single-runtime, single-entry-point tools** (e.g. a React-only lab page with no CLI) gain little from WIT/codegen; manual TypeScript interfaces + `TestEnvironment` suffice.
- **Heavy OpenBindings-first services** may only need ILC at the **handler body** inside an `ob op exec` binding—not a new IDL stack.
- **Premature triple-SDK publication** burns time before a second consumer validates the abstractions.

### Suggested pilot (prove utility before scaling)

| Candidate | Why |
| --- | --- |
| **BFT decode/encode core** | Already dual-target (browser + Node); pure tree transforms; FS is “read .bft / write tree” in CLI and upload/download in UI. |
| **`devalbo-cli` (external repo)** | Natural CLI host; ILC avoids growing another ad-hoc context type. |
| **New lab command with 1 HTTP + 1 file** | Smallest slice to validate `TestEnvironment` queue mocks. |

**Selected (2026-05-30): BFT decode/encode core** — currently duplicated `bft.js` / `bft.py`, a real cross-language-duplication forcing function; binary + pure transform + already multi-target. See *Phase 4*. (Phase 1 did not require a pilot — only a cross-language “hello `ConsoleIo`” sample.)

---

## Phase 1 decisions (locked)

| Decision | Choice |
| --- | --- |
| **Repo home** | [`devalbo/devalbo-ilc`](https://github.com/devalbo/devalbo-ilc) — local path `/Users/ajb/Projects/devalbo-ilc` |
| **WIT layout** | Split files, e.g. `wit/console-io.wit`, `wit/environment.wit` (not a monolithic `core.wit` yet) |
| **Capability name** | **`ConsoleIo`** — console I/O without implying POSIX streams in handler code |
| **Output semantics** | **Logger-shaped**, void side-effects: `info`, `error` (not `write_out` / `write_err`) |
| **Input** | **Keep stdin**: `readLine` on `ConsoleIo` (WIT: `read-line`) |
| **Returns (Phase 1)** | Output: **unit/void**. Stdin: WIT `read-line() -> option<string>`; emitted `readLine()` (`none` = EOF) |
| **Naming** | WIT kebab-case in `.wit`; emitters project **camelCase** (`ConsoleIo`, `consoleIo`, `readLine`) |
| **Who picks bindings** | **Hosts only** — handlers use `env.consoleIo.*`; never `console.log` / `process.stdout` |
| **Host output mapping** | Node: `info` → stdout, `error` → stderr; browser/DevTools: `info` → `console.log`, `error` → `console.error`; test: in-memory log buffer |
| **Browser stdin** | **`readLine` → `window.prompt()`**; user cancel or dismiss → `none`; non-empty input → `some(line)` |
| **Serverless invocation** | Treat serverless (e.g. AWS Lambda) as another **host**. `info`/`error` map to the platform logger (`console.log` / `console.error` in Node Lambda; language equivalents elsewhere). `readLine` should generally return `none` (no interactive stdin). |
| **Callbacks** | Internal SDK mechanism (`consoleIoFromCallbacks`) — not exposed to handler authors |
| **Emitter layout** | **Separate package/crate per language** (unpublished initially is fine) |
| **TS runtimes (Phase 1 demo)** | Node CLI, browser app, DevTools REPL — three **host factories**, one generated interface |

### `ConsoleIo` vs `window.console`

Generated **`ConsoleIo`** is the ILC contract. **`window.console`** is only what a **browser host** calls under the hood. Handler code says `env.consoleIo.info("…")`, not `console.log`.

---

## Relationship to OpenBindings and existing patterns

> **Reconciled (2026-05-30):** ILC I/O is now typed by **protobuf**, not OBI/JSON-Schema (see *Handler
> I/O & serialization*). OBI is no longer the I/O-typing layer; if integrated at all (Phase 5), an OBI
> operation maps onto ILC proto messages. The complementary-layers framing below still holds at the
> *operation-vs-capability* level.

| Layer | OpenBindings | DEVALBO-ILC |
| --- | --- | --- |
| **Unit of contract** | Operation (`tasks.create`) | Capability (`FileSystem.read_bytes`) interface (**WIT**) |
| **I/O typing** | ~~JSON Schema~~ → maps onto ILC **protobuf** messages | **protobuf** messages (`TInput`/`TOutput`) |
| **Transport** | Bindings (REST, CLI, MCP, gRPC, …) | Host adapter + the proto envelope (serial / network / in-process) |
| **Audience** | Service boundary, clients, CI compatibility | In-process handler implementation |
| **Complement** | “What does this service expose?” | “What does this handler need to run?” |

**Integration sketch:** An OBI operation’s binding executor constructs an `Environment`, calls the ILC-registered handler, maps the result (proto `TOutput` / `CapabilityError`) to operation output/errors. ILC does **not** replace OpenAPI/MCP; it **normalizes intakes** inside the app, as DEVALBO-ILC.md suggests.

**Overlap with sw-principles:** Conceptually aligned with command unification and `CliContext`. Before building parallel abstractions, **audit** whether ILC *is* the renamed `CliContext` or a lower layer beneath it.

### ILC ↔ `CliContext` reconciliation (2026-05-30)

Audited the sibling repos (local). `CliContext` is a **principle** in `PRINCIPLES_AND_GOALS.md`, not a
shipped type — but two concrete realizations exist: `tb-solid-pod` `CliContext` and `devalbo-cli`
`CommandRuntimeContext`. **Two divergent context shapes across two repos is itself the "parallel
abstractions" risk the principle warned about — ILC's `Environment` unifies them** (a concrete utility
argument for ILC).

De-facto field → ILC home:

| Context field (tb-pod / devalbo-cli) | ILC home |
| --- | --- |
| `addOutput` / `clearOutput` | **`ConsoleIo`** (clearOutput = shell sugar) |
| `store` (TinyBase / `DevalboStore`) | **`Store`** capability |
| `driver` (`IFilesystemDriver`) | **`FileSystem`** capability *(already an adapter iface)* |
| `connectivity` (`IConnectivityService`) | **`Network`** capability *(already an adapter iface)* |
| `pod.handleRequest` | `FileSystem`/`Network` (HTTP-shaped resource API) |
| `cwd`/`setCwd`, `currentUrl`, `baseUrl` | `FileSystem` path resolution + app config |
| `session` / `config` | registration-injected deps / config |
| `commands` registry | **ILC registration** (proto `service` + explicit registry) |
| `ParsedArgs` | CLI lib → proto `TInput` |
| `CommandResult<T>` + `ErrorCode` | `Result<TOutput, AppError>` + **capability-error taxonomy** (≈1:1) |
| `validate` / `subcommands` | `Validate` step / CLI-lib routing |
| **residual:** `setBusy`, `clearScreen`, `createProgram`, Ink/React output rendering, `exit?` | **terminal-host shell** (presentation/process control) — *not* the contract |

**Conclusion:** the IO/state/capability half of both contexts maps onto the ILC `Environment`
(`ConsoleIo`+`FileSystem`+`Network`+`Store`); the only residual is terminal/UI presentation state, which
rightly stays in the terminal host. **No new ILC primitive needed** — and since `devalbo-cli` already
injects FS/Network as interfaces and its `ErrorCode` ≈ the ILC taxonomy, migration is **re-homing, not
rewriting**.

**Migration (adapter-first):** make the existing context *compose* an ILC `Environment` (store→`Store`,
driver→`FileSystem`, connectivity→`Network`, addOutput→`ConsoleIo`); keep presentation state in the
Ink/React shell; route args via the CLI shim → proto `TInput`. The planned `useCliContext.ts` should
wrap an `Environment`, not stand as a parallel interface.

**Pending:** doc edits in `site-devalbo.com` (sw-principles note + lab plan mirror at
`docs/lab/DEVALBO-ILC-PLAN.md`) — awaiting OK.

---

## Harvested patterns & references (2026-05-30)

Auditing the sibling repos surfaced **working TS implementations of patterns ILC formalizes** — design
anchors and migration targets (paths below are in the local sibling repos `devalbo-cli` / `tb-solid-pod`).

| Pattern | Source | What ILC takes |
| --- | --- | --- |
| **FileSystem capability** | `devalbo-cli/packages/filesystem/src/interfaces.ts` — `IFilesystemDriver`: `readFile→Uint8Array`, `writeFile(_, Uint8Array)`, `readdir`, `stat`, `mkdir`, `rm`, `exists` (all async) | **Confirms binary-first `read_bytes`/`write_bytes`**; near-direct method set for the WIT `FileSystem`. |
| **One capability, many host adapters** | `devalbo-cli/packages/filesystem/src/drivers/{native,zenfs,tauri,memory,browser-store}.ts` | Proves the host model concretely → Rust hosts: `native`→tokio, `zenfs`/`browser-store`→browser-WASM, `memory`→test host. **`tauri`** flags a likely **4th ILC runtime (desktop)**. |
| **Runtime detection + capability introspection** | `devalbo-cli/packages/shared/src/types/environment.ts` — `RuntimePlatform` (NodeJS/Browser/Worker/Tauri) + `RuntimeEnv { hasSharedArrayBuffer, hasOPFS, hasFSWatch }` | Anchors **auto-detect default host (overridable)** + **introspectable Environment**; feature flags *are* the `unavailable` decision inputs. |
| **Network / connectivity capability** | same file — `IConnectivityService { isOnline(), onOnline(cb)→unsub }` + `AlwaysOnline…`/`Browser…` impls | Connectivity shape; `AlwaysOnline` = **null-object test host**; `onOnline(cb)→() => void` = the unsubscribe-closure idiom. |
| **Watch / subscribe** | `IWatcherService.watch(path, cb)→() => void`; `…/filesystem/src/watcher/{node,browser,polling}-watcher.ts` | Reference for the **deferred `Store` subscribe** + FS watch; **polling-watcher** is the constrained/embedded fallback (no native watch). |
| **Error taxonomy** | `tb-solid-pod/src/cli/types.ts` — `ErrorCode` enum (`INVALID_PATH`, `PATH_NOT_FOUND`, `PERMISSION_DENIED`, `NOT_SUPPORTED`, …) | ≈1:1 with the ILC `capability-error` taxonomy — adopt the variant set. |
| **Explicit registry + dispatch** | `devalbo-cli/packages/cli-shell/src/lib/command-runtime.ts` — `CommandRuntimeContext`, `buildCommandOptions(ctx)`, `executeCommand(name, args, ctx)` | Prior art for **explicit entry-point registration** + route→handler map + option projection. |
| **Branded types** | `devalbo-cli/packages/shared/src/types/branded.ts` + `sw-principles/BRANDED_TYPES.md` | Path/id typing discipline → ILC path & key types (feeds the virtual-path scheme). |

**Folded into the plan:** binary-first FileSystem (`Uint8Array`) is **validated** — keep `read_bytes`/`write_bytes`; a **Tauri/desktop runtime** is a natural 4th target (noted, not scoped); the **polling-watcher** is the embedded-friendly answer when `Store` subscribe lands; `RuntimeEnv` feature-flags are a concrete model for **capability introspection + `unavailable`**; adopt `tb-solid-pod`'s `ErrorCode` set as the `capability-error` variants.

---

## FileSystem — virtual path scheme (2026-05-30)

**Model: a mount table over a single POSIX-style namespace.** Handlers use plain absolute `/paths`
(backend-agnostic); the host binds path-prefix → backend at `Environment` construction. WASI-preopen-
aligned (keeps the WASM-future open), matches embedded VFS. *Foundation is mounting; simple apps never
see it; advanced apps can.*

- **Defaults (zero config):** each host auto-provides a sensible default mount, so a trivial handler
  just calls `env.fs.read("/foo")` — no mounts to declare. Native → app/cwd dir; browser →
  OPFS-or-`memory`; embedded → flash-or-`memory`. Behaves like a single bounded root for the simple case.
- **Introspection + advanced control (the "env object"):** the `FileSystem` capability exposes an
  **introspectable (and, host-permitting, mutable) mount table** — list mounts, resolve a path → its
  backend + availability, `mount`/`unmount`. The **typed escape hatch** for multi-backend / advanced
  apps (ties to the introspectable-`Environment` decision + harvested `RuntimeEnv` flags `hasOPFS`/
  `hasFSWatch`). Don't box out mounting — just keep it out of the simple path.
- **Resolution + validation (ported from `tb-solid-pod/src/cli/path.ts`):** one centralized resolver →
  a branded **`VirtualPath`** (parse-don't-validate). `..` **cannot escape its mount root** (→
  `invalid-input`/escape); trailing `/` = container, else file; segment rules: non-empty, no `/`, no
  control chars, max length (255 std / smaller on embedded). `no_std`-able (`&str`/`heapless`, no full
  URL parser).
- **Availability → errors:** path under no mount, or a backend the host lacks (no OPFS / no flash) →
  **`unavailable`**; malformed path → **`invalid-input`** (per the error taxonomy).
- **Embedded:** the mount table is a **static const table** (`&'static [(prefix, backend)]`, no alloc);
  dynamic `mount()` may return `unavailable`; segment caps via `heapless`. (Same static-vs-dynamic
  tiering as the handler registry.)

---

## Recommended V1 scope (phased)

> **Reconciled (2026-05-30):** Phase 1 (below) is **done** and accurate. "Later phases" and "Out of
> scope" are partly superseded — see the reordered **Implementation plan** and the dated decision
> sections. Key deltas: Phases 2+ are **Rust-lead**; message I/O is **protobuf**; a **`Store`**
> capability is added (Phase 4+); **registration/dispatch** is designed; Rust **does** compile to
> WASM for the browser SPA; and ILC **binds to** CLI libraries rather than building a CLI framework.

Adopt the spirit of DEVALBO-ILC.md V1, but **sequence** work so Phase 1 does not try to complete the whole core surface area.

### Phase 1 in scope (contract pipeline)

- **Languages:** TypeScript, Python, and Rust—all consuming generated **`ConsoleIo`** and minimal **`Environment`** (WIT `console-io()`; emitted `env.consoleIo()`).
- **IDL:** `wit/console-io.wit`, `wit/environment.wit`. **WIT is source of truth from day one.**
- **`ConsoleIo` in WIT:** `info(string)`, `error(string)`; `read-line() -> option<string>`.
- **Compiler:** Rust CLI + `wit-parser` → separate per-language packages; CI golden snapshots.
- **Basic FileSystem (now in V1, per review):** binary-capable `read_bytes` / `write_bytes`
  (text convenience on top); in-memory + Node + browser adapters. Needs a virtual-path scheme.
- **Async-first + IR-decoupled compiler (per review):** capabilities emit as async; the
  compiler parses WIT into an internal IR that the emitters target (IDL stays swappable).
- **Not required in Phase 1:** `Network`, formal host SDK crates, handler registration, pilot.
- **Samples:** Trivial handler using `env.consoleIo`; three TS host factories (Node / browser / DevTools) + one each for Python/Rust.
- **Invocation use cases:** CLI, browser app, DevTools REPL, and **serverless function** (Phase 1 can treat serverless as “Node host with non-interactive stdin”).

### Later phases (full V1 target) — *Rust-lead (2026-05-30)*

- **Capabilities (complete emit):** `FileSystem`, `Network`, bundled `Environment`, plus **`Store`** (Phase 4+).
- **Errors:** WIT-native `capability-error` for the local ABI + proto `CapabilityError` wire mirror (see *Compiler dual front-end* §1).
- **Messages:** protobuf `TInput`/`TOutput` + envelope; `protovalidate` constraints; JSON ⇄ binary via canonical mapping.
- **Hosts:** the **three Rust runtimes** built deepest (native / embedded-Embassy / browser-WASM) + Rust test host; TS/Python FS/Network hosts deferred until a consumer needs them.
- **Dispatch:** explicit entry-point registration, `envelope.target` routing (Phase 4+).
- **Pilot:** after capabilities are emitted and Rust hosts exist.

### Out of scope until post-V1

- Registry publishing (NPM / PyPI / Crates.io) unless explicitly needed.
- ~~WASM compilation of handlers~~ → **in scope** as the browser-WASM **app build** (`wasm32-unknown-unknown`); WIT **Component-Model** WASM stays out of scope (the deferred endgame).
- Streaming HTTP, subprocess, clocks; `Store` watch/subscribe (cross-runtime streaming).
- ~~Full CLI framework~~ → ILC **binds to** clap/Typer/commander instead of building one (resolved, in scope as shims).
- Pyodide/browser Python host.

---

## Implementation plan

### Phase 0 — Remaining decisions

- [x] Repo home → standalone `devalbo-ilc`
- [x] WIT split → `console-io.wit` + `environment.wit`
- [x] Phase 1 capability → `ConsoleIo` (logger output + stdin)
- [x] Output returns → void / unit (`info`, `error`)
- [x] Emitter layout → separate package per language
- [x] `read-line` → `option<string>` (WIT); emitted `readLine` → `Option<String>` (etc.)
- [x] Naming → WIT kebab-case; emitted **camelCase** (`consoleIo`, `readLine`, type `ConsoleIo`)
- [x] Browser/DevTools stdin → `window.prompt()` (fake stdin dialog)
- [x] Sync vs async → **async-first contract**; sync is non-portable host sugar (never in the contract)
- [x] IDL/compiler → keep WIT, compile via an **internal IR** (emitters target the IR)
- [x] Name/tagline → **"Inverted Line of Command"**; V1 = capability injection (registration deferred)
- [x] V1 capabilities → `ConsoleIo` + stdin + **basic binary-capable `FileSystem`**
- [x] Virtual path scheme for `FileSystem` → **resolved (2026-05-30):** mount table over a single POSIX namespace; zero-config default mount per host; introspectable/mutable mounts as the advanced escape hatch; resolver ported from `tb-solid-pod` path.ts → branded `VirtualPath`. See *FileSystem — virtual path scheme*.
- [x] Registration / dispatch model ~~(deferred from V1)~~ → **design settled (2026-05-30):** proto `service` declares → codegen types + routes → **explicit** entry-point registration → `envelope.target` routing; dynamic registry (std) / static table (embedded). Implementation lands **Phase 4+**. See *Binding & registration*.
- [x] Pilot project → **BFT codec core (2026-05-30):** port the duplicated `bft.js` / `bft.py` codec to one Rust core via ILC; ship as CLI + browser SPA, retiring both copies. Exercises ConsoleIo + binary FileSystem + proto I/O + test host across two runtimes; no Network/Store needed. See *Phase 4*.
- [x] ILC vs `CliContext` from sw-principles → **reconciled (2026-05-30):** audited sibling repos; field-level mapping done (see *ILC ↔ `CliContext` reconciliation*). `CliContext` is a principle with two divergent realizations (`tb-solid-pod`, `devalbo-cli`); the IO/state/capability half maps onto the ILC `Environment`, residual is terminal-host presentation state. No new primitive needed; migration is re-homing not rewriting. **Pending only** the sw-principles + lab-mirror doc edits in `site-devalbo.com` (awaiting OK to write cross-repo).
- [x] **Compiler dual front-end (before Phase 2):** ~~one IR or two pipelines~~ → **resolved: two independent front-ends** (WIT via `ilc compile`; proto via off-the-shelf `buf`/`protoc`), meeting at a generated handler harness keyed off proto `service` descriptors. See *Compiler architecture — dual front-end*. Capability-error-vs-app-error **locked (2026-05-30)**; remaining sub-decision: service-as-manifest.
- [x] **`buf` for the `.proto` side — LOCKED (2026-05-30).** Local-only (no BSR required): `buf.yaml` (deps incl. `protovalidate`; lint; breaking = `WIRE_JSON`) + `buf.gen.yaml` (prost + prost-serde/pbjson, es, python); `buf build -o` produces the FileDescriptorSet the ILC harness reads. Gives lint + breaking-change detection + `protovalidate` deps + canonical-JSON codegen. See *Compiler architecture — dual front-end*.
- [ ] **Distributed-state coordination model:** how endpoints share state without shared memory (see *Distributed state* §; lead bet: `Store` capability + **crdt-kit** across all three Rust runtimes, Automerge for polyglot, MQTT/shadow for embedded↔host).

### Phase 1 — Shared `ConsoleIo` + minimal `Environment` (3 languages) (2–4 weeks)

**Goal:** Prove the codegen pipeline and the host/handler split—not a complete ILC SDK.

| Task | Detail |
| --- | --- |
| Author `console-io.wit` | `info(string)`, `error(string)`; `read-line() -> option<string>`. Emitter: `info` / `error` / `readLine`. |
| Author `environment.wit` | `environment` interface with `console-io() -> console-io` → emitted `env.consoleIo()` → `ConsoleIo`. |
| Stub `ilc-result.wit` | For Phase 2 fallible capabilities; not used by `ConsoleIo` output in Phase 1. |
| Rust ILC compiler | `wit-parser` → separate TS / Python / Rust packages. |
| Repo layout | `devalbo-ilc/`: `wit/`, `compiler/`, `packages/ilc-ts`, `packages/ilc-py`, `packages/ilc-rs` (names TBD). |
| Golden tests | Snapshot WIT → emitted types per language. |
| Per-language smoke | Handler uses `env.consoleIo.*`; entry point constructs host only. |
| TS runtime matrix | `createNodeEnvironment()`, `createBrowserEnvironment()`, `createDevToolsEnvironment()`, `createServerlessEnvironment()` (or config flag on Node env). Browser/DevTools: `readLine` uses `prompt()`. Serverless: `readLine` returns `none`. |

**Explicitly deferred from Phase 1:**

- `FileSystem`, `Network` in WIT (comments/TODOs OK).
- Expanding `Environment` beyond `consoleIo`.
- Formal `NativeHost` / `TestEnvironment` crates (hand-written host factories OK).
- Handler registration, pilot.

**Exit criterion:** `console-io.wit` change → `ilc compile` updates all three languages; smokes pass; handlers never import runtime I/O APIs.

> **Sequencing note (2026-05-30):** Phases 2–4 are now **Rust-led**. WIT stays source of
> truth; each capability is designed against the Rust emitter first, TS/Python regenerate and
> golden-test in lockstep (drift surfaces immediately), but TS/Python **hosts** for the new
> capabilities are deferred until a consumer needs them. The existing `ConsoleIo` hosts from
> Phase 1 (all three languages) remain.

### Phase 2 — Expand WIT + emit, Rust-first (remaining core types) (1–2 weeks)

| Task | Detail |
| --- | --- |
| Design each capability in Rust first | Add `FileSystem`, `Network`, `Environment`, full `IlcResult` + error taxonomy — prove each shape against the Rust emitter (`no_std`-able, `Send`-agnostic, no mandated `Arc`/`Box`/`dyn`) **before** locking the WIT. |
| Implement the error shape | `capability-error { kind, code: u32, message }` — `kind`+`code` zero-alloc; `message` tiered (`String` / `&'static str` / `heapless::String<N>`). Resolved; see *Error taxonomy*. |
| Regenerate TS/Python types | Same emitters re-run; new golden snapshots. An awkward or failing emit = a Rust-shaped abstraction leaking into the contract → **fix the WIT, not the emitter.** |
| Define message types in protobuf | Three `.proto` layers — `common` (+ `IlcError` oneof), per-handler payloads, envelope. `TInput`/`TOutput` are proto, **not** CLI-native (per *Handler I/O & serialization*). |
| Generate proto bindings | Rust `prost` (std) / `micropb`-`femtopb` (`no_std`); TS `ts-proto`; Python `protobuf`. Protobuf is the wire encoding — **not gRPC**. |
| Cross-language round-trip parity tests | A message encoded by one language's codegen decodes identically in the others; bytes match the wire format. Beyond type-shape snapshots. |
| **Per-language host *behavior* tests (standing rule)** | Every new capability lands with per-language host tests, not just codegen/type parity. **Lesson (2026-05-30):** `ilc-py`'s ConsoleIo host shipped "done" but crashed at runtime (awaited a non-awaitable) because nothing tested host *behavior* — only the golden codegen snapshots existed. Behavior parity is what keeps "Rust-lead, TS/Python in lockstep" honest. Pattern: the per-language test suites added for ConsoleIo (`unittest` / `node:test`). |
| Document handler signature | e.g. Rust `async fn handle(env: &Environment<…>, input: TInput) -> Result<TOut, IlcError>`; TS `Promise<Result<…>>`; Python awaitable. |

**Exit criterion:** Generated types cover the full V1 capability table in Rust and emit cleanly (or with understood, recorded gaps) to TS/Python; cross-language round-trip parity passes; **each capability has per-language host behavior tests** (no "done" without them). No requirement for production hosts yet.

### Phase 3 — Rust hosts (lead), other languages deferred (2–4 weeks)

Build the **three Rust runtimes** deepest as the reference implementation; extend hosts to FS +
Network there first. TS/Python keep generated types but their FS/Network hosts wait.

| Task | Detail |
| --- | --- |
| Rust **native** host (tokio) | The reference. `ConsoleIo`→std streams; `FileSystem`→`tokio::fs`; `Network`→`reqwest`. |
| Rust **test** host | In-memory: capture `info`/`error`, queue `readLine` lines, memory FS, queued HTTP mocks. The `TestEnvironment` for Rust. |
| Rust **browser-WASM** host | `wasm-bindgen`: `console.*` / `prompt()` / `fetch` (`gloo-net`) / OPFS-or-`memory://`; driven by `wasm-bindgen-futures`. Mirrors the TS browser host. |
| Rust **embedded** host (Embassy) | Boot-time `Environment`: UART/RTT, flash-or-`unavailable`, `embassy-net`-or-`unavailable`. |
| Shared internal adapter | `console_io_from_callbacks({ on_info, on_error, on_read_line })`-style pattern; hosts are thin wrappers over it. |
| TS/Python FS+Network hosts | **Deferred** — types stay generated and golden-tested; build these hosts only when a consumer needs them. (TS `ConsoleIo` Node/browser/DevTools hosts already exist from Phase 1.) |

**Exit criterion:** The same Rust handler module runs unchanged under the native, test, browser-WASM, and embedded hosts; capabilities a host lacks return `unavailable`; **each host has behavior tests** (per the Phase 2 standing rule — behavior parity, not just codegen).

### Phase 4 — Pilot: BFT codec core (Rust) (1–2 weeks)

**Pilot (selected 2026-05-30):** `brute-force-transfer` — currently **duplicated** as `bft.js` + `bft.py`
(+ `deflate`/`inflate` each, separate `test_bft.{js,py}`). Port the codec to **one Rust core** behind
ILC, retiring both copies. Binary + pure transform + already multi-target = the sweet-spot first pilot.

| Task | Detail |
| --- | --- |
| Port codec to Rust | deflate/inflate + tree transform as a **pure Rust core** (`flate2`/`miniz_oxide`); no I/O in core. Seed **golden vectors** from `test_bft.{js,py}`. |
| Define proto I/O | `.proto` messages for decode/encode requests + results; `service` methods → handler routes. |
| Use the `Environment` | core reads/writes via `env.fs` (binary `read_bytes`/`write_bytes`) + `env.consoleIo`; **no** direct `std::fs` / `web-sys`. |
| Wire 2 entry points | **CLI** (clap + `ilc-rs-clap` shim → native host): `bft decode in.bft` / `bft encode dir/`. **Browser SPA** (`wasm_bindgen(start)` → browser host): upload `.bft` / download. One crate, both. |
| Tests | Rust `TestEnvironment` (in-memory FS + captured output) runs the shared golden vectors; **byte-parity** vs the legacy JS/Py outputs. |

**Exit criterion:** one Rust crate runs the BFT codec under CLI + browser + test hosts with **zero
duplicated logic**, producing **byte-identical** output to the retired `bft.js` / `bft.py`.

**Phase 4b — cross-language front-ends (stretch, additive).** Prove the cross-language *client* story:
non-Rust **CLI front-ends** that bind to the *same* Rust codec — **never** reimplement it.

| Front-end | Binding | Path to the Rust core |
| --- | --- | --- |
| Python (**Typer**) | `ilc-py-typer` shim | **PyO3/maturin** (or `uniffi`) native module; Typer parses → proto `TInput` → Rust handler |
| TypeScript (**Optique**) | `ilc-ts-optique` shim | **napi-rs** native, *or* reuse the browser **WASM** artifact in Node; Optique parses → `TInput` → Rust |

Rule: **codec logic stays single-source (Rust); these are front-ends only** (re-implementing it would
resurrect the `bft.js`/`bft.py` duplication the pilot kills). Validates client-stub codegen + the
language-barrier crossing. **Not** in the minimal exit criterion — do it after the Rust CLI+browser
proves out; it pulls FFI/binding machinery (PyO3 / napi / `uniffi`) in earlier than the core pilot needs.

### Phase 5 — OpenBindings + publishing (deferred)

- OBI executor → `Environment` adapter for one operation.
- NPM / PyPI / Crates.io only when consumers exist outside the monorepo.

---

## Questions

### Before Phase 2–4

1. **Pilot:** BFT, `devalbo-cli`, or greenfield (Phase 4)?
2. ~~**CLI ownership:** ILC owns argv/subcommands, or only handler + `Environment`?~~
   **Resolved (2026-05-30):** only handler + `Environment`; CLI libs (Typer/clap/commander) own
   dispatch; a thin per-language shim binds them. See *Invocation & CLI binding*.
3. ~~**Async:** Always `async`, or sync-with-Result for V1?~~ **Resolved (2026-05-30):** async-first contract; sync is non-portable host sugar only.
4. **Browser FS:** OPFS required for pilot, or in-memory virtual paths enough? *(still open)*
5. **TS DevTools delivery:** pasteable UMD/IIFE vs documented `import()` *(TS deferred under Rust-lead; revisit later)*.
6. **OpenBindings:** Integrate after Phase 4, or document boundary only? *(partly resolved: OBI no longer types I/O — protobuf does; any OBI integration is Phase 5)*
7. ~~**Relationship to sw-principles `CliContext`:** Merge, supersede, or keep separate layers?~~ **Resolved (2026-05-30): layer/supersede** — `CliContext` is a thin terminal-host convenience built from the `Environment`. Field-level mapping done against both sibling realizations; see *ILC ↔ `CliContext` reconciliation*. Only the cross-repo doc edits remain.

---

## Concerns

1. **Framework vs convention.** The goose-liver IoC metaphor is compelling, but teams may ignore it without codegen enforcement or lint rules (e.g. ban `fs` imports in `handlers/`).
2. **Two IDLs.** ~~WIT + OpenBindings JSON Schema~~ → **Reconciled (2026-05-30): now WIT (capabilities) + protobuf (messages), two IDLs *by design*, split by concern and audience — not a source-of-truth conflict.** OBI/JSON-Schema dropped for I/O. The discipline (no hand-written contract types) still applies.
3. **Handler discovery magic.** ~~Auto-registration hurts tree-shaking; prefer explicit registry.~~ **Addressed (2026-05-30): explicit entry-point registration locked** (no decorators/side-effect imports). See *Binding & registration*.
4. **Error ergonomics in TS.** Forcing `IlcResult` everywhere may push developers to wrap/unwrap constantly; consider helpers (`tryIlc`, `mapIlc`) in the SDK.
5. **Network mocking fidelity.** Queue mocks won’t catch URL typos across refactor unless tests assert full URL + method + body.
6. **Maintenance bus factor.** A Rust compiler is the long pole—but Phase 1 intentionally invests there to pay down cross-language drift early. *(2026-05-30, evaluated: pre-1.0 deps — crdt-kit, validation crates, id_effect — are now **wrapped behind ILC-owned traits** (`Replica` / `Validate`) so they're swappable; validation leans toward owning the standard-constraint codegen outright. Pin versions. See *Distributed state* + *Validation*.)*
7. **Versioning.** Changing the contract breaks consumers — need a semver policy. *(2026-05-30: now spans **both** front-ends — WIT changes break emitters; `.proto` changes break the wire. `buf` breaking-change detection covers the proto side.)*

---

## Comments

- **`ConsoleIo` naming** avoids collision with `window.console` and signals “I/O contract,” while output methods read like a **logger** (`info` / `error`), not POSIX stream writes.
- **Do not block Phase 1 on `IlcResult`**—reserve it for fallible capabilities in Phase 2; `ConsoleIo` output stays void.
- **“Write once, run anywhere”** should be qualified: same *handler logic*, different *hosts*—not identical binaries across Node/browser/Python.
- **TypeScript browser gap in DEVALBO-ILC.md** (“browser runtime - none”) is inaccurate today; the gap is **standardized ILC host**, not absence of browser JS.
- **Align with TODO:** `devalbo-cli?` is the strongest forcing function; implementing ILC only inside `site-devalbo.com` may not justify a compiler.
- **Lint/report integration:** Optional follow-up—ESLint rule `no-restricted-imports` in `**/handlers/**` for `fs`, `node:fs`, direct `fetch` if policy wants enforcement.
- **QuickJS/Python:** Out of scope for ILC V1; if Python must call TS handlers, that’s a separate bridge (Node subprocess, not ILC core).

---

## Error taxonomy (sketch)

> **Restructured (2026-05-30) — kind + code + tiered message** (resolves the no-alloc embedded open
> item). Capability error is **WIT-native** (local ABI); proto `CapabilityError` is the **wire mirror**
> (Compiler dual front-end §1) — same shape both sides. The old string-payload `variant` is replaced.

Split so the machine-actionable part is always present and zero-alloc, and only the human detail
degrades on bare metal:

```wit
// capability-error — WIT-native local ABI; proto CapabilityError mirrors it on the wire
enum error-kind {       // small, closed, stable — ALWAYS present, zero-alloc, drives control flow
  not-found,            // FS ENOENT, HTTP 404
  permission-denied,    // EACCES, CORS, auth
  invalid-input,        // bad argument / malformed path or URL
  unavailable,          // capability not provided by this host
  timeout,              // deadline exceeded
  io,                   // generic I/O failure
  other,                // anything else (see code)
}

record capability-error {
  kind: error-kind,
  code: u32,            // optional host-native code (0 = unset): ENOENT / HTTP status / errno … as a NUMBER
  message: string,      // human detail — tiered (below)
}
```

**`message` tiering (Rust emitter, per build profile):**

| Profile | `message` type |
| --- | --- |
| std / alloc (default) | `String` |
| `no_std`, no alloc | `&'static str` (static literal or `""`) |
| opt-in (`heapless` feature) | `heapless::String<N>` (default N = 64) |

`kind` + `code` are always present and zero-alloc, so embedded errors stay fully machine-actionable
with no allocation; only the human `message` degrades. Proto wire form:
`CapabilityError { ErrorKind kind = 1; uint32 code = 2; string message = 3; }` — embedded sends
`kind` + `code` with an empty `message`.

**Mapping rule:** every host adapter maps a native failure to the nearest `error-kind` **and** records
the native code in `code` (nothing machine-actionable is lost), with `message` best-effort. E.g. Node
`ENOENT` → `kind=not-found, code=<ENOENT>`; `fetch` 404 → `kind=not-found, code=404`; abort/deadline →
`kind=timeout`. The old string `host-error.code` ("ENOENT") becomes the **numeric** `code`
(embedded-safe); string detail rides in `message` (alloc only). Variant set seeded from
`tb-solid-pod`'s `ErrorCode` (harvested).

**Open:** closed variant set (simpler, but additions are breaking) vs. open/extensible? How
are partial successes (wrote N of M bytes) modeled — an error, or a success payload + count?

## Prior art & positioning

ILC sits in well-trodden territory; positioning against it sharpens the value claim and may
reduce what we build.

| Prior art | What it is | Relationship to ILC |
| --- | --- | --- |
| **Ports & adapters / hexagonal** | Core logic depends on *ports* (interfaces); *adapters* implement them per runtime. | ILC *is* this pattern, made a cross-language **convention + generated ports** with bundled adapters (hosts). |
| **Effect-TS** (`Effect<A, E, R>`) | Typed effects: requirements (`R`) channel, typed errors (`E`), async. | TS-only — a candidate **implementation** of the TS host/SDK, *not* the cross-language contract. |
| **WASI / Component Model (WIT)** | Capability-based host APIs for WASM components, defined in WIT. | ILC borrows WIT-as-schema **without** WASM; the real Component Model is the WASM-future endgame. |
| **DI frameworks** (NestJS, …) | Runtime dependency injection within one language/app. | Narrower (single language/runtime); ILC's novelty is the **cross-language, cross-runtime** contract. |

**ILC's delta:** one capability contract emitted to TS/Python/Rust, with host adapters (incl.
browser) and a memory-backed test host, so the *same handler logic* runs across runtimes and
languages with bounded, mockable I/O. If only the TS slice ever ships, Effect-TS likely covers
it and the compiler isn't justified — which is precisely why **the matrix is the point**.

## Success metrics

### Phase 1 complete

| Metric | Target |
| --- | --- |
| Languages with generated `ConsoleIo` + minimal `Environment` | 3 (TS, Python, Rust) |
| Hand-written duplicate `ConsoleIo` APIs | 0 |
| `console-io.wit` change → CI updates all emitters | Yes (golden tests) |
| TS hosts exercised | Node CLI + browser app + DevTools REPL |
| Cross-language smoke | 1 handler per language; host wires logger + stdin |

### Full V1 complete (after Phase 4)

| Metric | Target |
| --- | --- |
| Generated `Environment` + all V1 capabilities | 3 languages |
| Pilot handlers using `Environment` | 100% of I/O in pilot module |
| Duplicate logic CLI vs UI | 0 (shared handler module) |
| `TestEnvironment` covers pilot core paths | Yes |
| Published packages | Optional (workspace/local OK) |

---

## Suggested next actions

1. ~~Scaffold `devalbo-ilc`~~ — done: `wit/`, `packages/`, `compiler/`, `docs/PHASE1.md`. Next: Rust compiler + golden tests.
3. Align `DEVALBO-ILC.md` capability table with `ConsoleIo`.
4. Revisit pilot at Phase 4.

---

*Review date: 2026-05-27. Updates: tri-language Phase 1, `ConsoleIo` (logger output + stdin), minimal `Environment`, host-only bindings, standalone repo.*

*Revision 2026-05-30 (authoritative direction; see dated sections + reconciliation notes): **Rust-lead**
sequencing; **three Rust runtimes** (native-tokio / embedded-Embassy / browser-WASM) on one
`Send`-agnostic, `no_std`-able contract; **CLI/dispatch bind to existing libraries** (clap/Typer/commander);
**two IDLs by concern** — WIT capability interfaces + **protobuf** message types (OBI/JSON-Schema dropped
for I/O); proto 3-layer messages (common/payload/envelope), `protovalidate`, JSON⇄binary canonical mapping;
**dual-front-end compiler** (WIT-IR + off-the-shelf `buf`) meeting at a generated handler harness; capability
error (WIT) vs app error (proto); **explicit entry-point registration**; **`Store`** capability for
distributed state (crdt-kit / Automerge / MQTT-shadow). Recorded in this file; not yet committed or mirrored
to the sibling planning repo.*
