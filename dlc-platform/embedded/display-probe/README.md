# Device bring-up probes

A firmware whose only job is to find out whether a peripheral works, and why not.
The ST7789 display is the first one; **it is meant to gain siblings**, because
every device on a board arrives with its own baggage and none of it is visible
from the application firmware.

## Why this exists as a separate crate

The badge firmware links Wasmtime: **~90s to build, ~1.1 MB to flash.** Every one
of those seconds sits between a hypothesis and its answer. This crate has no
Wasmtime, no PSRAM, no component, no payload region — **~11s and ~90 KB.**

That difference is not convenience. At ninety seconds a cycle you start
*reasoning* about what is probably wrong instead of *measuring* it, and reasoning
about hardware you cannot observe is how a day disappears.

## What it cost to learn this

Nine flash cycles chasing a display that was **not powered**. `POWER_EN` (GPIO41)
gates the panel's rail; Pimoroni's board header describes it as *"I2C power for
talking to RTC"*, so it was skipped. Every consequence looked like a driver bug:

| symptom | actual cause |
| --- | --- |
| four different init sequences all reported success | writes went into an unpowered controller |
| every pin measured good | they *were* good |
| screen dark but faintly lit | backlight is a separate rail and worked fine |

The thing that finally settled it was the one test that could not be faked.

## The pattern — four rules, in order of how much they saved

### 1. Make the device answer, not just listen

**Everything before the readback test was write-only.** `init()` returning `Ok`
meant "no pin returned an error", and the pins are infallible, so it could never
have meant anything else. The bus can be perfect and the device absent and the
code cannot tell.

`readback.rs` sends `RDDID` and reads the reply. `00 00 00 00` versus real ID
bytes is the difference between "my driver is wrong" and "there is nothing
there" — two problems with no overlap, which had been indistinguishable for a
day. **Build this first for any new device.** Almost everything has an ID
register, a WHO_AM_I, or a status byte.

### 2. Give it a console before it needs one

`usblog.rs` puts a USB CDC serial port on the same cable that powers the board —
no adapter, no probe. **A device that cannot say what it did is one you debug by
rebuilding it**, and that is exactly the loop this crate exists to escape.

Log lines carry milliseconds since boot, and the serve loop appends a live "end
of run" marker, so a replayed buffer is never mistaken for a fresh one.

### 3. Test the wires before the protocol

Probe the pads: drive a pattern, read the pin state back. `0xA5` then `0x00`
catches a stuck, isolated or mis-multiplexed pin in microseconds.

**Probe ALL of them.** The first version checked the eight data lines and not
WR, RS or CS — so "the data bus is fine" was true and useless, and four cycles
went on init sequences that a stuck strobe would have explained just as well.

### 4. Sweep candidates in one flash

Do not flash once per hypothesis. Run each configuration in turn, paint a
distinct colour (or emit a distinct signal) after each, and hold it long enough
to see. **One flash eliminated four init sequences at once**, which is what made
it clear the problem was not the init sequence at all.

## Adding a device

Add a bin to this crate — the harness is already shared:

- `usblog.rs` — buffered log + CDC serving, device-agnostic
- `readback.rs` — the pattern for bus turnaround and reading a device back
- the pad-probe loop in `main.rs` — copy it, change the pin list

Keep the new probe's `main.rs` disposable. The **modules** are the asset; the
harness around them is scaffolding for one investigation.

## Running it

```
make display-probe          # -> build/display-probe.uf2
```

Flash it, wait for the run, then read the log:

```
ls /dev/cu.usbmodemprobe*
cat /dev/cu.usbmodemprobe1
```

## What moves back into the firmware

`panel.rs` (Pimoroni's init sequence) and `cs.rs` (per-transaction chip select)
are written to be lifted into `rp2350/` unchanged once proven. `main.rs` is not.
