# Sessions and surfaces — implementation plan

**Status: PROPOSED 2026-08-17.** Nothing here is built. Written in the shape of
[`EMBEDDED-PLAN.md`](./EMBEDDED-PLAN.md), [`PAYLOAD-LOADING-PLAN.md`](./PAYLOAD-LOADING-PLAN.md) and
[`WORLD-INPUT-PLAN.md`](./WORLD-INPUT-PLAN.md): decisions first, phases that each leave the tree green,
nothing claimed until it has been broken on purpose.

**The world is a SHELL, not an operating system**, and this plan is about saying that precisely enough to
build.

---

## 0. Where this starts

The badge runs exactly one command and then stops forever:

```
menu -> instantiate -> manifest -> keyboard -> execute ONCE -> render -> rest() for eternity
```

No way back to the menu. No second command. No second app without a power cycle. That is the whole gap,
and naming it that plainly is most of the work — "the badge needs an OS" invites a scheduler, IPC,
drivers and a VFS, and the actual missing piece is a loop.

**What an OS would provide, and what is already here:**

| | status |
| --- | --- |
| memory isolation between app and world | **already have it** — Wasmtime sandboxes the guest completely |
| a filesystem | a catalog today; a real one is `PAYLOAD-LOADING-PLAN.md` |
| device drivers | the world owns them; an app never touches hardware |
| scheduling, preemption, concurrency | **absent, and no consumer** — one core, a synchronous guest |
| process lifecycle | **the gap** |

### The forcing function

**Tictactoe is on the badge and cannot work.** It was flashed on 2026-08-16, sits in the payload region
as `TTT.CWA`, and the current model can only ever run one move of it. It needs `get-state` → render →
input → `play` → render → repeat.

Decision 31 already anticipated this: `MinimalHost::execute` is documented as "callable many times on one
instance". The machinery exists. Nothing calls it twice.

---

## 1. Design decisions

### D1 — A SESSION IS INSTANTIATE-ONCE, EXECUTE-MANY

Instantiation costs 2.9 MB of heap and most of a second. Re-instantiating per command would make an
interactive app unusable and would also throw away the app's in-memory state between turns — tictactoe's
board would reset every move.

So a session is: instantiate, send the manifest, then loop over commands, then tear down and return to
the payload menu. The expensive part happens once.

### D2 — THE WORLD DRIVES THE LOOP; THE APP CANNOT

An app cannot ask for anything. It has one export, it is called, it returns (Decision 31), and it has no
way to say "now show me a board and wait". Any design where the app runs the loop needs the app to call
out, which means an import, which means every tier supplies it — and a browser cannot synchronously block
on a DOM event anyway (Decision 22).

**So the world runs the loop and the app stays request/response.** This is not a compromise forced by the
architecture; it is the property that keeps one artifact working on three tiers.

### D3 — THE LOOP IS A COMMAND SHELL, and it needs no app cooperation

The world already has everything it needs to be a generic shell for an app it was never built for:

| what the shell needs | where it comes from | status |
| --- | --- | --- |
| which commands exist | `GetCommandSurface` (id 4) | **built** |
| what each command takes | `GetCommandSpec` (id 5) | **built** — WORLD-INPUT-PLAN Phase 1 |
| collecting a value | the keyboard | **built** — Phase 2, verified typing `bert` |
| encoding a request | `request::encode_string_field` | **built** |
| showing a result | `stdout`, drained and drawn | **built** |

The turn becomes: pick a command → collect its inputs → execute → show what it printed → repeat.

**Nothing in that list is app-specific**, which is the point. A badge that can do it for `hello` can do it
for an app written next year, because every step is driven by what the app advertises rather than by what
the firmware was compiled against.

### D4 — TWO RENDER CHANNELS: `stdout`, and a RESERVED event vocabulary

The badge cannot render a typed response: decoding protobuf needs the schema, and one loader runs apps it
was never built for. That asymmetry is settled (WORLD-INPUT-PLAN D1) and does not change.

**An earlier draft of this decision said an app that only emits events is "invisible on a badge". That was
wrong, and the correction matters.** An event is a topic string and payload bytes, and the topic is
already human-readable — the badge prints it today:

```
event ilc.environment-changed (2 bytes)
event ilc.status (3 bytes)
```

So the world always knows THAT something happened and what it was called. What it cannot do is interpret
app-specific payload bytes — and there is already a counterexample in the platform proving the way round
that: `ilc.status` is a RESERVED topic with a known shape, three bytes, parseable by any tier through
`ParseStatus`, with no app-specific knowledge anywhere.

So there are two channels, and they answer different questions:

| channel | carries | who can render it |
| --- | --- | --- |
| `stdout` | characters | any world with a screen; unstructured |
| `ilc.*` events | a known shape | any world, including one with only an LED |
| `app.*` events | app-specific bytes | the topic name only |

