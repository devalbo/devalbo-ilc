//! MILESTONE 2 — does the badge RUN a component?
//!
//! Milestone 1 asked whether the board boots with Wasmtime linked, and everything
//! it checked is still checked here, in order, because the order IS the diagnosis:
//! each STAGE of the run is a checkpoint, and the stage the output stops in names
//! what failed (see `BRINGUP.md`). Clocks and UART, then PSRAM, then the payload
//! catalog, then instantiation, then one command.
//!
//! **THREE WORDS, AND THEY ARE NOT INTERCHANGEABLE** — `BRINGUP.md` states the
//! same table, because "step 2 failed" used to name three different things:
//! **step** is what a human does in the runbook, **stage** is one of the six
//! checks below (`report.rs` numbers them), and **milestone** is which of the
//! questions above this firmware has answered.
//!
//! **WHAT THIS FIRMWARE RUNS IS DECIDED AT FLASH TIME, not here.** `build.rs`
//! turns two environment variables into three modes — a hard-coded app, a
//! built-in default that the flash region can add to, or an empty loader that
//! runs only what has been dragged onto the board. This file just asks
//! `payload::discover()` what is available and runs the first thing; a badge
//! with a screen picks a different index and nothing else changes.
//!
//! WHAT WILL PROBABLY BREAK FIRST, in order:
//!
//!   1. PSRAM. `psram.rs` has never run on hardware, and it runs with XIP
//!      disabled, so a mistake there cannot print anything. The banner prints
//!      before it, which is what makes a silent hang legible.
//!   2. The heap. Instantiation needs 2911 KB (measured in QEMU at this pointer
//!      width) against 520 KB of SRAM, so PSRAM is a prerequisite for milestone 2
//!      and not an optimisation.
//!   3. A settings mismatch. A payload built by a compiler with a different
//!      feature set fails at `deserialize_raw` with "compilation settings are not
//!      compatible" — which is why the artifact comes from `dlc-precompile` and
//!      not the stock `wasmtime` CLI.
#![no_std]
#![no_main]

extern crate alloc;

mod board;
mod buttons;
mod chooser;
mod installed;
mod passthrough;
mod console;
mod display;
mod keyboard;
mod menu;
mod spinner;
mod cs;
mod siobus;
mod usblog;
mod payload;
mod platform;
mod psram;
mod report;
mod shared;
mod world;

use world::{Status, WORLD};

use dlc_platform_embedded::control;
use dlc_platform_embedded::manifest;
use dlc_platform_embedded::minimal::MinimalHost;
use dlc_platform_embedded::pulley::PulleyWidth;

// Brought in for its #[panic_handler]; nothing calls it directly.
use panic_halt as _;

use embedded_alloc::LlffHeap as Heap;
use rp235x_hal as hal;

use hal::fugit::RateExtU32;
use hal::uart::{DataBits, StopBits, UartConfig};
use hal::Clock;


#[global_allocator]
static HEAP: Heap = Heap::empty();

/// The bring-up log, kept so it can be served over USB after the run.
///
/// A `static mut` behind a single-threaded firmware with no interrupts touching
/// it. It has to outlive the borrow the report holds, and it must survive into
/// `rest()`, which is where the USB polling happens.
static mut LOG: usblog::LogBuffer = usblog::LogBuffer::new();

/// The panel, reachable from an import handler.
///
/// See the comment where this is filled: a `wasi:cli/stdout` write arrives
/// through a bare `fn(&[u8])`, which can only see statics. This is what lets the
/// world DRAW a guest's output while the command is still running rather than
/// only after it returns.
static mut SCREEN: Option<display::Display> = None;

/// The app's output so far, for repainting.
///
/// Separate from `LOG`, which interleaves the bring-up report — a repaint wants
/// only what the app said. Sized for a screenful and a bit; the panel shows 13
/// rows of 40, and anything older has already scrolled out of usefulness.
static mut APP_OUT: usblog::LogBuffer = usblog::LogBuffer::new();


/// WHAT THE DATA BUS ACTUALLY DOES, measured rather than assumed.
///
/// Everything about the display is downstream of one unverified fact: whether
/// writing GPIO32..39 through SIO changes the pads. Four flash cycles went on
/// init sequences and timing while this stayed untested, so it is now the first
/// thing the firmware reports.
///
/// Writes a pattern, reads the pad INPUT register back — `gpio_hi_in` is the
/// state of the pin itself, not the value we asked for, so a pad that is
/// isolated, mis-multiplexed or not output-enabled shows up as a mismatch.
fn probe_data_bus() -> (u32, u32) {
    let sio = unsafe { &*hal::pac::SIO::ptr() };
    const MASK: u32 = 0xFF;
    sio.gpio_hi_out_clr().write(|w| unsafe { w.bits(MASK) });
    sio.gpio_hi_out_set().write(|w| unsafe { w.bits(0xA5) });
    cortex_m::asm::delay(1000);
    let high = sio.gpio_hi_in().read().bits() & MASK;
    sio.gpio_hi_out_clr().write(|w| unsafe { w.bits(MASK) });
    cortex_m::asm::delay(1000);
    let low = sio.gpio_hi_in().read().bits() & MASK;
    (high, low)
}

/// The backlight on GPIO26 — the badge's status light until the TFT has a driver.
type Backlight = hal::gpio::Pin<
    hal::gpio::bank0::Gpio26,
    hal::gpio::FunctionSio<hal::gpio::SioOutput>,
    hal::gpio::PullDown,
>;

/// Show a status. **The whole of the minimal world's output, and half of the
/// normal world's.**
///
/// One GPIO today, because the TFT is an 8-bit parallel interface with no driver
/// yet (Phase 3) — so this is on-or-off rather than the colour `Status::rgb565`
/// names. That is a real signal, not a placeholder: a badge with no serial
/// adapter attached can still tell you whether it ran. When the display lands,
/// this function grows a fill and every caller stays as it is.
fn show(backlight: &mut Backlight, screen: &mut Option<display::Display>, status: Status) {
    use embedded_hal::digital::OutputPin;
    // THE COLOUR IS THE STATUS. `Status::rgb565` has carried this mapping since
    // before there was a driver to use it — it is the meaning, and this is the
    // first caller able to show it.
    //
    // OPTIONAL, because a panel that failed to initialise must not take the badge
    // down with it: the UART and the backlight still work, and reporting beats
    // halting.
    if let Some(panel) = screen.as_mut() {
        panel.fill(status.rgb565());
    }
    // THE BACKLIGHT IS A STATUS CHANNEL ONLY WHEN IT IS THE ONLY CHANNEL.
    //
    // This read `if status.backlight_on()` for every case, which meant the very
    // first call — `show(.., Status::Broken)` before the narration — turned the
    // backlight OFF and left the whole bring-up drawn onto an unlit screen. On
    // real hardware that looked exactly like a display that does not work.
    //
    // The encoding was written when there was no driver and one GPIO was all the
    // badge had. There is a panel now, status is the COLOUR, and the backlight's
    // only job is to make that visible.
    let _ = if screen.is_some() {
        backlight.set_high()
    } else if status.backlight_on() {
        backlight.set_high()
    } else {
        backlight.set_low()
    };
}

