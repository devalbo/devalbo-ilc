//! The Tufty 2350's pin map — READ OFF PIMORONI'S BOARD DEFINITION, not guessed.
//!
//! Source, and it is worth naming so the next person can re-check rather than
//! re-derive: <https://github.com/pimoroni/tufty2350>, `board/pins.csv` and
//! `board/pimoroni_tufty2350.h`. Pulled 2026-08-07.
//!
//! WHY A FILE RATHER THAN COMMENTS. Every one of these started life as a
//! `// CONFIRM AGAINST THE BOARD` next to a plausible value, and two of them were
//! wrong — see `PSRAM_CS`. A wrong pin does not produce an error; it produces
//! silence, which is indistinguishable from a crash on a board whose only output
//! is the thing you got wrong. Putting them in one place with a citation makes
//! the next wrong one cheap to find.
#![allow(dead_code)] // The bring-up uses a handful; the rest are Phase 3's.

/// PSRAM chip select — **GPIO8, and the plan said GPIO47**.
///
/// `BW_PSRAM_CS (8)`. The 47 came from Pimoroni's *other* RP2350 boards (the Pico
/// Plus 2), which is exactly the kind of near-miss that survives review: it is a
/// real pin, on a real Pimoroni board, for the same purpose. On the RP2350 the
/// QMI's second chip select is reachable from GPIO 0, 8, 19 and 47, so both
/// numbers are individually plausible and only one is this board.
pub const PSRAM_CS: u8 = 8;

/// **PSRAM IS PRESENT AND WORKS — measured 2026-08-16**, before any of our code
/// ran on the board: Pimoroni's MicroPython reported `gc.mem_free()` of
/// **8,159,840 bytes**, which 520 KB of SRAM cannot supply. So the part is
/// fitted, its QMI init succeeds, and 8 MB is addressable.
///
/// Worth stating because it splits a failure that would otherwise be ambiguous:
/// if `psram::init` reports NOT FOUND, the driver is wrong — not the board, not
/// the chip select, not a missing part.
pub const PSRAM_BYTES: usize = 8 * 1024 * 1024;

/// The crystal, in Hz. **12 MHz, confirmed by absence.**
///
/// Neither board header overrides `XOSC_HZ`, and the pico-SDK's default is 12 MHz
/// — so the value the bring-up firmware already used is right. Recorded because
/// "we checked and it is the default" and "we never checked" look identical in a
/// source file, and only one of them is worth trusting.
pub const XTAL_HZ: u32 = 12_000_000;

/// The system clock the shipped firmware actually runs at: **250 MHz, measured
/// on the board 2026-08-16** — `machine.freq()` over the MicroPython REPL.
///
/// The header file says `SYS_CLK_HZ (200000000)` and this constant said 200 MHz
/// on that basis. The running firmware disagrees, and the running firmware wins:
/// a header default can be overridden at boot, which is evidently what happens.
///
/// It matters when PSRAM timing is computed, because the QMI divisor derives
/// from the system clock — so a value 25% low would ask the PSRAM to run faster
/// than intended. Nothing reads this yet; `psram::init` takes the live clock
/// from `clocks.system_clock.freq()` rather than this constant, which is the
/// safer arrangement and the reason the discrepancy was harmless.
pub const SYS_CLK_HZ: u32 = 250_000_000;

// --- the flash map ----------------------------------------------------------
//
// XIP maps flash at 0x10000000, so anything here is directly addressable and a
// payload never has to be copied into RAM — the fact `deserialize_raw` turns
// into "the artifact stays in flash" (EMBEDDED-PLAN Phase 0c).
//
// **THE SPLIT IS ENFORCED BY THE LINKER, not by this comment.** `memory.x`
// declares FLASH as 1 MB rather than the board's 16, so firmware that grew into
// the payload region would fail to link instead of overwriting an app. The two
// files have to agree, and only one of them can produce an error — so if you
// change either, change both.

/// Where the firmware ends and the payload catalog begins: 4 MB into flash.
///
/// **4 MB rather than the 1 MB this started as, and the linker found the bug.**
/// FLASH has to hold the firmware AND any BUILT-IN payload, because `BADGE_PAYLOAD`
/// becomes `.rodata` — 189 KB of image plus hello's 869 KB overflowed a 1 MB cap
/// by 360 KB. 4 MB clears a tictactoe-sized default (~2.2 MB) with room, and
/// still leaves more region than the catalog will scan.
pub const PAYLOAD_BASE: usize = 0x1040_0000;

