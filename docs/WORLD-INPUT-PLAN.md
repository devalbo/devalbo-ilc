# World inputs — implementation plan

**Status: PROPOSED 2026-08-16.** Nothing here is built. Written in the shape of
[`EMBEDDED-PLAN.md`](./EMBEDDED-PLAN.md) and [`PAYLOAD-LOADING-PLAN.md`](./PAYLOAD-LOADING-PLAN.md):
decisions first, phases that each leave the tree green, nothing claimed until it has been broken on
purpose.

One app that asks for a name, running on a terminal, in a browser, and on a badge — where the three have
nothing in common about how a person types.

---

## 0. Where this starts

An interactive `hello` needs a name from the user. Each tier can already collect input; two of them do:

| tier | how a name arrives today |
| --- | --- |
| CLI | `--name` / `-n`, from the GENERATED clispec |
| browser | `<input id="name">` in the slot's HTML, read by hand |
| badge | it does not — `host.execute(method, &[])` sends an empty request |

**The CLI's half is already solved and generalised.** `commands.cli.pb.go` is generated from the proto and
describes the input surface without anyone hand-writing it:

```go
{Name: "name", Field: 1, Kind: clispec.KindString, Short: "n", Help: "who to greet"}
```

That is not a CLI fact. It is a description of what the command takes, which happens to have exactly one
consumer.

---

## 1. Design decisions

### D1 — A GENERIC HOST CAN BUILD THE REQUEST, and this is the unlock

The badge runs apps it was never built for, so the standing assumption has been that it cannot construct
an app's protobuf request — the same reason it shows `stdout` rather than rendering `result.output`.

**That assumption is wrong for inputs, and only for inputs.** Rendering a RESPONSE needs the schema:
field numbers alone do not tell you what a value means or how to display it. Building a REQUEST does not.
Encoding a string into field 1 is:

```
tag(1, LEN) + varint(len) + bytes
```

Field number and wire kind are sufficient, and the badge already hand-encodes protobuf for the
environment manifest (`dlc-platform/embedded/src/manifest.rs`). So a loader can produce a valid
`GreetRequest` for an app whose schema it has never seen.

This is what makes one app work on three worlds without the app knowing which one it is on.

### D2 — THE SPEC IS QUERIED FROM THE ENGINE, not carried by the host

A new platform method returns, for a method id, the fields it accepts: number, name, kind, help. It is a
sibling of `GetCommandSurface` (id 4), which already answers "what is registered right now" for the same
reason — a host needs to know something only the engine can say.

**Generated from the same source as the clispec**, so there is one description of an app's inputs and
three renderers of it. A second hand-maintained description is the bug this exists to avoid: the browser
slot's `<input id="name">` is exactly that today, and it is why renaming a proto field silently leaves the
browser asking for something that no longer exists.

**No new WIT import.** It rides the existing command boundary — one export, method dispatch, a proto
request in and a proto response out (Decision 31). Nothing here is async, so Decision 22 is untouched.

### D3 — THE BADGE GETS A KEYBOARD, not a list of canned names

Three buttons mapped to Alice/Bob/Charlie was the first plan and is superseded. It only ever suited an app
whose input is a person's name — a filename or a port number would get three answers that make no sense —
and it made the badge the one tier that could not express an arbitrary value.

**A character picker costs two rows and makes every string input reachable.**

```
 alice_                                [SP]
 abcdefghijklmnopqrstuvwxyz_<#
                        ^ active, drawn inverse
```

| row | holds |
| --- | --- |
| 1 | the buffer being typed, with a cursor, and the ACTIVE KEY'S NAME at the right |
| 2 | 26 letters, then space, backspace, enter |

**One column per key, and the specials are single characters** (`_` space, `<` backspace, `#` enter) so
all 29 fit in 29 of the 40 columns with room to spare. Single-character keys are unreadable on their own,
which is what the label at the right of row 1 is for: it names whatever is selected, so `_` is ambiguous
only until you land on it.

The active key is drawn INVERSE — a filled cell with the glyph knocked out — rather than marked from
below. A marker costs a third row, and the panel is already shared with the app (`Split` gives the app 13
of its rows).

