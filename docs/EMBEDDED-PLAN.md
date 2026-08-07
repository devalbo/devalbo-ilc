# The embedded tier — implementation plan (§5.6, §10, Decision 18)

**Status: PROPOSED 2026-08-02.** Nothing here is built. Written in the shape of
[`INDEX-PLAN.md`](./INDEX-PLAN.md) and [`EVENTS-PLAN.md`](./EVENTS-PLAN.md): design decisions first, phases
that each leave the tree green, and nothing claimed until it has been broken on purpose.

How the **same engine artifact** that runs in a browser tab and a terminal also runs on a badge you can
wear — without a second build, and without giving up the Component Model to get there.

---

## 0. What changed, and why

Decision 18 sends ESP32-S3 / RP2350 to **WAMR**, and `spikes/README.md` records the reasoning that made
embedded "a separate, deferred build": WAMR runs only core WASM + WASI Preview 1, so a wasip2 *component*
cannot run on it. Chasing one artifact across every tier was tried, found to cost adapter fragility, and
abandoned — correctly, given what WAMR can do.

**Two things changed.**

1. **The target is a real board.** [Pimoroni Tufty 2350](https://www.adafruit.com/product/6463) — RP2350B
   (dual Cortex-M33 @ 250 MHz, 520 KB SRAM), **16 MB flash, 8 MB PSRAM**, a 320×240 TFT, five buttons.
   Flash and RAM are no longer the binding constraint they were on an RP2040.
2. **Wasmtime's Pulley interpreter supports `no_std` *with* `component-model`.** Straight from the
   platform-support docs: the supported `no_std` Cargo features are `runtime, gc, component-model, pulley,
   async, debug, debug-builtins, demangle, anyhow`, and "if you can compile Wasmtime for a Rust target then
   Pulley can run on that target". WAMR's limitation is not a WebAssembly limitation.

So the goal that was abandoned is available again, by a different route — and this time there is no adapter,
because nothing is being lowered to wasip1 and lifted back up.

**Measured, not assumed** (with the `wasmtime` already in devbox):

| Artifact | Size |
| --- | --- |
| tictactoe wasip2 component | 1.82 MB |
| → `wasmtime compile --target pulley32` | **2.21 MB** (+21%) |
| dlc's own engine, same transform | 1.70 MB → 1.93 MB |

2.21 MB into 16 MB of flash. The AOT step works today, on the toolchain already pinned.

---

## 1. Why now

- **The core constraint is explicit:** the same wasm across every tictactoe host. Everything below serves
  that, and any decision that quietly forks the artifact has failed the brief.
- **The capability seam was designed for this and has never been tested against it.** Decision 33 chose
  flat scalars + bytes for `events` *specifically* so the import lowers to a pointer/length pair a
  `//go:wasmimport` can express, "so the same boundary shape survives on the embedded tier". That sentence
  has been load-bearing and unverified for months.
- **A badge is the honest demo.** "Write your business logic once" is a claim a terminal and a browser tab
  only half-prove — both are big computers. A 520 KB microcontroller drawing the same game from the same
  bytes is the version of the claim that cannot be hand-waved.

---

## 2. Design decisions

### D1 — ONE component, AOT-compiled per target. Say precisely what that means.

The engine is built once, as the wasip2 component the web tier already consumes. The badge runs a
`.cwasm` produced from it by `wasmtime compile --target pulley32`.

**This is not byte-identical, and the plan must not claim it is.** It is a *deterministic transformation of
one source artifact* — one engine build, one component, mechanically retargeted. That is a far stronger
unity than a separate wasip1 build (which is a different compilation of different code paths), and it is
weaker than "the same bytes". Both halves of that sentence belong in the README, because the second half is
what someone will otherwise discover on their own and feel misled by.

### D2 — Wasmtime + Pulley ON THE BADGE, not WAMR. This reverses Decision 18's runtime choice.

WAMR cannot run a component; Pulley can. Everything else follows from that one fact.

**Pulley is the badge's runtime — that is the point of it, not a development convenience.** The RP2350 is
32-bit, so the badge runs `pulley32`, AOT-compiled on a dev machine and flashed. Read Phase 1's
`pulley32: FAIL` line in that light: a host can only *execute* the Pulley pointer width it is, so a 64-bit
laptop cannot run `pulley32` — which says nothing about the badge, where 32-bit IS the native width. The
laptop harness (Phase 1) is scaffolding that de-risks the component boundary before hardware; the badge is
the destination.

**What it costs:** WAMR is C and ubiquitous in the ESP world; Wasmtime `no_std` is Rust and newer on
microcontrollers. We trade a well-trodden path for the one that preserves the architecture.

**What it buys:** the WIT world, Decision 31's single entry point, and §6.6's "mirror the standard" story
all survive on embedded. Under WAMR every one of those becomes an embedded-only exception.

### D2b — A TIER is a slot; a TARGET is an artifact. They are not the same axis.

`dlc build web` made them look like one thing, because web and native happen to be 1:1. Embedded breaks
that, and the reason is worth stating plainly: **Pulley bytecode is ISA-independent.** One `pulley32`
artifact runs on the RP2350's Cortex-M33, on its Hazard3 RISC-V cores, and on an ESP32-P4.

| | granularity | how many |
| --- | --- | --- |
| **tier** — host slot: display, buttons, HAL, boot block | per chip | grows with every board |
| **target** — the artifact | per pointer width | **two, and always will be** |

So `dlc build <tier>` is the fine-grained verb a user types, and it resolves to one of two artifacts.
Naming artifacts after boards would invent variants that do not exist.

**A target is a triple PLUS a profile.** `pulley32` alone does not determine whether a runtime can load the
result — `pulley32 + no-CoW + no-signals + this feature set` does, because a `.cwasm` records the settings
of the compiler that produced it. That is `TargetSpec.NoStdProfile` in `engine/tiers.go`, and it is a field
rather than a flag precisely because remembering it is what cost an afternoon.

**Tiers are named for the CHIP, not the board** (`rp2350`, `esp32p4`): the HAL, memory map and boot block
are chip-level, and only crystal, pins and flash size are the board's. `badge-wamr` and `esp32-wamr` are
gone — the first is now `rp2350` on Pulley, and the second named the one ESP32 family this approach cannot
reach (D8).

**`dlc build <tier>` routes on the target, and that is the whole payoff of the split.** The verb takes a
tier because that is what a user has (a board); it switches on the target because that is what a compiler
needs. `esp32p4` therefore required no builder code at all — it is a row in `TierLandscape` whose `Target`
says `pulley32`, and it produces the same `build/engine.pulley32.cwasm` as `rp2350`. Had `dlc build` matched
on tier names, every board would have been a new `case` emitting bytes identical to an existing one.

### D3 — The badge host is Rust (`no_std`), not C — and the RUNTIME IS INHERITED, not per-app

Decision 18 assumed C/ESP-IDF. On RP2350 the mature paths are `rp-hal`/Embassy in Rust or the pico-sdk in
C, and Wasmtime is a Rust crate — a C host would mean FFI around a Rust runtime, which is the worst of both.

**Where it lives matters more than what it is written in.** `HOST-LAYER-PLAN` already draws this line —
inherited runtime in `dlc-platform/`, this app's presentation and input in `hosts/<tier>/` — and the web
tier is the worked example: `dlc-platform/web/` is the jco worker, the OPFS bridge and the vendored shim,
while an app's `hosts/web/` is just a DOM slot. Embedded mirrors it exactly:

**Layout, and the naming is load-bearing.** `dlc-platform/embedded/src/` names no chip — checked, not
claimed: nothing in it mentions RP2350, Cortex-M, or a HAL. Anything that does lives in a sibling named for
its target:

```
dlc-platform/embedded/
  src/            the portable half — Pulley, the WASI host, the capability imports
  rp2350/         firmware for the badge's CHIP (board values flagged in-source)
  qemu-armv7m/    the emulator harness — Cortex-M3, and the name says so
```

"embedded" therefore means "the part with no target in it", and the day it runs on a second chip that stops
being a hope. Naming the emulator crate `qemu` would have hidden that it is not an RP2350 at all.

| | `dlc-platform/embedded/` (inherited) | the app's `hosts/embedded/` |
| --- | --- | --- |
| Pulley runtime, component instantiation | ✅ | |
| WASI 0.2 host implementations (D4) | ✅ | |
| the `devalbo:ilc/events` import | ✅ | |
| drawing a board on the TFT, five buttons → commands | | ✅ |

A Pulley host inlined into each app would be the mistake §16.6 exists to prevent, in a third language: code
copied into a scaffold is frozen there, so a fix to the WASI filesystem shim could never reach an app
someone generated last month.

**Consequence: a third distribution artifact** — a Rust crate beside the Go module and the npm package.
Cargo handles this better than npm did: a git dependency finds a crate in a subdirectory of a repo, so no
`subtree split` branch is needed. The Rust half is closer to the Go story than the npm one.

**The embedder owes real work, and it is bounded:** Wasmtime `no_std` requires the equivalent of
`wasmtime-platform.h` — allocating virtual memory and friends — or the `custom-virtual-memory` /
`custom-native-signals` features. Missing symbols are a link error, not a runtime surprise, which is the
good kind of unimplemented.

### D4 — WASI 0.2 is implemented BY THE HOST, minimally, and that is the actual work

The engine's world does `include wasi:cli/imports@0.2.0`. On a machine with no OS, `wasmtime-wasi` (which
is `std`) is unavailable, so the badge host implements the interfaces the engine actually touches:

