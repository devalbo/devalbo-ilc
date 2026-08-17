# Flash these, in this order

Rebuilt 2026-08-16 with the button polarity CORRECTED — the first build had them
active-HIGH, which would have read every button as held and launched slot 0
before the menu was visible. Measured against Pimoroni's own firmware on the
board; see `../dlc-platform/embedded/rp2350/src/board.rs`.

Built from a watchable firmware
(`make badge-uf2 BADGE_BEAT_MS=700`) — each bring-up stage lingers ~700 ms so a
person can read it. The default build (`BADGE_BEAT_MS` unset) runs the same
stages at full speed; use that once the board is known good.

| file | what | size |
| --- | --- | --- |
| `badge-bringup.uf2` | the firmware — family `rp2350-arm-s` | ~1.0 MB |
| `badge-payload.uf2` | hello, as a payload — family `data`, → `0x10400000` | ~1.7 MB |
| `display-probe.uf2` | panel-only probe, no Wasmtime — seconds to build | ~80 KB |
| `tufty-v3.0.1-micropython-with-filesystem.uf2` | **the way back** — Pimoroni's shipped software | 6.8 MB |

**Checksums live in `CHECKSUMS.txt`, not in this table.** They used to be typed
in here, and they went stale immediately: the recorded prefix said `288285c1`
while the file on disk hashed to `c1f8f215`, and nothing anywhere noticed. A
hash written by hand beside a file that is rebuilt by a different command is a
claim with no mechanism behind it.

Now EVERY BUILD STAGES ITSELF HERE and re-hashes the directory on its way out
(`scripts/stage-artifact.sh`). `make badge-uf2`, `make badge-payload` and
`make display-probe` each drop their image in and rewrite `CHECKSUMS.txt`; there
is no step to remember and no way to end up with a fresh image beside a stale
hash. `make flash-testing` just runs all three.

The ledger is regenerated WHOLE rather than patched, so a file that arrives some
other way still gets an entry — the factory MicroPython image below is exactly
that, dropped in by hand and never built here.

**Nothing in this directory is committed** (see `.gitignore`). These are build
outputs of one firmware revision, and a repo carrying them would be promising
they match the source beside them — a promise only the build can keep. If the
directory is empty or stale, that is the expected state after a fresh clone or a
firmware change:

```
make flash-testing
```

**The first two are both needed** — the third is only the way back. The firmware is app-agnostic: alone it boots to an empty
loader that blinks and runs nothing, which looks identical to a badge you have
not finished setting up.

---

## 0. The retreat — already here

`tufty-v3.0.1-micropython-with-filesystem.uf2` is the **with-filesystem** variant, which
restores factory state rather than leaving whatever filesystem is present. Drag
it like any other UF2 and the badge is back to how it shipped.

Worth knowing you can always do that, because the firmware below replaces the
shipped software outright. Nothing in this directory is destructive in a way
that image cannot undo.

## 1. Prove the flashing path, writing nothing

Hold **BOOT** (far left), briefly tap **RESET** (to its right). A drive named
**`RP2350`** appears. Eject, tap RESET — the shipped software returns.

If that drive appears, flashing works, and every later failure is ours rather
than the board's.

**`RP2350` is the bootloader volume, and it is the ONLY volume.** Nothing mounts
while the shipped software is running — measured on this board 2026-08-16:
`bDeviceClass = 239` with only `AppleUSBCDCCompositeDevice` attached, i.e. a
serial port and no mass-storage interface. So an empty `/Volumes` before you
press BOOT+RESET is correct rather than a symptom.

To check the badge is alive without BOOTSEL, look for the USB device itself:

```
ioreg -p IOUSB -w 0 | grep Tufty      # names itself when connected
ls /dev/cu.usbmodem*                  # its REPL
```

## 2. Drag both

BOOT+RESET → drag `badge-bringup.uf2` → it reboots on its own.
BOOT+RESET → drag `badge-payload.uf2`.

Order does not matter and neither overwrites the other: firmware lives below
`0x10400000`, payloads above, enforced by the linker rather than by care.

## 3. Watch the badge

```
1*  clocks / crystal              OK
2*  display ST7789                OK
3*  PSRAM 8 MiB                   OK
4*  payload region                OK
    [0] HELLO.CWA 869 KB
5   instantiate hello             OK
6   execute 10000                 OK
    hello, world — from hello

OK  *4/4
```

`*` marks the stages QEMU cannot model — the crystal, the display, PSRAM and the
payload region. Those four are the only ones carrying information the emulator
has not already given us, which is why the verdict counts them separately.
Stages 5 and 6 already pass under `make qemu` at this pointer width through this
same host, so a failure there means the board disagrees with the emulator, which
is a far more interesting bug.

**A stage announces BEFORE it runs**, so a hang leaves the name of what it hung
in on screen.

---

## If something goes wrong

| what you see | most likely |
| --- | --- |
| nothing at all | did not boot, or the display is not working — indistinguishable without serial |
| stops after a stage name | it hung in that stage; `3* PSRAM` is the likeliest, and it runs with XIP off so it cannot print |
| stage 4 `empty`, backlight blinking | the payload did not land — see below |
| menu selects instantly or never responds | button polarity; the code assumes active-HIGH |
| colours inverted (OK reads magenta) | `ColorInversion` in `../dlc-platform/embedded/rp2350/src/display.rs` |
| image mirrored or upside down | `Rotation` in the same call |

**The payload drag is the step I would bet against.** Whether the RP2350
bootloader honours a `data`-family UF2 by drag-and-drop is verified only as far
as the block addresses being right — not that the bootloader writes them. If
stage 4 says `empty`, try:

```
devbox run -- picotool load flash-testing/badge-payload.uf2
```

That separates "the bootloader ignored the family" from "the address is wrong"
in one step.

**For the reason behind any failure, attach serial** — `CL0`/`CL1` at 115200 8N1,
and do NOT connect the adapter's 3.3 V or 5 V line. Full procedure in
`../dlc-platform/embedded/rp2350/BRINGUP.md`, step 4.
