# DEVALBO-ILC — Second-pass review (Claude)

Companion to [DEVALBO-ILC.md](./DEVALBO-ILC.md) and [DEVALBO-ILC-PLAN.md](./DEVALBO-ILC-PLAN.md).
This is a reviewer's pass focused on **holes, structural challenges, and decisions to make** —
the existing PLAN is already thorough and self-aware, so this only adds what it doesn't cover.

Each actionable item has a **Response:** line for you to answer inline.

_Review date: 2026-05-27._

---

## Verdict

- **Feasibility:** Technically feasible. The main problem is **sequencing**: the plan
  front-loads the riskiest, least-validated, most expensive component (the bespoke
  WIT→3-language codegen compiler) before proving single-language value.
- **Utility:** Real — but only materializes at a **multi-runtime *and* multi-language**
  scale that isn't yet demonstrated. Both currently-named consumers (this site,
  `devalbo-cli`) are TypeScript.
- **Core tension:** "Prove the pipeline first" ≠ "prove the idea has value." The plan funds
  the expensive bet (codegen + 3 languages) before the cheap one (one real pilot in TS).

---

## Big structural challenges

### 1. Codegen pipeline is upside-down on cost/benefit at V1 scale
For `ConsoleIo` (`info`/`error`/`read_line`), each language's hand-written contract is
~20–30 lines, trivially kept in sync by hand. Avoiding that hand-sync costs: a Rust CLI +
`wit-parser` + three emitters + golden snapshot CI. That machinery only pays off when the
capability surface is large *and* genuinely multi-language. Phase 1 as written builds the
long-pole maintenance burden (your Concern #6) to solve a problem you don't have yet.

- **Decision:** Invert the sequence — hand-write TS interfaces, prove the host/handler split
  with a real pilot, build codegen only when the surface (or a real second-language consumer)
  justifies it?
- **Response:** As features get developed and contracts get implemented, keeping developed implementations tested and working will prevent regressions and can be rethought/reworked simultaneously across languages rather than couple features to language idiosyncracies.
- **Reviewer reply:** Understood — "matrix-first" is a valid *de-risk-the-hardest-assumption* call; adopting it. The cross-language consistency you describe is exactly codegen's payoff. One guardrail: make the compiler **IR-decoupled** (see #2) so the bet stays reversible.

### 2. Is WIT the right IDL, given you're not targeting WASM?
Not compiling to WASM removes WIT's reason for existing. WIT brings Component-Model
semantics (resources, an in-flux async story) and its **non-WASM native-interface codegen is
immature because that isn't its purpose**. For "define once, emit idiomatic TS/Python/Rust,"
**TypeSpec** and **Smithy** are purpose-built and far more mature; or make a `.ts` file the
source of truth and use existing TS→other generators.

- **Decision:** Justify WIT over TypeSpec/Smithy/TS-as-source, or switch?
- **Response:** I can envision WASM becoming a more common runtime. How hard will it be to switch from any one of these to another?
- **Reviewer reply:** Cheap **if you design for it**: parse WIT into your **own internal IR** and write every emitter against the IR, never against WIT types directly. Then switching IDLs later = rewrite the parser front-end only; the emitters (the expensive ~80%) are untouched. That makes keeping WIT a low-regret choice *and* preserves the WASM-future option. → add "IR-decoupled compiler" as a Phase-1 design rule.

### 3. The defining "line of command" idea has been descoped
The vision is *Interface for a **Line of Command*** — handlers register and get dispatched.
The plan (rightly flagged High risk) defers registration/routing indefinitely, leaving V1 as
"portable capability injection." That's worth building, but it **isn't the vision and the name
no longer fits**.

- **Decision:** Reframe/rename ILC as "typed capability injection" (drop "line of command"),
  or bring a registration story back into V1 scope?
- **Response:** It's really gotta be the reverse of CLI since that's wordplay... if you can find better candidates for the acronym ILC, i'm all ears.
- **Reviewer reply:** Keep `ILC` (it *is* `CLI` reversed). Expansions that also name the actual pattern:
  - **Inverted Line of Command** — cleanest "reverse of CLI" (suggested tagline)
  - **Inversion-of-control Line of Command** — names the IoC pattern directly
  - **Injected Line of Command**
  My pick: tagline **"Inverted Line of Command,"** with "inversion of control" in the first sentence. Registration scope stays open, but "capability injection" V1 reads fine under either framing.

### 4. The BFT pilot is incompatible with V1's text-only FileSystem
V1 `FileSystem` is `read_text`/`write_text` only, but BFT reads a **`.zip` (binary)** and
base64 blobs (`bft-codec.ts` works in `Uint8Array`). So the top-listed pilot (PLAN line ~75)
can't run on the V1 capability set.

- **Decision:** Add a binary read/write to V1, or pick a genuinely text-only pilot?
- **Response:** This is OK... maybe we re-order the plan. V1 should focus on printing hello world, getting input from user, and maybe a basic filesystem IO.
- **Reviewer reply:** Adopted: **V1 = `ConsoleIo` (hello world) + stdin + basic FileSystem IO**; Network + BFT pilot deferred. Make the basic FS **binary-capable** (`read_bytes`/`write_bytes`, with text as a convenience) so it isn't boxed in later (cf. #9) — text-only would block BFT and most real files.

### 5. Async (and Rust's runtime) is a Phase-0 decision, not Phase-2
The handler signature *is* the core contract, and async-ness shapes it in all three
languages. Deferring until Network (Phase 2) risks reworking everything. Worse: an `async`
Rust handler pulls in a runtime (tokio), **leaking runtime into the "pure" logic** and partly
defeating the decoupling goal.

- **Decision:** Pin the async model now — sync-with-Result for V1, or async everywhere? May
  Rust handlers be async (and thus runtime-coupled)?
- **Response:** We can do async everywhere at teh fundamental level and turn it into sync at the generated code level, right?
- **Reviewer reply:** ⚠️ **Partly — and the exception is load-bearing.** You **cannot** block on async in **browser JavaScript** (no `block_on`; `await` only works in an async context), so a generated *sync* `env.console.info()` is impossible in the browser. Rust (`block_on`), Python (`asyncio.run`), and Node-via-worker can fake sync, but the browser can't. Therefore: **make the shared contract async** and emit it idiomatically (TS `Promise`, Rust `async fn`, Python `async def`/awaitable); handlers are async (`await env.console.info("hi")`). Treat sync as **non-portable host sugar only** — if "sync" leaks into the shared contract, browser handlers silently break, defeating the matrix you're protecting. Pin this before any codegen.

---

## Smaller holes

### 6. God-object `Environment` vs. least-privilege
Passing one bundle containing every capability is the un-IoC option. Consider handlers
declaring the typed *subset* they need, so the compiler enforces least privilege and tests
mock only what's used (cf. Effect-TS's requirements channel).