| Interface | On the badge |
| --- | --- |
| `wasi:cli/stdout`, `stderr` | UART |
| `wasi:clocks` | the onboard PCF85063A RTC, or a monotonic tick |
| `wasi:random` | RP2350's hardware RNG |
| `wasi:filesystem` | **RAM first** (D5), littlefs later (Phase 4) |

This is the "standard capabilities bifurcate" problem stated honestly. Custom capabilities — events,
display, buttons — are the *easy* half, because Decision 33 already gave them a shape that works on both
substrates. It is the standard ones that need writing.

**The real import list, read off the compiled artifact** rather than the WIT (`spikes/wasmtime-go-cm/`
introspects it) — longer than the table above guessed:

```
devalbo:ilc/events                  ← the custom capability
wasi:cli/environment@0.2.0
wasi:io/error@0.2.0                 ← these arrive WITH stdio, called or not
wasi:io/streams@0.2.0
wasi:cli/stdin|stdout|stderr@0.2.0
wasi:clocks/monotonic-clock@0.2.0
wasi:clocks/wall-clock@0.2.0
wasi:filesystem/types@0.2.0
wasi:filesystem/preopens@0.2.0
wasi:random/random@0.2.0
                                    → exports: execute   (one, as Decision 31 requires)
```

**But the MINIMUM host is smaller than that list and bigger than "what the app calls"** — and it was found
by walking the trap chain (`dlc-platform/embedded/src/minimal.rs`, run on the host under `pulley64`). Stub
everything, add back only what fails, and the component names what it needs in the order it needs it:

| # | What failed | Why |
| --- | --- | --- |
| 1 | `wasi:random/random@0.2.0#get-random-u64` | TinyGo's `_initialize` seeds map hashing |
| 2 | …and `get-random-bytes` | overriding an instance REPLACES it, so a partial override is a type error |
| 3 | `wasi:cli/stdout@0.2.0#get-stdout` | `runtime.initAll` acquires stdout **at init**, before any command |

