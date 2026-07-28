# The host layer — implementation plan (§6.4, §16.6)

**Status: decided, not started.** **Decision 34** is recorded in
[`DEVALBO-ILC-GO-PLAN.md`](./DEVALBO-ILC-GO-PLAN.md) along with §6.4's third render path; the phases below
are the work list. Written in the shape of
[`EVENTS-PLAN.md`](./EVENTS-PLAN.md) — design decisions first, phases that each leave the tree green, and
nothing claimed until it has been broken on purpose.

Where Events answered *how does the engine announce a change*, this answers *who decides what that change
looks like* — and therefore where per-tier code is allowed to live at all.

---

## 1. Why now

`engine/` vs `engine/platform/` drew a line that `hosts/` never got. `AGENTS.md` §3 states it for the
engine — "`engine/platform/` is what every app **inherits**; `engine/` is `dlc`'s own app code" — and the
same sentence has no counterpart one directory over. Empirically:

| Path | What it actually is |
| --- | --- |
| `hosts/web/` | pure inherited runtime — worker, OPFS, Comlink, `api.ts`, the pinned shim. Already `@devalbo/ilc-web`. |
| `hosts/native/` | **mixed** — the in-process engine binding is runtime; `commands.go`, `build.go`, `gen.go`, `manifest.go` are `dlc`'s own app code |
| `frontend/` | `dlc`'s web-tier app code, under a name that says "web UI" rather than "web host" |
| `example-apps/notes/hosts/native/` | app code |
| `example-apps/notes/frontend/` | app code — the same layer as the line above, under a different name |

So notes already ships per-tier app code on two tiers and calls it two different things, and the root repo
has runtime and app code sharing a directory. Nothing is broken today because the only per-tier app code
anyone writes is a UI, and a UI is obviously per-tier. That stops being true the moment a host is expected
to *interpret* an app's events rather than merely repaint on them.

**The forcing case.** A tic-tac-toe engine emits `board`, `turn`, `winner`. A browser draws DOM, an
ESP32-S3 draws a TFT grid, a terminal prints ASCII — three renderings with no visual structure in common,
from one payload that is identical everywhere. §6.4 has no path for this: its two render paths
(draw-command list, retained widget tree) both put presentation in the **app**, branching on the
environment manifest. Host-side interpretation inverts that, and it is better precisely where the state is
small and well-known and the *presentation* is what differs per tier.

Which means per-app, per-tier code becomes a thing people write on purpose — and it is the one place this
architecture permits tiers to differ. It needs a name, a contract, and a rule, before there are three of
them.

---

## 2. Design decisions

### D1 — `hosts/` splits the way `engine/` already did

Two layers, one name each:

| Layer | Portability | Contents |
| --- | --- | --- |
| **host runtime** — inherited | same for every app | instantiate the engine, wire capabilities, deliver events, build requests from argv/forms. `@devalbo/ilc-web`, and a native equivalent. |
| **tier slot** — `hosts/<tier>/`, per app | **per app, per tier** | this app's presentation and input on this tier |

A **tier slot** is the unit: one directory per tier in `dlc.toml`, holding only this app's code for that
tier. `[tiers.web] root = "frontend"` in `notes/dlc.toml` is already this field; the slot just has no
agreed shape behind it.

**Sequencing constraint, stated because it will otherwise look like an oversight:** in the *root* repo,
`hosts/web/` is the runtime and `frontend/` is `dlc`'s slot. Renaming `frontend/` → `hosts/web/` collides
head-on until the runtime is extracted (§16.4). So **notes adopts the uniform layout first** and `dlc`
follows at extraction. `dlc` being an app like any other (`AGENTS.md` §3) is what makes the temporary
asymmetry a debt rather than a design.

### D2 — The app's `.proto` is the interface between an engine and its hosts

Commands in, events out, nothing else crosses. Not a convention — the whole interface.

