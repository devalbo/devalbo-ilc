# `hello` — four shapes of conversation

**For a person, reading.** Every command here has been run. Where something does not work yet, this document
says so instead of pretending.

The tic-tac-toe tutorial builds **one app in depth**. This one reads **one app in breadth**: `hello` has four
commands, and each is here because it exercises a different way a device and a client can talk. Read them in
order and you have seen the range.

| Command | The shape | What it proves |
| --- | --- | --- |
| `greet` | ask once, get an answer | the simplest round trip |
| `count` | ask once, **watch it happen** | output *during* a command, paced by the app |
| `math` | several inputs, a structured answer | multi-field input, an enum, a reply worth decoding |
| `light` | ask for a change to the device | a capability that may not exist, and that being fine |

> The shape of this list is borrowed from [qroma](https://www.qroma.dev/), whose examples are organised the
> same way — monitor, send-and-receive-a-string, send-and-receive-protobuf, change-the-hardware. It is a good
> spine because it sorts examples by **the conversation**, not by the feature being demonstrated.

Everything below runs against `$ILC/example-apps/hello/`.

---

## 0. The one rule

**The schema is the contract, and it is written once.**

`proto/hello/v1/commands.proto` declares the commands, their fields, their defaults, their help text and their
choices. Everything else is derived from it:

```
commands.proto
  ├─ Go + TypeScript message types        (protoc-gen-*-lite)
  ├─ method ids, locked                   (protoc-gen-dlc-registry -> method-ids.lock)
  ├─ the command surface                  (a CLI, a browser form, a badge's widgets)
  └─ the COMMAND SPEC the app answers with (GetCommandSpec, method 5)
```

That last one is the interesting one and it comes back later: an app can **describe itself at runtime**, so a
host that has never heard of `hello` can still collect its input and render its output.

---

## 1. `greet` — the simplest round trip

```proto
rpc Greet(GreetRequest) returns (GreetResponse) {
  option (devalbo.options.v1.method_id) = 10000;
}

message GreetRequest {
  string name = 1 [(devalbo.options.v1.help) = "who to greet", (devalbo.options.v1.short) = "n"];
}
message GreetResponse {
  string text = 1 [(devalbo.options.v1.help) = "the greeting"];
}
```

```console
$ hello greet --name ILC
hello, ILC - from hello
```

Nothing in `hosts/native/main.go` declares a `greet` subcommand, a `--name` flag, or a usage string. The rpc
**is** the subcommand and the field **is** the flag.

### The trap this command encodes

The engine prints `hello, ILC - from hello` with an ASCII hyphen. It used to be an em dash, and the badge drew
`?`.

The bytes crossed every boundary intact — Component Model `string` is UTF-8, `wasi:cli/stdout` is opaque
`list<u8>` — and then died at the last step, where characters become pixels: the badge's font is ASCII, so
`embedded-graphics` substitutes for any glyph it lacks.

**An app cannot know which tier it is on, so it should not spend characters the poorest one cannot draw.**

---

## 2. `count` — output *during* a command

This is the one that proves **the world does not own time**.

```console
$ hello count 3 rocket
T-minus 3
T-minus 2
T-minus 1
liftoff
(3 ticks)
```

The first four lines arrive a second apart, from the *engine*. The last line is the *host's* renderer printing
the reply. That split is the whole lesson:

- `fmt.Println` inside the engine reaches `wasi:cli/stdout.write`, which is an **import** — host code running
  while the app's goroutine is suspended inside the call. So a world sees each tick as it happens.
- The reply arrives once, at the end, and carries what a stream could not: `counted`, a number.

### Why the app sleeps rather than the world ticking it

A world could have called `tick` repeatedly instead. That works, and it puts the **interval** in the shell —
and "how often" is a policy, and a policy is logic, and logic does not belong in a shell.

So the app decides where to start, how long a tick is, and when it is done. The world provides a clock.

> **On a tier whose clock is a stub, this finishes instantly.** That is wrong but not broken, and it is exactly
> what the badge did before its hardware timer was wired to `monotonic-clock`.

### The enum is the app's vocabulary

`style` is `plain` / `rocket` / `words`. A host renders those choices **because the command spec carries them**,
not because any host knows what a countdown style is. The next app's enum will be `X O EMPTY`.

---

## 3. `math` — a structured answer

```proto
message MathRequest {
  int64 left    = 1 [(default) = "6", (cli_positional) = 1];
  Operator op   = 2 [(default) = "add", (cli_positional) = 2];
  int64 right   = 3 [(default) = "7", (cli_positional) = 3];
}
message MathResponse {
  int64 result       = 1;
  string expression  = 2;
  Problem problem    = 3;
}
```

```console
$ hello math 6 multiply 7
6 x 7 = 42
42
```

Two lines again, and the same split: the engine prints prose for tiers that cannot decode a response, the host
renders the field for tiers that can.

### Three fields is where input surfaces stop being a formality

`cli_positional` is set on all three, so the command reads as an expression — `6 multiply 7` — rather than as
three unrelated flags. **That ordering is the app's**, and every surface honours it: the CLI parses positionals
in that order, and the badge asks for them in that order.

### Divide by zero is not an error

```console
$ hello math 5 divide 0
cannot divide by zero
5 / 0: cannot divide by zero
$ echo $?
0
```

Exit status **zero**. The command ran; it was asked something with no answer and said so.

Returning Go's `error` would have put the failure in the command-result envelope, where it reads as *"the app
broke"*. Instead the failure is a **field** — `problem`, an enum — which means:

- a host can act on it,
- a test can assert on it,
- and a world with nothing but a status light can render it as **amber**.

That last one is the real argument. An error string reaches a terminal and nowhere else; a declared enum
reaches every surface, including the ones that cannot show text at all.

### `int64`, not `double`, and that is recorded

`SpecKind` has no float. A `double` here would land in `SpecCommand.unsupported`, no host could collect it, and
the command would exist and be unusable from every surface.

**This tier does integers** until somebody adds a float kind end to end. That is written in the `.proto` as a
decision rather than left to be discovered as a bug.

---

## 4. `light` — changing the device

```console
$ hello light amber
set
```

The command whose **point is not its reply**. On a badge a colour changes that somebody across the room can
read, with no text anywhere. In a terminal:

```console
$ hello light amber          # on a tier with no light
this world has no light to set
```

Not an error. The app asked, the world did what it could, and `shown` reports which of those it was — so a
caller is never left guessing whether the command worked. **A capability that is absent is a no-op, never a
failure** (Decision 33).

---

## 5. The same app, three surfaces

Nothing above was tier-specific. The same `engine/` package is:

| Tier | How it runs | How input is collected |
| --- | --- | --- |
| **native** | linked in-process | flags and positionals, generated from the schema |
| **browser** | wasm via jco | a form, generated from the schema |
| **badge** | AOT-compiled to a `.cwasm`, run by firmware | **widgets**, chosen from the schema at runtime |

The badge is the interesting one, because the firmware **was never built with `hello` in it**. It runs whatever
payload is in flash, for apps it has never seen. So it asks:

```
world → app:  GetCommandSpec(method 10002)
app   → world: math takes int64 left, enum op (add|subtract|multiply|divide), int64 right
world:         → a spinner, then a chooser, then a spinner
```

An `int64` gets the number spinner. An enum gets a chooser showing **the app's own declared names**. A world
offering "plain or rocket" as a built-in would be guessing at a vocabulary; here the app supplies the meaning
and the world supplies only the surface.

---

## 6. Driving it over the cable

The badge answers a control channel, so a person — or a test — can drive it without touching it. Build the
firmware with the channel compiled in:

```console
$ make badge-uf2 BADGE_CONTROL=on
```

> **Off by default, and compiled out rather than disabled.** A channel that can invoke methods and press
> buttons is exactly as powerful as it sounds, and anyone with the cable gets it.

Ask the app to describe itself:

```console
$ badgectl -port /dev/cu.usbmodemilc1 -spec 10002
math  (method 10002)  Do arithmetic on two numbers
  -set left=<int64>    field 1  default 6
  -set op=<enum>       field 2  default add  one of unspecified|add|subtract|multiply|divide
  -set right=<int64>   field 3  default 7
  answers MathResponse:
    result     <int64>    field 1  the answer
    expression <string>   field 2  what was computed
    problem    <enum>     field 3  one of unspecified|divide_by_zero|overflow
```

Then run it:

```console
$ badgectl -port /dev/cu.usbmodemilc1 -execute 10002 -set left=6 -set op=multiply -set right=7
  result     42
  expression 6 x 7 = 42
ok

$ badgectl -port /dev/cu.usbmodemilc1 -execute 10002 -set left=5 -set op=divide -set right=0
  expression 5 / 0
  problem    divide_by_zero
ok
```

**`badgectl` contains nothing about `hello`.** It cannot import `hellov1.MathResponse` — the payload is
whatever somebody dragged onto the flash region. It encodes the request by the field numbers and kinds the app
declared, and decodes the reply by the app's own `SpecResult` descriptions. `-set op=multiply` would work for
any app with an `op`, and for a field this tool has never heard of.

Other things it can do:

```console
$ badgectl -port ... -screen        # the panel's text, not its pixels
$ badgectl -port ... -press c       # a press indistinguishable from a finger
$ badgectl -port ... -list          # what is installed, and what will not run and why
$ badgectl -port ... -follow 10s    # the log, as frames, backfilled from the start of the run
```

---

## 7. Two traps worth knowing about

These are not hypothetical: both were live bugs in this repo, and both are the kind that **fail silently**.

### An ordinal is not a value

```proto
enum Problem {
  PROBLEM_UNSPECIFIED = 0;
  PROBLEM_DIVIDE_BY_ZERO = 7;
  PROBLEM_OVERFLOW = 11;
}
```

`hello`'s `Problem` is numbered sparsely **on purpose**, as a live test.

A host that encodes or decodes an enum by its **position** in the list reads `PROBLEM_OVERFLOW` as `2` — which
is not a declared value at all — and `PROBLEM_DIVIDE_BY_ZERO` as `1`. Both are legal-looking, so nothing
rejects them; the app simply does the wrong thing and reports success.

proto3 requires only that the **first** value be zero. Everything after it is the author's choice. So the
command spec carries `enum_numbers` beside `enum_values`, emitted from one loop over the descriptor so they
cannot fall out of step.

**Every enum in this app is sparse or dense on purpose.** If a host ever regresses to position-based encoding,
`math` breaks and `count` does not.

### Zero and absent are the same bytes

Proto3 scalars have no presence: an omitted field and an explicit `0` are identical on the wire. So:

- an app cannot tell "the user said 0" from "the user said nothing";
- a *default* therefore has to live in the **schema**, where every host can read it, not only in the engine;
- and a host that collects `0` should send **nothing**, because the shorter request is the honest one.

`count`'s `from` defaults to `5` in the `.proto` for exactly this reason.

---

## 8. Where to go next

- **Build an app of your own** — [the tic-tac-toe tutorial](./TIC-TAC-TOE-TUTORIAL.md) does it start to
  finish, with a browser front end and a real rules engine.
- **Read the engine** — `example-apps/hello/engine/commands.go` is about 200 lines and is the whole app.
- **Read the schema** — `example-apps/hello/proto/hello/v1/commands.proto` is the contract everything else is
  derived from, and it is commented as such.

---

## Appendix: state of things

Honest about what has and has not been exercised.

| | |
| --- | --- |
| `greet`, `count`, `math`, `light` on the **CLI** | ✅ verified |
| `count`, `math` on the **badge**, via widgets | ✅ verified on hardware |
| `math` over the **control channel**, request and response by name | ✅ verified on hardware |
| `light` **on a badge** | ⚠️ the status bytes are emitted and the world renders them; the colour mapping has not been checked against a physical panel |
| all four in the **browser** | ⚠️ renderers are written and typecheck; not driven by hand |
| a **float** operand | ❌ no float kind exists; see §3 |
