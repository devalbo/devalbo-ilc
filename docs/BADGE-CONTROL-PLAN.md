# Driving the badge from a laptop — implementation plan

**Status: PROPOSED 2026-08-17.** Nothing here is built. Written in the shape of
[`PAYLOAD-LOADING-PLAN.md`](./PAYLOAD-LOADING-PLAN.md) and
[`SESSION-AND-SURFACE-PLAN.md`](./SESSION-AND-SURFACE-PLAN.md): decisions first, phases that each leave
the tree green, nothing claimed until it has been broken on purpose.

**The badge can talk. It cannot answer.** This is about the second half.

---

## 0. Where this starts

Everything the badge says arrives through one channel: a buffered, one-way, best-effort log over USB CDC.
It has been enough to build a tier on, and it has a shape of failure that keeps costing whole debugging
cycles.

**Three mis-measurements in one session, 2026-08-17**, all from the same cause:

| what happened | why the log could not settle it |
| --- | --- |
| timed a countdown and read 0 ms of elapsed time | timestamps recorded when the HOST read, not when the badge wrote — the buffer replays on attach |
| a run appeared stuck at stage 2 | the reader was line-buffered and stage lines stay OPEN until `[OK]` |
| the stream froze mid-boot while the badge ran on | aliasing UB in the log buffer; indistinguishable from a hang without asking the badge anything |

Each cost a flash cycle and a guess. None was a hardware problem. The pattern is that **the diagnostic
channel is the thing you reach for when something looks wrong, and a one-way channel cannot tell you
whether it is the badge or itself that is broken.**

There is also a verification gap. `check-embedded.sh` cross-compiles firmware and cannot execute it, so
the badge tier's tests are builds. Everything else — that a payload runs, that input reaches an app, that
a session works — has been verified by a person watching a panel.

---

## 1. Design decisions

### D1 — ONE TRANSPORT, TWO VOCABULARIES

The framing is the same request/response protocol `PAYLOAD-LOADING-PLAN` D7 specifies for WebSerial:
length-prefixed frames with a magic and a checksum, over the CDC port already carrying the log. **Build it
once.** A second framing for control would be a second thing to get right, on the same wire, for the same
reason.

What differs is what the frames say:

| vocabulary | what it is | new schema? |
| --- | --- | --- |
| **pass-through** | `execute(method, request)` → `CommandResult` | **none** — that is already the contract |
| **world control** | press a button, read the screen, list payloads, reboot | yes, and small |

### D2 — PASS-THROUGH NEEDS NO NEW SCHEMA, and that is the point

`execute(u32, list<u8>)` is the whole engine boundary (Decision 31). A control frame carrying a method id
and request bytes, returning the `CommandResult`, is the world relaying what it already does.

**What that buys immediately:** a test can send `count{from: 30}` without pressing a button 25 times, and
assert on RESPONSE BYTES rather than on prose scraped from a log. It is the difference between checking
that the badge printed something plausible and checking that the app returned the right answer.

It also makes the badge a tier the parity runner could reach. Today `verify-parity.sh` compares native
against wasm; the badge is absent because nothing can drive it.

### D3 — WORLD CONTROL IS A SMALL, RESERVED VOCABULARY

Not "expose everything". Each verb has to earn its place by answering a question that has actually cost
time:

| verb | the question it answers | cost paid |
| --- | --- | --- |
| `GetWorldState` | which world, which screen mode, which input mode, what stage | "is it stuck or is the log stuck?" |
| `GetScreen` | what text is on the panel right now | verifying rendering without a camera |
| `PressButton` | drive the menu, the keyboard, the spinner | typing `bert` took a person; a test cannot |
| `ListPayloads` | what is in the catalog, with checksums | already in the boot log, but not askable |
| `SelectPayload` | run a specific app without the menu timing out | 8 s per test otherwise |
| `Reboot` | start clean | exists via picotool; belongs here for symmetry |

**`GetScreen` returns TEXT, not pixels.** The console already renders from a string buffer, so the text is
what the world knows; pixels would need a framebuffer read the driver does not support and would compare
badly anyway.

### D4 — IT IS A CONTROL SURFACE ON A WEARABLE, and it is OFF by default

A channel that invokes arbitrary methods and presses buttons is exactly as powerful as it sounds. Anyone
with the cable gets it.