**Buttons, from the measured map in `board.rs`:**

| button | pin | does |
| --- | --- | --- |
| A | 7 | move left (wraps) |
| C | 10 | move right (wraps) |
| B | 9 | select the active key |
| DOWN | 6 | hide / show the input section |
| UP | 11 | unassigned — kept free deliberately |

Wrapping matters more than it sounds: with hard ends, reaching `z` from `a` is 25 presses; wrapping makes
it one.

**DOWN hides rather than cancels.** The buffer survives, so hiding is a way to see what is underneath and
come back — not a way to lose what you typed. Cancelling needs its own answer and does not have one (§4).

`UP` stays unassigned because an unused button is recoverable and a wrongly assigned one is a habit.

### D3a — IT IS A WORLD COMPONENT, CONFIGURED AT WORLD SETUP

The keyboard lives beside `menu.rs`, which already does the same job for a different question: draw
something, read the measured buttons, time out sensibly, hand back a value. An app never sees it and
cannot tell it existed.

**Configured where the other world choices are** — `rp2350/build.rs`, alongside `BADGE_WORLD`,
`BADGE_SCREEN`, `BADGE_PAYLOAD` and `BADGE_BEAT_MS`:

```
make badge-uf2 BADGE_INPUT=keyboard   the picker (default where a screen exists)
make badge-uf2 BADGE_INPUT=off        no input; apps take their defaults (D5)
```

Flash-time rather than runtime for the same reason the world itself is: a world is a claim about what the
badge can do, and a claim that changes underneath a running app is the bug the environment manifest exists
to prevent. **The minimal world gets `off` and cannot be given anything else** — it simulates a device
with a status LED, so an input surface would contradict what it advertises.

Being a component rather than a feature of `hello` also means it is reusable for anything the world needs
a string for — naming a payload, a future settings screen — instead of being welded to one app.

### D3b — SHOWN ONLY WHEN THE APP ADVERTISES AN INPUT

The world does not guess. It asks the engine what the command takes (D2), and the answer decides:

| the app advertises | the world does |
| --- | --- |
| a string field | show the keyboard for it, labelled with the field's help text |
| no fields | skip it entirely and execute — no prompt, no delay |
| a kind it cannot render | skip that field; the app takes its default (D5) |

**This is what keeps a loader honest about apps it was never built for.** The badge cannot know that
`hello` wants a name; it can know that method 10000 accepts a string in field 1 called `name`, because
that is generated from the app's own proto and travels with it. An app that declares nothing gets a badge
that boots straight into it, exactly as today.

It also composes with D3a rather than duplicating it: `BADGE_INPUT=off` means the world never offers,
whatever the app advertises. **Two gates, and both must be open** — the world must be able, and the app
must ask. Either alone is an assumption.

### D3c — AN APP THAT WANTED INPUT AND GOT NONE SAYS SO, via status

D5 says a missing input is not an ERROR. It is not nothing, either: an app that greets "world" because
nobody told it a name did a less useful job than it could have, and that is worth surfacing.

**The app already knows, and no new mechanism is needed.** An empty field IS the signal:

```go
name := req.Name
if name == "" {
    name = "world"
    platform.SetStatus(StatusDegraded, 0, 0)   // ran, on a default
} else {
    platform.SetStatus(StatusOk, 0, 0)
}
```

`SetStatus` exists, rides `devalbo:ilc/events`, and is already emitted on every run — the badge logs
`event ilc.status (3 bytes)` today. This costs one branch in the app and nothing on the wire.

**It also keeps D4 intact.** The app is not detecting a world, asking about a capability, or branching on
a tier. It is reporting on ITS OWN RUN — "I used a default" is a fact about the app, true identically on a
terminal where someone omitted `--name` and on a badge with `BADGE_INPUT=off`. The app cannot tell those
apart and does not need to.

This is the shape [`status.go`](../dlc-platform/status.go) sketched `Status1` for: a PERSISTENT condition,
the one a tier with a single indicator should render.