**Neither random nor stdio can be deferred**, and stdio is the one that costs: `get-stdout` returns a
`resource` handle, so `wasi:io/streams` must be implemented properly rather than with a `func_wrap`. On the
badge that is UART — always the plan — but it is needed for *instantiation*, not for output, so it cannot
be scheduled after "get it booting".

**And most of that work already exists.** `wasmtime-wasi-io` is `default = ["std"]` — so it builds
`no_std` — and it implements `wasi:io/{error,poll,streams}` as **traits an embedder impls**
(`OutputStream`, `InputStream`, `Pollable`). **Verified compiling for `thumbv8m.main-none-eabihf`.**

That changes this decision's cost, and it is the difference between an afternoon and a fortnight:

| | Hand-written | With `wasmtime-wasi-io` |
| --- | --- | --- |
| `wasi:io/streams` | 15 functions across 2 resources, `stream-error` variants wrapping an `error` resource | `impl OutputStream for Uart` |
| `wasi:io/poll` | a `pollable` resource + `poll` | provided |
| `wasi:io/error` | an `error` resource | provided |
| `wasi:cli/stdout` | 1 function returning a resource handle | still ours — it is where UART is chosen |

So D4's real shape is narrower than "implement WASI 0.2": implement **`OutputStream` over the UART**, hand
back handles from `wasi:cli/stdout|stderr`, and let the crate do the resource plumbing. `wasmtime-wasi`
(the big one) stays unusable — it is `std` and drags in `cap-std` — but `wasmtime-wasi-io` is exactly the
layer that was going to hurt.

**Where this stands (2026-08-02): `OutputStream` is implemented; handing out the handle is not.**
`SinkStream` in `dlc-platform/embedded/src/uart.rs` implements `OutputStream` + `Pollable` over any byte
sink, and compiles. Wiring it to `wasi:cli/stdout` fails at instantiation with `resource type mismatch`,
for a reason worth writing down: `get-stdout` returns `own<output-stream>` — the resource defined in
`wasi:io/streams` — and a hand-written `func_wrap` can only declare `Resource<DynOutputStream>`. Wasmtime
matches resource types by **registered identity, not structure**, so the two never unify. Reordering does
not help; it was tried both ways.

The fix is the pattern `wasmtime-wasi` itself uses: generate the `wasi:cli` bindings with `bindgen!` and a
`with` map pointing `wasi:io/streams.output-stream` at `DynOutputStream`, exactly as `wasmtime-wasi-io`'s
`bindings.rs` does for the io package. That needs the `wasi:cli` WIT vendored into the crate, and the same
pattern then serves clocks and filesystem — so it is one piece of work that unblocks all three.

**RESOLVED (2026-08-02), and the fix changes the shape of D4.** `bindgen!` with a `with` map pointing
`wasi:io/streams.output-stream` at `wasmtime-wasi-io`'s `DynOutputStream` generates a `wasi:cli/stdout`
whose resource type unifies — `SinkStream` is now reachable as real stdout.

**But only once `define_unknown_imports_as_traps` was removed entirely.** That was the actual blocker, and
it took an experiment rather than reasoning to see: with the stubs in place, `get-stdout` failed with
"resource type mismatch" in BOTH registration orders, because a stub defines the *resource type* too and a
later real definition does not displace it. Delete the stubs and stdout links immediately — the error moves
on to the next unimplemented import, which is how the cause was identified.

**So the badge implements every import it declares, and trap stubs are not a shortcut it can take.** That is
not a hardship; it is what a capability-injected host *is*. A tier states what it can do, including the
parts it does cheaply, and a stub it wants (no filesystem) gets written deliberately — with a failure mode
chosen rather than inherited.

Two syntax details that cost a run each: `with` keys use **`interface.resource`** (a `/` reads as an
interface path and bindgen says "not referenced in the target world"), and the generated `Host` methods
return the resource **directly**, not wrapped in `Result`.

**A useful consequence:** `func_wrap` is fine for capabilities whose signatures are scalars and bytes —
which is every ILC capability, by Decision 33 — and insufficient the moment a WASI interface passes a
resource. That is a clean line, and it is why the custom half of D4 stayed easy while the standard half did
not.

#### 🟢 The whole host, hand-written, works (2026-08-02)

`dlc-platform/embedded/src/minimal.rs` now satisfies **every import a component declares**, with **no
`wasmtime-wasi`** — which is the badge's exact constraint, proven on a laptop where failures are legible:

| Interface | How |
| --- | --- |
| `wasi:io/{error,poll,streams}` | `wasmtime-wasi-io` (no_std-capable), + our `OutputStream` over a byte sink |
| `wasi:cli/{stdout,stderr,stdin}` | generated bindings; stdin is **closed**, because a badge has no keyboard |
| `wasi:cli/environment` | empty — a badge is not launched from a shell; its "arguments" are five buttons (§14) |
| `wasi:clocks/{monotonic,wall}` | a tick counter and epoch-zero, until the RTC is wired |
| `wasi:filesystem/{types,preopens}` | **no preopens**, 28 descriptor methods that refuse |
| `wasi:random` | xorshift, standing in for the hardware RNG |
| `devalbo:ilc/events` | collected; on the badge this drives the screen |

Two results, and the second is the more interesting one:

```
dlc        execute(1)      → success, "dlc 0.0.0-bootstrap"
tictactoe  execute(10002)  → success: false, "mkdir /: errno 2"
```

**tictactoe failing is the design working.** No filesystem was granted, so a command that persists fails
with an app-level error naming the operation — not a trap, not a panic, not a wrong answer. That is exactly
what D5 replaces: a RAM-backed filesystem changes one block in `minimal.rs`, and the engine, the app and
every other interface stay untouched.

