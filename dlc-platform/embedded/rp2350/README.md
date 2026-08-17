# Flashing the badge

Bring-up firmware for the Pimoroni Tufty 2350. See
[`docs/EMBEDDED-PLAN.md`](../../../docs/EMBEDDED-PLAN.md) for why any of this exists.

**Running the badge for the first time? Follow [`BRINGUP.md`](./BRINGUP.md)** — the step-by-step, with the
expected output at each stage and what every failure mode means. This file is the reference; that one is the
procedure.

```
make badge-uf2        # -> build/badge-bringup.uf2   the firmware
make badge-payload    # -> build/badge-payload.uf2   hello, draggable
```

**Two artifacts, and that is the design.** The firmware is app-agnostic by default: it runs whatever it finds
in a well-known flash region and shows what comes back. Adding an app means dragging a payload UF2 onto the
board, not rebuilding firmware.

## What gets flashed, and what it can run

Both are **flash-time** choices — `make` variables, not source edits (see [`build.rs`](./build.rs)).

| | |
| --- | --- |
| `make badge-uf2` | **empty loader** — runs only what is in the region *(default)* |
| `make badge-uf2 BADGE_PAYLOAD=$PWD/build/hello.pulley32.cwasm` | that app baked in; the region still adds more |
| `… BADGE_PAYLOAD=… BADGE_REGION=off` | that app and nothing else |
| `make badge-uf2 BADGE_WORLD=minimal` | the minimal world (below) |
| `make badge-uf2 BADGE_BEAT_MS=700` | watchable bring-up — each stage lingers ~700 ms (default 0: full speed) |

### The two worlds

**Not two WIT worlds** — same component, same imports, same `.cwasm`. A WIT world is an import/export set, and
forking one would fork the artifact, which is the constraint the whole embedded tier is built to preserve.
What differs is which channel reaches a human:

| | `normal` *(default)* | `minimal` |
| --- | --- | --- |
| the app's text | shown | **not shown** |
| status colour | shown | shown |

`minimal` is a **simulation, and that is its point**: the Tufty has a screen and a UART, so this world is the
badge pretending to be a device that has only a status LED. It exists to answer what no rich tier can — what
can an ILC app still say when its host has almost no way to speak? A sensor node or a keyfob is a real target,
and running the constraint on hardware that *could* have shown text is what keeps the serial log available to
see what the app tried to say.

Its capabilities are a **strict prefix** of `normal`'s, checked by a `const` assertion in
[`src/world.rs`](./src/world.rs) rather than by convention — a simulated floor is only useful if it is
genuinely a subset.

## Adding an app without a toolchain

Hold **BOOT**, tap **RESET**, drag `build/badge-payload.uf2` onto the `RP2350` drive, reset. The bootloader
writes each block to the address that block names — the payload region, well clear of the firmware — so
**the firmware is untouched**.

**It must be a UF2.** The BOOTSEL drive is a synthetic FAT12 volume, not storage: its only real operation is
parsing UF2 blocks. A `.cwasm` dragged onto it is accepted by the Finder and discarded by the bootloader,
silently, with no error anywhere.

Several payloads go in one image, each carrying its own entry method so the loader can run apps it was not
built for:

```
make badge-payload PAYLOADS="hello=build/hello.pulley32.cwasm ttt@10002=build/ttt.pulley32.cwasm"
```

`picotool` does the ELF→UF2 conversion, and `make badge-uf2` **gates on the result**: a UF2 whose family is
not `rp2350` will not boot this board, and the only symptom is a badge that does nothing at all. `elf2uf2-rs`
was used first and got exactly that wrong — it emitted family `rp2040` from RP2350 firmware. picotool is in
`devbox.json` (2.2.0-a4), so no separate install is needed.

## The problem this procedure exists to solve