/// Hold a status forever, blinking if that is how it is shown.
///
/// THE END OF EVERY PATH THROUGH THIS FIRMWARE, and the reason it is not just
/// `wfi` in a loop: a board with nothing to run used to halt with the backlight
/// off, which is exactly what a board that never booted looks like. Someone
/// without a serial adapter — the common case, per BRINGUP.md — could not tell
/// the two apart. A blink can only mean code is running.
fn rest(
    backlight: &mut Backlight,
    screen: &mut Option<display::Display>,
    status: Status,
    sys_hz: u32,
) -> ! {
    use embedded_hal::digital::OutputPin;
    let has_screen = screen.is_some();
    show(backlight, screen, status);
    // BLINK ONLY WITHOUT A SCREEN. A lit panel showing a status colour and a
    // verdict already says "alive, and here is why"; blinking it would strobe
    // the one thing worth reading. Blind, the blink is the only way to tell
    // "waiting" from "never booted", so it stays.
    if has_screen || !status.blinks() {
        // SERVE THE LOG rather than sleeping. `wfi` here is what made four
        // flash cycles necessary: the badge knew exactly what happened and had
        // no way to say it.
        serve_log()
    }
    // A short flash about once a second: unmistakably deliberate, and dim enough
    // not to drain a LiPo sitting on a desk.
    loop {
        let _ = backlight.set_high();
        cortex_m::asm::delay(sys_hz / 16);
        let _ = backlight.set_low();
        cortex_m::asm::delay(sys_hz);
    }
}

/// Idle forever, offering the bring-up log over USB CDC.
///
/// WHY IT RUNS AT THE END rather than throughout: USB needs polling to
/// enumerate, and the bring-up is a straight line that must not stall waiting
/// for a host that may never attach. Buffering during the run and serving after
/// means nothing blocks, and a host that connects late still gets the whole log
/// — including the part written before it plugged in, which is the part that
/// matters.
///
/// Never returns. The badge has finished; this is its afterlife.
fn serve_log() -> ! {
    // THE DEVICE IS ALREADY UP, and already being driven by USBCTRL_IRQ — it was
    // started before the bring-up report so that a stall would still be
    // reportable. This function used to build the whole USB stack here, which is
    // exactly what made a hang invisible: no device existed until the run
    // finished, so a run that never finished had no way to say so.
    //
    // What is left is the afterlife: keep servicing, and periodically rewind so
    // a terminal attached after the fact sees the whole run rather than an empty
    // port.
    //
    // `pump()` as well as the interrupt, deliberately. If the interrupt were
    // never unmasked — a future refactor, a build where `start` bailed — this
    // loop still drains, so the worst case degrades to the old behaviour instead
    // of to silence.
    let mut idle = 0u32;
    loop {
        usblog::pump();
        idle = idle.saturating_add(1);
        if idle > 2_000_000 {
            usblog::rewind();
            idle = 0;
        }
    }
}

/// Microseconds, from TIMER0's raw counter.
///
/// THE RAW REGISTERS, not the latching pair. `TIMELR` latches `TIMEHR` as a side
/// effect, which is fine for one reader and wrong the moment an interrupt reads
/// the timer between another reader's two halves — and this firmware has a USB
/// interrupt that could.
///
/// Reading high/low/high and retrying on a change is the documented way to get a
/// consistent 64-bit value without latching. The retry is almost never taken: it
/// costs a loop iteration once every 71 minutes, when the low word wraps.
fn now_us() -> u64 {
    let timer = unsafe { &*hal::pac::TIMER0::ptr() };
    loop {
        let high = timer.timerawh().read().bits();
        let low = timer.timerawl().read().bits();
        if timer.timerawh().read().bits() == high {
            return ((high as u64) << 32) | low as u64;
        }
    }
}

/// Append guest output to the boot log, as the guest writes it.
///
/// SAFETY: single core, and the log's only other writer is the report — which
/// cannot run here, because this is called from inside a guest call that the
/// report is waiting on.
fn echo_to_log(bytes: &[u8]) {
    use core::fmt::Write;
    let Ok(text) = core::str::from_utf8(bytes) else {
        return;
    };

    // NOT INTO `LOG`, and this was a real bug rather than a preference.
    //
    // The report holds a `&mut LogBuffer` for its whole lifetime, inside its
    // `Tee` sink. Taking a SECOND `&mut` here — through `addr_of_mut!`, which
    // silences the borrow checker without removing the aliasing — is undefined
    // behaviour, and it behaved like it: the optimiser is entitled to cache
    // `len` across the two writers, so the published length drifted below the
    // ISR's `SENT` cursor and `pending()` returned empty FOREVER.
    //
    // The symptom was a USB log that froze mid-boot while the badge kept
    // running perfectly — the worst shape of failure available here, because
    // the diagnostic channel is what you reach for when something looks wrong.
    //
    // App output still reaches the log: it is drained after the command and
    // written as the `stdout:` note, which is where it was before any of this.
    // What is lost is INTERLEAVING, and the panel is the live channel now.

    // The app's own buffer, which is what a repaint draws from. Nothing else
    // holds a reference to it, so this one is not an alias.
    let out: &mut usblog::LogBuffer = unsafe { &mut *core::ptr::addr_of_mut!(APP_OUT) };
    let _ = write!(out, "{text}");

    // REPAINT ON A NEWLINE, not on every write.
    //
    // A full frame is 153,600 bytes on a bit-banged parallel bus. Repainting per
    // character would make an app that prints a word slower than one that
    // prints a paragraph, and would spend the whole command in the display
    // driver. A LINE is the natural unit: it is what the reader is waiting for,
    // and an app that prints without newlines is already asking for its output
    // to be treated as one piece.
    if !text.contains('\n') {
        return;
    }
    if !WORLD.can(world::Capability::Text) {
        return;
    }
    // SAFETY: single core. The report is not drawing here — this runs inside
    // `host.execute`, which the report is waiting on and cannot re-enter.
    let screen: &mut Option<display::Display> = unsafe { &mut *core::ptr::addr_of_mut!(SCREEN) };
    if let Some(panel) = screen.as_mut() {
        console::render(
            panel,
            Status::Ok,
            "running",
            core::str::from_utf8(out.as_bytes()).unwrap_or(""),
        );
    }
}

/// The RP2350 boot block. Without it the chip does not consider this a valid
/// image and will sit in the bootloader instead of telling you why.
#[link_section = ".start_block"]
#[used]
pub static IMAGE_DEF: hal::block::ImageDef = hal::block::ImageDef::secure_exe();

/// The crystal on the Tufty, in Hz. **CONFIRMED 2026-08-07** — see `board.rs`:
/// neither Pimoroni board header overrides `XOSC_HZ`, so the pico-SDK default of
/// 12 MHz is this board's value. A wrong one here shows up as garbled UART rather
/// than as an error, which is why it was worth checking rather than assuming.
const XTAL_HZ: u32 = board::XTAL_HZ;

/// The FALLBACK heap, in SRAM, used only when PSRAM does not come up.
///
/// Deliberately small. It cannot instantiate anything — measured in QEMU,
/// Wasmtime's structures alone need 863 KB before the guest's memory — so its
/// only job is to keep the firmware alive far enough to REPORT why PSRAM failed.
/// A board that halts silently teaches nothing.
const HEAP_BYTES: usize = 64 * 1024;
static mut HEAP_MEM: [u8; HEAP_BYTES] = [0; HEAP_BYTES];

