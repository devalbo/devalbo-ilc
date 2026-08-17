# Badge bring-up: a test procedure

A runbook for exercising a Tufty 2350 with this firmware for the first time.
[`README.md`](./README.md) is *how to flash*; this is *how to find out what happened*.

**What is actually being tested.** `src/psram.rs` has never run on hardware, and neither has the payload
region or the backlight. Everything else here is exercised in QEMU at the badge's pointer width — Wasmtime
links, an engine is created, a component instantiates and executes through this same host — so this procedure
asks, in order: does the board boot, does PSRAM come up, does the firmware find a payload, and does Wasmtime
run it here as it does there.

## One vocabulary, and it is three words

Bring-up talk is full of numbers, and this document had three competing ones — which meant "step 2 failed"
could name the paragraph you were reading, the check the badge was running, or a summary line. So:

| word | numbered | what it names |
| --- | --- | --- |
| **Step** | 0–5, the headings below | what **you** do: factory flash, build both files, flash both, watch, attach serial, bisect |
| **stage** | 1–6, on the screen and in the log | what the **firmware** checks, in order — the numbering `report.rs` prints |
| **verdict** | not numbered | the firmware's one-line summary: `OK`, `FAILED`, `IDLE` or `BROKEN` |

"Stage 3 failed" therefore always means PSRAM, never "I am on the flashing paragraph".

**There is no `step N: PASS` line, and there never was one on this firmware.** Earlier revisions of this
document showed one — with two different numbers in two places, and a word the badge has never printed. The
summary is the `verdict:` line, and it is the same word on the screen and in the log because both take it from
`Status::name`.

One word more, used by the plan rather than by this procedure: a **milestone** is a question the firmware as a
whole has answered — 1 does it boot with Wasmtime linked, 2 does it run a component, 3 does it run tictactoe.
`Cargo.toml` lists them. Nothing here is numbered by milestone.

---

## Before you start: do you have a USB-serial adapter?

**This decides how much you learn, not whether you can start.** The badge has two output channels: the UART
on the crocodile-clip pads, which carries the full diagnosis, and the SCREEN plus backlight, which carry a
go/no-go. There is still no plain GPIO LED — the Tufty's is on the CYW43 module — so the backlight is the
blink.

| You have | Do this |
| --- | --- |
| A USB-serial adapter (3.3 V) | Follow this procedure as written — the serial log is the full diagnosis. |
| A second Pico / debug probe | Better still — set up `probe-rs` first. Flashing, RTT output and a debugger in one loop, and `psram.rs` faults become backtraces instead of silence. |
| Neither | **You can now get a useful go/no-go**, which was not true before the screen and the blink existed. The badge fills the panel with a status colour and BLINKS the backlight when it is alive with nothing to run. What you cannot get is *why* something failed — for that, the UART is still the only channel. |

**The screen is the no-adapter signal, and it is new.** Earlier revisions of this document told you to stop
here without a serial adapter; that advice is stale. What one panel and one GPIO can say:

| what you see | what it means |
| --- | --- |
| **blinking backlight** | alive, booted, **no payload installed** — flash `badge-payload.uf2` |
| **steady green** | a payload ran and reported success |
| **steady red** | a payload ran and reported failure — an app-level error, not a crash |
| **steady amber** | broken: instantiation failed, or the board did not get that far |
| **nothing at all** | did not boot, or the display is not working — indistinguishable without the UART |

Kit: a USB-C cable. Optionally a 3.3 V USB-serial adapter and three jumper leads or clips — worth having,
and no longer required to get a first answer.

---

## Step 0 — factory firmware, to prove the path

**Do not skip this.** It removes "the UF2 didn't take" from every later diagnosis, permanently.

1. Download `tufty-v3.0.0-micropython-with-filesystem.uf2` from
   <https://github.com/pimoroni/tufty2350/releases/latest>.
2. Hold **BOOT** (far left), briefly tap **RESET** (to its right).
3. A drive named **`RP2350`** appears. Copy the `.uf2` onto it.

**Expected:** the badge reboots and the TFT shows Pimoroni's menu.

**This also gives you two facts you will want later:** the display and backlight work, and BOOT+RESET puts
this board into the bootloader. If backlight blink codes ever become the status channel, this is the run that
proved the hardware side of them.

