# Events — implementation plan (§6.3)

**Status: complete.** Settled as **Decision 33**; the rules that outlive this document are in `AGENTS.md`.
Supersedes the outline in `EVENTS-PLAN-PROMPT.md`. Kept for the findings, not as a work list.

| Phase | State |
| --- | --- |
| 1 — seam + platform API | ✅ **done** |
| 2 — parity records + compares events | ✅ **done** |
| 3 — web host | ✅ **done** |
| 4 — notes uses it | ✅ **done** |
| 5 — document it (and **not** declare it) | ✅ **done** |

**Follow-ups, deliberately out of scope** (also in the tasks doc): cross-tab delivery via
`BroadcastChannel` — a second tab does not see this one's writes; and the desktop tier's
`runtime.EventsEmit`, which has no host to wire into yet.

Events is the first **custom capability import**. Console and Filesystem are standard WASI, so the engine
has never imported anything a *host* must provide. Whatever shape this takes is inherited by Display, the
SQLite index, and network — so the cost of getting it wrong is not one capability, it is the pattern.

---

## 1. Why now

`example-apps/notes` calls `refresh()` by hand after every command. That is correct only while exactly one
tab, driven by its own UI, is the sole writer. It is already wrong for:

- a second browser tab on the same OPFS origin
- the CLI writing `records/*.json` while the browser is open
- `import-fs` replacing the whole store underneath a rendered list
- any future sync

The loop §6.3 describes is `emit-event(topic, payload)` → host subscription → UI invalidates.

**Three things this forces, each load-bearing beyond Events:**

1. **The first custom WIT import.** Its shape becomes the template for every later capability.
2. **The `caps_native` / `caps_wasip2` seam** (§5.3) finally gets built. Designed long ago, still unchecked
   because nothing needed it: native satisfies the import by direct Go call, wasm through the component
   boundary, and the business logic above the seam is identical.
3. **A parity decision.** The check diffs results and written filesystems. An event is part of what a
   command *does*, so it belongs in the comparison — untested side effects are exactly how the OPFS
   write-only flush bug survived.

---

## 2. Design decisions

### D1 — The boundary is an IMPORT, flat, `(topic: string, payload: list<u8>)`

```wit
// The first capability the HOST provides to the engine.
interface events {
  emit: func(topic: string, payload: list<u8>);
}

world engine {
  include wasi:cli/imports@0.2.0;
  use types.{command-result};
  import events;                 // ← new
  export execute: func(method: u32, request: list<u8>) -> command-result;
}
```

**Flat scalars + bytes, mirroring `execute`.** A rich WIT record or variant would require the Component
Model and strand the embedded tier, where WAMR speaks core wasm + WASI p1 only (§5.3). `string + list<u8>`
lowers to pointer/length pairs, which `//go:wasmimport` can express. Same reasoning that shaped `execute`;
the same reasoning should shape everything that follows it.

**No return value.** Emitting is fire-and-forget. A return would invite hosts to answer, and an answer
would make the engine wait on a host — which on the web tier means blocking the worker inside a
synchronous component call.

**Does this violate Decision 31 ("one boundary")?** No, and it is worth stating why: Decision 31 governs
what the engine *exports* — one command entry point, no second way in. Imports are the other direction and
are how capabilities were always meant to arrive (§5.1). What Decision 31 forbids is a second
`execute`-like export, and this is not one.

### D2 — Emit is synchronous, and the host must not re-enter

The engine calls `emit` inline from a handler and continues immediately. **Hosts must not call `execute`
from inside an event callback** — that re-enters the engine while a command is on the stack. The web host
satisfies this naturally by forwarding over Comlink (a message boundary); native hosts must defer
explicitly.

This is a rule that fails far from its cause, so it belongs in `AGENTS.md`, not only here.

**Not buffered.** An earlier draft buffered events per command and flushed them with the result, for
deterministic ordering. Rejected for v1: buffering only helps events *caused by a command*, and the
reactivity story needs spontaneous ones too (a watcher, a timer, a sync). One mechanism beats two.
Determinism comes from handlers being deterministic, which parity then enforces.

### D3 — Topics are namespaced strings; payloads are proto bytes

```
ilc.data-changed          platform-defined, emitted by inherited verbs (import-fs, reset-fs)
notes.record-changed      app-defined
```

Payload is a proto-encoded message (§7.2, one serialization story). The platform defines
`devalbo.ilc.v1.DataChangedEvent`; apps define their own in their own `.proto`.