**The reserved set should grow, carefully.** `ilc.status` is one member; a countdown wants something like
`ilc.progress` (a value and a maximum), which a 320x240 panel renders as a bar and a terminal renders as a
percentage. That is the shape worth adding — structure a world can DO something with, not text it can only
echo. Each new member is a vocabulary every tier must implement, so the bar is high and D6 of
ENVIRONMENT-PLAN applies: no member without a consumer.

**What is genuinely unrenderable** is an app that emits only `app.*` topics and never prints. It gets its
topic names shown and nothing else, which is honest rather than broken.

### D4a — IMPORTS ARE CALLBACKS, so the world CAN repaint while the guest runs

An earlier draft of this decision said nothing is visible while the guest is running. **That is wrong**,
and the correction changes what is buildable.

**An import is host code.** When the guest calls `wasi:cli/stdout.write`, Wasmtime dispatches into our
`SinkStream::write` — synchronously, inside `execute`, while the guest is suspended mid-call. That is not
a new mechanism to build; it is running on every print the badge has ever made. `MinimalState::stdout` is
documented as "SHARED with the stream handed to the guest" precisely because the bytes arrive as they are
written, not afterwards.

So the guest already has a callback into the world. It just does nothing but append to a buffer.

**The browser proves this is not badge-specific.** jco maps `wasi:cli/stdout` to `preview2-shim`, whose
browser build calls `console.log` on each write — and the Playwright test added on 2026-08-16 observes
exactly that, arriving as the guest writes. Live output already works on one tier by accident.

**What actually blocks it on the badge**, and it is ownership rather than architecture: the `Display` is
owned by `main`, and an import handler can only reach `MinimalState`. Giving the store a paint handle is
the whole change.

**What it would cost**, which is the part to respect: repainting per write on a bit-banged parallel panel
is 153,600 bytes a frame. Per-character repaint is unusable; per-LINE, throttled, is the shape that could
work — and it is a measurement nobody has taken.

So `stdout` becomes a STREAM rather than a batch, and "nothing is visible until the command returns" stops
being true. `host.events()` remains a batch — it is drained after — but the same reasoning applies to
`emit`, which is also an import, also host code, and also running mid-guest.

### D4b — LIVE OUTPUT IS SOLVED; LIVE TIMING IS THE OPEN QUESTION

A countdown needs two things: to be SEEN as it counts (D4a: yes, via the import callback) and to be PACED
(the guest must wait a second between ticks). The second is the one still in doubt.

Waiting means `wasi:clocks` and `wasi:io/poll`. The badge wires those through `block_on`, an executor that
**polls once** — deliberately, because "a UART is never not ready from the guest's point of view" and a
chip with no scheduler needs a loop rather than a runtime. A future that resolves immediately is fine for
a stream that is always writable; a sleep that must actually elapse is a different demand and may simply
return.

**So there are two shapes for a countdown, and the choice is empirical:**

| | who owns time | needs |
| --- | --- | --- |
| **world-driven** | the shell calls `tick` ten times | nothing new — works today |
| **guest-driven** | the app sleeps between ticks in one call | a `block_on` that can actually wait |

World-driven is guaranteed to work and is what Phase 4 builds. Guest-driven is more natural to write and
depends on a question worth answering separately: **does a wasip2 sleep elapse on the badge, or return
immediately?** That is a spike, not a plan — measurable in QEMU, and cheap.

### D5 — A SESSION ENDS WHEN THE PERSON SAYS SO

Not when the app says so. An app has no way to express "I am finished" that a generic shell could trust —
and one that could would be a new capability for a question the person in front of the badge can answer
by pressing a button.

`HOME` (gpio 22) leaves the session and returns to the payload menu. It is the one button with no job
today, and "get me out of here" is what a home button means everywhere else.

Tearing down and returning to the menu also means a second app can be run without a power cycle, which is
the other half of what "one command and stop" costs today.

### D6 — ONE APP AT A TIME, AND NO PREEMPTION

Explicitly out of scope, not deferred:

- **Two resident apps** would need two 2.9 MB heaps on an 8 MB budget, and there is no use case.
- **Preemption** would need a scheduler and a way to interrupt a synchronous guest mid-`execute`.
  Wasmtime can do it with fuel or epochs; nothing here wants it.
- **Background apps** need both of the above.

This is the line between a shell and an operating system, and it is where this plan stops. If it is ever
crossed, it should be because something concrete needs it — not because the word "OS" was used.

### D7 — SURFACE OWNERSHIP IS NEGOTIATED, not fixed at flash time

`BADGE_SCREEN=split|full` is a static first cut, and the keyboard currently seizes the whole panel and
repaints it — which is fine for one-shot input and wrong for a session where the app has something on
screen worth keeping.

A session needs a division that holds across a turn:

```
+----------------------------------------+
| world band: app name, status, HOME hint |   1 row, always the world's
+----------------------------------------+
| the app's output                        |   the rest, until input is needed
+----------------------------------------+
| keyboard, when collecting               |   2 rows, borrowed and returned
+----------------------------------------+
```

