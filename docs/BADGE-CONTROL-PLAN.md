# Driving a world from outside it — implementation plan

**Status: PROPOSED 2026-08-17.** Nothing here is built. Written in the shape of
[`PAYLOAD-LOADING-PLAN.md`](./PAYLOAD-LOADING-PLAN.md) and
[`SESSION-AND-SURFACE-PLAN.md`](./SESSION-AND-SURFACE-PLAN.md): decisions first, phases that each leave
the tree green, nothing claimed until it has been broken on purpose.

**The badge can talk. It cannot answer.** This is about the second half — and about what that unlocks:
a browser that can BE a badge world, checked against the real one.

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

### D1a — THE VOCABULARY IS WORLD-AGNOSTIC; only the transport is a badge

Everything in D3 below — press a button, read the screen, list payloads, report the world's configuration
— is a question about a WORLD, not about an RP2350. Nothing in it mentions a pin, a panel or flash.

That is deliberate, and it is the difference between a debug hatch and an interface:

| | badge | browser world (D7) |
| --- | --- | --- |
| transport | framed over USB CDC | postMessage, or a direct call |
| vocabulary | **the same** | **the same** |

So this is a WORLD CONTROL protocol with a badge transport, not a badge protocol. Naming it otherwise
would guarantee a second, subtly different vocabulary the first time a non-badge world needed driving —
and then the two could not be compared, which is the entire point of D7.

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

### D3a — WORLD ACTIVITY AND APP ACTIVITY ARE DIFFERENT QUESTIONS

`Activity` above says what the WORLD is doing: starting, choosing, collecting,
running, resting. `RUNNING` means the guest is executing — and nothing more. A
countdown on tick 3 of 5 and a bundle import on file 400 of 1000 are both
`RUNNING`, which is exactly the resolution a stuck-or-slow question needs to
distinguish and cannot.

**Only the app knows, so the app has to say.** That makes app activity a second
value with a different owner, not a finer enum on the first.

**THE DEFAULT SHOULD BE AUTOMATIC, not template boilerplate.** The obvious move
is a line in the scaffold — `platform.SetActivity("greet")` at the top of each
handler — and this repo has already learned what that costs: a hand-mirrored fact
is one an app can forget, get wrong, or leave behind after a rename. The clispec
lesson applies directly.

**The dispatcher already knows.** `Execute` routes on a method id and the
generated registry holds the command's name, so "currently running: greet" is
derivable for EVERY app with no app code at all. An app that wants better says
so:

```go
platform.SetActivity("tick 3 of 5")   // refines the default
```

and one that says nothing still reports something true.

**It travels as a RESERVED EVENT, because that channel already reaches the world
mid-command.** `emit` is an import, so host code runs while the guest is
suspended (SESSION-AND-SURFACE D4a) — an `ilc.activity` event arrives as the app
changes it, not when the command returns. Anything else would deliver progress
after the thing it was reporting on had finished.

This is also what `status.go` sketched `Status2` for — its candidate name is
literally ACTIVITY, "a short-term value where the CHANGE is the point". A byte
can drive a blink; a string can say what is happening. They are the same idea at
two resolutions, and the byte should stay for tiers that have only an LED.

**`GetWorldState` then carries both** — what the world is doing, and what the app
last said it was doing. Which is the answer to "is it stuck?" that neither field
gives alone.

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

### D6a — WORLD POLICY IS PORTABLE; ONLY DRIVERS ARE NOT

The browser is not the only second world. An ESP32-S3 with a screen, a desktop app, and a badge all want
the same behaviour and differ only in how pixels reach a person.

**The shared crate is already portable and already proven so.** `dlc-platform-embedded` holds the
catalog, the host, the manifest and request codecs, the clock and the executor, and CI cross-compiles it
for `riscv32imac` precisely because that triple covers the badge's Hazard3 cores AND every RISC-V ESP32.

