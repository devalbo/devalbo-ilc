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
mod console;
mod display;
mod menu;
mod payload;
mod platform;
mod psram;
mod report;
mod world;

use world::{Status, WORLD};

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
    // The backlight still matters: a filled panel with the backlight off is a
    // black screen, and it is what makes the blink in `rest` visible.
    let _ = if status.backlight_on() {
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
    show(backlight, screen, status);
    if !status.blinks() {
        loop {
            cortex_m::asm::wfi();
        }
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
    let mut screen = match screen {
        Ok(panel) => Some(panel),
        Err(_) => None,
    };
    // Captured before the report borrows `screen`, so stage 2 can report on the
    // very thing it is drawing to.
    let screen_ok = screen.is_some();
    show(&mut backlight, &mut screen, Status::Broken);

    let sys_hz = clocks.system_clock.freq().to_Hz();

    // BRING-UP AS A WATCHABLE SEQUENCE (report.rs). Each stage announces itself
    // before it runs, so a hang shows the name of what it hung in — and the ones
    // QEMU cannot model are marked `*`, because those are the only checks that
    // carry information `make qemu` has not already given us.
    let mut report = report::Report::new(
        &mut uart,
        &mut screen,
        sys_hz,
        format_args!("ILC {} [{}]", env!("CARGO_PKG_VERSION"), WORLD.name()),
    );

    // 1 — THE CRYSTAL, and it is hardware-only for a non-obvious reason: the UART
    // divisor is derived from it, so if this number is wrong the evidence is
    // garbled text rather than a wrong figure. QEMU has no crystal at all.
    report
        .stage(report::Scope::HardwareOnly, format_args!("clocks / crystal"))
        .ok(format_args!("RP2350B @ {} Hz", sys_hz));
    report.note(format_args!(
        "firmware {} v{}",
        env!("CARGO_PKG_NAME"),
        env!("CARGO_PKG_VERSION")
    ));

    // 2 — THE PANEL. An 8-bit parallel ST7789 on a bit-banged bus: QEMU models no
    // RP2350 peripherals, so nothing about this has ever executed. If you are
    // reading this stage ON the badge, it passed.
    let stage = report.stage(report::Scope::HardwareOnly, format_args!("display ST7789"));
    if screen_ok {
        stage.ok(format_args!("320x240 parallel"));
    } else {
        stage.fail(format_args!("init failed — UART only"));
    }

    // 3 — PSRAM. The single most likely thing to fail: register-level code that
    // has never run, executing with XIP DISABLED so a mistake cannot print. QEMU
    // has no QSPI device to model.
    let stage = report.stage(report::Scope::HardwareOnly, format_args!("PSRAM 8 MiB"));
    let psram_ok = match psram::init(board::PSRAM_CS, sys_hz) {
        Ok(ps) => {
            // Re-init the allocator onto PSRAM. Sound only because nothing above
            // has allocated: `LlffHeap::init` may be called once, and the SRAM
            // block was never handed out.
            unsafe { HEAP.init(ps.base as usize, ps.len) };
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
    let stage = report.stage(report::Scope::HardwareOnly, format_args!("payload region"));
    let available = payload::discover();
    if available.is_empty() {
        // NOT a failure. This is what an empty loader is FOR, and saying so
        // plainly is the difference between "waiting" and "broken".
        stage.ok(format_args!("empty"));
        report.note(format_args!("{}", payload::MODE));
        report.note(format_args!("drag a payload UF2 onto the RP2350 drive"));
        report.finish(Status::Idle);
        rest(&mut backlight, &mut screen, Status::Idle, sys_hz);
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
            report.item(format_args!(
                "[{index}] {shown} {} KB",
                found.bytes.len() / 1024
            ));
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
    let mut buttons = menu::Buttons {
        up: pins.gpio11.into_pull_down_input(),
        down: pins.gpio6.into_pull_down_input(),
        a: pins.gpio7.into_pull_down_input(),
    };
    let choice = report.with_screen_and_log(|screen, log| {
        menu::choose(&available, screen, &mut buttons, sys_hz, log)
    });
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
            .stage(report::Scope::HardwareOnly, format_args!("verify payload"))
            .fail(format_args!("checksum mismatch"));
        report.note(format_args!(
            "{} is corrupt — re-drag the payload UF2",
            selected.name
        ));
        report.finish(Status::Broken);
        rest(&mut backlight, &mut screen, Status::Broken, sys_hz);
    }

    // 5 — INSTANTIATION. **This one QEMU already proved**, at this pointer width,
    // through this exact host: 81 KB to load, 2911 KB to instantiate. Running it
    // here asks a narrower question — does the BOARD agree with the emulator? — and
    // the interesting failure is a settings mismatch or PSRAM being too slow, not
    // "does Wasmtime work".
    let mut advertisement = [("", ""); world::ADVERTISEMENT_MAX];
    let advertisement = WORLD.advertise(&mut advertisement);
    let before = HEAP.used();
    let stage = report.stage(report::Scope::Emulated, format_args!("instantiate {}", selected.name));
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
        rest(&mut backlight, &mut screen, Status::Broken, sys_hz);
    };

    // 6 — RUN IT. Also proven in QEMU, and it is still the claim the whole tier
    // rests on: the same engine, the same bytes, executing on this chip.
    let method = selected.entry_method;
    // The app's text, held for the last frame. A fixed buffer because this is
    // still the pre-menu world of "do not allocate what you can put on the stack",
    // and a screen holds far less than this anyway.
    let mut output = [0u8; 512];
    let mut output_len = 0usize;
    let stage = report.stage(report::Scope::Emulated, format_args!("execute {method}"));
    let status = match host.execute(method, &[]) {
        Ok(result) => {
            if result.success {
                stage.ok(format_args!("success"));
            } else {
                stage.fail(format_args!("app reported failure"));
            }
            // TEXT IS A CAPABILITY, and the minimal world does not have it. The
            // report itself is firmware diagnostics and keeps going either way;
            // what is gated is the APP's output.
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
                let printed = host.stdout();
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
            report.note(format_args!("peak heap {} KB", (HEAP.used() - before) / 1024));
            world::status_of(&result)
        }
        Err(e) => {
            stage.fail(format_args!("{e:?}"));
            Status::Broken
        }
    };

    let verdict = if report.failed() { Status::Broken } else { status };
    report.finish(verdict);

    // THE LAST FRAME, and it is where the two worlds visibly differ.
    //
    // `normal` shows what the app said, under a band of the status colour — so
    // the badge answers "did it work?" across a room and "what did it say?" up
    // close. `minimal` falls through to `rest`, which floods the panel: that
    // world has no text capability, and showing text anyway would make the
    // advertisement a lie.
    if WORLD.can(world::Capability::Text) && output_len > 0 {
        if let Some(panel) = screen.as_mut() {
            let text = core::str::from_utf8(&output[..output_len]).unwrap_or("");
            console::render(panel, verdict, selected.name, text);
        }
        // The backlight still carries the status for a badge whose panel failed.
        use embedded_hal::digital::OutputPin;
        let _ = if verdict.backlight_on() {
            backlight.set_high()
        } else {
            backlight.set_low()
        };
        loop {
            cortex_m::asm::wfi();
        }
    }
    rest(&mut backlight, &mut screen, status, sys_hz);
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