**It gives the status render path its first real consumer.** Worlds emit these bytes and no world renders
them yet — deliberately deferred, because a channel with no consumer is a guess about what consumers want.
This is one: a badge showing amber for "ran on defaults" against green for "ran with what you gave it"
answers a question a person actually has, from across a room, with no text.

**What it must NOT become** is a way for the app to complain. A world with `BADGE_INPUT=off` is correctly
configured, not broken, and an app that reported failure there would be wrong. The claim is about the RUN,
not the world.

### D3-general — AN INPUT IS A LEGAL SET AND A STRATEGY FOR PICKING FROM IT

The decisions below were written bottom-up — a keyboard, then a spinner, then dynamic choices — and that
order hid the structure. Stated properly, and it reorders everything that follows:

> **Every input is a SET OF LEGAL VALUES. A widget is a strategy for choosing from a set of that size and
> shape. Context-sensitive choices (D3e) are the GENERAL case; kind-based widgets (D3d) are what you fall
> back to when the set cannot be enumerated.**

| the legal set | strategy | the name we gave it |
| --- | --- | --- |
| small, enumerable, state-dependent | list picker | "legal choices" (D3e) |
| two elements | toggle | "bool widget" (D3d) |
| ordered, unbounded | step navigation | "spinner" (D3d) |
| all strings over an alphabet | construct one | "keyboard" (D3) |
| exactly one element | confirm, or skip entirely | not built |
| empty | do not offer the command | D3e |

A spinner is a NAVIGATOR over an unbounded ordered set. A keyboard is a CONSTRUCTOR for the set of all
strings. Neither is about types — both are what you do when enumeration is impossible.

**Why this matters rather than being a nicer way to say the same thing:**

It predicts the protocol. There is ONE question — *what may this field be, right now?* — and two shapes of
answer: an enumerated set, or a description of an unenumerable one. `GetCommandSpec` answers the second
statically and for free, generated from the `.proto`; `RegisterChoices` answers the first, dynamically,
and costs app code. A world asks once and picks a strategy from the answer.

It also predicts inputs nobody has asked for yet. "Pick a payload from the catalog" and "pick a date" are
not new widget types — they are a set that is enumerable and a set that is ordered, and both already have
strategies above.

**The generated spec stays the DEFAULT ANSWER, not a lesser one.** Requiring every app to write a
callback for something the generator already knows would tax the common case to serve the rare one. The
dynamic answer SHADOWS the static one where an app provides it; where it does not, the static answer is
complete and correct.

**One consequence worth building for from the start:** the choices response should be able to say *"not
enumerable — an integer"* rather than only *"here is a list"*. Otherwise a world has to ask two questions
and merge them, and the two can disagree.

### D3d — ONE WIDGET PER KIND, chosen from what the app already declares

A character strip cannot enter a number. Not "cannot conveniently" — 26 letters
and no digits, so an `int32` field is simply uncollectable, and `countdown`'s
`from` is declared a STRING purely because that was the only thing the badge
could produce. The app pays for the world's limitation with a `strconv.Atoi` and
an error path for input it should never have received.

**The spec already says which widget to use.** Nothing new is needed on the wire:

| kind | widget | what it needs |
| --- | --- | --- |
| STRING | the character strip | built |
| INT32/64, UINT32/64 | a spinner: A/C step, B confirms | `default_value` as the start |
| BOOL | the same spinner, two positions | — |
| ENUM | a list over `enum_values` | already sent, and already documented as "the MENU a richer host would show" |
| BYTES | none — skipped, app takes its default | — |

The enum case is the original Alice/Bob/Charlie idea arriving properly:
**app-declared choices rather than world-invented ones**, which is what D3 gave
up when the keyboard replaced canned names.

**Dispatching on kind is the same rule as D3b, one level finer.** The world asked
"is there an input"; now it asks "what KIND", and the app answers both times
through its own `.proto`. A kind the world cannot render is skipped and the app
takes its default — a no-op, not an error (Decision 33).

**BOUNDS ARE DELIBERATELY OUT.** They would need new `min`/`max` field options,
and the app must validate regardless — a CLI user can pass `--from 999999` and no
widget stops them, so `countdown` keeps its clamp either way. Bounds would be a
badge affordance, not correctness, and the affordance can be bought more cheaply:
a STEP SIZE the spare button changes, so reaching a large number costs presses
rather than being impossible. Revisit if spinning turns out to be annoying in
practice rather than in theory.