**One more thing the badge inherits from this: an executor.** `wasi:io` is registered async, so the store
needs `async_support` and calls go through `call_async`. `block_on.rs` is 30 lines and spins, which is only
sound because every pollable this host owns is *always ready* — nothing waits on a timer or an interrupt.
That is written at the top of the file, because the day a genuinely blocking capability arrives, this
becomes a firmware that hangs with no clue why.

Three linker mechanics, each of which cost a run to learn:

- **Interface names are VERSIONED**: `wasi:random/random@0.2.0`, not `wasi:random/random`. An unversioned
  key silently defines a *different* instance and the stub still runs.
- **`define_unknown_imports_as_traps` goes FIRST**, with `allow_shadowing(true)` — it creates every
  instance unconditionally, so defining one beforehand fails with "defined twice".
- **Shadowing is per-instance, not per-function.** Override an instance and you own all of its exports.

### D5 — A RAM-backed filesystem is a real capability, not a stub

tictactoe persists its board through `platform.ReadFile` / `WriteTree`. A host that implements
`wasi:filesystem` over a RAM buffer is a legitimate ILC host: the game works, and the board does not survive
a power cycle.

**The engine does not change by one line.** That is the entire thesis under test, and it makes the first
milestone dramatically cheaper — no littlefs, no flash wear, no filesystem debugging on a device with one
UART. If this does not hold, the thesis is wrong and better to learn it in week one.

### D6 — Display and buttons are CUSTOM capabilities, flat-shaped, and the display one may not be needed at all

WASI has no display standard (`wasi-gfx` is Phase 2, unstable, WebGPU-centric) and no GPIO/button standard
(embedded-WG proposals only). So both are custom ILC imports, shaped like `events`: scalars + bytes, no
rich WIT records, per Decision 31/33.

**But start without a display capability.** Decision 34 made app-side Display optional precisely because
the alternative is better: the engine emits a *semantic* event and each host draws it however that tier
likes. tictactoe already works this way — `StateChangedEvent` carries the board, and the ASCII slot and the
DOM slot render it independently. **The badge is a third slot, not a new capability.** Buttons are input,
which §14 already answers: the host maps native input to a command.

### D7 — Parity extends to the badge, but only as far as software can carry it

Pulley can be exercised on a laptop, so the golden vectors can gain a third column **with no hardware** —
catching the class that matters most, the interpreter disagreeing with jco or native Go about what the
engine does.

**But not with the `wasmtime` CLI, which was the plan's first mistake — measured 2026-08-02.** The pinned
binary (46.0.1) compiles *for* `pulley32`/`pulley64` and then refuses to run the result:

```
$ wasmtime run --allow-precompiled engine.pulley.cwasm
Error: failed to load code for: … Module was compiled for architecture 'pulley32'
$ wasmtime run -C help
  -C compiler=winch|cranelift        ← no pulley backend in the distributed build
```

Compiling for Pulley and *executing* Pulley are separate features, and the shipped CLI has only the first.
So the third parity column needs a **small Rust harness** using the `wasmtime` crate with the `pulley`
feature — which is not a detour: it is the badge host minus `no_std`, so Phase 1 becomes the stepping stone
to Phase 2 rather than a throwaway.

**UPGRADED 2026-08-03: the column can be `pulley32`, not a proxy.** This decision originally settled for
`pulley64` on a laptop, because a host executes only its own pointer width and the CI machines are 64-bit.
`qemu-armv7m/` removes that compromise — it runs the **badge's actual width** on an emulated 32-bit core,
with no hardware, no probe, and no human. So the third column is the real artifact rather than a
same-semantics stand-in.

What it still cannot be is a substitute for the board: 4 MB of emulated SRAM against the RP2350's 520 KB
plus QSPI PSRAM, a Cortex-M3 rather than an M33, and no peripherals at all — the boot-block bug that would
have bricked the first flash was caught by `picotool`, not by QEMU. The division that holds: **QEMU owns
"does the portable half still work at 32 bits", the badge owns "does this board run it"** — which is
exactly the `src/` vs `rp2350/` split.

Blocked until the artifact/runtime settings mismatch (§Phase 0c) is resolved: the harness can create an
engine but not yet load a component, so its regression value is potential rather than banked.

What CI cannot check is the *host*: UART, the TFT, five buttons, the RTC. That stays a manual gate with a
photograph, and the plan should say so rather than implying a robot arm.

### D8 — RISC-V IS A REQUIREMENT, not a preference. Xtensa is out of scope.

Pulley's requirement is that Wasmtime compiles for the Rust target, which decides the whole question:
**an ILC embedded target must be a chip with an upstream Rust target.** In practice that means ARM Cortex-M
or RISC-V, and it means **Xtensa — and therefore ESP32-S3, which Decision 18 named — is out of scope.**
Reaching it would mean running Wasmtime on the esp-rs Xtensa fork of the toolchain, which is a research
project wearing a product's clothes.

Stating this as a requirement rather than a finding is what makes the rest of the plan small. There is one
host crate, one toolchain story, and no per-chip exceptions to carry.

| Chip | ISA | Verdict |
| --- | --- | --- |
| **RP2350** (Tufty 2350) | Cortex-M33 **and** Hazard3 RISC-V — the part ships both | ✅ the primary target |
| **ESP32-P4** | `riscv32imafc`, dual core, PSRAM | ✅ the ESP target to try — closest in shape to the RP2350 |
| ESP32-C6 / H2 | `riscv32imac` | ✅ likely; no PSRAM, so a smaller world |
| ESP32-C3 | `riscv32imc` — **no atomics** | 🚧 needs `portable-atomic` + `critical-section`; unverified |
| ESP32-S3 | Xtensa LX7 | ❌ **out of scope** — not an upstream Rust target |