This reframes what the schema is *for*. It was the platform contract (Decision 28/29); it is also the app's
published API to **its own hosts**, which are now multiple independent implementations. A TFT renderer and
a browser renderer are two consumers of one interface, and today that interface is a string and a byte
array.

Three things follow, and the third is the expensive one:

- The host already embeds the `buf build` FileDescriptorSet for command introspection (Decision 29). Event
  schemas ride the same descriptor set, so a host can **discover** an app's events exactly the way it
  discovers its commands.
- Typed subscriber helpers stop being ergonomics and become the host's API surface.
- **Decision 33's D3 reverses.** It says "topics are not a wire contract the way dispatch ids are." Once a
  host renders from a payload, they are exactly that: change `RecordChangedEvent` and every host drawing it
  breaks. Events therefore want the `method_id` treatment — declared in proto, **and locked**.

### D3 — Host code renders; it never decides

The load-bearing rule. A tier slot may present anything the engine told it, and may compute nothing the
engine could have computed.

*The engine decides `winner`; the host draws it. A host may highlight the winning line the engine named; it
may not find one.*

**Why this fails far from its cause:** parity compares command results, the written filesystem, and the
event stream — **all engine-side**. A tier slot is invisible to it, by construction. So two hosts that each
compute whether a board is won will eventually disagree, on one tier only, with every existing check green.
This is the single place the architecture stops defending itself, which is why the rule is a rule and not
a preference.

### D4 — Semantic events are a THIRD render path, and Display drops to optional

§6.4 gains a third row next to draw-command list and retained widget tree:

| Path | App emits | Who owns presentation | Costs |
| --- | --- | --- | --- |
| draw-command list | `rect(10,10,…)` | app | app branches on the manifest per tier |
| retained widget tree | `column[text, button]` | app structure, host look | host needs a widget vocabulary |
| **semantic events** | `board`, `turn`, `winner` | **host, entirely** | per-app host code, one slot per tier |

**It is already optional, for free, by Decision 33's D4.** A host that does not recognize
`game.state-changed` ignores it, and the app cannot tell. So an app emits semantic events unconditionally
and works with a dumb host (re-read the files, render app-side) and a smart host (draw the board natively)
with no app-side branching whatsoever. This path is **additive**; it replaces neither existing path, and
the right choice is per event, not per app — notes' open-ended record list still wants app-side rendering.

**Therefore Display is optional, and which path to use is the app author's call** — settled, and now
recorded in §6.4. The semantic path costs one small tier slot per tier and *no capability, no new schema,
no new WIT*; draw-list and widget-tree cost a whole capability (protobuf draw ops, a widget vocabulary both
ends agree on, a TFT rasterizer, a React canvas) and earn it only when an app genuinely wants to write
presentation **once** and have every tier inherit it. So the host layer goes first and Display is built
when an app asks for it.

**Knock-on for Decision 32,** worth stating because it changes that decision's urgency rather than its
correctness: the manifest's headline justification was letting a handler branch on display facts, and an
app on the semantic path never learns there is a screen. What remains load-bearing is the non-display half
— is there a SQLite index, what kind of FS root — which is where the strict/lenient knob was already
headed. The manifest survives; "prerequisite for Display" stops being the reason to build it.

### D5 — Cold start is a command, not an event

Events are ephemeral by design (`EVENTS-PLAN.md` §5 — no log, no replay). A host that renders purely from
the stream shows a blank board on reload, or in a second tab opened mid-game.

So the pattern is: **a query command to prime, events as deltas after.** Cheap — it is an ordinary command —
but if it goes unstated someone builds a slot that renders only from the stream and it is blank on
refresh. Stating it also keeps §7.1 intact: the primed state comes from the files, which are the truth.

**The §7.1 tension this creates, and its resolution.** Phase 2 of Events deliberately made
`ilc.data-changed` *not* a diff so a subscriber could not act without re-reading. A board carried in a
payload is precisely acting without re-reading. The resolution is a rule, not a prohibition: an event
payload may be **rendered** but never **written back from**. A render is not a mutation and a stale one
self-corrects on the next event; a write-back makes the event a second source of truth.

