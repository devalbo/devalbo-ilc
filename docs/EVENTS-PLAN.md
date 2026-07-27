# Events — implementation plan (§6.3)

**Status:** Phase 1 landed. Supersedes the outline in `EVENTS-PLAN-PROMPT.md`.

| Phase | State |
| --- | --- |
| 1 — seam + platform API | ✅ **done** |
| 2 — parity records + compares events | ✅ **done** |
| 3 — web host | ⬜ next |
| 4 — notes uses it | ⬜ |
| 5 — declare + document | ⬜ |

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

### Phase 3 — the web host

| File | Change |
| --- | --- |
| `hosts/web/worker.ts` | supply the `events` import at instantiation; forward via `Comlink.proxy`; **never `await` inside the import** |
| `hosts/web/api.ts` | `subscribe(fn): () => void` — returns an unsubscribe |
| `hosts/web/README.md` | the no-re-entrancy rule, and why the import must stay synchronous |

**The jco detail that will bite:** Spike 5 established that a host import returning a Promise requires
`--async-imports`, which we deliberately do not use (Decision 22 / `WASI-UPGRADES.md`). `emit` returns
nothing and must stay synchronous — the worker fires the proxied callback and returns immediately. If
someone makes it `async`, the failure surfaces as a jco type error far from the edit.

### Phase 4 — notes uses it, and the UI stops asking

| File | Change |
| --- | --- |
| `example-apps/notes/proto/notes/v1/commands.proto` | `RecordChangedEvent` |
| `example-apps/notes/engine/commands.go` | emit `notes.record-changed` on create + delete |
| `example-apps/notes/frontend/src/main.ts` | `subscribe(...)` → re-render; **delete the manual `refresh()` calls** |
| `example-apps/notes/frontend/test/web.spec.ts` | the test below |

**The test that makes this meaningful.** Asserting "click create, list updates" would still pass with the
manual refresh, so it proves nothing. Drive the engine *directly*, bypassing the UI's handlers:

```ts
// No UI handler runs here — if the list updates, an EVENT updated it.
await page.evaluate(async () => {
  const { execute } = await import("@devalbo/ilc-web/api");
  await execute(MethodCreateRecord, CreateRecordRequest.toBinary({ title: "From nowhere" }));
});
await expect(page.getByTestId("count")).toHaveText("1");
```

### Phase 5 — declare it, document it

| File | Change |
| --- | --- |
| `templates/component-model/dlc.toml.tmpl` | `capabilities = [..., "events"]` |
| `engine/commands.go` | accept `events` in `--caps` |
| `docs/DEVALBO-ILC-GO-PLAN.md` | Decision entry: shape, no re-entrancy, parity inclusion |
| `AGENTS.md` | never call `execute` from an event callback; `emit` stays synchronous |
| `README.md` | events 📋 → ✅ |
| `verify/scaffold/golden.txt` | re-bless |

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

## 6. Definition of done

1. `./scripts/ci.sh all` green.
2. Parity compares three dimensions, and the event dimension has been **watched to fail** on an injected
   divergence.
3. notes' browser test updates its list from an engine call no UI handler observed.
4. No `refresh()` call remains after a mutating command in notes' UI.
5. `AGENTS.md` carries the re-entrancy and synchronous-emit rules.
6. The `caps_native` / `caps_wasip2` seam exists and is used, closing the §5.3 task.
