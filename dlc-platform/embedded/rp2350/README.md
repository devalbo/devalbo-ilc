# Flashing the badge

Bring-up firmware for the Pimoroni Tufty 2350. See
[`docs/EMBEDDED-PLAN.md`](../../../docs/EMBEDDED-PLAN.md) for why any of this exists.

```
make badge-uf2      # -> build/badge-bringup.uf2
```

### Why `elf2uf2-rs` and not `picotool`

`picotool` is the official tool and does more (inspect a UF2, force a running board into BOOTSEL so you can
reflash without touching it, convert ELF→UF2, OTP and signing on RP2350). We convert with `elf2uf2-rs`
instead, which is pure Rust and comes from the pinned toolchain.

**Not because picotool is unavailable on macOS** — the pinned nixpkgs build fails for a reason that has
nothing to do with the platform:

```
CMake Error: Compatibility with CMake < 3.5 has been removed from CMake.
```

picotool's build (via the pico-SDK) declares a `cmake_minimum_required` below 3.5, and CMake 4 dropped
support for it; the same failure happens on Linux with this pin. Fixable with
`-DCMAKE_POLICY_VERSION_MINIMUM=3.5`, a newer nixpkgs, or `brew install picotool` — worth doing if you get
tired of holding the boot button, since `picotool load -f` reflashes a running board.

## 1. Prove the flashing path BEFORE flashing ours

Flash a known-good UF2 first — Pimoroni's shipped firmware, CircuitPython, or a stock RP2350 blink.

This is the single most useful habit here. Our bring-up firmware's only output is a UART on **unconfirmed
pins**, so a badge that flashes correctly and runs perfectly may show you *nothing at all* — which looks
exactly like a crash, a bad UF2, and a wrong BOOTSEL procedure. Establish that the path works while the
firmware is not in question, and every later failure has one fewer explanation.

## 2. Enter BOOTSEL and copy

1. Hold the **Home (boot)** button.
2. Plug in USB-C while holding it.
3. A drive appears (an `RP2350`-style volume).
4. Copy `build/badge-bringup.uf2` onto it. The badge reboots on its own.

## 3. Get a signal back — pick one before you flash

Three options, and the first needs no hardware and no pin guesses:

**Reboot-to-BOOTSEL as a liveness ping.** Call `rp235x_hal::reboot` into USB boot a few seconds after
start. If the drive *re-appears* on its own, your code ran — with no UART, no probe, and no knowledge of
which pin anything is on. It answers "did it boot" cleanly, which is exactly Phase 0's first question.

**UART.** What `src/main.rs` does today, on GPIO0/1 at 115200 — **confirm against the Tufty's schematic**,
along with the 12 MHz crystal. Needs a USB-serial adapter on the right pins. Wrong pins produce silence,
wrong crystal produces garbage; neither produces an error.

**A debug probe (SWD) with `probe-rs`.** The best loop by far — flashing plus RTT output plus a debugger —
and the only one that costs hardware (a second Pico works as a probe).

## What to expect from the bring-up firmware

It prints its heap size, then creates a Wasmtime engine configured for `pulley32`, then says `PASS`. It
does **not** load a component yet.

**The reason it does not has changed, so ignore any older note saying PSRAM comes first.** The blocker was
`Component::deserialize`, which copies the artifact into one contiguous allocation — 890 KB against 520 KB of
SRAM. `deserialize_raw` does not copy: the artifact stays in flash, and QEMU now loads hello in **81 KB of
heap** at the badge's real SRAM size. So the next firmware change *is* the component, and PSRAM waits on a
measurement of instantiation rather than on an assumption about loading.

Pin values are in [`src/board.rs`](./src/board.rs), read off Pimoroni's own board definition rather than
guessed — including the two the firmware had wrong.