### D6 — A tier slot is testable with no engine, and slots are cross-checked against each other

Two properties, both cheap, both scaffolded:

- **Slot without engine.** Feed a slot a synthetic event stream; assert what it renders. This is also the
  answer to "how do I develop the ESP32 rendering without flashing an engine."
- **Host parity.** Feed *two* slots the identical synthetic stream and compare their normalized renderings.
  Two hosts agreeing on a text projection of the same events is a weak claim visually and a strong one
  structurally — it is what catches D3 being violated, because a slot that decides something independently
  is a slot that diverges from its sibling. It is the only automatable check on the layer parity cannot
  reach.

Normalized rendering, not pixels: each slot exposes a text projection of its current view for the test. A
TFT slot that cannot run in CI still emits its projection from a stub driver.

### D7 — Generated apps are disposable, for now

**Re-scaffold rather than migrate.** A template layout change costs a golden re-bless
(`make scaffold-golden`) and re-running `dlc new` — not a migration path for apps already on disk.

This is a deliberate, temporary licence and it is what makes this plan cheap. The layout change in Phase 1
is exactly the kind of thing that becomes permanent by accident: `AGENTS.md` §3 warns that "code copied
into a scaffold is frozen there forever," and `templates/component-model/` **already** ships `frontend/`
alongside `hosts/native/`, so every app scaffolded from today's template inherits the two-names-for-one-
layer problem this plan exists to fix. The cost of correcting it is near zero now and rises with every
generated app.

It expires when apps outside this repo exist — at which point "regenerate / upgrade (re-apply templates
against a newer platform pin)", already in the tasks backlog, stops being a nice-to-have. Until then,
notes is a *fixture*, not a codebase to preserve.

---

## 3. Phases

Each phase leaves the tree green and is independently verifiable. **No phase is done until something can be
broken on purpose and observed going red** (`AGENTS.md` §5).

> **Reordered after Phase 1 (2026-07-27).** The original sequence was: type-and-lock events → harness +
> host parity → pilot → scaffold + document. Three things moved it.
>
> **The pilot came forward, ahead of the typed interface.** D2's case for locking event schemas is *"a host
> renders from the payload, so changing it breaks hosts"* — and no host does that yet: notes re-lists,
> `dlc`'s UI re-reads. Building the lock first meant blessing a format whose first real consumer arrived
> two phases later, which is how a shape turns out wrong once it is expensive to change. The same held for
> D3: *renders, never decides* had no test that could fail, because nothing decided anything. By this
> repo's own standard — a check nobody has watched fail is decoration (`AGENTS.md` §5) — both were claims
> rather than rules. Tic-tac-toe is what makes them testable, so it goes first and the typing follows it.
>
> **Host parity split away from the harness.** They were one phase on the assumption notes could exercise
> both. It cannot: host parity needs two *event-driven* renderers of the same state, and notes' native slot
> is argv in, print out, subscribing to nothing. The harness works today; parity has no subject until the
> pilot exists.
>
> **The old Phase 5 mostly dissolved.** Its documentation half landed with Phase 1 — Decision 34, §6.4's
> third path, the `AGENTS.md` rules — and D7 pulled the template's layout forward with it. What remains is
> scaffolding, which is a tail rather than a milestone.
>
> The cost of this order is one rewrite: tic-tac-toe's subscriber wiring is written raw, then typed. That
> is the cheaper direction.

### Phase 1 — draw the line in `hosts/`

| File | Change |
| --- | --- |
| `hosts/README.md` | the D1 table — runtime vs tier slot, and which existing directory is which |
| `example-apps/notes/hosts/web/` | `frontend/` moves here; `dlc.toml` `[tiers.web] root` follows |
| `example-apps/notes/dlc.toml` | every tier declares its slot `root`; native gains the field it lacks |
| `hosts/native/` | separate the in-process engine binding (runtime, lift-ready — already an open task) from `dlc`'s own `commands.go`/`build.go`/`gen.go` |
| `AGENTS.md` | one sentence: the same split as `engine/`, one directory over |