**Silence is ambiguous.** A wrong crystal, wrong UART pins, a hard fault in the PSRAM driver, and a board
working perfectly with no serial adapter attached all look *identical*. So each section below adds exactly one
assumption. Skipping ahead means debugging several unknowns at once, and this board gives you almost nothing
to debug with.

**These sections are deliberately unnumbered**, and [`BRINGUP.md`](./BRINGUP.md) holds the only numbering
there is: **Step** for what you do, **stage** for what the firmware checks. A reference file with its own
counter is how "stage 2 failed" came to mean two different things.

## Prove the flashing path BEFORE your firmware is in question

Flash Pimoroni's own firmware first: **<https://github.com/pimoroni/tufty2350/releases/latest>**

Take the **`-micropython-with-filesystem.uf2`** variant. The plain `-micropython.uf2` replaces only the
firmware and leaves whatever filesystem is present; the with-filesystem build restores the board to factory
state, which is the reference point you want to be able to return to.

**Why this one rather than a stock blink:** its success signal is the TFT lighting up with a working menu —
unmistakable, no squinting at an LED. This board has no plain GPIO LED at all (the LED is on the CYW43
module), so a generic blink UF2 may show you nothing even when it works. It also **validates the display and
backlight before anything of ours depends on them**, which matters if backlight blink codes ever become the
status channel.

## Enter BOOTSEL and copy

1. **Press and hold BOOT** (far left).
2. **Briefly tap RESET**, to the right of BOOT.
3. A drive named **`RP2350`** appears.
4. Copy the `.uf2` onto it. The badge reboots on its own.

**BOOT + RESET, not hold-while-plugging-in.** This README said the latter for a while: that is the generic
Raspberry Pi Pico procedure, and this board has a dedicated RESET button (`BUTTON_RESET`, GPIO14) that makes
it unnecessary. Straight from Pimoroni's own README.

**`RP2350` is the bootloader volume, and on this board it is the only one.** Guides describe a second
`Tufty2350` disk that MicroPython exposes at runtime for editing `secrets.py` — **the shipped build does not**,
checked rather than repeated: with MicroPython `bw-1.27.0` running, the badge enumerates as
`bDeviceClass = 239` with only `AppleUSBCDCCompositeDevice` bound, which is a serial port and no
mass-storage interface at all.

So `/Volumes` is empty until you press BOOT+RESET, and that is the expected state, not a fault. If a build
does expose a runtime disk, it is still not the one that takes `.uf2` files.

## Get a signal back, and pick it before you flash

**UART — what `src/main.rs` does today.** GPIO0/1 at 115200, 8N1. Those are `CL0`/`CL1`, two of the four
crocodile-clip pads, which is where a serial adapter can physically attach. **Both the pins and the 12 MHz
crystal are confirmed** against Pimoroni's board definition (see [`src/board.rs`](./src/board.rs)) — this
README used to call them unconfirmed guesses, and two of the original guesses were in fact wrong.

Two options that are *not implemented yet* but need no adapter, in the order they are worth adding:

- **Reboot-to-BOOTSEL as a liveness ping.** Call `rp235x_hal::reboot` into USB boot a few seconds after
  start. If the drive re-appears on its own, your code ran, the crystal is right, and the clocks came up —
  because the delay was *timed*. Zero pin assumptions, which makes it the one signal that cannot be confounded
  by the thing you are testing.
- **Backlight blink codes on GPIO26** (`LCD_BACKLIGHT`), encoding the PSRAM result. Turns the whole check into
  something you run with just the USB cable. Assumes active-high, which the factory firmware will have already
  shown you.

**A debug probe (SWD) with `probe-rs`** remains the best loop by far — flashing plus RTT plus a real
debugger — and the only one that costs hardware (a second Pico works). It is worth more now than it was:
`psram.rs` is register-level code that has never run, and a probe turns a hard fault into a backtrace instead
of a silence.

## Stop holding buttons