#[hal::entry]
fn main() -> ! {
    // NO HEAP YET, and that is a change from milestone 1 — where it went first,
    // because "anything below may allocate".
    //
    // The heap now goes on PSRAM, which cannot be probed until the clocks and the
    // UART are up: the QMI divisor comes from the system clock, and a PSRAM
    // failure is only useful if it can be *printed*. So the ordering inverts, and
    // it is safe for a reason that must stay true — **nothing between here and
    // `HEAP.init` below allocates.** `Peripherals::take`, `init_clocks_and_plls`,
    // `Pins::new`, `UartPeripheral::new` and `writeln!` are all allocation-free,
    // and `LlffHeap::init` may be called only once, which is why it cannot simply
    // be done twice to be safe. Adding an allocating call above it is a hard
    // fault with no message.
    let mut pac = hal::pac::Peripherals::take().unwrap();
    let mut watchdog = hal::Watchdog::new(pac.WATCHDOG);
    let clocks = hal::clocks::init_clocks_and_plls(
        XTAL_HZ,
        pac.XOSC,
        pac.CLOCKS,
        pac.PLL_SYS,
        pac.PLL_USB,
        &mut pac.RESETS,
        &mut watchdog,
    )
    .unwrap();

    let sio = hal::Sio::new(pac.SIO);
    let pins = hal::gpio::Pins::new(pac.IO_BANK0, pac.PADS_BANK0, sio.gpio_bank0, &mut pac.RESETS);

    // UART0 on GPIO0/1 — **CONFIRMED 2026-08-07, and for a better reason than it
    // was chosen.** The Tufty declares no default UART at all, so there was no
    // convention to be right about; GPIO0/1 are `CL0`/`CL1`, two of the four
    // crocodile-clip pads, which is where a serial adapter can physically attach.
    // See `board.rs`.
    let uart_pins = (
        pins.gpio0.into_function::<hal::gpio::FunctionUart>(),
        pins.gpio1.into_function::<hal::gpio::FunctionUart>(),
    );
    let mut uart = hal::uart::UartPeripheral::new(pac.UART0, uart_pins, &mut pac.RESETS)
        .enable(
            UartConfig::new(115_200.Hz(), DataBits::Eight, None, StopBits::One),
            clocks.peripheral_clock.freq(),
        )
        .unwrap();

    // THE STATUS CHANNEL, and it comes up early on purpose: it is the only output
    // this badge has when nobody has a serial adapter clipped on (BRINGUP.md's
    // first question). Both worlds have `Capability::Status`, so this is not
    // gated — the minimal world is the one that has ONLY this.
    let mut backlight = pins.gpio26.into_push_pull_output();

    // THE SCREEN, brought up before PSRAM and before Wasmtime — deliberately.
    // It is the badge's only output for someone with no serial adapter clipped
    // on, so it has to exist before the stages most likely to fail. See
    // display.rs: none of it has run on hardware.
    // mipidsi needs a DelayNs for the panel's power-on timing; the HAL timer is
    // one. Created here rather than inside the driver because it is a peripheral
    // the rest of the firmware may want.
    let mut timer = hal::Timer::new_timer0(pac.TIMER0, &mut pac.RESETS, &clocks);

    // A REAL CLOCK FOR THE GUEST. Until now `wasi:clocks/monotonic-clock` counted
    // CALLS and `subscribe_duration` ignored its argument, so an app's
    // `time.Sleep` returned immediately and nothing could unfold over time.
    //
    // TIMER0 counts microseconds in hardware, which is exactly what the guest
    // needs and what the comment on that stub always promised.
    dlc_platform_embedded::clock::install(now_us);

    // GUEST OUTPUT, LIVE. `wasi:cli/stdout.write` is an IMPORT — host code
    // running while the guest is suspended mid-call — so its bytes exist long
    // before the command returns. They were buffered and read afterwards, which
    // made every tier look like output only arrives at the end.
    //
    // Echoing into the boot log means a sleeping app's ticks appear as it prints
    // them, over the USB cable, at the moment it prints them. That is the whole
    // demonstration that an import is a callback.
    dlc_platform_embedded::uart::set_echo(echo_to_log);

    // AND WHILE A GUEST RUNS. `block_on` polls in a loop whenever the guest is
    // suspended — inside a sleep, or waiting on a stream — and that loop is world
    // code. Servicing USB there is what keeps the log flowing and the heartbeat
    // beating during a command, which is exactly when somebody asks whether the
    // badge is stuck.
    dlc_platform_embedded::block_on::set_pump(usblog::pump);

    // POWER THE PANEL. GPIO41 gates the display's rail — verified by toggling it
    // under Pimoroni's firmware and watching the screen blank and return. Their
    // header calls it "I2C power for talking to RTC", which is what kept it out
    // of this firmware and cost a day of chasing an unpowered controller.
    //
    // FIRST, and before any display work: everything after this assumes there is
    // something on the other end of the bus.
    use embedded_hal::digital::OutputPin as _;
    let mut power_en = pins.gpio41.into_push_pull_output();
    let _ = power_en.set_high();
    // Give the rail time to come up before the panel is addressed.
    cortex_m::asm::delay(10_000_000);
    // START THE LOG HERE — before the report, not after it.
    //
    // This used to hand the clock to a static and build the device only in the
    // idle loop, which meant a run that never reached the idle loop produced no
    // serial device at all. A stalled `execute` therefore showed one unresolved
    // line on the panel and said nothing over USB, and the only move left was to
    // reflash with a guess.
    //
    // Started here and driven by USBCTRL_IRQ, the log is live during the run and
    // survives a stall: the interrupt keeps answering the host whatever main is
    // doing. See usblog.rs.
    //
    // AFTER the last borrow of `clocks` — taking the field partially moves it,
    // and the timer above still needs the whole thing.
    usblog::start(pac.USB, pac.USB_DPRAM, clocks.usb_clock, &mut pac.RESETS);
    let screen = display::Display::new(
        pins.gpio27.into_push_pull_output().into_dyn_pin(),
        pins.gpio28.into_push_pull_output().into_dyn_pin(),
        pins.gpio30.into_push_pull_output().into_dyn_pin(),
        pins.gpio31.into_push_pull_output().into_dyn_pin(),
        [
            pins.gpio32.into_push_pull_output().into_dyn_pin(),
            pins.gpio33.into_push_pull_output().into_dyn_pin(),
            pins.gpio34.into_push_pull_output().into_dyn_pin(),
            pins.gpio35.into_push_pull_output().into_dyn_pin(),
            pins.gpio36.into_push_pull_output().into_dyn_pin(),
            pins.gpio37.into_push_pull_output().into_dyn_pin(),
            pins.gpio38.into_push_pull_output().into_dyn_pin(),
            pins.gpio39.into_push_pull_output().into_dyn_pin(),
        ],
        &mut timer,
    );
    // A screen that does not come up is NOT fatal: the badge still has a UART
    // and a backlight, and saying so is more useful than halting.
    // THE SCREEN LIVES IN A STATIC, and this is what Phase 4a needed.
    //
    // A guest's `stdout` write is an IMPORT — host code running mid-`execute`,
    // reached through a `fn(&[u8])` hook with no arguments and no captured
    // state. It can therefore only see statics. The panel used to be a local
    // owned by `main` and borrowed by the report, which is why output could be
    // collected during a command and not DRAWN during one.
    //
    // SAFETY, and it is a real argument rather than a shrug: this is a single
    // core with no preemption between the report and a guest call. The report
    // draws between commands; the echo draws inside one. They cannot interleave
    // because `execute` does not return until the guest is done, and the report
    // is not running while it is inside `execute`.
    let (screen, screen_err) = match screen {
        Ok(panel) => (Some(panel), ""),
        // KEEP THE REASON. Discarding it is what made a configuration mistake
        // look like dead hardware for a whole session: the badge blinked with a
        // blank screen, which is indistinguishable from a panel that is not
        // wired, while the actual answer was `InvalidDisplaySize`.
        Err(display::DisplayError(why)) => (None, why),
    };
    unsafe { SCREEN = screen };
    let screen: &mut Option<display::Display> = unsafe { &mut *core::ptr::addr_of_mut!(SCREEN) };
    // Captured before the report borrows `screen`, so stage 2 can report on the
    // very thing it is drawing to.
    let screen_ok = screen.is_some();
    show(&mut backlight, screen, Status::Broken);

    let sys_hz = clocks.system_clock.freq().to_Hz();

    // BRING-UP AS A WATCHABLE SEQUENCE (report.rs). Each stage announces itself
    // before it runs, so a hang shows the name of what it hung in — and the ones
    // QEMU cannot model are marked `*`, because those are the only checks that
    // carry information `make qemu` has not already given us.
    // SAFETY: single core, no interrupt touches LOG, and this is the only borrow
    // taken for the report's lifetime.
    let log: &mut usblog::LogBuffer = unsafe { &mut *core::ptr::addr_of_mut!(LOG) };
    let mut sink = usblog::Tee(&mut uart, log);
    // SAY WHAT THE WORLD IS DOING, at each transition. Without these a caller
    // asking "are you stuck?" gets UNSPECIFIED, which is the question restated.
    usblog::set_activity(dlc_platform_embedded::control::ACTIVITY_STARTING);
    let mut report = report::Report::new(
        &mut sink,
        screen,
        sys_hz,
        format_args!("DLC {} [{}]", env!("CARGO_PKG_VERSION"), WORLD.name()),
    );

    // 1 — THE CRYSTAL, and it is hardware-only for a non-obvious reason: the UART
    // divisor is derived from it, so if this number is wrong the evidence is
    // garbled text rather than a wrong figure. QEMU has no crystal at all.
    report
        .stage(control::STAGE_CLOCKS, report::Scope::HardwareOnly, format_args!("clocks / crystal"))
        .ok(format_args!("RP2350B @ {} Hz", sys_hz));
    report.note(format_args!(
        "firmware {} v{}",
        env!("CARGO_PKG_NAME"),
        env!("CARGO_PKG_VERSION")
    ));

    // 2 — THE PANEL. An 8-bit parallel ST7789 on a bit-banged bus: QEMU models no
    // RP2350 peripherals, so nothing about this has ever executed. If you are
    // reading this stage ON the badge, it passed.
    // THE MEASUREMENT THAT SHOULD HAVE COME FIRST. Everything about the display
    // is downstream of whether writing GPIO32..39 changes the pads, and four
    // flash cycles went on init sequences and timing while that stayed untested.
    {
        let stage = report.stage(control::STAGE_DATA_BUS, report::Scope::HardwareOnly, format_args!("data bus 32-39"));
        let (high, low) = probe_data_bus();
        if high == 0xA5 && low == 0x00 {
            stage.ok(format_args!("A5/00"));
        } else {
            stage.fail(format_args!("wrote A5/00 got {high:02x}/{low:02x}"));
        }
    }

    let stage = report.stage(control::STAGE_DISPLAY, report::Scope::HardwareOnly, format_args!("display ST7789"));
    if screen_ok {
        stage.ok(format_args!("320x240 parallel"));
    } else {
        stage.fail(format_args!("init failed: {screen_err}"));
    }

    // 3 — PSRAM. The single most likely thing to fail: register-level code that
    // has never run, executing with XIP DISABLED so a mistake cannot print. QEMU
    // has no QSPI device to model.
    let stage = report.stage(control::STAGE_PSRAM, report::Scope::HardwareOnly, format_args!("PSRAM 8 MiB"));
    let psram_ok = match psram::init(board::PSRAM_CS, sys_hz) {
        Ok(ps) => {
            // Re-init the allocator onto PSRAM. Sound only because nothing above
            // has allocated: `LlffHeap::init` may be called once, and the SRAM
            // block was never handed out.
            unsafe { HEAP.init(ps.base as usize, ps.len) };
            usblog::heap_is_ready();
            stage.ok(format_args!("{} MiB", ps.len / (1024 * 1024)));
            report.note(format_args!("heap {} KB at {:p}", ps.len / 1024, ps.base));
            true
        }
        Err(psram::PsramError::NotFound { kgd, eid }) => {
            // NAMED, not just "failed". 0x00/0xFF means nothing drove the bus —
            // wrong CS pin, or no PSRAM fitted. Any other value means a device
            // answered but is not an APS6404L.
            stage.fail(format_args!("kgd={kgd:#04x} eid={eid:#04x}"));
            report.note(format_args!(
                "cs=GPIO{} — 0x00/0xff means nothing answered",
                board::PSRAM_CS
            ));
            unsafe {
                let ptr = &raw mut HEAP_MEM as *mut u8;
                HEAP.init(ptr as usize, HEAP_BYTES);
                usblog::heap_is_ready();
            }
            report.note(format_args!(
                "falling back to {} KB SRAM — can report, cannot instantiate",
                HEAP_BYTES / 1024
            ));
            false
        }
    };

    // 4 — THE PAYLOAD CATALOG, in real memory-mapped flash. QEMU's harness uses
    // `include_bytes!` into its own image; nothing has ever read this region at
    // an XIP address on a real part, and a payload UF2 dragged to the wrong
    // offset looks exactly like an empty badge.
    let stage = report.stage(control::STAGE_PAYLOAD_REGION, report::Scope::HardwareOnly, format_args!("payload region"));
    let available = payload::discover();
    // PUBLISH BEFORE ANYTHING CAN FAIL. A client asking what is installed is most
    // useful on a badge whose payload is the reason it is not running.
    installed::publish(&available);
    if available.is_empty() {
        // NOT a failure. This is what an empty loader is FOR, and saying so
        // plainly is the difference between "waiting" and "broken".
        stage.ok(format_args!("empty"));
        report.note(format_args!("{}", payload::MODE));
        report.note(format_args!("drag a payload UF2 onto the RP2350 drive"));
        report.finish(Status::Idle);
        rest(&mut backlight, screen, Status::Idle, sys_hz);
    }
    let runnable = available.iter().filter(|p| p.runnable()).count();
    if runnable == available.len() {
        stage.ok(format_args!("{} found", available.len()));
    } else {
        // NOT a hard failure: the good ones still run. Saying it here is what
        // stops a corrupt file being discovered only when someone picks it.
        stage.fail(format_args!("{runnable}/{} usable", available.len()));
    }
    for (index, found) in available.iter().enumerate() {
        // The ADDRESS is the evidence that it stayed in flash — below 0x20000000.
        // The FILESYSTEM's name, so the log, the menu and a mounted USB volume
        // all call a payload the same thing.
        let mut filename = [0u8; 12];
        let shown = dlc_platform_embedded::names::display_filename(found.name, &mut filename);
        if found.runnable() {
            // THE CHECKSUM, NOT JUST THE SIZE.
            //
            // This line reported only `869 KB` and was the one place a stale
            // payload was visible — a build from before the platform changed,
            // silently running while every other stage said OK. It took
            // comparing that number against the artifact on disk to notice, and
            // a payload that changed WITHOUT changing size would not have shown
            // up at all.
            //
            // The checksum is already computed: `scan` verifies it to produce
            // `Integrity::Verified`. It was simply never printed, so the one
            // fact that identifies WHICH build is on the board stayed inside the
            // firmware. Printing it costs nothing and makes "is the board
            // running what I just built?" answerable by looking.
            report.item(format_args!(
                "[{index}] {shown} {} B #{:08x}",
                found.bytes.len(),
                dlc_platform_embedded::catalog::checksum(found.bytes)
            ));
        } else if found.integrity == dlc_platform_embedded::catalog::Integrity::WrongEngine {
            // NAMED, because the remedy differs. Corrupt means the file arrived
            // damaged and should be flashed again; this means it arrived intact
            // and was compiled against a different Wasmtime, so it must be
            // repacked. Before this line the mismatch showed up two stages later
            // as a deserialize error, which reads like a firmware fault.
            report.item(format_args!("[{index}] {shown} WRONG ENGINE - repack"));
        } else {
            report.item(format_args!("[{index}] {shown} CORRUPT"));
        }
        report.note(format_args!(
            "at {:p} #{} {:?}",
            found.bytes.as_ptr(),
            found.entry_method,
            found.integrity
        ));
    }

    // THE SELECTION. Shown only when there is more than one app — one payload is
    // not a menu, it is a delay. It always times out and runs the highlighted
    // entry, so a badge nobody is touching still boots.
    // EVERY BUTTON, OWNED ONCE, HERE.
    //
    // The menu and the keyboard SHARE pins — both want `a` and `down` — and a
    // pin can only be moved once. That was invisible while the menu ran exactly
    // once per boot; the moment a session could return to the menu, ownership
    // had to move out to where both can borrow it.
    //
    // PULL-UP, not pull-down: the buttons short to ground (measured against
    // Pimoroni's own firmware on the board — see menu.rs).
    let mut pin_up = pins.gpio11.into_pull_up_input();
    let mut pin_down = pins.gpio6.into_pull_up_input();
    let mut pin_a = pins.gpio7.into_pull_up_input();
    let mut pin_b = pins.gpio9.into_pull_up_input();
    let mut pin_c = pins.gpio10.into_pull_up_input();
    // UP LEAVES THE SESSION.
    //
    // NOT `HOME`, which was the first choice and a mistake `board.rs` had
    // already warned about: gpio22 is the BOOTSEL button — "the one held for
    // BOOTSEL, so it is not freely usable as a sixth input". Pressing it while
    // running is harmless, but overloading "leave" onto the button people hold
    // to flash the badge invites holding it across a reset and landing in the
    // bootloader instead.
    //
    // `UP` is the button that has been kept free for this. keyboard.rs says why:
    // an unused button is recoverable and a wrongly assigned one is a habit.
    // THE APP LOOP — the outer half of a session (Phase 1).
    //
    // `HOME` used to park the badge forever. Now it comes back here, so a
    // DIFFERENT app can run without a power cycle — which is the visible payoff
    // of a session and the thing "one command and stop" cost.
    //
    // The instance is created inside this loop and dropped at the end of each
    // pass. That is deliberate and it is the risky part: instantiation takes
    // 2.9 MB of an 8 MB heap, so a session that leaks even a few hundred KB
    // hangs two or three apps in — which looks like a hardware fault and is the
    // worst thing here to debug. The heap is measured on every pass below for
    // exactly that reason.
    let heap_at_start = HEAP.used();
    loop {
    usblog::set_activity(dlc_platform_embedded::control::ACTIVITY_CHOOSING);
    // BORROWED, not moved: the keyboard wants two of these back below.
    let mut buttons = menu::Buttons {
        up: &mut pin_up,
        down: &mut pin_down,
        a: &mut pin_a,
    };
    let choice = report.with_screen_and_log(|screen, log| {
        menu::choose(&available, screen, &mut buttons, sys_hz, log)
    });
    drop(buttons);
    installed::selected(choice);
    let selected = available
        .get(choice)
        .or_else(|| available.default_choice().map(|(_, p)| p))
        .expect("non-empty, checked above");

    // GUARD TWO OF TWO: what can be LAUNCHED.
    //
    // **This deliberately repeats a check the menu already made**, because a
    // payload reaches here by several routes that never touch the menu: a single
    // app skips it, a timeout selects without a press, and a built-in payload is
    // not in the region at all. A guard that assumes an earlier guard ran is not
    // a guard.
    //
    // Refusing here is also what turns corruption into a NAMED condition. Without
    // it, `deserialize_raw` rejects the bytes and the badge reports a Wasmtime
    // error, which reads as a firmware bug rather than "that file is damaged,
    // drag it again".
    if !selected.runnable() {
        report
            .stage(control::STAGE_VERIFY_PAYLOAD, report::Scope::HardwareOnly, format_args!("verify payload"))
            .fail(format_args!("checksum mismatch"));
        report.note(format_args!(
            "{} is corrupt — re-drag the payload UF2",
            selected.name
        ));
        report.finish(Status::Broken);
        rest(&mut backlight, screen, Status::Broken, sys_hz);
    }

    // 5 — INSTANTIATION. **This one QEMU already proved**, at this pointer width,
    // through this exact host: 81 KB to load, 2911 KB to instantiate. Running it
    // here asks a narrower question — does the BOARD agree with the emulator? — and
    // the interesting failure is a settings mismatch or PSRAM being too slow, not
    // "does Wasmtime work".
    let mut advertisement = [("", ""); world::ADVERTISEMENT_MAX];
    let advertisement = WORLD.advertise(&mut advertisement);
    let before = HEAP.used();
    let stage = report.stage(control::STAGE_INSTANTIATE, report::Scope::Emulated, format_args!("instantiate {}", selected.name));
    // SAFETY: `selected.bytes` is our own build's .cwasm — 16-byte aligned by the
    // catalog format (or by `Aligned`, for the built-in), and `'static` because it
    // is memory-mapped flash.
    let host = unsafe {
        MinimalHost::from_precompiled_advertising(selected.bytes, PulleyWidth::Bits32, advertisement)
    };
    // RESOLVE THE STAGE, THEN ANNOTATE IT — held apart from the branch below so
    // the advertisement is logged once and on both paths. It used to be printed
    // between the announcement and the result, which put `ILC_TIER=…` inside this
    // stage's own line; `report::Open` is why that no longer compiles.
    let host = match host {
        Ok(h) => {
            stage.ok(format_args!("{} KB heap", (HEAP.used() - before) / 1024));
            Some(h)
        }
        Err(e) => {
            // PRINT THE ERROR. Two failures are expected here and they read
            // differently: "compilation settings are not compatible" means a
            // mismatched compiler; an allocation failure means PSRAM.
            stage.fail(format_args!("{e:?}"));
            None
        }
    };
    // WHAT THE GUEST WILL SEE, and it is logged whether or not instantiation
    // worked: on the failure path it is evidence about what this world offered.
    for (key, value) in advertisement {
        report.note(format_args!("{key}={value}"));
    }
    let Some(mut host) = host else {
        if !psram_ok {
            report.note(format_args!("PSRAM did not come up — 2911 KB will not fit SRAM"));
        }
        report.finish(Status::Broken);
        rest(&mut backlight, screen, Status::Broken, sys_hz);
    };

    // 6 — RUN IT. Also proven in QEMU, and it is still the claim the whole tier
    // rests on: the same engine, the same bytes, executing on this chip.
    // STATE THE FACTS, before the app runs.
    //
    // No number in this comment: `Report::stage` assigns them sequentially at
    // runtime, so writing one here would be a second copy that goes stale the
    // moment a stage is added ahead of it — which is exactly what happened when
    // this was called "5b" and the docs kept calling the next one 6.
    //
    // The wasi keys set at instantiation are FROZEN: an app reads them once
    // during `_initialize` and can never re-read them. They are the bootstrap.
    // The manifest is the correctable half — revision-stamped and re-sendable —
    // so an app can poll it (`platform.Env()`) and be told when it changes
    // (`platform.OnEnvironmentChange`).
    //
    // TODAY THE TWO AGREE, because this world has one app, one layout, and the
    // budget is fixed at flash time. Sending it anyway is what makes the numbers
    // CORRECTABLE rather than a promise this world cannot keep: the day it takes
    // rows back for a menu, it re-sends with revision 2 and the app finds out.
    //
    // A REFUSED MANIFEST IS FATAL, and deliberately so. Every command after it
    // would run against an engine that does not know what this world can show —
    // the app would format for a screen it cannot see, or stay silent on one it
    // can. Continuing would produce a badge that looks like it works and
    // quietly does the wrong thing, which is the failure this whole tier is
    // built to make impossible.
    {
        let text = WORLD.can(world::Capability::Text);
        let outlet = if text {
            manifest::TEXT_OUTLET_DISPLAY
        } else {
            // The minimal world simulates a device with a status LED and nothing
            // else. NONE is a CLAIM — it tells an app not to spend heap
            // formatting prose nobody will read — and it is the honest one here.
            manifest::TEXT_OUTLET_NONE
        };
        // With no text outlet there is no budget to describe. Zero reads as
        // unmeasured, which is the only sensible answer when the question does
        // not apply.
        let (cols, rows) = if text {
            (console::APP_COLS as u64, console::APP_ROWS as u64)
        } else {
            (0, 0)
        };
        let env = manifest::encode(1, outlet, cols, rows);
        let stage = report.stage(control::STAGE_MANIFEST, report::Scope::Emulated, format_args!("manifest"));
        match host.execute(manifest::METHOD_SET_ENVIRONMENT, env.as_bytes()) {
            Ok(r) if r.success => stage.ok(format_args!("{cols}x{rows} {}", world::text_sink())),
            Ok(r) => {
                stage.fail(format_args!("engine refused: {}", r.error.as_deref().unwrap_or("no reason")));
            }
            Err(_) => stage.fail(format_args!("not delivered")),
        }
    }

    // THE KEYS OUTLIVE THE TURN. A session runs a command more than once, and
    // moving the pins out of the menu's struct can only happen once — the type
    // system says so, which is what forced this to be hoisted rather than
    // rediscovered on the second iteration.
    #[cfg(not(badge_input_off))]
    let mut keys = keyboard::Keys {
        a: &mut pin_a,
        b: &mut pin_b,
        c: &mut pin_c,
        down: &mut pin_down,
    };
    // THE SESSION LOOP (SESSION-AND-SURFACE-PLAN Phase 1).
    //
    // The badge used to run ONE command and stop forever: no second command, no
    // way back, no second app without a power cycle. Decision 31 always allowed
    // better — `MinimalHost::execute` is documented as callable many times on one
    // instance — and nothing called it twice.
    //
    // INSTANTIATE ONCE, EXECUTE MANY. Instantiation costs 2.9 MB and most of a
    // second, and re-doing it per turn would also throw away the app's in-memory
    // state between turns: tictactoe's board would reset on every move.
    //
    // The world drives; the app stays request/response (D2). An app cannot ask
    // for another turn and does not know it got one.
    let mut turn = 0u32;
    // The last turn's verdict, so leaving the session can rest on the result the
    // person actually saw rather than a default.
    let mut last_status = Status::Idle;
    loop {
        turn += 1;

    // COLLECT INPUT — for the field THE APP ADVERTISES, not a hardcoded one.
    //
    // Phase 2 wired `name?` and field 1 as constants so the keyboard could be
    // exercised before this existed. It asked hello's question of every app: a
    // countdown was prompted for a name, and waited 30 seconds to be told one.
    //
    // Now the world asks the engine what the command takes (method 5, generated
    // from the app's own .proto) and prompts for that, or skips entirely.
    usblog::set_activity(dlc_platform_embedded::control::ACTIVITY_COLLECTING);
    // SIZED FOR A PASS-THROUGH, not for a widget. A person types one field; a
    // control client sends the app's whole request (passthrough::REQUEST_MAX).
    let mut request = [0u8; passthrough::REQUEST_MAX];
    let mut request_len = 0usize;
    #[cfg(not(badge_input_off))]
    {

        // ASK WHAT THIS COMMAND TAKES. A zero-length request means "every
        // command"; naming the method keeps the answer to the one about to run.
        let mut ask = [0u8; 16];
        let ask = dlc_platform_embedded::request::encode_varint_field(
            1,
            selected.entry_method as u64,
            &mut ask,
        )
        .unwrap_or(&[]);

        let spec = host.execute(5, ask).ok().filter(|r| r.success);
        let answer: &[u8] = spec.as_ref().map(|r| r.output.as_slice()).unwrap_or(&[]);
        use dlc_platform_embedded::spec;

        // EVERY FIELD THE COMMAND TAKES, in the order the app asks for them.
        //
        // This was `first_flag` — one field per command, which was true only
        // while the only app took one. A calculator declaring `left op right`
        // would have been prompted for `left`, and the app would have taken
        // proto defaults for the rest: `5 UNSPECIFIED 0`, reported as a success.
        //
        // NOTHING HERE KNOWS ANY APP. The field numbers, kinds, names, defaults
        // and order all come from `GetCommandSpec`, generated from the app's own
        // .proto. What the world contributes is the other half of the bargain —
        // which widget can collect a given kind — because that is a fact about
        // this badge's buttons, not about the app.
        const MAX_FIELDS: usize = 8;
        let mut flags = [spec::Flag::EMPTY; MAX_FIELDS];
        let count = spec::flags_into(answer, &mut flags);
        if count == MAX_FIELDS {
            // SAID, NOT SWALLOWED. Collecting eight of nine fields is the same
            // failure as collecting one of three, and the app would report
            // success on a request nobody meant.
            report.note(format_args!("input: more than {MAX_FIELDS} fields; the rest take defaults"));
        }

        // WHERE THE VALUE CAME FROM, recorded by the WORLD rather than by
        // whichever widget produced it.
        //
        // There is already more than one path in — a person at a widget, and a
        // control frame supplying request bytes directly (BADGE-CONTROL D2) or
        // pressing buttons (D3). A log reading `input: 30` cannot tell those
        // apart, and "did that come from the spinner or from the cable?" is
        // exactly the question a failing test asks.
        let mut source = "none";
        for flag in flags.iter().take(count) {
            // APPEND. Each field is encoded after the last, so a request carries
            // everything collected rather than only whatever went last.
            let room = &mut request[request_len..];
            match flag.kind {
                // A STRING: the character strip, prompted with the app's own
                // help text — or its field name, which always exists. With more
                // than one field to collect, "which one is this?" is the question
                // a prompt has to answer.
                kind if kind == spec::KIND_STRING => {
                    let prompt = spec::help_of(answer, flag)
                        .or_else(|| spec::name_of(answer, flag))
                        .unwrap_or("input?");
                    let typed = report.with_screen_and_log(|screen, log| {
                        keyboard::read(prompt, screen, &mut keys, sys_hz, log)
                    });
                    source = "keyboard";
                    // AN EMPTY VALUE IS NOT SENT. Proto3 scalars have no
                    // presence, so an empty string and an absent field decode
                    // identically — and sending nothing keeps the request honest.
                    if !typed.is_empty() {
                        if let Some(encoded) = dlc_platform_embedded::request::encode_string_field(
                            flag.field as u8,
                            typed.as_str(),
                            room,
                        ) {
                            request_len += encoded.len();
                        }
                    }
                }

                // A NUMBER: the spinner, starting from the declared default.
                kind if spec::is_integer(kind) || kind == spec::KIND_BOOL => {
                    let prompt = spec::help_of(answer, flag)
                        .or_else(|| spec::name_of(answer, flag))
                        .unwrap_or("value?");
                    let start = spec::default_number(answer, flag);
                    let shape = if kind == spec::KIND_BOOL {
                        spinner::Shape::Boolean
                    } else {
                        spinner::Shape::Integer
                    };
                    let value = report.with_screen_and_log(|screen, log| {
                        spinner::read(prompt, shape, start, screen, &mut keys, sys_hz, log)
                    });
                    source = "spinner";
                    // ZERO IS NOT SENT, for the same presence reason as an empty
                    // string: proto3 cannot tell a zero from an absent field, so
                    // the app takes its default either way.
                    if value != 0 {
                        if let Some(encoded) = dlc_platform_embedded::request::encode_varint_field(
                            flag.field as u8,
                            value as u64,
                            room,
                        ) {
                            request_len += encoded.len();
                        }
                    }
                }

                // A CLOSED SET: the app's own choices, offered as they were
                // declared. This is the general case (D3-general) — a keyboard
                // and a spinner are worlds guessing at a universal vocabulary;
                // this one asks the app what the words are.
                kind if kind == spec::KIND_ENUM => {
                    let prompt = spec::help_of(answer, flag)
                        .or_else(|| spec::name_of(answer, flag))
                        .unwrap_or("choose?");
                    let picked = report.with_screen_and_log(|screen, log| {
                        chooser::read(prompt, answer, flag, screen, &mut keys, sys_hz, log)
                    });
                    source = "chooser";
                    // ZERO IS NOT SENT, the same presence rule as everywhere
                    // else — and for an enum it is doubly right, since a proto3
                    // enum's zero is its UNSPECIFIED member by convention.
                    if let Some(number) = picked.filter(|number| *number != 0) {
                        if let Some(encoded) = dlc_platform_embedded::request::encode_varint_field(
                            flag.field as u8,
                            number as u64,
                            room,
                        ) {
                            request_len += encoded.len();
                        }
                    }
                }

                // A KIND WITH NO WIDGET — bytes, a float. The field is
                // skipped and the app takes its default, which is a no-op rather
                // than an error (Decision 33).
                //
                // NAMED, because with several fields "which one did you skip?"
                // has an answer worth printing: an operator nobody could enter is
                // why a calculator returned a number nobody expected.
                kind => {
                    source = "unsupported";
                    let name = spec::name_of(answer, flag).unwrap_or("?");
                    report.note(format_args!("input: {name} is kind {kind}, no widget here"));
                }
            }

            // A REQUEST HAS ARRIVED, so asking a person for the rest is a wait
            // for something nobody is going to type (D2).
            if passthrough::waiting() {
                break;
            }
        }
        if count == 0 {
            // NO INPUT ADVERTISED: no prompt, no delay, straight to the app.
            // This is the case that made a countdown wait 30 seconds for a name
            // it never wanted.
            source = "not advertised";
        }

        // ONE LINE, EVERY TURN, WHATEVER HAPPENED. An empty request is as much a
        // fact as a full one — an app that took its default did so because
        // something decided not to send, and the log should say which something.
        report.note(format_args!(
            "input: {request_len} bytes from {source}"
        ));
    }

    // A CLIENT'S REQUEST WINS, and is checked AFTER collection rather than
    // before: a request that arrives while a widget is already on screen must
    // still take effect this turn, not next. The widgets give up as soon as one
    // is waiting (see passthrough.rs), so what they collected is nothing worth
    // keeping.
    let mut method = selected.entry_method;
    // Read only by the reply below, which is compiled out with the control
    // channel — the take() itself still has to run either way, because it is
    // what clears a request nobody can answer.
    #[cfg_attr(not(badge_control), allow(unused_variables))]
    let from_control = match passthrough::take(&mut request) {
        Some((asked, len)) => {
            method = asked;
            request_len = len;
            report.note(format_args!(
                "input: {request_len} bytes from the control channel, method {method}"
            ));
            true
        }
        None => false,
    };
    // The app's text, held for the last frame. A fixed buffer because this is
    // still the pre-menu world of "do not allocate what you can put on the stack",
    // and a screen holds far less than this anyway.
    let mut output = [0u8; 512];
    let mut output_len = 0usize;
    // HOW LONG THE APP ACTUALLY TOOK, and how many times it asked to wait.
    //
    // Added because a countdown that should have taken five seconds finished
    // instantly, and nothing outside the badge could tell whether the sleep was
    // ignored, resolved early, or never requested.
    usblog::set_activity(dlc_platform_embedded::control::ACTIVITY_RUNNING);
    let started_us = now_us();
    let sleeps_before = dlc_platform_embedded::clock::sleeps();
    let stage = report.stage(control::STAGE_EXECUTE, report::Scope::Emulated, format_args!("execute {method}"));
    let status = match host.execute(method, &request[..request_len]) {
        Ok(result) => {
            if result.success {
                stage.ok(format_args!("success"));
            } else {
                stage.fail(format_args!("app reported failure"));
            }
            // TEXT IS A CAPABILITY, and the minimal world does not have it. The
            // report itself is firmware diagnostics and keeps going either way;
            // what is gated is the APP's output.
            // DRAIN UNCONDITIONALLY, RENDER CONDITIONALLY.
            //
            // The guest's write ALWAYS succeeds — `SinkStream::write` returns
            // Ok on every world, because a world with no screen must not turn
            // an app's `fmt.Println` into a trap. What a world chooses to do
            // with the bytes afterwards is its own business.
            //
            // But it must still TAKE them. Leaving them in the buffer because
            // this world has no outlet makes the sink unbounded: an app that
            // prints in a loop grows the heap until the allocator gives out,
            // and on the minimal world — the one simulating a device with a
            // single LED — nobody would be watching to see it happen.
            //
            // So the read is outside the capability check and only the
            // rendering is inside it. Discarding is a decision; forgetting is
            // a leak.
            let printed = host.stdout();
            if WORLD.can(world::Capability::Text) {
                // STDOUT IS THE READABLE CHANNEL, and `result.output` is not.
                //
                // A command's return value is PROTOBUF, and rendering it needs
                // the app's schema — which the CLI has compiled in and this
                // firmware never will, because one loader runs apps it was not
                // built for. `hello` looked like it worked only because its
                // encoded response happened to be valid UTF-8 (the leading field
                // tag 0x0a reads as a newline, which is where that stray blank
                // line came from). An app with a numeric field prints nothing.
                //
                // So the badge shows what the app PRINTED. That is also what
                // `ILC_STDOUT=display` promises, so the advertisement and the
                // behaviour are now the same statement.
                if let Ok(text) = core::str::from_utf8(&printed) {
                    if !text.is_empty() {
                        // Held for the FINAL frame: the report is about to be
                        // replaced, and text drawn now would be erased.
                        output_len = text.len().min(output.len());
                        output[..output_len].copy_from_slice(&text.as_bytes()[..output_len]);
                        report.note(format_args!("stdout: {text}"));
                    }
                }
            }
            // Events are the SEMANTIC channel — what the minimal world turns into
            // a colour, and what Phase 3 draws.
            for (topic, body) in host.events() {
                report.note(format_args!("event {topic} ({} bytes)", body.len()));
            }
            report.note(format_args!(
                "took {} ms, {} sleeps",
                (now_us() - started_us) / 1000,
                dlc_platform_embedded::clock::sleeps() - sleeps_before
            ));
            report.note(format_args!("peak heap {} KB", (HEAP.used() - before) / 1024));
            // THE APP'S OWN BYTES, verbatim. The world does not read them and
            // could not: they are in the app's schema, not this firmware's.
            #[cfg(badge_control)]
            if from_control {
                usblog::reply(
                    result.success,
                    &result.output,
                    result.error.as_deref().unwrap_or(""),
                );
            }
            world::status_of(&result)
        }
        Err(e) => {
            stage.fail(format_args!("{e:?}"));
            // A TRAP IS STILL AN ANSWER. A client that got silence here could not
            // tell a crashed guest from a badge that never received the request,
            // and would wait out its whole deadline to learn nothing.
            #[cfg(badge_control)]
            if from_control {
                usblog::reply(false, &[], "the app trapped");
            }
            Status::Broken
        }
    };

    usblog::set_activity(dlc_platform_embedded::control::ACTIVITY_RESTING);
    let verdict = if report.failed() { Status::Broken } else { status };
    last_status = verdict;
    report.finish(verdict);

    // THE LAST FRAME, and it is where the two worlds visibly differ.
    //
    // `normal` shows what the app said, under a band of the status colour — so
    // the badge answers "did it work?" across a room and "what did it say?" up
    // close. `minimal` falls through to `rest`, which floods the panel: that
    // world has no text capability, and showing text anyway would make the
    // advertisement a lie.
    // THE FINAL FRAME ALWAYS SAYS SOMETHING.
    //
    // This used to require `output_len > 0`, so an app that returns a typed
    // response without PRINTING — which is most of them, and hello in particular
    // — fell through to `rest()`, which floods the panel with the status colour
    // and erases the report the user just watched. A solid green screen carrying
    // no information is a worse outcome than the report it replaced.
    //
    // The badge cannot render `result.output`: it is protobuf, and decoding it
    // needs the app's schema, which a loader running apps it was never built for
    // does not have. So when the app prints nothing, the HOST says what it knows
    // — which app, which method, and what it cost.
    if WORLD.can(world::Capability::Text) {
        // THROUGH THE REPORT, not around it. The report holds `&mut screen` for its
        // whole life, and once a session loop uses the report AFTER this point
        // the borrow no longer ends here — so reaching for the panel directly
        // stopped compiling, correctly.
        report.with_screen_and_log(|screen, _log| {
        if let Some(panel) = screen.as_mut() {
            let mut summary = usblog::LogBuffer::new();
            let text = if output_len > 0 {
                core::str::from_utf8(&output[..output_len]).unwrap_or("")
            } else {
                use core::fmt::Write;
                let _ = writeln!(summary, "execute {method}: success");
                let _ = writeln!(summary, "{} KB heap", (HEAP.used() - before) / 1024);
                let _ = writeln!(summary);
                let _ = writeln!(summary, "(app returned a typed response;");
                let _ = writeln!(summary, " it printed nothing to stdout)");
                core::str::from_utf8(summary.as_bytes()).unwrap_or("")
            };
            console::render(panel, verdict, selected.name, text);
        }
        });
        // The backlight still carries the status for a badge whose panel failed.
        use embedded_hal::digital::OutputPin;
        let _ = if verdict.backlight_on() {
            backlight.set_high()
        } else {
            backlight.set_low()
        };
    }

    // WAIT FOR WHAT TO DO NEXT, rather than parking forever.
    //
    // This was `loop { wfi() }` — the badge's whole afterlife. Now it is the end
    // of a TURN: B runs the command again, HOME leaves.
    // B ENDS A TURN in both builds: a world with no input surface cannot collect
    // a value, but it can still run the command again. Reached THROUGH `keys`
    // where that exists, because it holds the borrow — two mutable paths to one
    // pin is exactly what the borrow checker is for.
    #[cfg(not(badge_input_off))]
    let next = wait_for_turn(&mut keys.b, &mut pin_up, sys_hz);
    #[cfg(badge_input_off)]
    let next = wait_for_turn(&mut pin_b, &mut pin_up, sys_hz);
    match next {
        Turn::Again => {
            report.note(format_args!("session: turn {} finished, again", turn));
            continue;
        }
        Turn::Leave => {
            report.note(format_args!("session: left after {turn} turn(s)"));
            break;
        }
    }
    }

    // LEAVING THE SESSION — tear the instance down and go back to the menu.
    //
    // Dropping `host` releases the guest's linear memory and everything Wasmtime
    // allocated for it. Measured rather than assumed: a leak here is invisible
    // until the heap runs out, and by then the cause is several apps behind.
    drop(host);
    dlc_platform_embedded::activity::clear();
    let leaked = HEAP.used().saturating_sub(heap_at_start);
    report.note(format_args!(
        "session: ended after {turn} turn(s), {} KB not reclaimed",
        leaked / 1024
    ));
    }
}