**Why a string topic rather than a numeric id like `method_id`:** subscribers filter by prefix
(`notes.*`), topics are not a wire contract the way dispatch ids are, and an app inventing a topic must
not need a registry allocation. The cost is that a typo'd topic silently matches nothing — accepted,
because the alternative is a second id-locking mechanism for something that is not dispatch.

### D4 — Absence is a no-op, never an error

A tier that does not declare `events` in `dlc.toml` still compiles and runs; `platform.Emit` becomes a
no-op. An app must never need to know whether anyone is listening.

This is graceful degradation (§6.5) applied to a push capability, and it is the shape the SQLite index
will need too — `unavailable` must be survivable, not fatal.

### D5 — Events join the parity comparison

Parity currently diffs two things: command results, and the written filesystem. Events become the third.
The native runner records them into a sink; the wasm harness records them from the import it supplies;
both streams are compared as `topic\tbase64(payload)` lines, **in emission order**.

Justification: an event is an observable effect of a command. If native emits `notes.record-changed` and
wasm does not, the tiers have diverged in a way a user would notice and nothing else would catch.

---

## 3. Phases

Each phase is independently verifiable and leaves the tree green. **No phase is done until something can
be broken on purpose and observed going red** (§5).

### Phase 1 — the seam and the platform API ✅

| File | Change |
| --- | --- |
| `wit/ilc.wit` | add `interface events`, `import events` in `world engine` |
| `engine/platform/events.go` | `Emit(topic string, payload []byte)`; `SetEventSink(fn)`; default no-op |
| `engine/platform/caps_native.go` | `//go:build !tinygo` — deliver to the registered in-process sink |
| `engine/platform/caps_wasip2.go` | `//go:build tinygo` — call the generated WIT import |
| `engine/platform/events_test.go` | no-sink emit is a no-op; with a sink, topic + payload arrive verbatim; a panicking sink does not escape into the engine |

**The seam is the deliverable, not the feature.** After this phase `engine/` code can call `platform.Emit`
and both builds compile, with nothing yet listening.

**Landed as planned.** `wit-bindgen-go` generated `events.Emit(topic string, payload cm.List[uint8])`;
`caps_wasip2.go` is four lines over it, `caps_native.go` calls the installed sink. Native builds, vets, and
tests pass; `tinygo -target=wasip2` builds. Four unit tests: verbatim delivery, no-sink no-op, empty topic
dropped, and a panicking sink not escaping into the engine.

**Two things learned that the plan did not anticipate:**

- **An unsatisfied import does not break instantiation.** Adding `import events` to the world was expected
  to force every host to supply it immediately. It does not: parity and the browser still instantiate
  fine, because nothing emits yet and jco/wasmtime only need the function when it is called. That is
  convenient and a trap — the failure will arrive with the *first* emit, in Phase 4, far from the change
  that caused it. **Phases 2 and 3 must wire the import before anything emits.**
- **`SetEventSink` is dead code on wasip2.** The wasm build ignores it entirely — the "sink" is the
  component boundary. It compiles on both tiers because the API must be identical, but on wasm it is
  inert. Documented in `caps_wasip2.go` rather than hidden behind a build tag, because an app author
  reading the platform API should see one function, not two.

### Phase 2 — parity records and compares the event stream ✅

| File | Change |
| --- | --- |
| `cmd/parity-runner/main.go` | install a recording sink; print `EVENT\t<topic>\t<base64>` |
| `verify/parity/harness.mjs` | supply the `events` import to the transpiled component; record identically |
| `scripts/verify-parity.sh` | third `compare` — `wasm-parity [events]` |
| `verify/parity/*-vectors.json` | at least one vector whose handler emits |
| `scripts/verify-parity-selftest.sh` | probe must also diverge the event stream, asserting the new mismatch |

*Falsification:* extend the drift probe so a tinygo-only build emits a different topic, and confirm the new
comparison catches it. **A parity dimension nobody has watched fail is decoration.** Phase 2 precedes any
app usage precisely so that Phase 4 lands under a working check.

**Landed, with two deviations from the plan — both improvements.**

**Deviation 1: events are INTERLEAVED into the result stream, not compared separately.** D5 described a
third stream. Interleaving is strictly stronger and simpler: one run instead of two, and a divergence in
*which command* emitted is caught, not merely the set of events. Each side prints `EVENT\t<topic>\t<base64>`
after the result line of the vector that caused it.