**And the RP2350 is dual-architecture, which is a genuine opportunity here.** It ships two Cortex-M33s
*and* two Hazard3 RISC-V cores, and boots either. So a RISC-V requirement does not exclude the badge — it
lets the badge and the ESP32 targets be **the same Rust target family** (`riscv32imac-unknown-none-elf`),
which collapses the host crate's toolchain story from two ISAs to one.

Whether to boot the Tufty in RISC-V mode is worth deciding in **Phase 0**, where the cost of trying is one
target triple: `rp235x-hal` supports both, but the ARM path is the better-trodden one today. Cortex-M33 is
the safe default; Hazard3 is the one that makes D8 uniform. Measure both in the gate, pick then — the only
wrong move is picking now, on argument.

**None of this is on the critical path.** RP2350 first; ESP32 is Phase 6, and its Phase 0 is the same gate
run against a different chip.

---

## 3. Phases

Each leaves the tree green. **No phase is done until something can be broken on purpose and observed going
red** (`AGENTS.md` §5).

### Phase 0 — THE GATE: does Wasmtime `no_std` + Pulley + `component-model` fit on an RP2350 at all?

The smallest possible thing: a Rust `no_std` firmware that links Wasmtime with `pulley` +
`component-model`, instantiates a **trivial** component (not tictactoe), calls one export, and prints the
result over UART.

**What it answers, in order of how likely it is to kill the plan:**

1. **RAM.** 2.21 MB of `.cwasm` lives in flash, but Pulley's interpreter state, the component's linear
   memory, and the resource tables live in RAM — 520 KB of SRAM plus 8 MB of PSRAM over QSPI, which is
   slower. Nobody has measured this. It is the single most likely reason this plan dies.
2. **The platform layer.** How much of `wasmtime-platform.h` a bare-metal target really needs.
3. **Link size.** Wasmtime + Pulley + component-model as `.text`, on top of the 2.21 MB payload.

*Falsify:* it does not fit, or does not link. Then the fallbacks are, in order: trim the world and
re-measure; then native TinyGo, which **is measured**: the tictactoe engine builds for `pico2` as a **263 KB**
`.uf2` today, blocked only by a build-tag bug (§4).

**Do not skip to Phase 2 because Phase 0 is boring.** Every later phase assumes the answer.

#### 🟢 Phase 0a — it COMPILES (2026-08-02)

The half that needs no hardware is done. Wasmtime **46.0.2** with `default-features = false` and
`runtime + component-model + pulley`, `#![no_std]`, compiles clean for **both** badge targets:

| Target | Result |
| --- | --- |
| `thumbv8m.main-none-eabihf` (RP2350, ARM mode) | ✅ compiles |
| `riscv32imac-unknown-none-elf` (Hazard3, and every RISC-V ESP32) | ✅ compiles |

Two things this settles and one it does not. **Settled:** the crate graph really is `no_std`-clean with the
component model turned on — the single most likely place for this plan to have been fantasy — and D8's
"one Rust target family for badge and ESP" is real, because the same crate builds for both.

**Not settled, and still the gate:** *linking* (the `wasmtime-platform.h` symbols an embedder owes) and
**RAM at runtime**. Compiling proves the code exists for the target; it says nothing about 520 KB of SRAM.

#### 🟢 Phase 0b — the toolchain (2026-08-02)

Cross-compiling to a microcontroller needs per-target `core`, which **nixpkgs does not ship** — `rustc`
knows the triple and fails with "the target may not be installed". So `devbox.json` carries `rustup` and
the toolchain pin moved to a checked-in **`rust-toolchain.toml`** (channel, components, both targets).
Reproducibility is preserved; only the mechanism changed. This reverses the earlier "no rustup, it fetches
outside the lock" call, which was right in principle and impossible in practice.

### Phase 1 — the same component, under Pulley, on a laptop (`std` Rust)

A small Rust harness using the `wasmtime` crate with `pulley` + `component-model`: load the component,
provide WASI 0.2 (via `wasmtime-wasi`, which exists on `std`), bind `devalbo:ilc/events`, run the golden
parity vectors, diff against the native and jco columns.

**Not the `wasmtime` CLI** — it cannot execute Pulley (D7). And not a detour: this harness *is* the badge
host with `std` still attached, so Phase 2 is a port rather than a rewrite. Everything hard about the
component boundary — WASI wiring, the custom import, lifting `execute(u32, list<u8>)` — gets solved here,
where there is a debugger and a filesystem, instead of over a UART.

Cheap in the only currency that matters: it de-risks what hardware debugging is worst at — discovering the
*interpreter* disagrees about engine behaviour while you stare at a serial log.

**This adds Rust to the toolchain** (`devbox.json` gains `rustc` + `cargo`, not `rustup` — rustup fetches a
toolchain outside the lock, which is the thing `devbox.lock` exists to prevent). A real cost: every
contributor now provisions it. It was always coming with Phase 2; Phase 1 just makes it useful a week
earlier.

**🟢 GREEN (2026-08-02)** — `dlc-platform/embedded/`, `cargo run --bin pulley-probe`:

```
component: 1704107 bytes
pulley64: OK — compiled to 1929880 bytes of pulley bytecode
pulley32: FAIL — target 'pulley32' does not match the host
```

Pulley accepts a real ILC component, imports and all. **And the failure is the more useful half: Pulley
bytecode is pointer-width specific, and a host can only EXECUTE the width it is.** A 64-bit laptop can
compile for `pulley32` (the CLI does) but never run it; the badge runs `pulley32` and nothing else.

So the third parity column is **`pulley64`, and it is a proxy rather than the artifact**. It is a good
proxy — the guest is `wasm32` either way and the interpreter's semantics are the same — but the plan must
not claim the laptop runs what the badge runs. It runs the same component through the same interpreter at a
different pointer width. Anything that would differ between the two is, by construction, exactly what only
hardware can catch.