- **Decision:** Single `Environment` bundle, or per-handler capability subsets?
- **Response:** Let's have an introspectable Environment defined per environment. The per-handler capability should be pulled from that environment object.
- **Reviewer reply:** Good — introspectable, per-host `Environment`; handlers pull the capabilities they need from it. Pairs with the escape-hatch in #9 (discover/reach host-specific extras). On the TS side, Effect-TS's requirements (`R`) channel maps cleanly to this (see #12).

### 7. Error taxonomy is undesigned
"Full `IlcResult` + error enum" (Phase 2) hides the real work: what variants exist, how
extensible, and how host-specific errors (`ENOENT`, network timeout, CORS) map into the
standardized `E`. That mapping is the hard part of cross-language parity, not the `Result`
wrapper.

- **Decision:** Sketch the error variant set + host-error → `E` mapping before Phase 2?
- **Response:** yes, do this
- **Reviewer reply:** Will add an error-taxonomy sketch to the PLAN (variant set + host-error→`E` mapping). Note: with an async contract, `IlcResult` rides *inside* the future (e.g. `Promise<Result<T,E>>` / `async fn -> Result<T,E>`), so errors stay values, not thrown — keeps Rust/Python/TS parity.

### 8. "Default implementations" contradicts IoC
DEVALBO-ILC.md §3 says the SDK ships "default implementations." A default `FileSystem` /
`Network` reintroduces the ambient, implicit behavior the project exists to eliminate.

- **Decision:** Drop defaults, or scope them strictly to test/no-op adapters?
- **Response:** we need multiple implementations that the library/generated code creates at runtime by inspecting where it ascertains it is running.
- **Reviewer reply:** Workable, with one rule: keep auto-detection at the **host-selection layer only** and make it **overridable**. Detect-the-runtime to pick a *default* host (`detectEnvironment()` → Node/browser/…), but tests and power users must be able to inject an explicit `Environment`. Non-overridable detection re-introduces the ambient behavior ILC exists to remove. These are **adapters chosen at startup**, not the `§3` "default implementations" baked into the SDK — drop that phrasing from DEVALBO-ILC.md.

### 9. Capability surfaces are under-/over-specified
`fetch` as "url → bytes" is too thin for real handlers (no status/headers/method/body shape);
a full fetch is a big surface. Same for FS (no `exists`/`list`/`delete`/`stat`).