**The world's policy is not, and that is an accident of where it was written.** `console.rs`,
`keyboard.rs`, `spinner.rs`, `menu.rs`, `report.rs` and `world.rs` live in `rp2350/` and touch no
hardware: they wrap text, fold punctuation, run three-button state machines, and time out. A second
embedded world would copy them, and copies drift silently — the browser would wrap at 40 BYTES where the
badge wraps at 40 CHARACTERS, which is a bug this repo has already had once.

**The boundary is already half-drawn.** Those modules draw through `embedded-graphics`, which is portable
and is what ESP32 boards use anyway. So the extraction is: policy generic over a `DrawTarget` and a
button trait, moved into the shared crate. Every world then supplies two implementations:

| world | drawing | input |
| --- | --- | --- |
| RP2350 | ST7789 over SIO | GPIO |
| ESP32-S3 | its own panel driver | GPIO |
| browser | a canvas, via wasm | clicks |
| desktop | a terminal grid | keys |

**The immediate consequence, for anything built from here:** new world code goes in the SHARED CRATE
unless it names a peripheral. The control protocol in this plan is the first test of that rule — its
handler is policy, so it belongs beside the catalog rather than in `rp2350/`, and a browser world gets it
for free rather than reimplementing it.

**ESP32-S3 IS XTENSA, not RISC-V.** The CI triple covers C3 and C6; the S3 needs the esp-rs toolchain
fork, and whether Pulley builds there is unmeasured. That is a spike of the same shape as the browser
precompiler, and it should be run before anything claims the S3 as a target.

### D6b — A WORLD DESCRIBES WHAT IT CAN DO, NEVER WHO IT IS

Worlds differ, and richer ones should say so. An ESP32 with a 480x320 panel, a desktop window, a browser
tab and a badge are not interchangeable, and pretending they are would waste every one of them.

**The reconciliation with "an app does not know which world it is on" (WORLD-INPUT-PLAN D4) is a single
rule:**

> A capability is described by WHAT IT CAN DO, never by WHO PROVIDES IT.

```
TextOut { cols: 40, rows: 13 }      an app can act on this
is_badge: true                      forks the artifact
```

An app branching on `cols >= 40` is degrading gracefully — §6.5's promise, governed by Decision 33, where
absence is a no-op and never an error. An app branching on `tier == "rp2350"` has become a badge app that
happens to compile elsewhere. The first is the architecture working; the second is the failure it exists
to prevent.

This also keeps the door open for worlds nobody has built. A capability named for its shape is answerable
by a tier invented later; one named for a device is not.

**START GENERIC, AND `screen` IS THE CASE THAT MATTERS.** Today it is text and two dimensions — enough for
an app to size a progress bar or a board, and nothing more. That is deliberate: ENVIRONMENT-PLAN D6 says
a field with no consumer is a branch nothing tests, and richer descriptions (colour depth, pixel size,
input surfaces) should arrive WITH the app that needs them rather than ahead of it.

The order that keeps this honest is the one already used for `TextOut`: an app wants something, the
capability is described in the terms that app needs, and the description outlives the app because it was
never about a device.

### D7 — A BROWSER CAN BE A BADGE WORLD, and this is what makes it checkable

**A world is mostly policy, and only a little hardware.** Sorting the badge world by what a browser could
do:

| | hardware? |
| --- | --- |
| env advertisement (`ILC_TIER`, `ILC_WORLD`, `ILC_STDOUT`) | no — strings |
| the manifest (`TextOut display 40x13`) | no — a message |
| 40x13 text, wrap rules, ASCII folding | no — pure logic |
| keyboard and spinner state machines | no — three booleans in, a value out |
| status colour, timeouts, the payload menu | no |
| ST7789 driver, GPIO, PSRAM, the flash catalog | **yes** |

Almost all of it sits above the hardware line, and **the app already cannot see that line** — D4 of
WORLD-INPUT-PLAN. So a browser world is not a new capability; it is a SECOND IMPLEMENTATION of the same
policy, and an app running in it cannot tell.

**What that buys:** developing badge apps with no hardware, trying one from a link, and — the reason it
belongs in this plan — running badge-world tests in CI, where no badge is attached.