### D3e — WHAT IS LEGAL RIGHT NOW is a different question from what the field accepts

Everything above is SCHEMA: `play` takes a square, 1 to 9, forever, generated from the `.proto`. That is
enough to render a widget and encode an answer, and it is not enough to be USEFUL for an interactive app.

On turn three of a tictactoe game, X may play 2, 4 or 7. O may play nothing at all — not because the
field forbids it but because it is not O's turn. A spinner over 1..9 offers six illegal moves and one
illegal player, and the app can only refuse them after the fact.

**Only the app knows.** It holds the board; the world holds a screen. So this is the `RebuildIndex`
pattern (INDEX-PLAN): the PLATFORM owns the verb, the APP supplies the knowledge.

```go
platform.RegisterChoices(func(method uint32, field uint32) []platform.Choice {
    // the app's own state decides
})
```

exposed as a reserved method any world can call, without knowing what tictactoe is.

**THREE ANSWERS, AND THE THIRD IS THE ONE THAT MATTERS:**

| the app returns | the world does | means |
| --- | --- | --- |
| no provider registered | the kind's widget (D3d) | free-form: any value the type allows |
| a non-empty list | a picker over exactly those | these and only these are legal now |
| an EMPTY list | does not offer the command | nothing is legal now — not O's turn |

The empty case is not an edge condition; it is the feature. It lets a generic shell decline to offer a
command that cannot currently succeed, which is the difference between a badge that lets you make an
illegal move and one that does not. It is also adjacent to `GetCommandSurface` — that says what is
REGISTERED, this says what is APPLICABLE — and the two together are what
`SESSION-AND-SURFACE-PLAN.md` Phase 5 needs to make tictactoe playable with five buttons.

**Values are STRINGS, labels are separate.** One representation for every kind, parsed by the host to the
field's wire type — the same choice `default_value` already makes in the spec, and consistency is worth
more here than saving a parse. The label is what a person reads: value `5`, label `centre`.

**A CHOICE LIST IS A LIST, and rendering it as one is the honest first answer.** Tictactoe would show
`2 · 4 · 7` and a picker. It is generic, it works for an app written next year, and it is boring — a 3x3
grid would be much better and is app-specific structure a generic world cannot infer. That is a reserved
LAYOUT vocabulary (§4), the same shape of question as `ilc.progress`, and it should wait for the list to
prove insufficient rather than be assumed to be.

**The cost is a round trip before each input**, on a tier where a round trip is a Pulley-interpreted call.
Cheap for a query that returns nine numbers; worth measuring before anything asks per keystroke.

### D3e-a — CHOICES ARE `{id, label}` PAIRS, and a proto enum is just a static one

The enum widget was blocked on a schema gap: `SpecFlag.enum_values` carries value
NAMES and proto enum numbers are arbitrary, so a world knew what to show and not
what to send.

**The fix is to stop treating enums as special.** The general representation is a
list of `{id, label}` — the id is what goes on the wire, the label is what a
person reads — and that covers both sources:

| where the set comes from | example | when it is known |
| --- | --- | --- |
| a proto enum | `{1, "north"}` | compile time, generated |
| app state (D3e) | `{5, "centre"}` | per invocation |

Tictactoe is the case that makes this obvious: its choices are squares 1..9,
legal or not depending on the board. **They are not a proto enum and never could
be** — no static declaration can express "not on turn three". So a mechanism
built around proto enums would serve the easy case and miss the real one.

**So `{id, label}` is the DEFAULT pattern and the enum is a degenerate instance
of it.** The generator emits a proto enum's values as choices, numbers included,
which closes the gap without a new field for `enum_values` to carry them. The
world holds ONE picker, fed statically or dynamically, and cannot tell which.

This is D3-general applied one level down: an enumerable set is an enumerable
set, and where it came from is the app's business.

### D3f — THE WORLD ACCOMMODATES THE APP, never the reverse

