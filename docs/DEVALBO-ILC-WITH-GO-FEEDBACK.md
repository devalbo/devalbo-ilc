# DEVALBO-ILC-WITH-GO — Feedback

Reviewer's pass on [`DEVALBO-ILC-WITH-GO-DRAFT.md`](./DEVALBO-ILC-WITH-GO-DRAFT.md), read against the
authoritative ILC direction in [`DEVALBO-ILC-PLAN.md`](./DEVALBO-ILC-PLAN.md) (esp. the 2026-05-30
*Runtime decisions* / *Handler I/O* / *Compiler dual front-end* / *Distributed state* sections),
[`DEVALBO-ILC.md`](./DEVALBO-ILC.md), and [`PHASE1.md`](./PHASE1.md).

_Review date: 2026-07-24._

> **Status:** This review fed into **[`DEVALBO-ILC-GO-PLAN.md`](./DEVALBO-ILC-GO-PLAN.md)** (authoritative).

---

## TL;DR

The Go draft is a **good concrete app blueprint** and it *embodies* the ILC pattern in one place (the
SQLite database-inversion-of-control). But it is **an instance of ILC, not the ILC framework** — it
hard-codes a single capability (`sqlite-host`) and a single export (`execute-cli`), and it omits almost
every generalized subsystem the plan treats as the actual product: the introspectable `Environment`, the
capability set (`ConsoleIo` / `FileSystem` / `Network` / `Store`), the protobuf message layer, the
compiler/IR + golden tests, the error taxonomy, the test host, and registration/dispatch.

**Go can genuinely do most of what you need** — native, server, serverless, browser-WASM, and (via Wails)
desktop, with an interface-based capability model that's arguably *more natural* in Go than in Rust. The
**one thing it structurally cannot do** is the plan's declared linchpin: the **`no_std` bare-metal MCU
(Embassy) tier**. That single gap is the whole strategic question below — is embedded a real requirement
or an aspiration? Your answer decides whether Go **replaces** the Rust-lead or **complements** it.

---

## Where Go is genuinely strong for ILC (keep these)

| ILC concern | Why Go fits well |
| --- | --- |
| **Capability injection = interfaces** | ILC's whole model is "handler pulls typed capabilities from an `Environment`." Go interfaces are the most idiomatic possible expression of this — no traits+generics+`no_std` gymnastics, no `Send`/`Sync`/AFIT/`#[async_trait]` contortions the plan spends paragraphs on (PLAN lines ~94–115). |
| **Async-first pain evaporates** | The plan's hardest cross-cutting constraint is "async-first, executor-agnostic, browser can't `block_on`" (REVIEW #5; PLAN *Runtime decisions*). Go has **no function coloring** — goroutines + blocking calls work identically native and (with care) in WASM. Much of the async tax the Rust plan pays simply doesn't exist. |
| **Protobuf is home turf** | The I/O layer is *entirely* protobuf (`TInput`/`TOutput`, envelope, `common`/`IlcError`, `protovalidate`, canonical JSON — PLAN *Handler I/O & serialization*). Go has the most mature proto toolchain of the three target languages (`protoc-gen-go`, `buf` first-class). This is a strength the draft doesn't even use yet. |
| **Multi-runtime from one language** | Native (trivial), server, serverless, browser-WASM, desktop-Wails — all real Go targets. The draft's tri-target (Web/Desktop/CLI) already proves this. |
| **Desktop runtime (Wails)** | The plan only *notes* Tauri/desktop as "a likely 4th runtime" (PLAN line ~707) but never scopes it. The Go draft delivers a concrete, strong desktop story. This is Go **ahead** of the Rust plan. |
| **Build/iteration DX** | Simpler toolchain, faster builds, gentler learning curve than Rust + `no_std` + Embassy + trunk. |

---

## The one hard limitation: bare-metal embedded

The plan elevates the **`no_std` + Embassy MCU host** to the *reason Rust leads*: "The ultimate
constrained host and the contract's portability stress test: if it maps here, it maps anywhere" (PLAN
*Runtime decisions*). Go cannot serve that tier:

- Go (and TinyGo) ship a **garbage collector + goroutine scheduler runtime**; there is no `no_std`,
  no-alloc, `&'static str` / `heapless` tier. The "sleepy MCU, executor sleeps the core on idle,
  zero-alloc error `{kind, code}`" design (PLAN *Error taxonomy*, *Runtime × capability matrix*) has no
  Go analog.
- TinyGo *does* target some microcontrollers, but via its own GC'd runtime — it is **not** the same
  portability guarantee, and the whole `heapless`/static-dispatch-table/`micropb`-`femtopb` embedded
  substrate (PLAN *Handler I/O*, *Binding & registration*) doesn't apply.

**Consequence:** Go can be an excellent lead for **native + server + serverless + browser + desktop**, but
it **cannot be the bare-metal stress-test host**. If embedded is real, Go is a *complement* to a Rust
embedded core, not a replacement. If embedded is aspirational, Go is arguably a *better* lead for
everything else. **This is the decision the whole doc hinges on** — see *Decisions to surface* #1.

---

## Feature-by-feature: what the Go draft doesn't account for

Mapping the ILC vision's elements onto the draft:

| ILC element (source) | In the Go draft? | Gap / note |
| --- | --- | --- |
| **Introspectable `Environment`** (per-host bundle; handlers pull typed subsets; `capabilities()`/`tryGet()` escape hatch — PLAN *Review decisions* #6, FS mount table) | ❌ | The draft has one imported capability (`sqlite-host`), no `Environment` abstraction, no introspection, no escape hatch. This is ILC's **core primitive** and it's absent. |
| **`ConsoleIo`** (`info`/`error`/`readLine`; the *entire* Phase 1 — PHASE1, PLAN Phase 1) | ❌ | Not present. The draft skips straight to a DB app; it never models the logger-shaped console contract that is ILC's proven, shipped Phase-1 surface. |
| **`FileSystem`** (binary `read_bytes`/`write_bytes`, **mount table over a POSIX namespace**, branded `VirtualPath`, `unavailable` — PLAN *FileSystem virtual path scheme*) | ⚠️ partial | The draft's only persistence is SQLite. No generalized binary FS capability, no virtual-path/mount model, no OPFS-vs-`memory://`-vs-native abstraction as a *capability* (it's baked into the DB path instead). |
| **`Network`** (`fetch`-shaped, status/headers/method/body, CORS-bound in browser — PLAN §9, matrix) | ❌ | Absent (deferred in ILC too, but the draft has no seam for it). |
| **`Store`** + distributed state (CRDT / HLC / MQTT-shadow; `get`/`set`/`merge`/`state-vector`/`delta-since` — PLAN *Distributed state*) | ⚠️ different model | The draft's state model is **SQLite-per-node**, not multi-master convergence. No CRDT, no logical clock, no offline-first sync, no "who owns truth / how replicas converge." (See *What the Go draft adds* — SQLite-as-capability is a good idea, but it's not the Store design.) |
| **Protobuf message layer** (`common`+`IlcError`, per-handler `TInput`/`TOutput`, **envelope** with `message_id`/`target`/`content_type`; wire = serial + network + in-process; field-number evolution — PLAN *Handler I/O*) | ❌ | The draft's `execute-cli(args: list<string>) -> command-result` is a stringly-typed passthrough. No typed messages, no envelope, no wire format, no schema evolution, no serial transport. For a local app that's fine; for ILC it's a **whole missing subsystem** — and it's the one Go would do *best*. |
| **Registration / dispatch** (proto `service` → harness → explicit entry-point registration → `envelope.target` routing → **cross-language client stubs** — PLAN *Binding & registration*) | ❌ | One `execute-cli` export = no multi-handler routing, no dispatcher, no client stubs, no "caller in language A invokes handler in language B." |
| **Error taxonomy** (`capability-error { kind, code:u32, message }` + `unavailable` variant + proto `CapabilityError` wire mirror — PLAN *Error taxonomy*) | ❌ | The draft's `command-result { success, output, error-message? }` is stringly-typed. No `kind`/`code`, no `unavailable` for missing capabilities, no capability-vs-app error split. |
| **Test host / `TestEnvironment`** (memory-backed, captured `info`/`error`, queued `readLine`, memory FS, queued HTTP mocks — DEVALBO-ILC §6, PLAN Phase 3) | ❌ | No testing story at all. The plan's *standing rule* is "no capability is 'done' without per-language host **behavior** tests" (PLAN Phase 2 exit criterion — added precisely because `ilc-py` shipped a crashing host). The draft doesn't provide the seam that makes this possible. |
| **Compiler / IR + golden tests** (WIT→internal IR→idiomatic emit; IDL swappable; parity via golden snapshots — PLAN *Review decisions* #IR, Phase 1) | ⚠️ outsourced | The draft uses community `wit-bindgen-go` for Go bindings. That's fine for Go-only, but it **bypasses the IR-decoupling design rule** and the golden-test parity engine that keeps the *matrix* honest. See *Decisions* #3. |
| **Invocation binding** ("bind, don't supplant"; clap/Typer/commander/Optique shims — PLAN *Invocation & CLI binding*) | ❌ | `execute-cli` is a hand-rolled argv passthrough, not a shim onto a Go CLI framework (cobra/urfave-cli), and there's no browser-UI-event or embedded-boot invocation abstraction. |
| **Cross-language parity ("the matrix is the point")** (REVIEW #13, PLAN *Review decisions* posture) | ❌ | A Go-only plan collapses the tri-language matrix the plan calls its *whole value*. Not wrong — but it's a strategic reversal that must be made *explicitly*, not by omission. See *Decisions* #2. |

---

## Architectural divergences to resolve (not just gaps)

These aren't missing features — they're places where the Go draft takes a *different position* than the
plan, and you should pick deliberately:

1. **WASM Component Model "for real" vs. WIT-as-schema-only.**
   The plan is emphatic: *"We are strictly using WIT as a schema definition language, **not** targeting
   WebAssembly compilation"* (DEVALBO-ILC §3), and the Component-Model WASM is "the deferred WASM-future
   endgame" (PLAN *Runtime decisions*). The Go draft does the **opposite** — it genuinely compiles to a
   WASM component (TinyGo→wasi→`jco transpile`) and runs the Component Model at runtime. That's a real
   fork. It's not necessarily wrong (it's arguably where WIT *wants* to be used), but it commits you to a
   **bleeding-edge toolchain**: TinyGo + wasi-preview2 + `wit-bindgen-go` + `jco` component instantiation.
   Maturity/stability risk is real; the plan's "schema-only" stance exists partly to *avoid* exactly this.
   **Decide:** is Go the excuse to finally commit to the Component Model, or does Go also treat WIT as
   schema-only and compile to plain `GOOS=wasip1`/`js` without the component machinery?

2. **Goroutines vs. the async-first contract.**
   Go's no-coloring concurrency is a *win* (above), but note the hidden seam: `execute-query` looks
   synchronous (`func(...) -> string`) yet crosses a Web Worker + Comlink boundary that is **inherently
   async** on the JS side. In the browser you cannot block the main thread; the draft's worker offloads
   this, but any Go code that *blocks* on a host capability call needs the goroutine to be off the main
   thread or yielding to the JS event loop. This is manageable, but the draft doesn't state the rule.
   The plan's equivalent rule ("sync can never enter the shared contract") still has a Go shadow: **host
   capability calls must be safe to block a goroutine on, never the browser event loop.**

3. **TinyGo limitations bite the proto/reflection path.**
   TinyGo has incomplete reflection and stdlib coverage. Reflection-heavy protobuf runtimes may not work
   under TinyGo, which matters *a lot* if you adopt the protobuf message layer (you should — it's Go's
   strength). Validate `google.golang.org/protobuf` (or a reflection-free codegen) under TinyGo **early**,
   or the "one message layer across native + browser" goal breaks on the browser target.

4. **CGO-less SQLite is handled well — keep it, but generalize.**
   The draft correctly uses `modernc.org/sqlite` (pure Go) natively and delegates to `sqlite-wasm`/OPFS on
   web. That's a clean, correct CGO-avoidance. But it's **SQLite-specific**; ILC wants this expressed as a
   general capability (see next section).

---

## What the Go draft *adds* that ILC should harvest

Not everything flows one way — the draft contributes two things worth folding back into the plan:

- **Database/SQLite as an injected capability.** The web path (Go imports `sqlite-host`; a Web Worker runs
  `@sqlite.org/sqlite-wasm` on OPFS and executes on Go's behalf) is a genuinely elegant, concrete
  demonstration of ILC's *inverted* capability injection — arguably cleaner than the abstract `Store`
  sketch. The plan only gestures at relational state via `cr-sqlite` (PLAN *Distributed state* table). A
  first-class **`Database`/relational `Store` capability** (`execute-query`, host-provided, native-file vs
  OPFS-worker vs in-memory adapters) belongs in the plan, and the Go draft is the working prototype of it.
- **Desktop (Wails) as the concrete 4th runtime.** Promote the plan's parenthetical "Tauri/desktop likely
  4th runtime" to a named target; Go+Wails is the reference for it.

---

## Decisions to surface

1. **Is bare-metal embedded a real V1+ requirement, or aspirational?** *(the load-bearing one)*
   - Real → Go **complements** a Rust embedded core; keep Rust as the `no_std` lead, add Go as the
     native/server/desktop/browser lead. Two leads, one WIT+proto contract.
   - Aspirational → Go can *replace* the Rust-lead for everything that ships, with a far gentler
     toolchain. The "if it maps to the MCU it maps anywhere" guardrail is lost, but you may not need it.

2. **Does Go replace the TS/Python/Rust matrix, or join it?** The plan's stated *whole value* is
   cross-language parity as a guardrail (REVIEW #13). A Go-only ILC is coherent but is a **different
   project** — "one portable capability framework in Go" vs. "a language-neutral capability contract."
   If Go *joins* the matrix, `wit-bindgen-go` + `buf`/`protoc-gen-go` keep it honest and this is low-cost.
   If Go *replaces* it, say so explicitly and delete the matrix framing.

3. **Compiler: adopt `wit-bindgen-go` or add a Go emitter to the ILC compiler?** Community `wit-bindgen-go`
   is free but bypasses the IR-decoupling rule (PLAN *Review decisions* #IR) and the golden-test parity
   engine. For Go-only, `wit-bindgen-go` is fine. For Go-in-the-matrix, decide whether Go bindings must
   flow through your IR (parity-tested) or can be an ecosystem tool alongside it.

4. **Commit to the Component Model, or keep WIT schema-only?** (Divergence #1 above.) This decides your
   whole web toolchain and its maturity risk.

5. **Adopt the protobuf message layer + envelope + registration in the Go plan.** The draft's
   `execute-cli(list<string>)` is too thin to be ILC. If Go is in, the plan's proto `TInput`/`TOutput` +
   envelope + `service`-as-manifest + explicit registration should be ported to Go (where they're *easy*)
   — otherwise you've kept the app and dropped the framework.

---

## Net recommendation

Reframe the draft from **"a Go local-first app"** into **"the Go host tier of ILC."** Concretely:

1. **Keep** the strong parts: pure-Go CGO-avoidance, Wails desktop, TinyGo/WASM path (pending maturity
   validation), and the SQLite-as-capability idea (promote it to a `Database` capability).
2. **Add** the missing framework surface, in the order the plan already proved works:
   `ConsoleIo` + `Environment` first (Phase 1 parity), then binary `FileSystem` w/ mount table, then the
   **protobuf message + envelope + error-taxonomy** layer (Go's strength), then registration/dispatch,
   then a **test host** with behavior tests (the plan's non-negotiable standing rule).
3. **Decide embedded up front** (Decision #1). It's the only question that changes whether Go *leads* or
   *complements*. Everything else is Go doing ILC well — which, for the non-`no_std` runtimes, it plausibly
   does *better* than the current Rust-lead.

**Bottom line:** "Aside from UI, Go can do a lot of what I need" is right — Go covers the native, server,
serverless, browser, and desktop tiers cleanly, and it makes the async and protobuf stories *easier*, not
harder. The draft just currently describes the *app*, not the *framework*, and it silently drops the
embedded tier and the cross-language matrix. Name those two omissions as deliberate choices and the Go
direction is strong.