*Falsify:* inject a Pulley-only divergence and watch the parity check catch it, exactly as
`verify-parity-selftest.sh` does for the wasm column.

### Phase 0c — QEMU, and the first real answer about RAM 🟡

`dlc-platform/embedded/qemu-armv7m/` runs the firmware on an emulated 32-bit ARM core (QEMU `mps2-an385`,
semihosting for output). **Not the badge** — no RP2350 peripherals, no PSRAM, and a Cortex-M3 rather than
the M33, because QEMU's real M33 machine (`mps2-an505`) boots secure at an address `cortex-m-rt`'s default
layout does not match, and the claim under test does not depend on ARMv8-M.

**What it settles:**

```
=== ILC on an emulated 32-bit ARM (QEMU mps2-an385) ===
heap: 512 KB (pinned to the RP2350's SRAM)
engine: created for pulley32 on a 32-bit core     ← the width a 64-bit laptop cannot run
```

**Wasmtime + Pulley builds a `pulley32` engine on a 32-bit ARM core in 512 KB.** That was the largest
untested assumption in this plan and it holds.

**And the finding that changes the hardware plan — PSRAM is mandatory, not an optimisation:**

```
component: deserialize FAILED: out of memory (failed to allocate 1594168 bytes)
```

`Component::deserialize` wants **one contiguous allocation the size of the artifact**. hello's is 1.59 MB
and tictactoe's is 2.21 MB, so **520 KB of SRAM can never hold either** — no amount of trimming the world
changes an allocation that is by construction the artifact's size. The badge's 8 MB of PSRAM stops being
"where the heap should probably live" and becomes the thing without which nothing runs, which promotes it
from step 2 of the firmware to step 1.

#### 🟢 WITHDRAWN 2026-08-07 — it was `deserialize` that was mandatory, not PSRAM

The allocation above is real, and the conclusion drawn from it was wrong. **`Component::deserialize` copies**
— it routes through `MmapVec::from_slice_with_alignment`, which allocates and then memcpys — so the demand
for 890 KB of contiguous heap was a property of the *API call*, not of loading a component.

`Component::deserialize_raw` takes externally-owned memory instead, and `engine.rs` states the contract that
makes this safe: *"the memory provided is guaranteed to only be immutably [read] by the runtime"*. So the
artifact can stay in flash, where XIP already makes it directly addressable. **Pulley bytecode is
interpreted, never executed natively**, so there was never a reason for it to be in RAM — which is the part
that should have been noticed a week ago, from the architecture rather than from an allocator.

Same harness, same 890 KB payload, heap set back to the badge's real SRAM:

```
heap: 512 KB (pinned to the RP2350's SRAM)
payload: 890048 bytes of pulley32
payload at: 0x38200 (flash is below 0x20000000)
component: DESERIALIZED — it fits
heap used: 81 KB of 512 KB (artifact stayed in flash)
```

**81 KB, not 890 KB**, and the address is printed rather than asserted so a linker change that quietly moved
the payload into `.data` cannot make this pass while proving the opposite. *Falsified:* swapping back to
`deserialize` goes red at 512 KB with `out of memory (failed to allocate 890048 bytes)` — the original
finding, reproduced on demand.

One mechanical detail that will bite anyone repeating this: the payload must be **16-byte aligned**.
Wasmtime parses a `.cwasm` as an ELF and the header reads require it; `deserialize` never had to care
because an allocator returns aligned memory, and `include_bytes!` alone promises 1.

**What this does and does not settle.** Settled: loading is not the RAM gate, and PSRAM is not a
prerequisite for it. Not settled: *instantiation* additionally needs the component's linear memory and
resource tables, which nobody has measured. PSRAM may still be wanted — but it must now be sized against a
real number rather than against an artifact-sized allocation that no longer happens.

#### 🟢 Phase 0d — instantiation measured, and hello RUNS on a 32-bit core (2026-08-07)

The QEMU harness now instantiates through **`MinimalHost` — the badge's own hand-written host**, not a
stand-in. `dlc-platform-embedded` builds `--no-default-features` as `no_std` for `thumbv7m-none-eabi`
(a `std` feature gates `host.rs` and Cranelift), so the emulator and the firmware share one implementation
rather than two that drift.

```
heap after load:        81 KB   (artifact stayed in flash)
heap after instantiate: 2911 KB
execute(10000): success=true
output: hello, world — from hello
verdict: 2911 KB needed vs 520 KB of RP2350 SRAM -> PSRAM REQUIRED
```

**Two results, and they point opposite ways.**

The good one: **an ILC app runs end to end on an emulated 32-bit ARM core**, from a flash-resident AOT
artifact, through the host the badge will actually use, with no `wasmtime-wasi` and no OS. Phase 1b's rung
is cleared at the badge's pointer width.

The constraining one: **PSRAM is required after all — for instantiation, not for loading.** 2.9 MB against
520 KB of SRAM. So the original conclusion was right by accident and wrong in its reason, which matters,
because the reason determines the fix: no amount of shrinking the *artifact* would have helped, and the
2.9 MB is dominated by the guest's linear memory. That is a TinyGo build-time knob, so "does hello fit in
SRAM" is not yet a closed question — it is now a question about the guest's heap size rather than about
Wasmtime. Either way it fits comfortably in the badge's 8 MB.

**The bug that blocked this is worth more than the number, and it is a VERSION bug, not a coding one.**
`wasmtime-platform.h`'s TLS hooks changed shape between the wasmtime we started against and the one we pin:

```c
void *wasmtime_tls_get(void);              /* 35.0.0 */
void  wasmtime_tls_set(void *ptr);

void *wasmtime_tls_get(size_t slot);       /* 46.0.1 */
void  wasmtime_tls_set(size_t slot, void *ptr);
```