**Deviation 2: `ilc.data-changed` had to exist before parity could test anything.** The plan assumed a
vector could just emit. Nothing emitted, so the platform verbs now do: `import-fs` and `reset-fs` emit once
per command (never per file — a 1000-file bundle must not become 1000 messages), carrying a new
`devalbo.ilc.v1.DataChangedEvent{prefix, method}`. Deliberately **not a diff**: a precise change list would
be a second source of truth a subscriber could act on without re-reading the files, and the files are the
truth (§7.1).

**Phase 1's conclusion was wrong, and this is why.** I wrote that "an unsatisfied import does not break
instantiation". The real reason nothing broke is that **TinyGo dead-code-eliminated the import** — with
nothing calling `events.Emit`, `wasm-tools component wit` showed no `devalbo:ilc/events` at all. The moment
the platform verbs emitted, the import appeared and the harness had to satisfy it. So an unsatisfied import
*would* have failed; there simply wasn't one yet.

**How a host supplies it (the pattern every later capability inherits):** jco turns a WIT import into a
bare module specifier — the transpiled component does `import { emit } from 'devalbo:ilc/events'` — so a
host maps it at transpile time:

```
jco transpile engine.component.wasm -o out --map 'devalbo:ilc/events=../events-sink.mjs'
```

**Falsification is real, and it is the part worth keeping.** The self-test now runs TWO scenarios, because
a probe that breaks all three dimensions at once proves only that *something* is watching:

| Probe | Diverges | Asserts |
| --- | --- | --- |
| `templates` | results + filesystem | `PARITY MISMATCH` + `TREE MISMATCH` |
| `events` | **only** the event stream | `EVENT` in the diff **and** *no* `TREE MISMATCH` |

The second scenario is the one that matters: same commands, same responses, same files — one extra event.
If it goes red, the event comparison is demonstrably what caught it.

### Phase 3 — the web host ✅

| File | Change |
| --- | --- |
| `hosts/web/events.ts` | the mapped `devalbo:ilc/events` module — synchronous, non-throwing, copies the payload out of linear memory |
| `hosts/web/worker.ts` | install the forwarder once; batch a command's events; forward via `Comlink.proxy` |
| `hosts/web/api.ts` | `subscribe(fn): () => void`, main-thread fan-out, `TopicDataChanged` |
| `hosts/web/README.md` | the three rules, the flush ordering, and the transpile `--map` |
| `frontend/src/App.tsx` | subscribe → re-list; **`runImport` no longer calls `refresh()`** |
| `frontend/test/web.spec.ts` | an event repaints the UI with no `refresh()` call |

**The jco detail that will bite:** Spike 5 established that a host import returning a Promise requires
`--async-imports`, which we deliberately do not use (Decision 22 / `WASI-UPGRADES.md`). `emit` returns
nothing and must stay synchronous — the worker fires the proxied callback and returns immediately. If
someone makes it `async`, the failure surfaces as a jco type error far from the edit.

**Landed, with one hazard the plan did not see and two corrections to code written ahead of it.**

**The hazard: an event can outrun the write it announces.** The engine emits *mid-command*; the worker
flushes to OPFS *after* `execute` returns. Forwarding immediately means a listener told `ilc.data-changed`
can call `listFiles` and read a half-flushed tree — a race that would surface as a flaky list, not as an
error. The worker therefore **batches a command's events and delivers them after the flush**, so the host
can promise that *an event never arrives before the change it announces is durable*. Events with no command
in flight (a future watcher or sync) still go out immediately. This is a host-side ordering guarantee, not
a change to D2: `emit` is still synchronous and fire-and-forget from the engine's side.

**Correction 1 — `subscribe` supported exactly one subscriber, and its unsubscribe did nothing.** The
worker called `setForwarder` per subscription (so a second subscriber evicted the first), and `api.ts`
returned a closure that filtered a list nothing dispatched from. Now: the worker holds one listener, the
forwarder is installed once at module load, and `api.ts` keeps the subscriber set and fans out on the main
thread. A React app mounting three subscribing components is the normal case, not an edge one.

**Correction 2 — one relay proxy, registered lazily.** Each `Comlink.proxy` retains a `MessagePort`; one
per subscriber would leak on every mount/unmount cycle.

**Falsification (observed, not assumed).** Dropping the forward in `worker.ts` turns the new test red
*with the file list still stale after an import* — and it also reddens `exports a BFT bundle and re-imports
it` and `import --replace really deletes what the bundle omits`, because deleting `runImport`'s manual
`refresh()` put those two on the event path as well. That is the point of deleting it: the UI is now wrong
if events stop working, so the capability cannot rot unnoticed.