/// What a person asked for at the end of a turn.
enum Turn {
    /// Run the command again.
    Again,
    /// Leave the session.
    Leave,
}

/// Wait at the end of a turn.
///
/// BLOCKS RATHER THAN TIMING OUT, deliberately, and it is the opposite of every
/// other wait in this firmware. The menu and the input widgets time out because a
/// badge nobody is touching must still make progress — it has not run the app
/// yet. Here the app HAS run and its result is on screen, so there is nothing to
/// make progress toward: timing out would clear a result nobody had read.
fn wait_for_turn<AGAIN, LEAVE>(again: &mut AGAIN, leave: &mut LEAVE, sys_hz: u32) -> Turn
where
    AGAIN: embedded_hal::digital::InputPin,
    LEAVE: embedded_hal::digital::InputPin,
{
    // DEBOUNCE IS A WORLD CONCERN, and this is the third place that is true: an
    // app never sees a button, only a request field, so it could not debounce
    // even if it wanted to — and on a browser world or a control frame there is
    // nothing to debounce.
    //
    // 40 ms MATCHES THE WIDGETS, and the first version of this used 20, which
    // was a bug: sampling faster than a switch bounces means the release-wait
    // below can exit on a transient high and then read the SAME press as a new
    // one. That is the one-press-two-actions bug this function exists to
    // prevent, reintroduced by polling too fast.
    const POLL_MS: u32 = 40;
    // And a release must be STABLE, not merely observed once. Two consecutive
    // quiet samples cost 80 ms nobody notices and remove the whole class.
    let mut quiet = 0u32;
    while quiet < 2 {
        cortex_m::asm::delay(sys_hz / 1000 * POLL_MS);
        if again.is_low().unwrap_or(false) || leave.is_low().unwrap_or(false) {
            quiet = 0;
        } else {
            quiet += 1;
        }
    }
    loop {
        cortex_m::asm::delay(sys_hz / 1000 * POLL_MS);
        // SERVICE USB FROM HERE TOO. The interrupt only fires on bus activity,
        // and a host reading an idle port gets NAKs that generate almost none —
        // so a heartbeat driven purely by the interrupt never beats. This loop
        // runs every 40 ms whatever the bus is doing, which is exactly the
        // regularity a heartbeat needs.
        usblog::pump();
        // A REQUEST IS A REASON FOR ANOTHER TURN.
        //
        // Without this the badge sits here — the turn is over, the result is on
        // screen, and it is waiting for a finger. A client's request would be
        // queued with nobody to collect it, and the FIRST pass-through of a
        // session would work while every one after it timed out, which is
        // exactly how this presented.
        if crate::passthrough::waiting() {
            return Turn::Again;
        }
        if leave.is_low().unwrap_or(false) || crate::buttons::taken(control::BUTTON_UP) {
            return Turn::Leave;
        }
        if again.is_low().unwrap_or(false) || crate::buttons::taken(control::BUTTON_A) {
            return Turn::Again;
        }
    }
}