- **Decision:** Define the minimal *useful* request/response and FS shapes for V1.
- **Response:** Yes, minimal that handles common use cases is fine. I'm most concerned with not boxing out capabilities for common use cases (and providing metadata to reach into environment specific cabpabilities if necessary)
- **Reviewer reply:** Agreed: minimal core + a typed **escape hatch** to reach host-specific capabilities, surfaced via the introspectable Environment (#6) — e.g. `env.capabilities()` to discover, `env.tryGet("opfs")` to reach extras. Will spec minimal-but-useful `fetch` (status/headers/method/body) and FS (`read_bytes`/`write_bytes`/`exists`/`list`) shapes so common cases aren't boxed out.

### 10. Mock fidelity (your Concern #5)
Queue-and-expect won't catch URL/method drift unless mocks assert full request identity
(URL + method + body). Bake that into the matcher rather than relying on test-author
discipline.

- **Decision:** Require full-request assertions in `TestEnvironment` network mocks?
- **Response:** OK, let's see how that goes.

### 11. Timeline realism
2–4 weeks for a Rust `wit-parser` compiler + 3 emitters + golden tests is optimistic for a
solo effort, especially if Rust isn't a daily language.

- **Response (revised estimate / appetite):** My build agents can handle it.

---

## Things to add to the docs

### 12. Prior-art / positioning section
This is **ports-and-adapters / hexagonal architecture**; in TS specifically **Effect-TS**
already does typed capability/environment injection; **WASI / the Component Model** is
literally capability-based host APIs over WIT. Positioning ILC against these (what's new? why
not adopt Effect for the TS side?) sharpens the value case — and may reveal you need a
convention + a small TS layer, not a compiler.

- **Decision:** Add a prior-art section and explicitly state ILC's delta vs. Effect-TS /
  hexagonal / WASI?
- **Response:** If integrating Effect-TS reduces need for code we need to write, let's do it!!!!
- **Reviewer reply:** Caveat: Effect-TS is **TS-only**, so it can't be the *cross-language contract* — it's an **implementation choice for the TS host/SDK** (its `Effect<A, E, R>` gives you async + `IlcResult` + the Environment `R` channel for free, cutting TS code). Keep the **generated interface Effect-agnostic** so Python/Rust stay first-class; have the TS host *adapt* the generated interface onto Effect. Watch the trade: Effect is a large dep with a learning curve.

### 13. "Do we even need three languages for V1?"
Your own utility section says the strongest forcing function is `devalbo-cli` (TS), and both
real consumers are TS. The tri-language goal is currently aspirational ("validate the
abstraction" = building the hard thing to prove it's possible, not because a Python/Rust
consumer exists). A **TS-only V1** removes the compiler from the critical path entirely.

- **Decision:** TS-only V1, adding Python/Rust when a real consumer appears — or keep
  tri-language as a hard V1 goal?
- **Response:** These runtimes and their behaviors will act as guardrails vs ones we implmeent ourselves.
- **Reviewer reply:** Adopted — tri-language as a guardrail (real runtimes force an honest abstraction). Accepting the matrix-first posture; the PLAN is revised around it.

---

## What's strong (keep)

- Risk-ordered phasing; `ConsoleIo`-first; deferring `IlcResult` until fallible capabilities.
- **Host-only bindings** (handlers never pick a host) — the right invariant.
- Clear OpenBindings relationship table (capability contract vs. operation contract).
- The "qualify *write once, run anywhere*" comment (same handler logic, different hosts).
- Honest concerns/questions sections already in the PLAN.

---

## Net recommendation

Reshape V1 to: **TypeScript-only, hand-written capability interfaces, a `TestEnvironment`, and
one real pilot on a binary-capable I/O surface.** Let observed pain — contract drift, or a
genuine second-language consumer — pull in WIT/codegen later, if ever. This keeps the sound
core (bounded injected capabilities, trivial testing) while removing the most speculative,
expensive, and maintenance-heavy parts from the critical path.

- **Overall response / which direction:** Discovering if the matrix breaks early is more important than getting a single thing working.
- **Reviewer reply:** Accepted. Optimizing to **surface matrix breakage early** over a single working pilot is a coherent strategy — it front-loads the project's hardest assumption (cross-language parity) instead of its easiest. My original "TS-only first" recommendation is therefore **withdrawn** in favor of your posture; the two biggest risks to manage under it are (a) the **async/browser** constraint (#5) and (b) **IDL/IR decoupling** (#2) so WIT stays reversible. PLAN updated accordingly.
