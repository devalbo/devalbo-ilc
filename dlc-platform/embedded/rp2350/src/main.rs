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
mod cs;
mod siobus;
mod usblog;
mod payload;
mod platform;
mod psram;
mod report;
mod world;

use world::{Status, WORLD};

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

/// The USB clock token, handed from `main` to `serve_log`.
///
/// A static rather than a parameter because `rest()` has four call sites and the
/// token is only needed at the very last one; threading it through every early
/// exit would put USB plumbing in the middle of the bring-up flow, which is the
/// opposite of what this is for.
static mut USB_CLOCK: Option<hal::clocks::UsbClock> = None;

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
    // The peripherals were taken at boot, so steal them. Sound because main has
    // finished with everything this touches and nothing else runs.
    let pac = unsafe { hal::pac::Peripherals::steal() };
    // SAFETY: set once in main before any of this runs, single core.
    let Some(usb_clock) = (unsafe { core::ptr::addr_of_mut!(USB_CLOCK).replace(None) }) else {
        // No clock token means main never got that far; nothing to serve with.
        loop {
            cortex_m::asm::wfi();
        }
    };
    let mut resets = pac.RESETS;
    let usb = hal::usb::UsbBus::new(pac.USB, pac.USB_DPRAM, usb_clock, true, &mut resets);
    let bus = usb_device::bus::UsbBusAllocator::new(usb);
    let mut serial = usbd_serial::SerialPort::new(&bus);
    let mut device = usb_device::device::UsbDeviceBuilder::new(
        &bus,
        usb_device::device::UsbVidPid(0x2e8a, 0x000a),
    )
    .strings(&[usb_device::device::StringDescriptors::default()
        .manufacturer("devalbo")
        .product("DLC badge bring-up")
        .serial_number("ilc")])
    .unwrap()
    .device_class(usbd_serial::USB_CLASS_CDC)
    .build();

    // SAFETY: single core, no interrupts, and main has released its borrow.
    let log: &usblog::LogBuffer = unsafe { &*core::ptr::addr_of!(LOG) };
    // SAY SO IF THE LOG DID NOT FIT.
    //
    // `LogBuffer` records truncation and, until now, nothing ever asked — the
    // getter was dead code the compiler warned about. Deleting it would have
    // been the smaller change and the wrong one: the field is written on every
    // overflow, so the log was already silently dropping its tail, and a boot
    // log that stops mid-sentence reads as a HANG rather than a full buffer.
    // That is the most expensive thing a diagnostic can do — send someone
    // debugging a freeze that never happened.
    //
    // SENT ALONGSIDE THE BODY, not appended to it: the buffer being full is
    // precisely what truncation means, so writing the notice into it is the one
    // operation guaranteed to be dropped. It leads because a reader needs to
    // know before they start trusting what follows.
    let notice: &[u8] = if log.truncated() {
        b"(log truncated -- later lines were dropped)\r\n"
    } else {
        b""
    };
    let bytes = log.as_bytes();
    let mut sent = 0usize;
    let mut idle_since_open = 0u32;

    loop {
        // POLL, THEN WRITE REGARDLESS. `poll` reports whether there was USB
        // ACTIVITY, not whether the port is usable — gating the write on it
        // meant an idle host (the normal case: a terminal that only reads) never
        // triggered a single byte.
        device.poll(&mut [&mut serial]);
        // Drain anything the host types; a terminal opening the port often
        // sends nothing, but an unread RX buffer stalls some stacks.
        let mut discard = [0u8; 64];
        let _ = serial.read(&mut discard);

        // One cursor over two slices, so the repeat-on-idle logic below stays a
        // single `sent = 0` rather than two counters that can disagree.
        let total = notice.len() + bytes.len();
        if sent < total {
            let chunk = if sent < notice.len() {
                &notice[sent..]
            } else {
                &bytes[sent - notice.len()..]
            };
            match serial.write(chunk) {
                Ok(n) => sent += n,
                Err(usbd_serial::UsbError::WouldBlock) => {}
                Err(_) => {}
            }
        } else {
            // REPEAT THE LOG once a host has had it, so attaching a terminal
            // after the fact still shows something rather than an empty port.
            idle_since_open = idle_since_open.saturating_add(1);
            if idle_since_open > 2_000_000 {
                sent = 0;
                idle_since_open = 0;
            }
        }
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
    // Hand the USB clock over AFTER the last borrow of `clocks` — taking the
    // field partially moves it, and the timer above still needs the whole thing.
    unsafe { USB_CLOCK = Some(clocks.usb_clock) };
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
    let (mut screen, screen_err) = match screen {
        Ok(panel) => (Some(panel), ""),
        // KEEP THE REASON. Discarding it is what made a configuration mistake
        // look like dead hardware for a whole session: the badge blinked with a
        // blank screen, which is indistinguishable from a panel that is not
        // wired, while the actual answer was `InvalidDisplaySize`.
        Err(display::DisplayError(why)) => (None, why),
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
    // SAFETY: single core, no interrupt touches LOG, and this is the only borrow
    // taken for the report's lifetime.
    let log: &mut usblog::LogBuffer = unsafe { &mut *core::ptr::addr_of_mut!(LOG) };
    let mut sink = usblog::Tee(&mut uart, log);
    let mut report = report::Report::new(
        &mut sink,
        &mut screen,
        sys_hz,
        format_args!("DLC {} [{}]", env!("CARGO_PKG_VERSION"), WORLD.name()),
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
    // THE MEASUREMENT THAT SHOULD HAVE COME FIRST. Everything about the display
    // is downstream of whether writing GPIO32..39 changes the pads, and four
    // flash cycles went on init sequences and timing while that stayed untested.
    {
        let stage = report.stage(report::Scope::HardwareOnly, format_args!("data bus 32-39"));
        let (high, low) = probe_data_bus();
        if high == 0xA5 && low == 0x00 {
            stage.ok(format_args!("A5/00"));
        } else {
            stage.fail(format_args!("wrote A5/00 got {high:02x}/{low:02x}"));
        }
    }

    let stage = report.stage(report::Scope::HardwareOnly, format_args!("display ST7789"));
    if screen_ok {
        stage.ok(format_args!("320x240 parallel"));
    } else {
        stage.fail(format_args!("init failed: {screen_err}"));
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
        // PULL-UP, not pull-down: the buttons short to ground (measured against
        // Pimoroni's own firmware on the board — see menu.rs).
        up: pins.gpio11.into_pull_up_input(),
        down: pins.gpio6.into_pull_up_input(),
        a: pins.gpio7.into_pull_up_input(),
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
    // 5b — STATE THE FACTS, before the app runs.
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
        let stage = report.stage(report::Scope::Emulated, format_args!("manifest"));
        match host.execute(manifest::METHOD_SET_ENVIRONMENT, env.as_bytes()) {
            Ok(r) if r.success => stage.ok(format_args!("{cols}x{rows} {}", world::text_sink())),
            Ok(r) => {
                stage.fail(format_args!("engine refused: {}", r.error.as_deref().unwrap_or("no reason")));
            }
            Err(_) => stage.fail(format_args!("not delivered")),
        }
    }

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