The principle behind D3b, D3d and D3e, stated once so it can be applied to cases they do not cover:

> **An app declares what it needs. A considerate world serves that with the best affordance it has, and
> degrades rather than refusing.**

**This is not how it started, and the counter-example is in this repo.** `countdown` declared its `from`
field a `string` for exactly one reason: the badge's only widget was a character strip with no digits, so
a number was uncollectable and the app parsed text to work around it. The app carried a `strconv.Atoi`
and a "not a number" error path to accommodate a limitation of one tier — on every tier, including the
two that had numbers all along.

When the badge grew a spinner (D3d), the field became `int32` and both went away. **The schema got to be
honest because the world stopped being lazy.** Any time an app's `.proto` is shaped by what a world can
collect, this principle is being violated and the fix belongs in the world.

**Which means a widget gap is a DEGRADATION, not a refusal.** The current code skips any kind it has no
native widget for, which is the inconsiderate reading. The considerate one is a fallback chain, best
first:

| the app wants | best | fallback | last resort |
| --- | --- | --- | --- |
| integer | spinner | type the digits on the strip, parse | skip |
| bool | spinner, two positions | type `true`/`false` | skip |
| string | the strip | — | skip |
| enum | picker over the declared values | **blocked, see below** | skip |
| bytes | — | — | skip |

Skipping remains legitimate as the LAST step: the app takes its default, which is a no-op rather than an
error (Decision 33). What is not legitimate is skipping when a general widget could have expressed the
value.

**Not built yet, deliberately.** Today's badge has both widgets, so no fallback is reachable and building
one would be a branch nothing exercises (ENVIRONMENT-PLAN D6). It becomes real the moment a world ships
with one widget and not the other — a smaller board, or a `BADGE_INPUT=text` mode.

**AND IT EXPOSES A SCHEMA GAP THAT IS REAL NOW.** `SpecFlag.enum_values` carries the value NAMES and not
their NUMBERS. Proto enums do not have to be contiguous or zero-based, so a world holding `["north",
"south"]` cannot encode either one — it knows what to show and not what to send. The enum widget is
blocked on carrying the numbers alongside the names, which is a wire change and should happen before
anything claims enum support.

### D4 — THE APP DOES NOT KNOW WHICH WORLD IT IS ON, and this must not change it

The engine keeps taking a typed request. It does not gain an input capability, does not ask for anything,
and cannot tell the difference between a name typed at a terminal, a name entered in a browser, and a
name chosen with a button.

That is the whole point, and it is worth stating as a rule because the tempting design breaks it: an
`input.read()` IMPORT would make the engine pull rather than receive. It cannot work anyway — a browser
cannot synchronously block on a DOM event, and async imports were refused (Decision 22) — but it would
also fork the artifact, which the architecture cannot afford.

**Input is collected BEFORE `execute`, by the host, and arrives as the request.** The same shape the CLI
has always had.

### D5 — A MISSING INPUT IS NOT AN ERROR

A world that cannot collect input at all sends the request without it, and the app takes its default —
`hello` already greets "world" for an empty name. That matches Decision 33: a capability's absence is a
no-op, never a failure, and an app cannot detect the difference.

So the badge is free to skip the prompt on a timeout, exactly as the payload menu already does, and a
badge nobody is touching still runs.

---

## 2. Phases

**Phase 1 — the spec, and the CLI proving it.** The new platform method and its generation. The CLI keeps
using the compiled-in clispec and gains a test asserting the two AGREE — the generated description and
the queried one must not drift, and the CLI is where both are visible.

**Phase 2 — the keyboard, standalone.** The component (D3) against a HARDCODED field, wired to
`BADGE_INPUT` (D3a). It is the part with real interaction design in it, and it can be exercised on
hardware before the spec method exists — which is the point of doing it separately.

**Phase 3 — the badge collects for real.** Query the spec, decide by advertisement (D3b), encode by field
number, execute. Ends with a name TYPED on the badge appearing in the greeting.

**Phase 4 — the browser reads the spec.** Replace the hand-written `<input id="name">` with a field
rendered from the queried spec, so a renamed proto field changes the browser too.