**If nothing appears on screen** — the board, the cable, or the procedure is at fault, and nothing below will
be interpretable. Resolve it here.

---

## Step 1 — build BOTH files

**A badge needs two artifacts, and forgetting the second is the most likely way to end up confused.** The
firmware is app-agnostic by default — it runs whatever is in the payload region — so firmware alone boots to
an empty loader that blinks and runs nothing. That is a working badge with no app on it, and it looks
identical to a badge you have not finished setting up.

```
devbox shell
make badge-uf2 BADGE_BEAT_MS=700   # -> build/badge-bringup.uf2   the firmware
make badge-payload                 # -> build/badge-payload.uf2   hello
```

`BADGE_BEAT_MS` is how long each stage lingers on screen. **Zero is the default and the right default** —
normal boot should reach the app as fast as the board can, and pausing to be readable is a debugging mode,
not a tax on every power-on. At zero the stages still appear, just at machine speed. For the run where you
need to see *which* check failed, 700 ms puts the whole sequence at about ten seconds.

**Expected:** `flashable: ~1043000 bytes`. Most of that is Wasmtime; the bring-up logic is a rounding error
against it.

The target **gates on the UF2 family** and fails loudly if it is not `rp2350`. That gate exists because
`elf2uf2-rs` once emitted family `rp2040` from RP2350 firmware, and a wrong-family UF2 produces a badge that
does nothing at all — indistinguishable from a crash. If this step fails, do not flash the result.

---

## Step 2 — flash both, with nothing but the USB-C cable

**No serial adapter is needed for this.** The badge reports on its own screen; the adapter buys detail, and
it is Step 4 when you want it.

1. **Hold BOOT** (far left), **briefly tap RESET**. A drive named **`RP2350`** appears.
2. Drag **`build/badge-bringup.uf2`** onto it. The badge reboots on its own.
3. Hold BOOT and tap RESET again, then drag **`build/badge-payload.uf2`** onto it.

**Order does not matter and neither overwrites the other** — the firmware occupies flash below `0x10400000`
and payloads live above it, which is enforced by the linker rather than by care (see `memory.x`).

**Both files must be UF2s.** The BOOTSEL drive is a synthetic FAT12 volume, not storage: its only real
operation is parsing UF2 blocks. A `.cwasm` dragged onto it is accepted by the Finder and discarded by the
bootloader, silently, with no error anywhere.

**Reflashing later is one command, no buttons:**

```
picotool load -f build/badge-bringup.uf2
```

`-f` forces a *running* board into BOOTSEL, which matters because the PSRAM driver may take several
iterations.

---

## Step 3 — WATCH IT

**The badge narrates its own bring-up.** Each check announces itself before it runs, then shows `OK` or
`FAIL` beside its own name, paced at about 700 ms so a person can read it. The whole sequence is under ten
seconds and needs no serial adapter.

```
DLC 0.1.0 [normal]

1*  clocks / crystal              OK
2*  display ST7789                OK
3*  PSRAM 8 MiB                   OK
4*  payload region                OK
    [0] HELLO.CWA 869 KB
5   instantiate hello             OK
6   execute 10000                 OK

OK  *4/4
```

**Then the panel is replaced** — the app's own output over a band of the verdict colour (`console.rs`), so the
badge answers "did it work" across a room and "what did it say" up close. What the app printed is not one of
the stages; it arrives after all six.

**`*` means QEMU cannot model it**, and that is the whole point of the marking. Stages 5 and 6 are already
green under `make qemu` at this pointer width through this same host — running them here asks the narrower
question of whether the *board* agrees with the emulator. Stages 1–4 are the ones nothing but hardware can
answer: the crystal (the UART divisor is derived from it, so a wrong value shows up as garbled text rather
than a wrong number), the parallel ST7789, the QSPI PSRAM whose init runs with XIP disabled, and the payload
catalog at a real XIP address.

**`*4/4` in the summary is the number worth reporting to someone else.** "All checks passed" is mostly QEMU
repeating itself; the hardware-only count is what this run actually discovered.

**A stage announces BEFORE it runs**, so a board that hangs leaves the name of what it hung in on screen —
the one diagnosis a log that simply stops cannot give.

### If more than one app is installed