`platform.rs` was a **correct implementation of the older contract**. `extern "C"` links by name alone, so
the ABI changed with no error from the compiler, the linker, or a deprecation warning. Wasmtime's
documentation is accurate and the runtime is not buggy — the assertion is wasmtime correctly *detecting* a
broken embedder contract. Nothing in the toolchain connects the two.

**The mechanism is dumber than it looks.** Wasmtime calls `wasmtime_tls_set(0, ptr)`; on ARM `slot` lands in
r0 and `ptr` in r1. The one-argument version read r0 — so it stored `0` every time and discarded the
pointer. The TLS slot was permanently null, `push` recorded null as the previous head, and `pop` read null
and asserted it equalled `self`. The activation chain was never stored at all. **Not slot collision:** slot
1 is `component-model-async`, which this build does not enable, so wasmtime never passes it.

It could not have been caught earlier, because loading a component never touches TLS — only instantiation
reaches it. **The rule this earns:** every symbol in `platform.rs` must be diffed against wasmtime's own
`runtime/vm/sys/custom/capi.rs` **on every version bump**, because the linker will not do it for you and a
pinned version is exactly the thing that makes this invisible until the pin moves.

Two things ruled out on the way, so nobody re-runs them: the TLS slot being cached under LTO (making it
atomic changed nothing), and `wasmtime/async` lacking a backend — `wasmtime-internal-fiber` has a `no_std`
implementation and `target_arch = "arm"` selects real Thumb-2-compatible stack-switching assembly.
`wasmtime-wasi-io` has **only** `add_to_linker_async`, so there is no sync path to retreat to — and none is
needed.

#### 🟢 RESOLVED (2026-08-03) — and what `.cwasm` actually contains

**A `.cwasm` records what the COMPILER BINARY WAS BUILT WITH, not what flags it was given.** `-W gc=n` and
`-W component-model-async=n` change nothing, because the stock CLI *is* built with them — so a
`default-features = false` runtime rejects its artifacts one feature at a time (CoW → signals → GC →
collector → concurrency, five rounds, each adding weight to a microcontroller).

`dlc-platform/embedded/precompile/` is the fix: **`cranelift` WITHOUT `runtime`**. That is also why the
first attempt died at `Engine::new` — a *runtime* engine requires target == host; a compile-only one does
not, so `pulley32` from a 64-bit machine is fine. With both sides built alike, both can be **lean**.

Two settings must still agree explicitly, and do so in both files: `memory_init_cow(false)` and
`signals_based_traps(false)` — a `no_std` target has neither virtual memory nor host signal handlers.

**What the artifact is.** Not machine code: targeting `pulley32`, Cranelift emits **Pulley bytecode**, which
the device interprets. AOT buys not speed but the absence of a compiler, which `no_std` cannot ship anyway.
The container is an ELF (labelled `elf64-littleriscv` — Pulley is not a real ISA, so Wasmtime borrows
RISC-V's machine number):

| section | hello | what |
| --- | --- | --- |
| `.text` | 749 KB | the Pulley bytecode |
| `.wasmtime.addrmap` | **679 KB** | wasm↔code offset map, for backtraces |
| `.rodata.wasm` | 36 KB | data segments |
| `.name.wasm` | 17 KB | debug names |
| `.wasmtime.engine` | 823 B | the settings blob that caused all of the above |

**44% of it was debug metadata**, and on a device where the whole artifact must be ONE contiguous
allocation that is the cheapest RAM win available. `generate_address_map(false)`:

```
1,568,480 -> 890,048 bytes   (-43%)
heap needed: 3 MB -> 1 MB
```

The cost is honest: a trap on the badge reports an address rather than a wasm location. Worth it while the
question is still "does it load at all".

**~~Still true, and still the gate: PSRAM is a prerequisite.~~** ~~890 KB will not fit in 520 KB of SRAM
either. The margin just went from hopeless to close.~~ — **withdrawn 2026-08-07, see above.** 890 KB does
not need to fit in SRAM, because it does not need to leave flash. The size win below is still worth having
(it is 890 KB of flash rather than 1.57 MB, and less to read over XIP), but it was pursued as a way to fit
an allocation that turned out to be avoidable.

<details><summary>the original blocker, kept for the reasoning</summary>

**🟡 One thing still blocked, and it is not memory.** With a large enough heap the error becomes:

```
component: deserialize FAILED: compilation settings are not compatible with the native host
```

The `.cwasm` is produced by devbox's stock `wasmtime` CLI; the firmware links `wasmtime` with
`default-features = false`. Those two builds disagree about compilation settings, and pinning both to the
same version (=46.0.1) does **not** fix it — so it is the feature set, not the version. The artifact has to
be produced by a compiler built like the runtime.

`src/bin/precompile.rs` is the intended fix — AOT through the same crate, so the two cannot drift — but it
fails at `Engine::new`, which refuses a target that is not the host. The CLI manages it, so there is an API
for cross-compilation; finding it is the next step, and until then the badge cannot load a component.

</details>

**A process note worth keeping:** the first version of this firmware printed "deserialize FAILED (likely
out of memory)" without the error text, to save code size. That guess cost three builds of heap tuning
before the real message appeared and said something completely different. On a target with one output
channel, print the error.

### Phase 1b — `example-apps/hello`, the rung between "it runs" and "the game runs"

**Scaffolded with `dlc new`, not hand-written** — so what the badge runs is what a user would actually
produce, and the scaffolding path is exercised rather than assumed. One `greet` command, no persistence.

| | |
| --- | --- |
| component | **1.48 MB** (tictactoe is 1.82 MB) |
| `pulley32` artifact | **1.59 MB** |
| under the hand-written host | ✅ `execute(10000)` → `hello, world — from hello` |