// MILESTONE 3 STARTS HERE, and this note is the handover:
//
//   * tictactoe rather than hello — 2.21 MB against 890 KB, and a filesystem.
//     `MinimalHost` grants no preopens today, so tictactoe's persistence fails
//     with an app-level error naming the operation (`mkdir /: errno 2`), which is
//     EMBEDDED-PLAN D5's whole thesis working: the RAM-backed filesystem is one
//     block in `minimal.rs` and the engine does not change.
//
//   * A SELECTOR. `payload::discover()` already returns a list and this file
//     already runs `get(0)`; the five buttons in `board.rs` are what turns that
//     into a choice. That is Phase 3, and it wants the TFT first — a menu with no
//     screen is a serial prompt, which is fine but not the demo.
//
//   * UART stdout. `MinimalState` collects the guest's stdout into a `Vec` and
//     this file prints it after the fact, which is a laptop's shape. `uart.rs`
//     already implements `OutputStream` over any `ByteSink`, so the badge wants a
//     sink that writes the UART directly — then a component that prints as it
//     runs shows up live rather than at the end.
//
//   * The RTC and the hardware RNG. `wasi:clocks` is a tick counter and
//     `wasi:random` is xorshift; the board has a PCF85063A on I2C (`board.rs`)
//     and an RNG peripheral. Neither blocks anything — they are the difference
//     between a host that is honest and one that is right.