A menu appears between stages 4 and 5:

```
choose an app

> HELLO.CWA       869 KB  #10000
  TICTACTO.CWA   1204 KB  #10002
x BROKEN.CWA      CORRUPT

3 apps  2073 of 12288 KB used
UP/DOWN  A=run   auto in 6s
```

**The names are the FILESYSTEM's names.** Mount the badge over USB and you see `HELLO.CWA`; a menu calling
the same payload `hello` would be a second name for one thing. Safe as an identifier because the build
refuses a catalog whose short names collide.

**Sizes are COLUMN-ALIGNED, not parenthesised**, because they are the reason to look at this list at all —
which app is the big one, and will another fit. A left-aligned size after a variable-length name cannot be
compared by eye, so the name truncates rather than pushing the number out of line.

`#10000` is the app's **entry method**, carried in the payload's own catalog header — which is what lets one
loader run an app it was never built for. It is on screen because a payload running the wrong command looks
identical to a payload that is broken.

**`2073 of 12288 KB used`** is the other half of a size being useful: a payload region filling up has no
other symptom until a drag silently does nothing.

**It only appears when there is an actual choice** — one payload is not a menu, it is a delay. And it
**always times out** and runs the highlighted entry, because a badge is worn, sat on, and powered up in a
bag: one that waits forever for a button is a brick with a nice font.

**Corrupt payloads are listed, greyed out, and cannot be selected.** Listed, because a broken file that
vanishes is indistinguishable from one never installed — and sends you to re-drag a payload that is already
there. Greyed rather than reddened because the entry is *disabled*, not erroring: nothing is going wrong, it
simply cannot be picked. The word `CORRUPT` carries the same fact for anyone not relying on colour.

Corruption is caught by an **FNV-1a checksum** in the payload header, verified at scan. It catches the
realistic failure — a drag interrupted part-way, so the header is intact and the bytes are short — which no
amount of bounds-checking can see, because the length still fits the region.

**There are two guards, deliberately.** The menu refuses to *select* a corrupt entry; the launch path
re-checks before *starting* one. The second is not redundant: a payload reaches the launch path by routes
that never touch the menu — a single app skips it, a timeout selects without a press, a built-in payload is
not in the region at all. A guard that assumes an earlier guard ran is not a guard.

**Button polarity is an assumption.** Pimoroni drives these active-HIGH with external pull-downs, which is
the opposite of the usual bare-metal reflex. If the menu selects instantly or never responds, that is the
first thing to check — and it is one `is_high` → `is_low` in `menu.rs`.

### The serial log, for detail

**The log is the same stages, same numbers**, plus the detail that does not fit 40 columns. `(hardware-only)`
is the log's spelling of the screen's `*`.

**Full success looks like this:**

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

**One line per stage, and indented lines below it belong to the stage above.** `ILC_*` is what the guest will
see through `wasi:cli/environment`, printed after stage 5 resolves — it appears on the failure path too, where
it is evidence about what this world offered.

**Stage 4 reporting `empty` after step 2 means the payload did not land.** The firmware is running and found
no app: re-drag `build/badge-payload.uf2`, and if it still reads empty the payload went to the wrong address.
The region base is `board::PAYLOAD_BASE`, which `make badge-payload` reads rather than repeats — so a
mismatch means one of them was edited alone.

**On a badge flashed deliberately without an app, `empty` is not a failure at all.** An empty loader ends at
`verdict: IDLE` and **blinks the backlight**, which is how you tell "waiting for a payload" from "never
booted" with no serial adapter attached.

Read it stage by stage — each stage is a checkpoint, and the stage the output *stops in* is the diagnosis.

### Nothing at all

The board is not talking, or you are not listening. In order of likelihood:

1. **TX/RX swapped.** The single most common cause. Swap them; it is harmless.
2. **Wrong port or baud.** 115200 8N1.
3. **The firmware faulted before the UART came up.** Unlikely — clocks and UART are the first thing after
   entry — but a `psram.rs` fault cannot be it, because PSRAM is initialised *after* the banner.

Note that stages 1 and 2 print *before* PSRAM is touched. **Output that stops inside `3. PSRAM 8 MiB ...` is
therefore a strong signal: the board booted and `psram::init` hung or faulted.** Go to step 5.