`dlc`'s own `frontend/` does **not** move — see D1's sequencing constraint.

*Falsification:* point a tier's `root` at a directory that does not exist and confirm the build fails
rather than silently producing a tier with no slot. Today nothing reads these fields (`Tier.Capabilities`
has one writer and zero readers — `EVENTS-PLAN.md` Phase 5), so this phase is where `dlc.toml` stops being
decorative for at least one field.

**Landed 2026-07-27, and the template moved with it.** The plan deferred the template to the scaffolding
phase (then Phase 5, now Phase 6); D7 made that pointless caution — every scaffold emitted until then would have taught the old layout, so
`templates/component-model/frontend/` became `hosts/web/` in the same pass. Golden re-blessed.
`verify-scaffold`, `verify-scaffold-web`, `verify-example-apps`, `verify-example-apps-web` and `ci.sh fast`
are green; notes' two browser tests pass from the new slot, including the events-driven one.

**Falsified as specified:** disabling `checkSlots` turns three of the five new manifest tests red and
leaves the two positive cases green — so the gate is what fails, not the parser.

**Four things the plan did not anticipate, and one that was a latent bug:**

- **`platformPathFrom` ignored its own `subdir` argument.** It hard-coded a single `".."` and was correct
  only by coincidence while every caller was one level down. The web slot is two levels down, so the
  scaffold would have emitted an npm `file:` dependency pointing one directory short — an install
  resolving to nothing, reported nowhere near the manifest that caused it. Now derived from the path depth
  and pinned by a test.
- **`tierOf` got smaller, and that is the point.** It special-cased `frontend/` as the web tier; now
  `hosts/<tier>/` *is* the mapping, so a new tier needs a directory and nothing else. `frontend/` was
  precisely the one directory whose name did not say which tier it belonged to.
- **CI vetted `./hosts/...` but only TESTED `./engine/...`.** The new slot-gate test would have passed CI by
  never running. `ci.sh` now tests the tree it vets — worth noting as a class: a check that does not run is
  indistinguishable from one that passes.
- **`tierNames()` substitutes a `"(none)"` placeholder for an empty map**, so the first draft of the gate
  invented a missing-slot error for a project that declares no tiers at all. Pinned by
  `TestNoTiersIsNotASlotError`.
- **npm's lockfile and `node_modules` had to be regenerated**, not just moved — the `file:` depth changed,
  and a stale lock left `playwright` unresolvable in a way that named neither the path nor the cause.

**Deferred out of this phase:** splitting the in-process engine binding (runtime) out of `hosts/native/`
away from `dlc`'s own `commands.go`/`build.go`/`gen.go`. It is a separate open task ("lift-ready package"),
it touches the CLI's run path rather than the layout, and doing it here would have mixed a refactor into a
move. `hosts/README.md` names the collision explicitly so it is a stated debt, not an oversight.

### Phase 2 — a slot renders, with no engine

| File | Change |
| --- | --- |
| `hosts/testing/` (runtime) | synthetic event driver — replay a recorded or hand-written stream into a slot |
| notes' web slot | a test that renders from synthetic events with **no engine instantiated** |
| each slot | a normalized **text projection** of its current view, for tests to compare |

Works on today's `(topic, payload)` API and needs nothing from Phase 5 — which is exactly why it moved
first. Parity vectors already record event streams (`EVENTS-PLAN.md` Phase 2), so those recordings are the
natural input and keep the synthetic streams honest rather than hand-invented.

This is also the answer to "how do I develop the ESP32 rendering without flashing an engine," which is why
it wants to exist before there is a second renderer, not after.

*Falsification:* break a slot's rendering and confirm its test goes red with no engine in the loop.

