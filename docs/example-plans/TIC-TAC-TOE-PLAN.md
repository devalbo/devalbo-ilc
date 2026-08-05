# Tic-tac-toe — an example build plan

**Audience: a coding agent building this app with `dlc`.** Everything needed is here: the rules, the schema,
the command surface, the build order, and the exact invocations. Where something is a genuine choice, §8
says to ask rather than guess.

**The app:** one game of tic-tac-toe, playable **from a terminal and from a browser**, with the same engine
behind both. Small on purpose — the interest is not the game, it is that two front ends with nothing
visually in common are driven by one piece of business logic that neither of them can second-guess.

**§10 adds a third platform — a handheld badge with a TFT screen and five buttons — and it is IN SCOPE**, as
**two tiers**: the engine linked natively, and the *same component every other tier runs* under Wasmtime's
Pulley interpreter — on the same board. §§1–9
stand alone and come first. Read §10 before starting, though: one small platform fix is a prerequisite
(§10.1a), and knowing that up front is cheaper than meeting it as a build failure.

> **This app already exists in this repository** at `example-apps/tictactoe/`, built during the host-layer
> work. That makes this plan useful in two ways, and you should know which one you are doing:
>
> 1. **As an exercise** — build it fresh from this plan alone, scaffolding under a *different* name, and
>    then diff against the existing one. That is a test of the **plan**, and the only plan here that can be
>    validated that way. **Do not read `example-apps/tictactoe/` while building** if this is the mode; it is
>    the answer key.
> 2. **As a reference** — you are reading this to understand how an ILC app is put together, and the
>    existing app is the worked answer.
>
> If `example-apps/tictactoe/` exists and nobody said which mode this is, **ask** (§8).

---

## 1. Read these first

| Document | Why |
| --- | --- |
| [`AGENTS.md`](../../AGENTS.md) | the rules. §1 method ids · §2 the engine · §3 the platform boundary · §3a the host layer · §3b the generated command surface · §5 verification · §6 working style |
| [`DEVALBO-ILC-GO-PLAN.md`](../DEVALBO-ILC-GO-PLAN.md) §6.4, §8 | the three render paths; how a command reaches a handler |
| [`HOST-LAYER-PLAN.md`](../HOST-LAYER-PLAN.md) | what host code may and may not do — §4 below is the short version |

**The one rule this app exists to test**, so it is at the top rather than buried:

> **The engine decides; a host renders.** Whose turn it is, whether a move is legal, who won, and *which
> line* won are engine answers. A browser slot must not work any of that out, even though it trivially
> could. Two hosts that each derive a winner will eventually disagree on one tier only, with every existing
> check green — which is why §7 Phase 5 makes a slot's independence mechanically checkable.

---

## 2. The game

**State:** nine squares, each empty or `X` or `O`. Squares are numbered **1–9**, left to right, top to
bottom — human-facing numbering because a person types them.

```
 1 │ 2 │ 3
───┼───┼───
 4 │ 5 │ 6
───┼───┼───
 7 │ 8 │ 9
```

**Rules the engine owns, and no host may reimplement:**

| Rule | Detail |
| --- | --- |
| `X` moves first | so the turn is derivable from the board alone (D2) |
| a move must be on an empty square | otherwise the command fails; the state does not change |
| no move once the game is over | a win or a full board ends it |
| a win is three in a row | eight lines: 3 rows, 3 columns, 2 diagonals |
| the engine names the winning **line** | not just the winner — a host may highlight what the engine named, and may not go looking (§4) |
| a full board with no line is a **draw** | distinct from "still playing" |

**Storage:** one file, `game.json`, under the app's granted root. `new-game` overwrites it.

**Not split storage** (contrast `example-apps/notes/`): a game is a single document, not a collection, so
there is nothing to index and no per-record file. An app that models one thing should store one thing.

---

## 3. Decisions already made

Do not re-litigate these. If one turns out to be wrong while building, say so and stop.

### D1 — Turn and outcome are DERIVED, never stored

`game.json` holds the board and nothing else. Turn is `X` when the board has an equal number of marks and
`O` otherwise; the outcome is computed from the board on every read.

A stored `turn` is a second source of truth that goes stale the moment a move is written, and nothing would
catch it. The same argument retires a stored winner.

**They are still part of every ANSWER.** A response and an event both carry `turn`, `outcome` and
`winning_line` as computed fields — that is the engine doing the deciding, in the one place it belongs.
Deriving-then-sending is not the same as storing: one is the engine answering, the other is a fact that can
rot.

### D2 — One semantic event carrying the whole state

Topic `game.state-changed`, payload = the full state (board, turn, outcome, winning line).

**This is §6.4's third render path**, and tic-tac-toe is the app it exists for: a board as DOM, as terminal
ASCII, and as a grid on a TFT share no visual structure at all, but the *payload* is identical everywhere
and tiny. So the app ships no presentation, and each host draws what it likes.