### Stage 1 reports a wrong or absurd clock

Garbled characters mean the crystal is wrong (the UART divisor is derived from it). `board.rs` says 12 MHz,
confirmed by Pimoroni's board headers not overriding the pico-SDK default — but if the text is mojibake, this
is the first suspect.

### Stage 3 fails with `kgd=0x00` or `kgd=0xff`

Nothing drove the bus. The device never answered.

- **`0x00` / `0xff`** — the CS pin is the first suspect (`board::PSRAM_CS` = GPIO8, F9 = `XIP_CS1n`), then
  whether this board actually populates PSRAM. The Tufty's spec says 8 MB.
- **Any other value** — something answered but is not an APS6404L (expected KGD is `0x5D`). Note the value;
  it is real information about what is on CS1.

The firmware continues on 64 KB of SRAM and still reports (`falling back to 64 KB SRAM — can report, cannot
instantiate`), which is deliberate: it can tell you what it saw, it just cannot instantiate anything
afterwards.

### Stage 3 passes with less than 8 MiB

A device answered with the right KGD but its density decoded unexpectedly. Note the `eid`. Not fatal —
everything downstream still works, just with less heap than the board has.

### Stage 5 fails

PSRAM came up and the allocator is on it, but Wasmtime refused. The stage prints the error, and the two
expected ones read differently: *"compilation settings are not compatible"* means the payload came from a
different compiler, while an allocation failure means the heap — and the stage says so explicitly if stage 3
did not pass. This is otherwise the least expected failure, since QEMU does exactly this successfully.

---

## Step 4 — attach serial, when the screen is not enough (OPTIONAL)

Skip this if Step 3 ended `OK`. Reach for it when a stage failed and you want the reason, or when the screen
showed nothing at all — the UART is the only channel that can tell "did not boot" from "the display is not
working".

Wire **before** resetting, so you catch the boot banner on the first run rather than after a reset.

| Badge pad | Adapter |
| --- | --- |
| `CL0` (GPIO0) — badge TX | RX |
| `CL1` (GPIO1) — badge RX | TX |
| `GND` | GND |

**Do not connect the adapter's 3.3 V or 5 V line.** The badge is powered over USB-C and has a LiPo;
back-feeding it is the one way to do damage here.

Open the port at **115200 8N1** (`screen /dev/tty.usbserial-XXXX 115200`, or `minicom`, or your terminal of
choice) and tap RESET. Every stage on the screen also prints here, with the parts too long for 40 columns —
`kgd`/`eid` bytes, payload addresses, entry methods, and the full error text on a failure.

**A debug probe (SWD) with `probe-rs`** remains the best loop by far — flashing, RTT output and a real
debugger at once — and the only one that costs hardware, though a second Pico works. It is worth more than
usual here: `psram.rs` is register-level code that has never run, and a probe turns a hard fault into a
backtrace instead of a silence.

---

## Step 5 — bisect a silent hang

If the run stops inside stage 3, `psram::init` is the suspect, and it runs with **XIP disabled** — so a
mistake there cannot print anything, by construction.

**One flash separates the two explanations.** Comment out the `psram::init` match block in `src/main.rs`,
restore the SRAM fallback heap unconditionally, rebuild and reflash:

- **Stage 4 appears** → the driver is at fault. The RAM-residency check in `psram.rs`'s header is the first
  thing to re-run; after that, the CS pin and the pad ISO bit.
- **Still nothing after stage 2** → the fault is elsewhere and PSRAM was a red herring.

---

## What to write down

Whatever happens, capture these — they are what makes a second opinion possible:

- The **exact** serial output, including partial lines — and **the stage number** the last line belongs to.
- The `kgd` / `eid` bytes if stage 3 failed.
- The clock figure from stage 1.
- Whether step 0's factory firmware ran correctly.

---

## What "done" looks like

**`verdict: OK — hardware-only checks 4/4`**, with stage 3 having reported `8 MiB`. The count is the part
worth quoting: it says all four checks nothing but this board could answer did in fact pass.

That closes EMBEDDED-PLAN's definition-of-done item 1 except for the RAM headroom figure, and unblocks
milestone 3 — tictactoe's 2.21 MB artifact and the filesystem it wants. The handover note at the foot of
`src/main.rs` is the list.