**Phase 5 — legal choices (D3e).** `RegisterChoices`, the reserved verb, and a list picker. Ends with
tictactoe offering only the moves that are legal, and declining a command with none — which is what makes
it playable on five buttons at all.

**Phase 6 — more than one field, and kinds beyond string.** Deliberately last. One string input is the
case that exists; integers, bools and repeated fields are speculation until an app needs them.

---

## 3. What this plan does NOT do

- **No input capability, no new import.** D4. The engine receives; it does not ask.
- **No response rendering on the badge.** Building a request needs field numbers; rendering a response
  needs the schema. Only the first is solvable generically.
- **No text entry beyond ASCII lowercase.** 26 letters, space, backspace, enter. No shift, no digits, no
  punctuation — each is another key in a row with 11 columns to spare, and none has a use case yet.
- **No validation.** The app validates its own inputs, as it does now.

---

## 4. Open questions

**Where the spec method lives in the id ranges.** The core-lifecycle block (1–99) holds `Version`,
`SetEnvironment`, `GetCommandSurface`. This is the same kind of introspection, so probably there — but
the block is small and permanent.

**Whether the badge remembers the last value.** Retyping a name on every boot is tedious; keeping one
needs somewhere to put it, which is the payload region's neighbour and not free.

**Cancel, clear, and shift.** `UP` is deliberately unassigned (D3) and these are the candidates. Cancel is
the one with a real question behind it: what does an app receive when a person declines to answer? D5 says
a missing input is not an error, so cancel and timeout should probably be the same thing — which is an
argument for having no cancel button at all.

**Which physical button is "bottom".** The panel is rotated 180°, so the mapping from `DOWN` (gpio 6) to
the button under someone's thumb needs confirming on hardware rather than reasoning about. Cheap to check,
annoying to get wrong.

**Whether the keyboard blocks or overlays.** D3 has DOWN hide the section, which implies the app's output
is behind it — but input is collected BEFORE `execute` (D4), so at that moment there is nothing behind it
except the bring-up report. Either hiding is only useful on a re-run, or the keyboard outlives the command
and the world gains a notion of "the current input", which is a larger idea than this plan holds.

**What a badge does with two inputs.** Phase 4, but the answer shapes the prompt UI: three buttons cannot
cycle two fields without a mode, and a mode is the beginning of a menu system.

**Whether a reserved LAYOUT vocabulary is worth it.** A list picker renders any choice set; a 3x3 grid
renders tictactoe's properly and needs the app to say "these nine values are a grid". That is new shared
vocabulary every tier must implement — the `ilc.progress` question again — and the argument for waiting is
the same: build the list, see whether it grates.

**How often choices are asked for.** Before each input is obvious. Before each COMMAND in a shell loop
means a round trip per turn per command just to decide what to offer, which on a Pulley interpreter is not
free and has not been measured.

**Whether `suggestions` should exist in the message from day one.** The keyboard (D3) removed the need
for canned values, so nothing fills this now — and leaving room for something nobody fills is what
ENVIRONMENT-PLAN D6 warns against. Against that: a browser datalist and CLI completions are the obvious
consumers, and a wire change later costs every producer. Unresolved, and cheap either way while there is
one producer.

---

## 5. Definition of done

1. `./scripts/ci.sh full` green.
2. One `hello` artifact greets a name supplied by a terminal, a browser, and a badge — with **no tier
   conditional in the engine**.
3. The queried spec and the generated clispec are asserted to agree, and the assertion has been made to
   fail on purpose.
4. A renamed proto field changes all three tiers with no hand-editing.
5. A world that collects nothing still runs the app, and the app cannot tell.
6. Verified on hardware: a name is TYPED with A/C/B, and the badge shows the greeting for it.
7. `BADGE_INPUT=off` builds, runs, and reaches the app with no prompt — and CI builds that mode, because a
   flash-time mode nothing builds is a mode that rots (`check-embedded.sh`).
8. An app advertising no input never shows a keyboard, on a world that has one.
9. An app that fell back to a default is DISTINGUISHABLE at a glance from one that did not — the same
   artifact, on the same world, differing only in whether input was given.