**Why it earns its place rather than being ceremony:** it is the smallest thing that is still a real ILC
app, so a failure on hardware is unambiguous. If `hello` runs and tictactoe does not, the difference is
size or the filesystem — not the runtime, not the ABI, not the host. That is a much better position to
debug from than a single 2.21 MB attempt that either works or does not.

It also isolates the capability question cleanly: `hello` needs **no filesystem**, so it is the app that
should run on a badge with no storage at all, while tictactoe is the one that proves D5's RAM backing.

### Phase 2 — tictactoe on the badge, over serial, with no screen

The Rust host: instantiate the real component, implement `wasi:cli/stdout` → UART, `wasi:random`,
`wasi:clocks`, and a **RAM-backed `wasi:filesystem`** (D5). Drive `new-game` and `play` from serial input.

The milestone is a game of tic-tac-toe played over a serial console, on a badge, **by the same engine and
the same bytes the browser runs**. No display yet — the ASCII slot's projection already renders a board,
and reusing it is the point.

*Falsify:* comment out the engine's win detection and confirm the badge goes wrong **identically** to the
CLI and the browser — the same probe tictactoe's host-parity test already uses.

### Phase 3 — the TFT slot and the buttons

`hosts/embedded/` becomes a third slot: subscribe to `StateChangedEvent`, draw a 320×240 board, map five
buttons to `play`. Host parity gains a third column — same state in, three projections that must agree.

*Falsify:* the existing "decision probe" — a slot must not notice a win the engine did not report.

### Phase 4 — persistence

Swap the RAM filesystem for littlefs over the 16 MB flash. The board survives a power cycle.

**The app does not change.** If it does, D5 was wrong about what a capability is.

### Phase 5 — write it down

Decision 18 amended (WAMR → Wasmtime/Pulley for RP2350; C → Rust). §10's environment matrix gains a real
row. `AGENTS.md` gains whatever rule the embedded host turns out to need. `spikes/README.md`'s "one artifact
was never achievable" finding gets its correction — it was true of WAMR and is not true of Pulley.

### Phase 6 (deferred) — ESP32, and the CLI joining the same artifact

- **ESP32:** re-run Phase 0 against an **ESP32-P4 or C6** (D8). Not S3.
- **The CLI: probed 2026-08-02, and the answer is no** (`spikes/wasmtime-go-cm/`). v47 does ship a
  component API — it loads, introspects and serializes — but it cannot **define imports or call exports**,
  and has no WASI 0.2. So the CLI keeps its native in-process engine and "the same artifact" means the
  browser and the badge. Re-run that spike when `ComponentFunc` lands; if it goes green, Decision 26's
  justification is gone and the claim becomes literally true on every tier.

---

## 4. Risks

| Risk | Why it bites | Mitigation |
| --- | --- | --- |
| **RAM on the RP2350** | 2.21 MB in flash is easy; interpreter + linear memory in 520 KB SRAM is not, and PSRAM is slower QSPI | **Halved 2026-08-07**: `deserialize_raw` leaves the artifact in flash, so loading costs 81 KB, not 890 KB (Phase 0c). What remains unmeasured is instantiation — linear memory and resource tables |
| **Wasmtime `no_std` on Cortex-M is young** | the docs name no tested embedded architectures | the fallbacks are real and one of them is measured (263 KB native TinyGo) |
| **Interpreted speed** | Pulley is an interpreter; tictactoe is trivial, a future app may not be | measure in Phase 2; the badge is a demo tier, not a compute tier |
| **The build-tag bug** | `caps_wasip2.go` is `//go:build tinygo`, so any non-wasm TinyGo target tries to link `wasmimport_Emit` | one-line fix (`tinygo && wasm`); **verified** to unblock a `pico2` build |
| **`wasi:filesystem` is a big interface** | implementing it `no_std` could dwarf the rest | D5's RAM backend implements only what the engine calls; grow on demand |
| **Two runtimes to keep in step** | jco and Pulley can disagree | **Closed 2026-08-05** — Pulley is a parity column, on every `ci.sh full` |
| **Xtensa** | ESP32-S3 is not an upstream Rust target | D8: RISC-V ESP parts only, and not on the critical path |

---

## 5. What this plan does NOT do

- **No WAMR.** It cannot run a component, which is the whole reason this route exists.
- **No second engine build.** If a phase needs one, the plan has failed its own constraint.
- **No display capability** (D6) — the badge is a slot rendering semantic events, like every other tier.
- **No ESP32-S3.** D8.
- **No hardware in CI.** Phase 1 puts Pulley in CI; the board stays a manual gate.
- **No claim of byte-identical artifacts** (D1). One source artifact, mechanically retargeted.

---

## 6. Definition of done

1. [ ] A Rust `no_std` firmware runs a component under Pulley on the RP2350, with measured RAM headroom.
2. [ ] tictactoe is playable on the badge, from the same component the browser runs.
3. [ ] The engine has not changed to make any of it work.
4. [x] Pulley is a third column in the parity check, **watched going red** — done 2026-08-05.
   `dlc-platform/embedded/parity/` runs the golden vectors through the same interpreter the badge uses
   (`pulley64` here, `pulley32` there) and diffs against NATIVE, so a failure reads as "Pulley disagrees
   with the engine" rather than "two wasm runtimes disagree". 30 vectors and a 71-file tree, identical.
   The self-test asserts the pulley diff BY NAME: both its probes perturb the component, which Pulley also
   runs, so a generic "PARITY MISMATCH" grep would have stayed green if this column quietly stopped
   comparing anything.
5. [ ] Three slots render the same state and agree — CLI ASCII, browser DOM, badge TFT.
6. [x] Decision 18 says what is actually true, including why WAMR was dropped — done 2026-08-04, along with
   Decision 25 (the ABI toggle WAMR required) and the `templates/wamr/` skeleton family it implied.
7. [ ] The README's embedded row is ✅ with a check behind it, and states the AOT caveat rather than hiding it.