**Why the whole state and not a diff** — and note this differs from the sibling plan on purpose:
[`DONEBLOCK-EXAMPLE-PLAN.md`](../DONEBLOCK-EXAMPLE-PLAN.md) D6 emits a bare "something changed" because a
task graph is too big to put in every event. A 9-cell board is not. **The rule is per event, not per app**
(Decision 33): send state when it is small, send an invalidation when it is not.

### D3 — Cold start is a command, not an event

Events are ephemeral. A slot that renders only from the stream shows nothing on load, so **both slots
prime with `state` and then take events as deltas**. This is Decision 34's rule and the most common way a
new slot is subtly wrong: it works while you click, and it is blank after a refresh.

### D4 — Two slots, sharing nothing

`hosts/native/` prints ASCII. `hosts/web/` renders DOM. **No shared rendering code**, deliberately: the
whole claim is that presentation can differ per tier while behaviour cannot, and a shared renderer would
prove nothing.

### D5 — The app is single-user and stores no identity

No user concept, no `created_by`, no clock. If you want to know when a game was played, that is a host-side
concern and out of scope here — see `AGENTS.md` §3·5 and the sibling plan's D11/D12 for why identity is a
host matter.

---

## 4. What the engine owns, and what a host may do

| Engine | Host slot |
| --- | --- |
| whose turn it is | how a turn indicator looks |
| whether a move is legal | whether an illegal square is greyed out **after the engine refused it** |
| the outcome, and which line won | drawing a line through squares the engine named |
| the text of every error | where the error appears |

A host may render what the engine sent. It may not compute what the engine did not.

**The trap, stated concretely because a browser makes it so easy:** a DOM slot has the board in hand and
three lines of JavaScript would find a winner. Writing them is the bug this app is built to catch — see the
decision probe in Phase 5.

---

## 5. Schema

`proto/tictactoe/v1/commands.proto`. Package `tictactoe.v1`, `go_package` ending `;tictactoev1`.