**The risk is a third divergent implementation.** The wrap logic, the fold table, the keyboard's edge
detection and wrapping, the 30-second timeout: all Rust today, all reimplemented in TypeScript, all
drifting silently. The browser would wrap at 40 BYTES while the badge wraps at 40 CHARACTERS and nobody
would notice until an em dash — which is exactly the bug this repo already found once.

Two established answers, and this plan is what makes the first one possible:

- **Parity vectors.** `verify-parity.sh` already compares native against wasm, and
  `verify-parity-selftest.sh` proves the check can detect drift. A browser world is naturally a THIRD
  column — and the control vocabulary (D1a) is what lets the same button sequence drive both, so the
  comparison is between an emulator and the hardware it emulates rather than between two intentions.
- **Shared codegen.** `names/RULES.json` generates the Go and Rust name validators from one spec, so two
  implementations cannot disagree. Wrap and fold are the same shape of problem.

**WHAT CANNOT BE EMULATED, and it must not be quietly implied.** The browser has no 520 KB SRAM limit, no
PSRAM, no 153,600-byte frame cost, and no Pulley interpreter at 150 MHz. `hello` needs 3,168 KB on the
badge; a tab does not care. So this is a FUNCTIONAL emulator, not a performance one: an app that is green
in the browser may not fit, or may be unusably slow, on hardware. Green-in-browser must never be reported
as will-run-on-badge.

### D8 — A WORLD MAY MIRROR ITSELF TO ANOTHER WORLD

The badge runs the app; a browser shows what it is doing, live. Not an emulator
running its own copy (D7) — the SAME run, rendered twice.

**These are different things and the difference matters:**

| | who runs the app | what the browser shows |
| --- | --- | --- |
| emulator (D7) | the browser | its own run |
| **mirror (D8)** | the badge | the badge's run |

The mirror is the easier of the two and independently useful: a demo on a
projector, a teaching aid, a debugging view with more room than 40x13, and a
way to watch a badge that is in someone's hand.

**What is proxied is what the WORLD ALREADY OBSERVES** — the app's `stdout`, its
`ilc.*` events, its status bytes, the current screen text. Nothing new is
extracted from the app, no capability is added, and the app cannot tell (D6). A
world mirroring itself is doing what it already does, twice.

**The protocol consequence is unsolicited frames.** Everything in Phase 1 is
request/response: the host asks, the world answers. Mirroring means the world
speaking without being asked, so the frame format needs notifications as well as
replies — a verb space for events, or a direction flag. Cheap to add now and
awkward to retrofit once callers assume every frame answers a question of
theirs.

**It also makes parity live rather than replayed.** D7 compares two runs of the
same app; a mirror gives ONE run rendered by two implementations, so any
difference is a renderer difference and nothing else. That is a sharper test
than replay, and it falls out of building the mirror at all.

**The bandwidth is fine because the payload is text and events**, not pixels. A
countdown mirrors six lines and six status updates. Anything that wanted to
mirror a framebuffer would not fit, which is another reason `GetScreen` returns
text (D3).

**Practically, it is Phase 1 plus notifications plus a reader.** It cannot come
first: a world that cannot answer a question cannot usefully volunteer one, and
the framing has to work before anything streams over it.

### D8b — THE LOG IS ALSO FRAMED, and text stays anyway

A log line can be a message rather than a string: level, stage, outcome, fields.
Then a test asserts `stage == "manifest" && ok` instead of grepping prose, and the
mirror (D8) needs no vocabulary of its own — log frames ARE the event stream.

**But frames do not replace the text, and the reason is decisive: `BADGE_CONTROL`
is OFF BY DEFAULT (D4).** A shipped badge has no control channel compiled in at
all, so a frames-only log would leave the default build emitting nothing a person
could read.