/// The rest of the 16 MB part. Nothing else may live here.
///
/// Ends exactly at 0x11000000, which is where the PSRAM window begins — so a
/// scan that ran off the end of the catalog would leave flash entirely. It cannot:
/// `catalog::scan` bounds-checks against this length.
pub const PAYLOAD_LEN: usize = 12 * 1024 * 1024;

// --- UART ------------------------------------------------------------------
//
// **This board declares NO default UART** (`MICROPY_HW_UART_NO_DEFAULT_PINS`),
// so there is no "right" answer to inherit — which is why the bring-up firmware's
// choice could not be confirmed by looking for one.
//
// GPIO0..3 are the four CROCODILE-CLIP PADS, `CL0`..`CL3` — the badge's exposed
// I/O. They carry UART0 TX/RX as GPIO function 2 like any RP2350 pin pair, so
// clipping onto CL0/CL1 is how a serial console reaches this board. The existing
// choice of GPIO0/1 is therefore correct, and correct for a better reason than it
// was made: not "the RP2350 default pinout" but "the pins you can physically
// attach to".
/// `CL0` — the first clip pad. UART0 TX under function 2.
pub const UART_TX: u8 = 0;
/// `CL1` — the second clip pad. UART0 RX under function 2.
pub const UART_RX: u8 = 1;

// --- buttons (§14: a host maps native input to a command) -------------------
//
// **ACTIVE-LOW, with internal pull-ups. Measured, and it contradicted what this
// firmware assumed.** Pimoroni's MicroPython configures them
// `Pin(GPIO7, mode=IN, pull=PULL_UP)`, and sampling on the board gave `1` at
// idle and `0` with A held. Our menu had them as active-HIGH with pull-downs,
// which would have read every button as permanently pressed — the menu would
// have confirmed instantly and launched before anyone saw it.
//
// The technique generalises: the shipped firmware is a working reference for the
// same hardware and can be interrogated over its USB REPL, which is faster and
// more trustworthy than a pinout diagram.
pub const BUTTON_DOWN: u8 = 6;
pub const BUTTON_A: u8 = 7;
pub const BUTTON_B: u8 = 9;
pub const BUTTON_C: u8 = 10;
pub const BUTTON_UP: u8 = 11;
/// The one held for BOOTSEL, so it is not freely usable as a sixth input.
pub const BUTTON_HOME: u8 = 22;

// --- the display, and it is NOT SPI ----------------------------------------
//
// An 8-bit PARALLEL (Intel 8080) interface: eight data lines plus WR/RD/RS/CS.
// Worth knowing now rather than in Phase 3, because "ST7789" reads as SPI to
// anyone who has met one before, and the driver crate you would reach for first
// is the wrong one.
pub const LCD_BACKLIGHT: u8 = 26;
pub const LCD_CS: u8 = 27;
pub const LCD_RS: u8 = 28;
pub const LCD_WR: u8 = 30;
pub const LCD_RD: u8 = 31;
/// `LCD_DB0`..`DB7` are GPIO32..39 — contiguous, so a PIO program can drive them
/// as one 8-bit write.
pub const LCD_DB0: u8 = 32;

// --- the rest ---------------------------------------------------------------
/// The PCF85063A RTC, for `wasi:clocks/wall-clock` (EMBEDDED-PLAN D4).
pub const RTC_I2C_SDA: u8 = 4;
pub const RTC_I2C_SCL: u8 = 5;
pub const RTC_ALARM: u8 = 13;
pub const VBUS_DETECT: u8 = 12;
pub const VBAT_SENSE: u8 = 40;
pub const POWER_EN: u8 = 41;

/// **POWER_EN GATES THE DISPLAY. Measured 2026-08-16, and it cost most of a day.**
///
/// Pimoroni's board header calls this "I2C power for talking to RTC", which is
/// why it was skipped: a comment said it was about the RTC, so it was not about
/// the panel. Toggling it under their own firmware settles it — the screen goes
/// blank when this drops and returns when it is restored.
///
/// Without it the controller is unpowered, which is indistinguishable from a
/// broken driver on a write-only bus: four different init sequences all reported
/// success, `RDDID` answered `00 00 00 00`, and every pin measured good. They
/// were good. There was simply nothing on the other end.
///
/// **Assert this before touching the panel**, and leave it asserted.
pub const LIGHT_SENSE: u8 = 43;