**Method ids: 10000+ is yours** (1–9999 is the framework's). Permanent once written;
`proto/method-ids.lock` is committed and the build fails on a change.

| id | rpc | CLI | Positional | Flags |
| --- | --- | --- | --- | --- |
| 10000 | `GetState` | `state` | — | — |
| 10001 | `Play` | `play` | `<square>` (1–9) | — |
| 10002 | `NewGame` | `new-game` | — | — |

Messages:

```proto
// What is in a square, or whose turn it is. ONE representation of "nothing",
// and it is the zero value — proto3 defaults to zero, so a separate EMPTY would
// mean an unset square said UNSPECIFIED while the engine wrote EMPTY, and every
// reader would have to know the two were the same thing.
enum Mark {
  MARK_UNSPECIFIED = 0;  // empty
  MARK_X = 1;
  MARK_O = 2;
}

// How the game stands, as ONE value the engine computed.
enum Outcome {
  OUTCOME_UNSPECIFIED = 0;
  OUTCOME_IN_PROGRESS = 1;
  OUTCOME_WINNER_X = 2;
  OUTCOME_WINNER_O = 3;
  OUTCOME_DRAW = 4;
}

// The stored document — the board and nothing else (D1).
message Game {
  repeated Mark board = 1;   // exactly 9, index 0 = square 1
}

// What every answer carries. `turn`, `outcome` and `winning_line` are COMPUTED,
// which is why they live here and not in Game.
message State {
  repeated Mark board = 1;
  Mark turn = 2;                     // UNSPECIFIED once the game is over
  Outcome outcome = 3;
  repeated uint32 winning_line = 4;  // the three squares, 1-based; empty if no win
}

message StateChangedEvent {
  option (devalbo.options.v1.topic) = "game.state-changed";
  State state = 1;
}
```

**`Outcome` is ONE field, not a `winner` mark plus a `draw` bool plus an `over` bool.** This is worth
dwelling on because the multi-field version is the obvious first draft and it is wrong twice over:

- It makes every slot interpret a **combination** — "winner set? else draw? else in progress" — which is a
  small decision, repeated in each slot, that two of them can make differently. That is precisely the
  divergence §4 exists to prevent, smuggled in as a data shape.
- It lets **nonsense onto the wire**: a winner *and* a draw is representable, and nothing would reject it.

One enum makes the illegal states unrepresentable and leaves a slot with a `switch` over values the engine
named. **Prefer that shape over a set of booleans anywhere the engine is stating a judgement.**

**`repeated Mark board`, not `bytes`:** canonical JSON renders it as `[1,2,0,…]` where bytes would be
base64, and §7.1's promise that a human can read the store without the app is the reason to store it this
way at all.

`square` is `uint32` with `(cli_positional) = 1` so `play 5` reads naturally, plus `help` text. The engine
validates the range; do not rely on the CLI to do it, because the browser sends the same field.

---

## 6. Mechanics

Run inside `devbox shell`, or prefix with `devbox run --`.

```bash
# from the repo root
BIN="$(mktemp -d)"
go build -buildvcs=false -o "$BIN/dlc" ./hosts/native

cd example-apps
"$BIN/dlc" new tictactoe --tiers native --tiers web \
  --module github.com/devalbo/devalbo-ilc/example-apps/tictactoe \
  --platform-path ../..

cd tictactoe && PATH="$BIN:$PATH" make gen && go mod tidy && make verify
```

Flags may come before or after the positional name — that was fixed on 2026-07-29 along with the
scaffolded `devbox.json`, which now installs both codegen plugins. `--tiers` is **repeatable**, not
comma-separated, and takes only `native` or `web`: **the badge tier is added by hand afterwards** (§10.1). `--platform-path` is written into the generated `dlc.toml` verbatim, so it must be
relative to the *new project*: from `example-apps/`, `../..` is right.

| Task | Command |
| --- | --- |
| regenerate proto + registry + `dlcconfig` | `make gen` |
| build the native CLI | `make build` |
| unit tests | `go test ./...` |
| native smoke test | `make verify` |
| build the web tier | `make build-web` (needs `dlc` on `PATH`) |
| serve the web tier | `make dev-web` |
| browser tests | `cd hosts/web && npx playwright test` |
| the example-app suites | `./scripts/verify-example-apps.sh` · `./scripts/verify-example-apps-web.sh` |

**Never run `git`** — the maintainer runs every git command. **Never `go build ./cmd/x` bare** — it writes
the binary into the current directory; use `-o "$(mktemp -d)/x"`.

---

## 7. Build order

Each phase leaves the tree green, and **no phase is done until something is broken on purpose and observed
going red** (`AGENTS.md` §5).

### Phase 0 — scaffold and prove it runs

The invocation in §6. Do not proceed until `make verify` prints a version and a greeting. A scaffold that
does not run is a framework bug, not your bug — report it rather than working around it.

### Phase 1 — the rules, with no persistence

The engine, in Go, entirely under `engine/`: a board, legality, turn, winner, winning line, draw. Unit
tests only — no files, no CLI. **This is where all the thinking is**, and it is the smallest thing that
can be completely correct.

*Falsify:* break win detection on one diagonal and watch a test fail. A test suite that passes with a
diagonal missing is not testing eight lines.

### Phase 2 — `state`, `play`, `new-game` over one file

Wire the three commands, persist `game.json`, emit `game.state-changed` after every mutation.

*Falsify:* play on an occupied square → the command fails **and the file is unchanged**. Play after a win →
refused. Both matter: a rule that rejects the command but writes anyway is worse than no rule.

### Phase 3 — the terminal slot

`hosts/native/` prints the board as ASCII, marks the turn, announces a win and names the line. Prime with
`state` (D3).

*Falsify:* delete `platform.RegisterAll()` from the engine's init and watch the failure — it is
`unknown method_id 1` at **run** time, not a compile error, which is worth seeing once.

### Phase 4 — the browser slot

`hosts/web/` renders a DOM grid, clicking a square calls `play`, and an event repaints with no explicit
refresh. Prime with `state` on load.

*Falsify:* reload mid-game. If the board is blank, the slot is rendering only from events (D3).

### Phase 5 — host parity, including the decision probe

Two slots, two languages, no shared code: feed both the same synthetic states and compare normalized
renderings. Cover at least an empty board, a move in progress, a win, and a draw.

**Then the probe that gives this app its point:** hand both slots a state whose board contains three in a
row while `outcome` is `IN_PROGRESS` — a thing the engine would never send. **Neither slot may announce a
winner.** A slot that does is deriving rather than rendering, and this is the only check that can see it.

*Falsify:* make one slot compute the winner itself, and watch the probe go red. If it stays green, the
probe is not wired to anything.

---

## 8. Ask, do not guess

1. **Exercise or reference?** (see the note at the top) — if `example-apps/tictactoe/` already exists,
   which mode is this, and what should the new app be called?
2. **Does the browser need a "new game" button**, or is the terminal's `new-game` enough for now?
3. **Two human players only?** A computer opponent is a substantial addition — it is engine logic, so it
   would be shared by both tiers for free, but it is not in this plan.
4. **Nothing about the badge is open.** It is in scope (§10) and it does not persist (§10.4). What remains
   unknown is whether the interpreter fits in RAM, and that is a measurement, not a decision: run P0.

Anything requiring a **framework capability that does not exist**: stop and ask. Do not add a capability to
the platform from inside an app.

---

## 9. What this app is worth to the framework

It is a small app, and these are the claims it puts under load — worth writing down if any of them turns
out to be false:

- **Two front ends, no shared presentation, one engine.** The central claim of the whole architecture, in
  the smallest app that can demonstrate it.
- **"A host renders, it never decides" is mechanically enforceable.** Phase 5's probe is the only kind of
  check that catches a slot that got clever, and it caught a real mismatch the first time it was run on the
  existing implementation.
- **The semantic render path costs almost nothing.** No Display capability, no draw lists, no widget
  vocabulary — one small event and one slot per tier (§6.4, Decision 34).
- **A single-document app is a legitimate shape.** No split storage, no index, no collection. If the
  framework pushes you toward per-record files anyway, that is a finding.

And if §10 is built, three claims that are currently only *written down* get tested for the first time
anywhere in this repository:

- **The events import survives to core wasm** — `ilc.wit` marks that UNVERIFIED today.
- **A capability can genuinely be absent at run time** — `HasFilesystem() == false`, on a device where it is
  a real answer rather than a stubbed one.
- **The semantic render path reaches a screen that shares nothing with a browser**, which is the case §6.4
  was written for and the reason Display stayed optional (Decision 34).

---

## 10. Embedded platforms — three more tiers (in scope)

Two boards, three tiers, one engine. §§1–9 come first and stand alone; nothing below touches the engine.

| Tier | Board | Engine | Host | Screen |
| --- | --- | --- | --- | --- |
| **`rp2350-tinygo`** | Tufty (RP2350B) | TinyGo, linked directly | Go | 320×240 TFT |
| **`rp2350`** | Tufty (RP2350B) | the shared **component**, AOT'd to Pulley bytecode | Rust `no_std` | 320×240 TFT |
| **`rp2040-tinygo`** | KB2040 (RP2040) | TinyGo, linked directly — **no wasm possible** | Go | **none** |

### 10.0 The boards

**[Adafruit 6463](https://www.adafruit.com/product/6463) — Badgeware Tufty**, a wearable badge (Pimoroni).

| | |
| --- | --- |
| SoC | **RP2350B**, dual-core Cortex-M33 @ 250 MHz |
| Memory | 520 KB SRAM · **8 MB PSRAM** · 16 MB QSPI flash (XIP) |
| Display | 2.8" IPS TFT, **320 × 240**, adjustable backlight |
| Input | **five front buttons**, plus reset/sleep and home/boot |
| Radio | WiFi (RM2 / CYW43439) · Bluetooth 5.2 |
| Other | LiPo + USB-C, 4-zone case LEDs, phototransistor, **RTC (PCF85063A)**, STEMMA QT I²C |
| Stock toolchain | MicroPython (Pimoroni); vendor display libraries are C++ |

**[Adafruit 5302](https://www.adafruit.com/product/5302) — KB2040 "Kee Boar"**, a Pro-Micro-shaped keyboard
driver board, $8.95.

| | |
| --- | --- |
| SoC | **RP2040**, dual-core Cortex-M0+ @ ~125 MHz |
| Memory | **264 KB SRAM** · 8 MB SPI flash |
| Display | **none** |
| Input | reset and boot buttons only — but **20 GPIO** designed for key matrices (up to 100 keys) |
| Other | USB-C, RGB NeoPixel, STEMMA QT I²C, 4× 12-bit ADC |

**Why this board, for this app.** §6.4's semantic render path claims one payload renders as DOM, as terminal
ASCII, and as a grid on a TFT. Two of those exist, and they are the easy two — both text-shaped, both in
this repo's idiom, both written in the same sitting. The Tufty adds a C++ host drawing pixels; the KB2040
adds a host with **no screen at all**, which is the case that finds out whether "the app ships no
presentation" was ever really true.

**Nothing about the game changes on any tier.** Same `.proto`, same `method_id`s, same rules, same engine
source. If a rule needs restating for a tier, the architecture has failed and that is the finding — not a
patch.

### 10.1 The Tufty is TWO tiers, not a gate: `rp2350-tinygo` and `rp2350`

The earlier framing of this section was an either/or — a wasm runtime or native TinyGo, decided by a spike.
That was the wrong shape. **Decision 27 defines a tier as "the shared engine × a host/environment binding + ABI mode
+ cap set — not a per-tier fork of the logic."** One board running two runtimes is two host bindings over
one engine, which is exactly a tier apiece:

| Tier | Engine | Host | ABI |
| --- | --- | --- | --- |
| **`rp2350-tinygo`** | TinyGo compiled for RP2350, **linked directly** — no wasm | Go, with TinyGo display/GPIO drivers | direct Go calls |
| **`rp2350`** | the **same `engine.component.wasm` the browser runs**, AOT-compiled to a `pulley32` `.cwasm` | Rust `no_std`, Wasmtime + Pulley, `rp235x-hal` | WIT imports, satisfied by a hand-written host |

**Build `rp2350-tinygo` first.** It has exactly one unknown — does the pinned TinyGo target RP2350 — where
`rp2350` has a measured blocker: the component AOTs to ~890 KB against 520 KB of SRAM, so the badge needs
its PSRAM allocator first. Doing native first means the badge *works* while that is still open, and it
gives the interpreted tier a running reference to compare against instead of a blank screen.

**Why build the second one at all — the payoff is a check that does not exist today, and it got stronger.**
`verify-parity` compares native Go against a wasip2 component on a developer's machine. Two badge tiers
compare **the shared component against a natively linked engine, on the same hardware**. Under the old WAMR
plan the two tiers shared only *source*; now they share the **artifact**, so a disagreement can no longer be
blamed on a second build.

**The cost: the presentation is written twice**, in Go and in Rust — see §10.1e, which is the honest treatment
of whether it has to be. Short version: the two hosts share the engine and nothing else, so the TFT drawing
and button reading exist in both languages. That is not waste under this
architecture (two independent slots rendering the same engine answers is precisely what host parity checks)
but it **is** two display drivers, and it is the main reason to sequence rather than attempt both at once.

### 10.1a A platform gap that blocks `rp2350-tinygo` — the caps seam has only two of its three files

**Found 2026-07-29 by reading the code**, and it would otherwise surface as a mystifying build failure at
B1.

§5.3 says to keep the capability seam in `caps_wasip2.go` / `caps_wasip1.go` / `caps_native.go`, and §5.6
repeats it. Only **two** exist, and their build tags are:

```go
// dlc-platform/caps_native.go
//go:build !tinygo        // direct Go call
// dlc-platform/caps_wasip2.go
//go:build tinygo         // WIT import
```

Those tags conflate **"TinyGo targeting wasm"** with **"TinyGo targeting a microcontroller"**. A
`rp2350-tinygo` build is TinyGo, so it would select `caps_wasip2.go` and try to import WIT event bindings
that have no meaning on bare metal.

**What it needs:** a discriminator finer than `tinygo`. TinyGo sets a `baremetal` build tag for
microcontroller targets, which is the obvious candidate — `tinygo && !baremetal` for the WIT path,
`tinygo && baremetal` for direct calls — but **verify it against the pinned TinyGo rather than trusting this
paragraph**. Only two answers are needed — `rp2350` runs the wasip2 component unchanged, so there is no
third `//go:wasmimport` shape to discriminate.

**This is platform work, not app work** (`AGENTS.md` §3): fix it in `dlc-platform`, not from inside the app.
It is small, it is a prerequisite for `rp2350-tinygo`, and it is recorded in `DEVALBO-DLC-GO-TASKS.md`.

### 10.1b The ABI question, which answered itself

This section used to weigh Decision 25's warning that targeting embedded fixes the capability ABI to a
portable byte mode project-wide and that retrofitting is invasive. **Decision 25 no longer says that.**
Pulley runs components, so there is one ABI, no toggle, and nothing to retrofit — the question is closed,
not deferred.

What replaces it is a build step rather than a build *target*: `rp2350` consumes the same
`engine.component.wasm` and AOT-compiles it to Pulley bytecode. No second guest artifact exists.

**Two more things the tooling cannot express yet, both discovered by reading the code:**

- **`dlc new` can now scaffold any tier the template has a slot for.** Changed 2026-07-29: the offered tiers
  are **derived from the template tree**, and `[tiers.*]` defaults to `root = "hosts/<tier>"` for anything
  that is not web. So `--tiers rp2350-tinygo` works the moment
  `templates/component-model/hosts/rp2350-tinygo/` exists — no Go change.
- **The slot contents are what is missing**, and deliberately: an embedded host slot is backlog so nobody
  scaffolds an unverifiable stub. Two tiers here means two *slots* over one skeleton, not two skeletons —
  add each once its tier is proven, not before.

### 10.1e Can `rp2350` share code with `rp2350-tinygo`?

Worth asking properly, because the two tiers drive **the same screen and the same five buttons** — which is
the one case in this project where two tiers have everything visually in common. Three answers, in
increasing order of how much they change:

**First, what is NOT the constraint: the runtime exposing libraries.** Satisfying the guest's WIT imports
from host code is Wasmtime's core mechanism, and the embedded host already implements every WASI import the
component asks for by hand ([`EMBEDDED-PLAN.md`](../EMBEDDED-PLAN.md)). A Rust host can drive the TFT and
hand it to the guest. Nothing about this tier is blocked by what the runtime can expose.

The constraint is narrower and only about **host-side code**: the two hosts are written in different
languages, so code *neither* of them exposes to the guest — the glue that talks to the screen and the
buttons — exists twice.

**1. With a Rust host: no shared host code, only the shared engine.** The runtime is Rust `no_std` and
`rp2350-tinygo` links Go. Nothing crosses.

**And "written twice" overstates it, which is worth correcting:** neither host writes a display *driver*.
Both bind an existing one — an `embedded-graphics` driver on the Rust side, `tinygo.org/x/drivers` on the
native side. What is duplicated is a thin glue layer plus a nine-cell renderer, which is much less than two drivers
and is the reason this stays the default answer.

**2. A Go host embedding the runtime via cgo would allow sharing — and is not worth it.** In principle a
TinyGo host could embed a C wasm runtime and keep the display and input code shared with `rp2350-tinygo`.
Wasmtime `no_std` is Rust, so this would mean adopting a *different, weaker* runtime purely to share a
nine-cell renderer — trading the Component Model for glue reuse, which is the trade this whole tier exists
to refuse. **Do not attempt this as part of the app.**

**3. Sharing presentation is possible, and mostly should not be wanted — because RENDER DECISIONS BELONG TO
THE TIER.**

§6.4's first two render paths put presentation in the engine, so every tier gets it and each host only
rasterizes. That is the sharing being asked about. The reason not to reach for it is the sharpest statement of
the host-layer rule so far (maintainer, 2026-07-29):

> **A tier knows its timing and its constraints better than the engine ever can.** Refresh rate, whether
> partial updates are cheap, how much RAM a framebuffer may take, whether it is on battery, what is already on
> screen. An engine emitting draw commands knows none of it, and the only way to tell it would be a manifest
> field per constraint — a list with no end.

§6.4 argues for the semantic path from **dissimilarity of output** ("DOM, a TFT grid and terminal ASCII share
no structure"). The timing-and-constraints argument is stronger, and it survives the case where the other one
fails: this plan once claimed the reasoning *inverts* for the two badge tiers, since
they drive the same pixels. Under the constraints argument it does not invert — **one of those hosts is running
an interpreter and the other is not**, so their performance envelopes differ even though their screens are
identical, and each is still the better judge of how to paint.

*Honesty about this app:* nine cells repaint trivially on both, so nothing here would actually notice. The
principle is right and general; it does not bite at this scale.

**The synthesis, if a shared render is ever built.** Three parts, and they accommodate both a required
Display and tier authority:

| What | Whose | Why |
| --- | --- | --- |
| **Timing** — when to repaint | the **tier** | it is the only party that knows the cost. So **pull, never push**: a command the host calls, not an import the engine drives |
| **Whether to use a shared render at all** | the **tier** | a constrained tier may decline the draw list and render from semantic state instead |
| **Content, when asked** | the **engine** | so two tiers that do use it cannot disagree about what is true |

That is why the render-call shape (§10.1e, previous revision) is the right one if this is ever built: an
export the host pulls gives the tier timing for free, needs no WIT import, and obliges no stub on a screenless
tier. A required Display is then harmless because nothing has to call it.

**Recommendation for this app: duplicate the glue.**

Nine cells do not justify a WIT interface, a draw-list schema and a rasterizer per host, and the duplication —
glue, not drivers — is exactly what host parity is designed to keep honest.

**And the vocabulary is the author's to keep straight.** Whatever draw commands mean is agreed between the
app's engine code and its tier slots; the framework does not enforce it, and no cross-tier check verifies that
a rasterizer honours them. Host parity compares renderings, not intentions — which is why "the author
coordinates the tier and the engine" is a real responsibility rather than a formality.

### 10.1c `rp2040-tinygo` — the tier with no screen, and the smallest one

**RP2040, 264 KB SRAM, no display, two system buttons.** This is Decision 18's floor: §5.1 says 264 KB "is
too tight for a comfortable WASM runtime, so the *same Go engine source* is compiled **natively**". So unlike
the Tufty there is **no runtime choice here** — native TinyGo or nothing, and the interpreted track is
inapplicable: 2 MB of flash cannot hold a runtime plus a ~890 KB payload either.

**Where does a game with no screen render?** Over USB serial, into whatever terminal is attached — the
"serial REPL" the main plan describes for MCUs (§4.1, Decision 14). The board's screen is *someone else's
laptop*, which is a better illustration of the host layer than either Tufty tier: a slot **interprets state**;
owning pixels was never the requirement.

**Input, two options.** Start with the first:

1. **Typed over serial** — `play 5`, exactly the shape the terminal slot uses, and no extra hardware.
2. **A 3×3 key matrix on the GPIO** — this board exists to drive key matrices, and nine keys *is* a
   tic-tac-toe board. The purest input-map (Decision 14) in the whole project: press a key, send
   `play <square>`, render nothing locally.

**The NeoPixel is output that is not a screen.** Whose turn as a colour, a flash on a win. Worth doing
precisely because it is not a rendering of the board — it is a host deciding how to express `turn` and
`outcome` with one LED, which is the semantic path taken to its limit.

**What this tier tests that neither Tufty tier does:**

- **"An app ships no presentation" gets its hardest test.** Tic-tac-toe has never needed the Display
  capability (Decision 34); a tier with no display is where that stops being a claim.
- **No filesystem at all**, not merely "we chose not to persist" (§10.4). RP2040 has no WASI, and §5.3 says
  caps are direct Go calls with "no wasm, no WASI".
- **One engine source, two TinyGo targets** — RP2040 and RP2350 are different builds of the same code.
- **It is $8.95**, so the demo is reproducible by anyone. That is not an engineering property, but it is why
  this tier is worth having.

**Track K — `rp2040-tinygo`.** Same shape as Track N, minus the display:

| Phase | Work | Falsification |
| --- | --- | --- |
| **K0** | the platform fixes: the caps seam (§10.1a) **and** a `Boot` that can say "no filesystem" (§10.1d). Both are shared with Track N | each has its own falsification below |
| **K1** | the engine runs on the board and answers `state` over USB serial | flash with `platform.RegisterAll()` removed → `unknown method_id` at run time |
| **K2** | the serial slot: a typed line in, an ASCII board out | write its renderer **independently** — do not import the desktop slot's. Two Go slots that share a projection make host parity vacuous |
| **K3** | the NeoPixel expresses `turn` and `outcome` | send a state whose `outcome` is `IN_PROGRESS` with three marks aligned → the LED must not signal a win |
| **K4** | *(optional)* a 3×3 key matrix replaces typing | press a key for an occupied square → the engine refuses; the host does not pre-empt it |
| **K5** | the serial slot joins **host parity** as another slot | make it disagree with the others about one state → parity goes red |

### 10.1d A second platform gap — `Boot` cannot say "there is no filesystem"

**Found 2026-07-29 by reading the code, and it blocks every tier in this section.**

`platform.Boot` refuses an empty `Root` and then unconditionally sends
`Filesystem{Availability: PRESENT, …}`. There is no way for a Go host to report an absent filesystem — and
the refusal message promises otherwise:

```
boot: no filesystem root — grant one (see platform.AppRoot) or say so explicitly
```

**There is no way to say so explicitly.** The code documents an option it does not have.

**Why nobody noticed:** the web host does not use `Boot`. `dlc-platform/web/worker.ts` hand-builds the
manifest and already handles the absent case (`hasFilesystem ? {available:true,…} : {available:false}`), which
is why the absent branch is tested at all. So the path exists on the TypeScript side and is **unreachable
from Go** — and every Go host so far has had a filesystem, so the gap sat behind a correct-looking error
message.

**What it needs:** an explicit way for a host to declare no filesystem — an option that says so, with `Boot`
sending `AVAILABILITY_ABSENT` and skipping `SetRoot`. Keep the refusal for a host that says *nothing*, since
silence is still the mistake the message was written for; what must change is that saying so becomes
possible.

*Falsification when it lands:* boot a host that declares no filesystem, and confirm `HasFilesystem()` is
false, the filesystem verbs are unregistered under `RegisterDiscovered`, and `Root()` still panics if anything
reaches for it.

**Platform work, not app work.** It is recorded in `DEVALBO-DLC-GO-TASKS.md`.

### 10.2 What this tier verifies that nothing else can

The events import was *designed* to survive to embedded and has never been run there. `dlc-platform/wit/ilc.wit`
says so in a comment: `string + list<u8>` lowers to pointer/length pairs a `//go:wasmimport` can express —
**"(UNVERIFIED: there is no embedded tier to run it yet.)"** This tier is that verification, and it is the
most valuable thing in §10.

**And with both tiers built, that verification has a control**: the same engine source running as core wasm
and as a native binary on one board, which is as close to embedded parity as this project gets (§10.1).

Two more firsts:

- **`HasFilesystem() == false` becomes real** (§10.4). No app has ever branched on it.
- **The input-map path** (Decision 14): buttons → a command, with no REPL and no argv anywhere.

### 10.3 Input on the Tufty — five buttons, and the host builds the request

Decision 14 and Decision 28: the host turns native input into a request. A cursor on the 3×3 grid plus a
select button is all five buttons need to express — four directions and select, or three (next / previous /
select) if the fifth is wanted for `new-game`.

**The host sends `play <square>`.** It does not decide whether the square is legal, whose turn it is, or
whether the game is over — it sends the command and renders what comes back, including the refusal. A host
that greys out an occupied square may do so **from the board the engine sent**, never from its own bookkeeping
(§4).

### 10.4 Storage — the badge does NOT persist, and that is the interesting part

**Decided: no persistence on this tier.** A badge that forgets the game when it sleeps is fine, and the
alternative — littlefs behind a WASI-p1 preopen — is a pile of work for a nine-cell board.

The way to say this is for **the host to report no filesystem**, so that `platform.HasFilesystem()` returns
false and the app holds the board in memory on this tier while writing `game.json` on the others.

**A Go host cannot say that yet** — see §10.1d, which is a prerequisite for this phase and for Track K.

**This makes tic-tac-toe the first app anywhere in this repo to use the manifest for its intended purpose.**
`HasFilesystem()` has existed since the environment manifest landed and no app has ever branched on it: dlc
and notes assume a filesystem, and the absent branch has only ever been exercised by parity vectors and a
manifest-driven browser test. Here it is a real tier with a real answer.

It is also a **legitimate** branch, and worth being precise about why, because Decision 33 D3 bans the
lookalike: branching on *what you must do* is allowed, branching on *who is listening* is not. "Can I write
a file?" is the first; "am I on the badge?" is the second. **Write the branch so it would still be correct
if the browser lost its OPFS** — that is the test of whether you branched on the right thing.

*Falsification for B4:* run the **native** tier with a manifest reporting no filesystem and confirm the game
still plays, in memory, with nothing written. If that only works on the badge, the branch is reading the
tier rather than the capability.

### 10.5 What the framework does not have yet

Say these out loud rather than working around them:

- **The caps seam is missing its third file** — §10.1a. Blocks `rp2350-tinygo` and `rp2040-tinygo`. Platform work.
- **`Boot` cannot report an absent filesystem** — §10.1d. Blocks §10.4 and all of Track K. Platform work,
  and the more surprising of the two: the capability exists in TypeScript and is unreachable from Go.
- **The badge cannot instantiate the component yet.** The AOT step works and the runtime boots, but ~890 KB
  of Pulley bytecode does not fit 520 KB of SRAM: **the PSRAM allocator is the blocker**, and it is measured,
  not suspected. Blocks `rp2350`.
- **No embedded skeleton.** An embedded slot is backlog until one can `verify` — and there are
  **three** shapes to eventually template: a Go host with a screen, a Go host without one, and a Rust
  `no_std` host running the component. **The hand-written build/flash target for each tier is
  the best input that template will ever get**; write it as though it were going to be scaffolded, because
  it will be.
- **`dlc build <tier>` does not know these tiers.** Declaring `[tiers.rp2350-tinygo]` by hand works — arbitrary
  tier names are accepted, and the slot gate requires the directory to exist first — but toolchain
  orchestration (TinyGo flash; or component → AOT → `cargo` → `.uf2`) is Decision 27 work in progress. It is
  a hand-written `make` target for now.
- **Parity cannot reach either tier.** `verify-parity` compares native Go against the wasip2 component;
  neither a bare-metal binary nor a `.cwasm` on hardware is in its world. Two things partly cover
  it: **host parity** (Phase 5) can take each badge renderer as another slot driven by synthetic states, with
  no device in the loop; and the two badge tiers can be compared against **each other** on hardware
  (§10.1), which is the closest thing to embedded parity this project will have.

### 10.6 Build order

Three tracks — **N** (badge native), **P** (badge Pulley), **K** (keeb native, §10.1c). **The native tracks
first**, and each phase leaves the tree green. Phases assume §§1–9 are done:
**the engine already exists and is already correct** — none of this touches it.

**Track N — `rp2350-tinygo` (TinyGo linked directly, Go host)**

| Phase | Work | Falsification |
| --- | --- | --- |
| **N0** | does the pinned TinyGo target RP2350 at all? Write the answer in `spikes/`. **The one hard dependency** — if no, the native track stops and `rp2350` becomes the only route | a stated version and target name, not a recollection |
| **N1** | **platform:** split the caps seam (§10.1a) **and** give `Boot` a way to declare no filesystem (§10.1d) — both shared with Track K | build for the board with the old tags and watch it fail on the WIT import; and watch `Boot` refuse a host that has no filesystem to grant |
| **N2** | the engine runs on the board and answers one command over USB serial | flash a build with `platform.RegisterAll()` removed → `unknown method_id` at run time, on hardware |
| **N3** | display: draw the board, the turn, and the winning line **the engine named** | send a state whose `winning_line` is empty while three marks align → nothing is highlighted (the §4 rule, on glass) |
| **N4** | input: five buttons → `play <square>` | press select on an occupied square → the engine refuses and the screen says so; the host must not pre-empt it |
| **N5** | no persistence (§10.4) | power-cycle → a fresh game, nothing written. Then run the **native desktop** tier with a no-filesystem manifest and confirm the game still plays — proving the branch reads the capability, not the tier |
| **N6** | the badge renderer joins **host parity** as a third slot | make it disagree with the others about one state → parity goes red |

**Track P — `rp2350` (the shared component under Pulley, Rust `no_std` host)** — only after Track N is running.
Phases P0–P2 are **already done** outside this app ([`EMBEDDED-PLAN.md`](../EMBEDDED-PLAN.md)); they are listed
so the sequence reads honestly.

| Phase | Work | Falsification |
| --- | --- | --- |
| **P0** | ✅ AOT the shared component to `pulley32` with a compiler built like the runtime | load a `.cwasm` from the stock `wasmtime` CLI and watch it be rejected — the artifact records the compiler's feature set, not its flags |
| **P1** | ✅ the runtime boots on the board; `picotool info` confirms the UF2 family and boot block | flash a UF2 built by a tool that guesses the family and watch `picotool` name it `rp2040` |
| **P2** | ✅ a hand-written `no_std` host satisfying every WASI import the component asks for | remove one import definition and watch instantiation fail by name, before any command runs |
| **P3** | **the blocker: PSRAM allocator** — ~890 KB of bytecode will not fit 520 KB of SRAM | instantiate without it and watch the allocation fail; that is the measurement, not a guess |
| **P4** | the engine answers one command over UART | flash a build with `platform.RegisterAll()` removed → `unknown method_id`, on hardware |
| **P5** | the Rust display and input slot | the same two probes as N3/N4, in the other language |
| **P6** | **the payoff: compare the two tiers on the board** — one running the shared artifact, one natively linked | make the two disagree on one state and watch the comparison catch it |

### 10.7 The sibling board

`demo-platforms.txt` also lists [Adafruit 6464](https://www.adafruit.com/product/6464), the STEM-kit variant
of the same badge (adding a game controller and sensors). Same SoC, so **nothing above changes** — a
controller is more buttons, which is still the input-map path. Worth using if it is the board on the desk;
not worth a fourth tier.

**The rule for adding boards:** a new board is a new **tier** only when it changes the engine binding, the
ABI, or the capability set (Decision 27). A different screen or more buttons is a different *slot* on an
existing tier, and a slot is cheap. Three tiers here is already the interesting maximum — RP2350 native,
RP2350 interpreted, RP2040 native — because those are three genuinely different bindings.