`BADGE_CONTROL=on|off`, flash-time, **default off** — the same reasoning as `BADGE_INPUT` (D3a of
WORLD-INPUT-PLAN): a world is a CLAIM about what it can do, and "this badge can be driven by whoever
plugs into it" is a claim somebody should have to make deliberately.

Flash-time rather than runtime also means the code is not in a shipped image at all, rather than present
and disabled — which is a weaker property that depends on a check nobody audits.

### D5 — THE WORLD ANSWERS EVEN WHEN THE APP IS RUNNING

The value of this channel is highest exactly when the badge looks stuck, and a control loop that only runs
between commands would go quiet at that moment — which is the same failure as the log freezing, with more
machinery.

The mechanism already exists: [`block_on`](../dlc-platform/embedded/src/block_on.rs) takes a PUMP, called
on every poll while a guest is suspended. Servicing a control frame there is what makes "what stage are
you in" answerable during a hang rather than after it.

**It cannot answer during a tight guest loop that never yields** — a guest computing without printing or
sleeping returns to the host only when it finishes. That is a real limit and it is the same one the
architecture already has (SESSION-AND-SURFACE-PLAN D4a).

### D6 — THIS IS FOR TESTS AND DEBUGGING, not a second app protocol

An app never sees control frames and cannot send them. There is no capability, no import, and nothing an
app can detect — a badge being driven over the cable behaves exactly as one being driven by a person, which
is what makes it a valid test.

**If a test can do something the panel cannot**, that is a bug in the plan: the point is to reach the same
surfaces a person reaches, faster and repeatably.

---

## 2. Phases

**Phase 1 — the framing, and `GetWorldState`.** The smallest useful round trip: ask what the badge is and
what it is doing. Proves the transport in both directions and is immediately worth having.

**Phase 2 — pass-through `execute`.** Drive an app directly. Ends with a test sending `count{from: 30}`
and asserting the response, with no buttons and no menu.

**Phase 3 — buttons and the screen.** `PressButton` and `GetScreen`, which together make the keyboard, the
spinner and the menu testable. Ends with a scripted `bert`.

**Phase 4 — payload control.** `ListPayloads` and `SelectPayload`, so a test picks an app rather than
waiting out a timeout.

**Phase 5 — CI.** A host-side runner that drives a connected badge, skipped when none is attached. This is
where the tier stops being verified by a person watching a panel — and it must SKIP rather than fail on a
machine with no hardware, or it becomes a check people learn to ignore.

---

## 3. What this plan does NOT do

- **No app-visible capability.** D6. An app cannot tell.
- **No pixel readback.** D3 — text is what the world knows.
- **No control in a shipped image.** D4, and it is compiled out rather than disabled.
- **No second framing.** D1 — this is the WebSerial transport with more verbs.

---

## 4. Open questions

**Whether pass-through should bypass the world's input collection.** A test sending `count{from: 30}`
skips the spinner entirely, which is the point — but then the spinner is only tested through
`PressButton`, and the two paths could diverge. Probably fine; worth watching.

**What `GetScreen` returns for the keyboard.** The console renders app text from a buffer, but the
keyboard and spinner draw directly. Either they gain a text representation or the verb answers "not text"
for those screens, which is honest and less useful.

**Whether a hung guest should be interruptible.** D5 says the world answers during a sleep but not during
a tight loop. Wasmtime can bound execution with fuel or epochs; adding it would make the badge
recoverable, and it is the first thing in this whole tier that starts to look like preemption
(SESSION-AND-SURFACE-PLAN D6).

**How a test asserts on a repaint.** Rendering is now live (WORLD-INPUT-PLAN Phase 4a), so a screen read
races the app's output. A test wanting "the panel showed 3" may need the world to record a render
sequence rather than a snapshot.

---

## 5. Definition of done

1. `./scripts/ci.sh full` green, and green on a machine with no badge attached.
2. A test drives `countdown` with an explicit value and asserts the returned bytes — no buttons, no
   menu, no log scraping.
3. A test types a name with `PressButton` and asserts the greeting, matching what a person gets.
4. `GetWorldState` answers **while a guest is sleeping**, and this has been demonstrated against a badge
   mid-countdown.
5. `BADGE_CONTROL=off` builds an image with none of it, and CI builds that mode — a flash-time mode
   nothing builds is a mode that rots.
6. **The three failures in §0 have been re-run against the control channel**, and each is answerable in
   one query. That is the test of whether this was worth building.