### Phase 3 — the pilot: tic-tac-toe across two slots

App #3, not a retrofit of notes. Notes' host does the dumb thing correctly today, and the semantic path
needs an app whose state is small, fixed, and visually tier-specific.

| File | Change |
| --- | --- |
| `example-apps/tictactoe/proto/…` | `GameState{board, turn, winner}`; `StateChangedEvent`; a `GetState` query command (D5) |
| `…/engine/` | move validation, win detection, turn advance — **all** decisions |
| `…/hosts/web/` | DOM grid, subscribes and renders |
| `…/hosts/native/` | ASCII board in the terminal, from the same events |
| `…/hosts/*/…_test` | both slots under the Phase 2 harness |

Built against the **current** string+bytes event API on purpose. Phase 5 types it afterwards, with this app
as the consumer that says what the codegen should emit.

*Falsification:* comment out the engine's `winner` computation and confirm **both** slots go wrong
identically (not one) — that is the proof presentation carries no logic.

### Phase 4 — host parity

| File | Change |
| --- | --- |
| `scripts/verify-host-parity.sh` | two slots, one synthetic stream, compare normalized projections |
| `scripts/ci.sh` | wire it in |

Deliberately **after** the pilot, because it has no subject before one: host parity needs two *event-driven*
renderers of the same state, and notes cannot supply them — its native slot is argv in, print out, and
subscribes to nothing. That was an assumption this plan made and Phase 1 corrected.

*Falsification:* make one slot compute something the engine did not send and confirm the check reddens.
This is the one that matters — it is the only mechanical enforcement D3 will ever have, and until it has
been watched to fail, D3 is a sentence rather than a rule.

### Phase 5 — the published interface: typed and locked events

Folds in the typed-events work, motivated by D2 rather than by typo-avoidance — and sequenced last on
purpose, because D2's argument is *"a host renders from the payload, so changing it breaks hosts,"* and
until Phase 3 no host does. Locking a format before its first consumer exists is how the shape turns out
wrong after it is expensive to change.

| File | Change |
| --- | --- |
| `proto/devalbo/options/v1/options.proto` | `topic` option, declared on the event **message** |
| `cmd/protoc-gen-dlc-registry` | emit Go topic constants + a named `platform.Topic`; emit TS constants and typed `onXxx` helpers; extend the lock to event schemas |
| `proto/method-ids.lock` (or a sibling) | event topics locked — a payload change is a breaking change, per D2 |
| `engine/platform/events.go` | `EmitEvent(msg)` derives the topic from the message; `Emit(string, []byte)` retires |
| `hosts/web/api.ts`, `engine/platform/events.go` | the four hand-mirrored topic literals delete |
| tic-tac-toe's slots | subscriber wiring rewritten onto the typed helpers |

The literals in question — `"ilc.data-changed"` in `events.go:29` **and** `api.ts:107`,
`"notes.record-changed"` in `commands.go:140` **and** `main.ts:111` — are exactly the hand-mirroring
`AGENTS.md` §1 bans for method ids. Events got a pass only because they were designed before that rule had
teeth.

**The known cost:** tic-tac-toe's subscriber wiring gets written twice. Accepted — it is a small app, and
the rewrite is how the codegen's shape gets decided by a real consumer instead of by guesswork. Cheaper
than un-locking a format.

*Falsification:* a mismatched topic/payload pair must stop compiling (`Emit(TopicDataChanged,
recordChangedBytes)` is legal today and uncaught); a changed event payload without a re-bless must fail the
lock; and the existing parity `events` probe must stay red on a diverged stream.

### Phase 6 — scaffold the slot, and ask which tiers

What survives of the original Phase 5. **The documentation half is already done** — Decision 34, §6.4's
third render path, and the `AGENTS.md` rules landed with Phase 1 rather than at the end, and the template's
layout moved there too (D7). What is left is genuinely scaffolding work.