**Left for Phase 4/5:** `new` still refreshes by hand — it does not emit yet. The scaffold template's web
UI is untouched.

### Phase 4 — notes uses it, and the UI stops asking ✅

| File | Change |
| --- | --- |
| `example-apps/notes/proto/notes/v1/commands.proto` | `RecordChangedEvent{id, method}` |
| `example-apps/notes/engine/commands.go` | `emitRecordChanged` on create + delete |
| `example-apps/notes/engine/commands_test.go` | the emitted stream, and that the write lands first |
| `example-apps/notes/frontend/src/main.ts` | `subscribe(...)` → re-list; **both manual `refresh()` calls deleted** |
| `example-apps/notes/frontend/test/driver.ts` | a second writer, outside the UI (not part of the app) |
| `example-apps/notes/frontend/test/web.spec.ts` | the test below |

**The test that makes this meaningful.** Asserting "click create, list updates" would still pass with the
manual refresh, so it proves nothing. Drive the engine *directly*, bypassing the UI's handlers:

```ts
// No UI handler runs here — if the list updates, an EVENT updated it.
await page.evaluate(async () => {
  const { createDirect } = await import("/test/driver.ts");
  await createDirect("From nowhere");
});
await expect(page.getByTestId("count")).toHaveText("2");
```

**Landed. One deviation, from a constraint the plan's snippet could not satisfy.**

**The driver is a module, not an inline import.** `page.evaluate` runs in the BROWSER, which Vite never
transformed — so `import("@devalbo/dlc-web/api")` inside it cannot resolve a bare specifier. The write
therefore comes from `test/driver.ts`, imported by URL (`/test/driver.ts`), which the dev server transforms
on fetch. Nothing in the app imports it, so a production build never sees it; it shares the page's engine
because both modules import the same api URL and ES modules are singletons per graph. This is a stronger
setup than the sketch anyway — a real second writer in the same page, not a call spliced into the test.

**What notes proves that the platform verbs could not.** `ilc.data-changed` is emitted by inherited code;
`notes.record-changed` is an APP's own topic and an app's own payload, defined in the app's own `.proto`
with no registration anywhere — which is the D3 claim ("`notes.` is notes' to spend") actually exercised.

**Two engine-side rules the plan did not state, now tested:** a delete that removed nothing emits nothing
(a no-op is not a change, and counting events would say otherwise), and reads emit nothing at all — an
event per `list` would loop any subscriber that re-lists on an event.

**Falsification (observed).** Commenting out the create emit turns it red in both directions:
`TestMutationsEmitRecordChanged` reports 1 event instead of 2 and `TestEventArrivesAfterTheWrite` finds no
file; after `make build-web`, BOTH browser tests hang at `count = 0`, because with the manual refresh gone
the UI has no other way to learn anything. Reverted, rebuilt, green.

**Note for Phase 5:** notes' `dlc.toml` still declares only `console` + `filesystem`, and events already
work — nothing enforces the declaration yet. That gap is Phase 5's job.

### Phase 5 — document it ✅ (and do NOT declare it)

| File | Change |
| --- | --- |
| `docs/DEVALBO-ILC-GO-PLAN.md` | **Decision 33** — the import shape, no-op absence, reach-vs-announce, parity inclusion |
| `AGENTS.md` | the three emit rules, absence-is-a-no-op, and what `dlc.toml` capabilities mean |
| `README.md` | events 📋 → ✅, plus the cross-tab gap stated as a gap |
| `docs/DEVALBO-DLC-GO-TASKS.md` | Events ticked, with its two follow-ups |
| ~~`templates/component-model/dlc.toml.tmpl`~~ | **dropped** — see below |
| ~~`engine/commands.go` (`--caps`)~~ | **dropped** |
| ~~`verify/scaffold/golden.txt`~~ | **not needed** — nothing the scaffold emits changed |

**The plan said declare `events` in `dlc.toml`. We are not going to, and the reason generalizes.**

Two findings forced it. Empirically, `Tier.Capabilities` is parsed by `hosts/native/manifest.go` and read
by **nothing** — one write, zero consumers. It is already decorative for `console` and `filesystem`, so
adding a third entry would have made the list *look* more authoritative while gating exactly as much as
before: nothing. (A related inconsistency, left alone: `engine/commands.go` validates `--caps` and rejects
anything outside console/filesystem, then writes a hardcoded `capabilities = [...]` into the generated
`dlc.toml` regardless of what was passed.)