That contradicts the reason `usblog.rs` exists, stated in its own header: *"a
device with no console is a device you debug by rebuilding it. Give it one before
it needs one."* Four flash cycles went on guesses before that log existed. **A
last-resort diagnostic that needs a decoder is a weaker last resort** — today
`cat /dev/cu.usbmodem` works from any machine with no tooling, which is what made
the badge diagnosable before `badgectl` was written.

So both, on one wire:

| stream | when | who reads it |
| --- | --- | --- |
| text | always, including default builds | a person with a terminal |
| log frames | `BADGE_CONTROL=on` | `badgectl`, a browser world, a test |

The magic already separates them — that is what it is for, and the
"noise before a frame" test already covers prose interleaved with frames.
`badgectl` renders frames back as text, so nothing is lost for tooling users
while the no-tooling path stays intact.

**Two costs worth stating.** Every line becomes an encode plus a frame where it
is currently a `write!` into a buffer — small, but on a firmware that counts
kilobytes. And the log must work BEFORE the control channel is up: stages 1 and 2
run before USB enumerates, which text handles by buffering and frames must handle
the same way.

### D6c — A WORLD CONFIG IS ONE OBJECT, copyable and exportable

The knobs have multiplied: `BADGE_WORLD`, `BADGE_SCREEN`, `BADGE_INPUT`,
`BADGE_CONTROL`, `BADGE_HEARTBEAT_MS`, `BADGE_BEAT_MS`, `BADGE_PAYLOAD`,
`BADGE_REGION`. Each was a reasonable addition; together they are a
configuration with no name, no shape, and no way to write down.

The consequences are already visible. `GetWorldState` returns a `config` map with
THREE of them, chosen ad hoc, because there was no object to return. "What was
this badge built with?" is answerable only by whoever ran `make`. And a world
built a month ago cannot be reproduced without remembering the command line.

**So: one object.** A world's configuration is a message — the same one whatever
the world — that can be reported, copied, saved, diffed and fed back to a build:

```
badgectl -port … -export > badge.world     # what this badge IS
make badge-uf2 WORLD=badge.world           # build that again
```

**That round trip is the point.** A configuration you can read but not reproduce
is documentation; one you can feed back is a fact. It also gives the obvious
answers to questions that are currently awkward: what changed between two
badges, what a bug report should include, and what to hand someone who wants the
same setup.

**TWO HALVES, and they must not be confused.** Some of this is fixed when the
firmware is built and some can move while it runs:

| | example | changes at runtime? |
| --- | --- | --- |
| built | `world`, `screen`, `input`, `control` | no — compiled in or out |
| set | heartbeat interval, later a subscription | yes (D8c) |

A single object reports both, marked, because "what could this badge do" and
"what is it doing" are different questions and answering them in one field is how
they get conflated.

**It is the same object for a browser world (D6b/D7)** with different values —
which is what makes two worlds comparable at all, and what stops the browser
growing its own vocabulary for the same facts.

**Where it does NOT go:** into the app's `dlc.toml`. That declares what an APP
needs; this declares what a WORLD provides. They meet at the manifest, and
merging them would put a badge's screen layout in an app's build file.

### D8c — A HEARTBEAT, so that SILENCE MEANS SOMETHING

The failure this answers cost an hour on 2026-08-17. Every diagnostic here is
"the world says things"; nothing says "the world is still here". So silence had
four possible meanings — hung, finished, starved, or a reader whose cursor was
already at the end — and no way to tell them apart without reflashing.

**A periodic beat collapses that.** If it stops, the world stopped. If it
continues while nothing else arrives, the world is alive and the silence is about
something else. That is a different claim from anything the log makes, and it is
the one that was missing.

**The text stream already has one, by accident.** The log rewinds and
re-transmits when it has nothing new (usblog.rs), so a `cat` user sees the run
repeat — which IS a liveness signal for someone with no tooling. This decision is
the structured equivalent for a control client: uptime, world activity, app
activity, in a frame.

**THE WORLD DECIDES, and it must be able to say no.** A badge on battery should
not transmit forever, and a shipped badge with no control channel emits nothing
at all. So:

| | |
| --- | --- |
| default | flash-time, with the world's other choices (`BADGE_*`) |
| runtime | a control verb turns it on or off |
| locally | eventually a settings item in the world's own UI |

The last one is a bigger idea than it looks: the world does not yet HAVE a UI
beyond the payload menu, and "a screen the world owns for its own settings" is
the beginning of one (SESSION-AND-SURFACE D7's surface ownership).

**It is a subscription with a timer**, which is why it should not be built before
subscriptions exist. A world that pushes a beat nobody asked for is the same
mistake as pushing log frames nobody asked for — that filled the outbound queue
and starved the text log, and the fix in both cases is that a world stays SILENT
until a client asks.

**The rate is a trade nobody has measured.** Once a second is invisible on USB
and meaningful to a human watching; once a minute is cheap and useless for
spotting a hang. It should be settable by whoever subscribes rather than fixed by
the world, since the subscriber is the one who knows what it is watching for.

### D8a — MIRRORING IS A SINK LIST, and that is the payoff of framing

**A frame names no transport.** `control::frame()` takes bytes and returns bytes;
USB CDC is simply the first sink it was pointed at. A UART is another byte
stream, and so is a radio, a pipe, or a file.

So proxying a world's events to another world is a CONFIGURATION question — which
sinks — rather than a code question. That is the return on choosing framed
protobuf over ad-hoc text: a text log can be read by a person and demultiplexed
by nobody, while a framed message can be teed anywhere and parsed identically by
every consumer, including ones written later in other languages.

The machinery is already in place: `Tee` writes one stream to two sinks, and the
magic exists precisely so frames survive sharing a wire with prose.

**TWO THINGS ARE NOT FREE, and calling them free would be the mistake:**

**An event needs a message.** Everything so far is request/response — the world
answers what it is asked. An event is unsolicited (D8), so there must be a
notification frame carrying topic and payload. Small, and it does not exist yet.

**A SLOW SINK MUST NOT STALL THE APP.** This is the real constraint. USB is
interrupt-driven and buffered, so a write returns immediately; a UART at 115200
does not, and a blocking write inside an event emit would suspend the GUEST —
`emit` is an import, so the app is waiting inside it. A mirror that pauses the
app it is mirroring has changed what it observes.

The discipline that follows: mirrored output is queued and dropped when the queue
is full, never blocked on. **Losing a frame is better than changing the timing of
the thing being measured**, and a dropped-count in the next frame keeps the loss
visible rather than silent.

Bandwidth is otherwise a non-issue: a countdown mirrors six lines and six status
updates. Anything wanting to mirror pixels would not fit, which is another reason
`GetScreen` returns text (D3).

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

**Phase 5 — CI against hardware.** A host-side runner that drives a connected badge, skipped when none is
attached. This is where the tier stops being verified by a person watching a panel — and it must SKIP
rather than fail on a machine with no hardware, or it becomes a check people learn to ignore.

**Phase 6 — the browser world (D7).** A second implementation of the world's policy, in the existing web
tier, speaking the same control vocabulary over a different transport. Ends with badge apps runnable
without hardware.

**Phase 6a — the mirror (D8).** Unsolicited frames, and a browser page that
renders a live badge's output and events. Independently useful before the
emulator is finished, and it turns parity from a replay into a comparison of two
renderers on one run.

**Phase 7 — world parity.** The same command sequence driven against both, compared. This is what stops
the two worlds drifting, and it is only possible because Phases 1–4 gave the hardware a way to answer.

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

**Where the shared policy should live.** D7 offers parity vectors or codegen. A third option nobody has
priced: compile the Rust policy (`console`, `keyboard`, `spinner`) to wasm and have the browser world
call it, so there is ONE implementation rather than two agreeing ones. The precompile spike proved a much
larger crate builds for wasm32; this one is tiny.

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
7. A browser world and a real badge, driven by the SAME command sequence, produce the same screen text
   for `hello` and `countdown` — and the comparison has been made to fail on purpose by changing one
   wrap rule in one of them.