`picotool load -f build/badge-bringup.uf2` forces a *running* board into BOOTSEL and reflashes it. One
command instead of the BOOT+RESET ritual, which matters because the PSRAM driver will take several
iterations.

## What to expect from the bring-up firmware

It brings up clocks and the UART, initialises PSRAM, puts the heap there, finds the payloads, instantiates one
through the badge's own hand-written host, and runs one command — as six numbered **stages**, on the screen and
on the UART at once:

```
=== DLC badge bring-up ===
1. clocks / crystal (hardware-only) ... RP2350B @ 150000000 Hz [OK]
     firmware dlc-rp2350-bringup v0.1.0
2. display ST7789 (hardware-only) ... 320x240 parallel [OK]
3. PSRAM 8 MiB (hardware-only) ... 8 MiB [OK]
     heap 8192 KB at 0x11000000
4. payload region (hardware-only) ... 1 found [OK]
     [0] HELLO.CWA 869 KB
     at 0x10400040 #10000 Verified
5. instantiate hello ... 2911 KB heap [OK]
     ILC_TIER=rp2350
     ILC_WORLD=normal
     ILC_STDOUT=display
     ILC_STATUS=color
6. manifest ... 40x12 display [OK]
7. execute 10000 ... success [OK]
     stdout: hello, world — from hello
     peak heap 2911 KB
verdict: OK — hardware-only checks 4/4 (the rest are QEMU regressions)
```

**`(hardware-only)` marks what QEMU cannot model**, and the verdict counts only those: "all checks passed" is
mostly the emulator repeating itself, whereas `4/4` is what this run discovered. It is the figure to quote to
somebody else.

**The firmware names itself and its version in stage 1**, before anything likely to fail, so "did the flash
take" and "is this the build I meant" stay separate questions.

**Stage 4 reporting `empty` is not a failure** — an empty loader has done its job. It ends at `verdict: IDLE`
and the backlight **blinks**, which is the one thing a single GPIO can say that neither on nor off can: code is
running. A board that never booted is dark; a board waiting for a payload flashes.

**`ILC_*` is the STARTUP BOOTSTRAP**, delivered through `wasi:cli/environment` because it is an interface
every component already imports — no new capability needed. It is what shows up in a boot log, and it is all
a world that cannot push a manifest would ever have.

**Nothing in the platform reads it any more.** The wasi environment is read once, during `_initialize`, and can
never be re-read — so it can state a capability but not an ALLOCATION, which moves the moment this world takes
screen back for a menu. The authoritative source is the manifest (`SetEnvironment`, the `manifest` stage above): it is
revision-stamped and re-sendable, so an app can poll it (`platform.Env().GetTextOut()`) and be told when it changes
(`platform.OnEnvironmentChange`). See docs/ENVIRONMENT-PLAN.md D12.

The manifest is where `outlet=none` now lives on the minimal world — the signal for an app to emit an event
instead of formatting text nobody will see. That is ADVICE: this world still accepts every byte an app writes
and simply discards them, because a failing write would make `fmt.Println` a trap on a tier an app cannot
identify from the inside (D13).

`ILC_STDOUT` said `uart` until the panel genuinely rendered text and **not a day before** — an advertisement
that runs ahead of the hardware is silent and believed.

**Stage 3 failing with `kgd=0x00` or `0xff` means nothing drove the bus** — wrong CS pin, or no part fitted.
Any other `kgd` means something answered but is not an APS6404L. On that path the heap falls back to 64 KB of
SRAM, which is enough to keep reporting and *not* enough to instantiate anything: Wasmtime's structures alone
need 863 KB (EMBEDDED-PLAN Phase 0d), which is why PSRAM is required rather than merely desirable.

**If the flash took but you see nothing here**, comment out the `psram::init` call and reflash. That
separates "my driver faults" from "I cannot see output" in a single iteration — `psram.rs` is the least
tested thing on the board, and it runs with XIP disabled, where a mistake cannot print anything at all.