And in principle: **capabilities declare what an app can REACH, not what it can ANNOUNCE.** Console,
filesystem, display, the index, network are each inbound data or an effect on something the app does not
own — privileges a host could refuse. `emit` has no return value (D1), so it carries nothing back and
grants nothing; D4 says absence must degrade to a no-op, so it cannot be refused either. **A manifest entry
that gates nothing and can be denied by no one is a comment, not a permission.** That line is now in
`AGENTS.md`, which is the point — it tells the *next* capability which side it is on.

**The strict/lenient knob, considered and deferred with a trigger.** Should a tier lacking a capability
error instead of no-op'ing, configurably? Two reasons not to, here. It contradicts D4 and the first rule in
`events.go`: an app that can tell whether anyone is listening behaves differently per tier, which is the
divergence the architecture exists to prevent. And events cannot exercise the knob anyway — **"absent" is
not a state this capability can be in**: natively, no sink *is* the no-op; on wasm the import is satisfied
at instantiation or the component does not load. There is no third outcome to switch on.

It gets teeth at the **SQLite index**, which §7.1 already says must survive `unavailable` — a capability
genuinely missing at runtime, where strict mode would catch a fallback path quietly becoming the only path.
Build it there, **host-side**, never observable by app code. The hazard it aims at is real and already bit
us once (Phase 1's dead-code-eliminated import looked like a working one); the cheap interim version is a
host-set debug switch that panics on an emit with no sink.

---

## 4. Risks

| Risk | Why it bites | Mitigation |
| --- | --- | --- |
| **Re-entrancy** — host calls `execute` from a callback | engine re-entered mid-command: corrupt state or deadlock | rule in `AGENTS.md`; web host safe by construction (message boundary); consider a debug-build guard |
| **`emit` made async** | jco then needs `--async-imports`, which we refuse | keep the WIT return empty; note in README; failure is a confusing jco type error |
| **Event storms** | per-record emission turns a 1000-record import into 1000 messages | emit per *command*, not per record — `import-fs` emits once |
| **wasip1 / WAMR** | `//go:wasmimport` cannot take Go strings/slices directly | the flat pointer+len shape is compatible, but there is no embedded tier to run it — record as **unverified**, do not claim portability |
| **Ordering across tiers** | async delivery or map iteration could reorder | emit synchronously in handler order; parity compares the stream in order, so a reorder fails |
| **Parity flake** | payloads carrying timestamps or ids would never match | parity vectors must emit deterministic payloads — the host supplies clocks, as `notes` already does for `created_at` |

---

## 5. What this plan does NOT do

- **No subscribe/unsubscribe commands.** The host *is* the subscriber and needs no method id. 300–309 stay
  reserved (`reserved_method_id`) for the day an app must enumerate or filter topics from inside.
- **No event log or replay.** Events are ephemeral. Durability is the filesystem's job (§7.1); conflating
  them would make events a second source of truth.
- **No cross-process delivery.** Same process only: worker → main thread, or native in-process. A second
  tab does not yet receive another tab's events — that needs a BroadcastChannel and is the natural
  follow-up.
- **No desktop tier.** §6.3 names Wails `runtime.EventsEmit`; there is no desktop host to wire it into.

---

## 6. Definition of done — all met

1. ✅ `./scripts/ci.sh all` green.
2. ✅ Parity compares three dimensions (events **interleaved** into the result stream, which catches *which
   command* emitted), and the `events` probe in `verify-parity-selftest.sh` has been watched to fail on an
   injected divergence — reddening the event comparison **without** a `TREE MISMATCH`, so it is
   demonstrably the event dimension doing the catching.
3. ✅ `repaints for a write no UI handler made` — `test/driver.ts` calls the engine, no handler runs, the
   list updates.
4. ✅ No `refresh()` after a mutating command in notes' UI. The three that remain are the initial load, the
   event handler itself, and the function's own definition.
5. ✅ `AGENTS.md` §3 carries re-entrancy, synchronous-emit, emit-after-the-write, absence-is-a-no-op, and
   the reach-vs-announce rule.
6. ✅ `engine/platform/caps_native.go` + `caps_wasip2.go` exist and are the live path on both tiers,
   closing the §5.3 seam task.

**Falsified at every phase, not just asserted:** the parity `events` probe (Phase 2), dropping the web
host's forward (Phase 3 — the browser list goes stale), and commenting out notes' create emit (Phase 4 —
two native tests red, both browser tests hung at `count = 0`). Each was reverted and re-verified green.