**Tier selection becomes a setup QUESTION, not just a flag.** `--tiers` already decides which slots get
emitted (`tierOf` in `engine/scaffold.go`), and Phase 1 made that choice consequential: a tier is now a
directory of host code plus a `dlc.toml` entry that is checked to exist. "Which tiers?" is therefore the
first thing `dlc new` should ask when it is not told — alongside caps/ui/storage — rather than defaulting
silently and leaving someone to delete a slot by hand.

Host-side by Decision 28, so it may use anything (`huh` menus are already the noted choice for missing enum
args); the engine still receives a resolved `NewRequest` and never prompts, because it also runs in a
browser tab where there is no terminal to prompt on. The web tier's form asks the same question as a set of
checkboxes — same request, two front ends, which is the inversion working as intended.

| File | Change |
| --- | --- |
| `hosts/native/` | prompt for tiers when `--tiers` is absent |
| `templates/component-model/` | the Phase 2 harness + a slot test, wired per tier |
| `verify/scaffold/golden.txt` | re-blessed |
| `docs/DEVALBO-DLC-GO-TASKS.md` | ticked, with follow-ups |

*Falsification:* scaffold an app with two tiers and confirm both slots build and their slot tests pass out
of the box; then delete a slot and confirm `verify` fails rather than producing a tier that silently
renders nothing.

---

## 4. Risks

| Risk | Why it bites | Mitigation |
| --- | --- | --- |
| **Divergence parity cannot see** | tier slots are outside all three parity dimensions, by construction | D3 as a rule; host parity (Phase 3) as the only mechanical check; keep slots thin |
| **Logic creep into slots** | the fastest fix for a rendering gap is always a line of logic in the renderer | host parity catches independent decisions; review rule in `AGENTS.md` |
| **Blank render on cold start** | events are ephemeral; a reload has no stream to replay | D5 — prime with a query command; scaffold it into the template so the shape is inherited |
| **N apps × M tiers** | per-app host code multiplies; five tiers means five renderers per app | semantic path is opt-in per event; the other two §6.4 paths stay for apps that want one renderer |
| **Event payloads become breaking changes** | a host renders from them, so a field change breaks hosts, not just subscribers | D2 — lock them like `method_id`; `buf breaking` covers wire compat, the lock covers identity |
| **Embedded slots unverifiable in CI** | no ESP32 in the runner | text projection from a stub driver (D6); record as **unverified**, do not claim it works |

---

## 5. What this plan does NOT do

- **Not the Display capability.** §6.4's draw-command list and retained widget tree are untouched; this adds
  a third path beside them and builds neither of the other two. Display is now **optional** and waits for an
  app that wants app-side rendering across tiers (D4) — so "not now" here is a decision, not a deferral.
- **No embedded host.** The ESP32-S3 slot is the obvious beneficiary and there is still no WAMR spike.
- **No input capability.** Slots render; symmetric input stays §14 risk 5.
- **No cross-app widget library.** Two apps rendering similar things duplicate; sharing that is a later
  question and a worse one to answer early.
- **Does not move `dlc`'s `frontend/`.** Blocked on the §16.4 runtime extraction — D1.

---

## 6. Definition of done

Order-independent — the reordering above changes when each is earned, not what is required.

1. `./scripts/ci.sh full` green.
2. ✅ Every tier in a `dlc.toml` names a slot, and a missing slot fails the build — `dlc.toml` gates
   something for the first time (Phase 1).
3. An app's event schemas are declared, locked, and generate both the emit side and the subscribe side; no
   topic string is hand-written in Go or TypeScript.
4. A tier slot's rendering is tested with **no engine instantiated**.
5. Host parity has been watched to fail on a slot that decided something the engine did not send — D3 has
   mechanical enforcement, not only a sentence.
6. Tic-tac-toe renders from one engine as DOM and as ASCII, and commenting out the engine's win detection
   breaks **both** identically.
7. `dlc new --tiers web,native` scaffolds working slots with their tests.