**Borrowed and returned is the rule.** The keyboard takes two rows while it is collecting and gives them
back — it does not clear the panel. That is what makes `DOWN` (hide) meaningful, which WORLD-INPUT-PLAN §4
flagged as unresolved: today there is nothing behind the keyboard to reveal, and in a session there is.

The app is still told its budget through the manifest (`TextOut.rows`), and re-sending it when the
division changes is exactly what the revision field is for.

---

## 2. Phases

**Phase 1 — the loop, with one command.** Instantiate, manifest, then repeat: collect input, execute,
render, wait. `HOME` exits to the payload menu. `hello` gains nothing but becomes re-runnable, which is
the cheapest possible proof that the session works.

**Phase 2 — the command picker.** More than one command per app, chosen from `GetCommandSurface` filtered
by `GetCommandSpec`. Reuses the menu component. Ends with tictactoe's `new-game` / `play` / `get-state`
selectable.

**Phase 3 — surface ownership.** The world band, the borrowed keyboard rows, and a re-sent manifest when
the division changes. This is where `DOWN` starts to mean something.

**Phase 4 — a countdown, world-driven.** A command called repeatedly BY THE WORLD, rendering between
calls. Guaranteed to work with what exists (D4b), and the first consumer for a reserved `ilc.progress`
event if one is added.

**Phase 4a — streaming `stdout`, if the repaint cost allows.** Give the store a paint handle so
`SinkStream::write` can repaint per line (D4a). This is what makes output live rather than batched, and it
is gated on measuring what a per-line repaint costs on a bit-banged panel.

**Phase 5 — tictactoe on the badge.** The acceptance test for all of it: a full game played with five
buttons.

**Phase 6 — teardown and a second app.** Drop the instance, reclaim the heap, return to the menu, run a
different payload — without a power cycle. Deliberately last because it is the one that can leak.

---

## 3. What this plan does NOT do

- **No scheduler, no concurrency, no preemption.** D6.
- **No app-drawn graphics.** The app prints; the world draws. Giving an app the panel means a display
  capability, a second render path, and a way for a badge-only app to stop being portable.
- **No new capability of any kind.** Everything in D3 already exists and is already exercised.
- **No response rendering.** D4. Unchanged and unsolvable generically.

---

## 4. Open questions

**What the shell shows between turns.** After `execute` returns, the app's `stdout` is on screen. Does
the next turn clear it, append to it, or scroll? Appending needs a scrollback buffer the badge has no room
for; clearing loses the board tictactoe just drew.

**Which reserved events to add, if any.** `ilc.progress` is the obvious candidate and has no consumer
until a countdown exists. Adding it before then is the mistake ENVIRONMENT-PLAN D6 names; adding it after
means the countdown ships rendering as text first and gains a bar later, which is the right order.

**Does a wasip2 sleep actually elapse on the badge?** `block_on` polls once. If a `monotonic-clock` wait
returns immediately, guest-driven timing is impossible on this tier and every timed app must be
world-driven. Measurable in QEMU without hardware, and it decides whether D4b's second column ever exists.

**What a per-line repaint costs.** 153,600 bytes a frame on a bit-banged parallel bus. Streaming output
(D4a) is only viable if a line can be drawn without a full frame, which the current `Display` may or may
not allow.

**How fast the world may drive a loop.** A countdown ticking once a second is one `execute` per second,
and each one costs a Pulley-interpreted call. Cheap for `tick`; not obviously cheap for an app that does
real work per turn, and nothing has measured it.

**Whether a turn can produce no output.** `play` might print nothing and change state. A shell that
clears on each turn would then show an empty screen after a valid move, which reads as a failure.

**How the person picks a command when there are many.** Tictactoe has three; an app with fifteen needs
paging, and that is where a shell starts growing a menu system.

**What happens to guest state on teardown.** The catalog is read-only and there is no filesystem, so a
session's state dies with it. Tictactoe's board would not survive returning to the menu — acceptable, but
it should be a stated property rather than a surprise.

**Whether `HOME` should confirm.** Leaving mid-game loses it (see above). A confirm costs a second press
on the one button whose meaning is "get me out".

---

## 5. Definition of done

1. `./scripts/ci.sh full` green.
2. `hello` runs twice in one session, without a reboot, with different input each time.
3. A full game of tictactoe is played on the badge with five buttons.
4. `HOME` returns to the payload menu from any point in a session, and a DIFFERENT app then runs — no
   power cycle.
5. Heap after returning to the menu and instantiating a second app is within 5% of the first
   instantiation, measured. **A session that leaks is a session that works once.**
6. The keyboard borrows and returns its rows: the app's output is still on screen after input is
   collected, unchanged.
7. An app that prints nothing still shows something — the world says what ran and what it cost, rather
   than a blank panel (already true today; it must stay true per turn).
