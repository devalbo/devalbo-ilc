//! Does this panel light up, and under what?
//!
//! A firmware with ONE job. The bring-up image links Wasmtime and takes ~90s to
//! build; every question about the ST7789 had to wait behind that. This asks the
//! questions directly, in seconds.
//!
//! # How to use it
//!
//! It walks a list of CANDIDATE init sequences. After each one it fills the
//! screen with a distinct colour, holds it, and logs which candidate is on. So
//! one flash tests every hypothesis at once, and the answer is "the screen went
//! green" — no serial correlation needed, though the log says it too.
//!
//! **Watch the badge, note the first colour you see, and read it off the table
//! the log prints.** A candidate that produces nothing is eliminated; the first
//! that produces anything is the answer.
#![no_std]
#![no_main]

mod cs;
mod readback;
mod panel;
mod siobus;
mod usblog;

use core::fmt::Write;

use embedded_graphics::pixelcolor::raw::RawU16;
use embedded_graphics::pixelcolor::Rgb565;
use embedded_graphics::prelude::*;

use panic_halt as _;
use rp235x_hal as hal;

use embedded_hal::digital::OutputPin;
// Brings set_duty_cycle into scope for the PWM channel.
use embedded_hal::pwm::SetDutyCycle;
use hal::fugit::RateExtU32;
use hal::Clock;

use mipidsi::interface::ParallelInterface;
use mipidsi::options::{ColorInversion, Orientation, Rotation};
use mipidsi::{Builder, NoResetPin};

#[link_section = ".start_block"]
#[used]
pub static IMAGE_DEF: hal::block::ImageDef = hal::block::ImageDef::secure_exe();

const XTAL_HZ: u32 = 12_000_000;

static mut LOG: usblog::LogBuffer = usblog::LogBuffer::new();
static mut USB_CLOCK: Option<hal::clocks::UsbClock> = None;

/// What each candidate is, and the colour it paints if it works.
///
/// Colours are chosen to be unmistakable from each other across a room, because
/// "which one lit" is the entire result.
const CANDIDATES: [(&str, u16); 4] = [
    ("A: tufty init, inverted, rot90", 0xF800), // red
    ("B: tufty init, NORMAL colours", 0x07E0),  // green
    ("C: mipidsi stock ST7789", 0x001F),        // blue
    ("D: tufty init, rot0 (portrait)", 0xFFE0), // yellow
];

/// Log a line stamped with milliseconds since boot.
///
/// WHY EVERY LINE. The log is served on a loop, so a read can land mid-replay or
/// on a buffer from a previous flash. A timestamp makes both obvious: stages
/// hundreds of ms apart are this boot, and a line claiming 4 ms when the device
/// has been up for minutes is a replay, not fresh news.
macro_rules! logln {
    ($log:expr, $timer:expr, $($arg:tt)*) => {{
        let ms = $timer.get_counter().ticks() / 1000;
        let _ = write!($log, "[{ms:>6}ms] ");
        let _ = writeln!($log, $($arg)*);
    }};
}

#[hal::entry]
fn main() -> ! {
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

    let mut timer = hal::Timer::new_timer0(pac.TIMER0, &mut pac.RESETS, &clocks);

    // POWER THE PANEL. GPIO41 gates the display's rail — verified by toggling it
    // under Pimoroni's firmware and watching the screen blank and return. Their
    // header calls it "I2C power for talking to RTC", which is what kept it out
    // of this firmware and cost a day of chasing an unpowered controller.
    //
    // FIRST, and before any display work: everything after this assumes there is
    // something on the other end of the bus.
    let mut power_en = pins.gpio41.into_push_pull_output();
    let _ = power_en.set_high();
    // Give the rail time to come up before the panel is addressed.
    cortex_m::asm::delay(10_000_000);
    unsafe { USB_CLOCK = Some(clocks.usb_clock) };
    let sys_hz = clocks.system_clock.freq().to_Hz();

    // BACKLIGHT VIA PWM, not a GPIO held high — and this may be the whole bug.
    //
    // Pimoroni configure GPIO26 as `mode=ALT, alt=PWM` and set brightness to
    // 230/255. We drove it as plain DC-high, and the badge came up "very very
    // dimly lit" where MicroPython was clearly bright. If this backlight driver
    // wants a switching signal rather than a level, then the panel may have been
    // rendering correctly all along and simply been invisible — which would
    // account for every observation so far, including the ones I attributed to
    // the bus.
    //
    // Slice 5, channel A: GPIO26 -> slice (26/2) % 8 = 5, and even pins are A.
    let pwm_slices = hal::pwm::Slices::new(pac.PWM, &mut pac.RESETS);
    let mut backlight_pwm = pwm_slices.pwm5;
    backlight_pwm.set_ph_correct();
    backlight_pwm.set_top(255);
    backlight_pwm.enable();
    let channel = &mut backlight_pwm.channel_a;
    channel.output_to(pins.gpio26);
    // 230/255, the value their firmware uses.
    channel.set_duty_cycle(230).ok();

    let log: &mut usblog::LogBuffer = unsafe { &mut *core::ptr::addr_of_mut!(LOG) };

    // BACKLIGHT TEST FIRST, and on its own. It has been the one ambiguous
    // variable for the whole investigation: the panel was reported "very very
    // dimly lit", which is exactly what a barely-driven backlight looks like —
    // and if the backlight is only trickling, a correctly rendered image is
    // invisible and every other measurement is chasing a phantom.
    //
    // Three slow pulses, unmistakable from across a room and impossible to
    // confuse with a static image. This needs NOTHING from the panel: it is one
    // GPIO and a PWM slice.
    logln!(log, timer, "backlight: 3 pulses, ~1s each — WATCH THE SCREEN");
    for _ in 0..3 {
        for duty in (0..=255u16).step_by(5) {
            let _ = channel.set_duty_cycle(duty);
            cortex_m::asm::delay(sys_hz / 400);
        }
        for duty in (0..=255u16).rev().step_by(5) {
            let _ = channel.set_duty_cycle(duty);
            cortex_m::asm::delay(sys_hz / 400);
        }
    }
    let _ = channel.set_duty_cycle(230);
    logln!(log, timer, "backlight: held at 230/255");
    logln!(log, timer, "=== display probe ===");
    logln!(log, timer, "sys clock: {sys_hz} Hz");
    logln!(log, timer, "candidates, in order:");
    for (name, colour) in CANDIDATES {
        logln!(log, timer, "  {name}  -> {colour:#06x}");
    }

    let cs = pins.gpio27.into_push_pull_output().into_dyn_pin();
    let rs = pins.gpio28.into_push_pull_output().into_dyn_pin();
    let mut rd = pins.gpio31.into_push_pull_output().into_dyn_pin();
    // CONCRETE, not type-erased: these have to be RE-FUNCTIONED for PIO further
    // down, and `into_function` is not available on a `DynPinId`. They start as
    // SIO outputs because the readback probe below drives them by hand.
    let wr_out = pins.gpio30.into_push_pull_output();
    let d0 = pins.gpio32.into_push_pull_output();
    let d1 = pins.gpio33.into_push_pull_output();
    let d2 = pins.gpio34.into_push_pull_output();
    let d3 = pins.gpio35.into_push_pull_output();
    let d4 = pins.gpio36.into_push_pull_output();
    let d5 = pins.gpio37.into_push_pull_output();
    let d6 = pins.gpio38.into_push_pull_output();
    let d7 = pins.gpio39.into_push_pull_output();
    // PROBE THE CONTROL PINS — the measurement that was missing.
    //
    // The badge firmware verified DB0..DB7 (wrote 0xA5, read it back off the
    // pads) and stopped there, leaving WR, RS and CS untested. If WR does not
    // strobe, nothing latches, and the symptom is indistinguishable from a bad
    // init sequence — which is what four flash cycles were spent on. These pins
    // are all below GPIO32, so they read from the LOW SIO bank.
    {
        let sio_p = unsafe { &*hal::pac::SIO::ptr() };
        for (name, mask) in [
            ("CS/27", 1u32 << 27),
            ("RS/28", 1u32 << 28),
            ("WR/30", 1u32 << 30),
            ("RD/31", 1u32 << 31),
        ] {
            sio_p.gpio_out_set().write(|w| unsafe { w.bits(mask) });
            cortex_m::asm::delay(500);
            let hi = (sio_p.gpio_in().read().bits() & mask) != 0;
            sio_p.gpio_out_clr().write(|w| unsafe { w.bits(mask) });
            cortex_m::asm::delay(500);
            let lo = (sio_p.gpio_in().read().bits() & mask) != 0;
            logln!(
                log,
                timer,
                "  pin {name}: high={hi} low={lo} {}",
                if hi && !lo { "OK" } else { "*** STUCK ***" }
            );
        }
    }

    // ASK THE PANEL WHO IT IS — the first bidirectional test in this whole
    // investigation, and the one that decides where the fault is.
    //
    // Everything before this was write-only, so "it worked" only ever meant "no
    // pin errored". A controller that answers is powered, clocked, and parsing
    // commands; one that does not is why four init sequences all looked the same.
    {
        // SAFETY: these pins are ours, configured as SIO outputs above, and
        // nothing else touches them while this runs.
        let ids = unsafe { readback::read_command(0x04, 27, 28, 30, 31) };
        logln!(
            log,
            timer,
            "RDDID(0x04) -> {:02x} {:02x} {:02x} {:02x}  {}",
            ids.bytes[0],
            ids.bytes[1],
            ids.bytes[2],
            ids.bytes[3],
            if ids.plausible() {
                "PANEL ANSWERS — bus works both ways"
            } else {
                "*** NO ANSWER — nothing is listening ***"
            }
        );
        // RDDST (0x09) too: a second opinion, and its bits say whether the
        // display is on, inverted and out of sleep — which would tell us the
        // init actually took.
        let st = unsafe { readback::read_command(0x09, 27, 28, 30, 31) };
        logln!(
            log,
            timer,
            "RDDST(0x09) -> {:02x} {:02x} {:02x} {:02x}",
            st.bytes[0],
            st.bytes[1],
            st.bytes[2],
            st.bytes[3]
        );
    }

    // CS is NOT held low here any more — `ChipSelect` brackets each transaction
    // with it, matching Pimoroni. RD stays high: this driver never reads, and a
    // floating RD is an input the controller may sample as a read strobe.
    let _ = rd.set_high();

    // CONFIRMATION RUN: stock mipidsi only, from cold, cycling colours.
    //
    // The candidate sweep said the stock ST7789 model renders and our transcribed
    // Pimoroni sequence does not — but each candidate ran on a panel the previous
    // one had already configured, so that reading is circumstantial. This starts
    // from a cold panel and uses ONE init, so the result stands on its own.
    //
    // Five fills, because one cannot distinguish "the driver works" from "the
    // panel happens to be showing something".
    // THE PIO BUS, and the symptom that finally justified it.
    //
    // Bit-banged, `clear()` takes mipidsi's fast path for a solid colour: write
    // the byte once, then toggle WR in a tight loop with NOTHING between the
    // edges. That is the shortest strobe this code can emit, the ST7789 wants
    // ~15ns either side of it, and a missed strobe is a DROPPED PIXEL — which
    // shows up as rows walking sideways ("1 pixel jumps") and the far end of the
    // framebuffer never being reached ("top half static"). One mechanism, both
    // symptoms.
    //
    // PIO makes the pulse a hardware guarantee: `out pins, 8 side 0` then
    // `nop side 1`, one clock each, identical on every byte.
    // PIO takes the pins by FUNCTION, and a type-erased DynPinId cannot be
    // reassigned — so these must be converted before erasure. `data` was built
    // as dyn pins for the SIO bus; the PIO path needs them back as concrete
    // pins, which is why they are re-taken from `pins` here rather than reused.
    // KEEP THE PINS ON SIO. The PIO port is written (`piobus.rs`) and cannot be
    // used through this HAL: PIO reaches GPIO32..47 only via the RP2350's
    // GPIOBASE window, which rp235x-hal 0.3 does not model — the type system
    // refuses, correctly. Pimoroni set that register from C.
    //
    // So the CPU keeps driving the bus and `siobus.rs` fixes what was actually
    // wrong with it: mipidsi's solid-fill fast path strobes WR without
    // re-driving data, which is the narrowest pulse this code can emit and the
    // mechanism behind both the walking rows and the unwritten half.
    let data_pins = (d0, d1, d2, d3, d4, d5, d6, d7);
    let _ = data_pins; // owned so nothing else reconfigures them

    let bus = cs::ChipSelect::new(siobus::SioInterface::new(rs, wr_out), cs);

    logln!(log, timer, "stock mipidsi ST7789, from cold");
    match Builder::new(mipidsi::models::ST7789, bus)
        .display_size(240, 320)
        .orientation(Orientation::new().rotate(Rotation::Deg90))
        .invert_colors(ColorInversion::Inverted)
        .init(&mut timer)
    {
        Ok(mut display) => {
            logln!(log, timer, "  init ok");
            for (name, colour) in [
                ("RED", 0xF800u16),
                ("GREEN", 0x07E0),
                ("BLUE", 0x001F),
                ("WHITE", 0xFFFF),
                ("BLACK", 0x0000),
            ] {
                logln!(log, timer, "  fill {name} ({colour:#06x})");
                let _ = display.clear(Rgb565::from(RawU16::new(colour)));
                cortex_m::asm::delay(sys_hz * 2);
            }
            logln!(log, timer, "  cycled RED GREEN BLUE WHITE BLACK");

            // AN ORIENTATION MAP, because "half of it is static" is not a
            // reportable observation without knowing which half.
            //
            // Four quadrants in distinct colours, plus a WHITE BAR along what
            // the firmware believes is the TOP edge and a MAGENTA BAR down what
            // it believes is the LEFT. Where those bars physically land tells us
            // the rotation directly — if the white bar runs down a side, MADCTL
            // and our `Rotation` disagree by 90 degrees.
            //
            // It also localises a partial render: a quadrant that stays as noise
            // is a specific region of display RAM that never got written, which
            // points at the address window rather than at the bus.
            use embedded_graphics::primitives::{PrimitiveStyle, Rectangle};
            let w = panel::WIDTH_LANDSCAPE as i32;
            let h = panel::HEIGHT_LANDSCAPE as i32;
            let quad = |x: i32, y: i32| {
                Rectangle::new(Point::new(x, y), Size::new((w / 2) as u32, (h / 2) as u32))
            };
            let fill = |c: u16| PrimitiveStyle::with_fill(Rgb565::from(RawU16::new(c)));

            let _ = display.clear(Rgb565::from(RawU16::new(0x0000)));
            let _ = quad(0, 0).into_styled(fill(0xF800)).draw(&mut display); // TL red
            let _ = quad(w / 2, 0).into_styled(fill(0x07E0)).draw(&mut display); // TR green
            let _ = quad(0, h / 2).into_styled(fill(0x001F)).draw(&mut display); // BL blue
            let _ = quad(w / 2, h / 2).into_styled(fill(0xFFE0)).draw(&mut display); // BR yellow

            // The two reference bars, drawn last so they sit on top.
            let _ = Rectangle::new(Point::new(0, 0), Size::new(w as u32, 8))
                .into_styled(fill(0xFFFF))
                .draw(&mut display); // WHITE = top
            let _ = Rectangle::new(Point::new(0, 0), Size::new(8, h as u32))
                .into_styled(fill(0xF81F))
                .draw(&mut display); // MAGENTA = left

            logln!(log, timer, "  ORIENTATION MAP drawn:");
            logln!(log, timer, "    WHITE bar   = the TOP edge, as the firmware sees it");
            logln!(log, timer, "    MAGENTA bar = the LEFT edge");
            logln!(log, timer, "    quadrants: TL red, TR green, BL blue, BR yellow");
            logln!(log, timer, "  report where the WHITE bar is, relative to the buttons");
        }
        Err(_) => {
            logln!(log, timer, "  init FAILED");
            serve_log();
        }
    }

    logln!(log, timer, "done — note the FIRST colour you saw");
    serve_log()
}

/// Milliseconds since boot, read straight from TIMER0.
///
/// `serve_log` does not own the `Timer` — main still has it — so this reads the
/// raw latching registers instead. LOW FIRST: reading `timerawl` latches the
/// high word, and taking them the other way round can straddle a wrap.
fn raw_uptime_ms() -> u64 {
    let timer = unsafe { &*hal::pac::TIMER0::ptr() };
    let low = timer.timerawl().read().bits() as u64;
    let high = timer.timerawh().read().bits() as u64;
    ((high << 32) | low) / 1000
}

/// A one-line buffer for the live marker.
struct UptimeLine {
    bytes: [u8; 64],
    len: usize,
}

impl UptimeLine {
    fn new() -> Self {
        Self { bytes: [0; 64], len: 0 }
    }
    fn as_bytes(&self) -> &[u8] {
        &self.bytes[..self.len]
    }
}

impl core::fmt::Write for UptimeLine {
    fn write_str(&mut self, s: &str) -> core::fmt::Result {
        for b in s.bytes() {
            if self.len == self.bytes.len() {
                break;
            }
            self.bytes[self.len] = b;
            self.len += 1;
        }
        Ok(())
    }
}

/// Idle forever, offering the log over USB CDC.
fn serve_log() -> ! {
    let pac = unsafe { hal::pac::Peripherals::steal() };
    let Some(usb_clock) = (unsafe { core::ptr::addr_of_mut!(USB_CLOCK).replace(None) }) else {
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
        usb_device::device::UsbVidPid(0x2e8a, 0x000b),
    )
    .strings(&[usb_device::device::StringDescriptors::default()
        .manufacturer("devalbo")
        .product("DLC display probe")
        .serial_number("probe")])
    .unwrap()
    .device_class(usbd_serial::USB_CLASS_CDC)
    .build();

    let log: &usblog::LogBuffer = unsafe { &*core::ptr::addr_of!(LOG) };
    let bytes = log.as_bytes();
    let mut sent = 0usize;
    let mut idle = 0u32;
    let mut marked = false;

    loop {
        device.poll(&mut [&mut serial]);
        let mut discard = [0u8; 64];
        let _ = serial.read(&mut discard);
        if sent < bytes.len() {
            if let Ok(n) = serial.write(&bytes[sent..]) {
                sent += n;
            }
        } else {
            // THE LIVE MARKER. Everything above is a recording of the boot; this
            // is the only line written at the moment you read it. If the run
            // finished at 21s and this says 400s, the device has been sitting
            // idle since — which is exactly the confusion a replayed buffer
            // otherwise causes.
            if !marked {
                let now_ms = raw_uptime_ms();
                let mut line = UptimeLine::new();
                let _ = write!(line, "[{now_ms:>6}ms] --- end of run; log replays from here ---\r\n");
                if serial.write(line.as_bytes()).is_ok() {
                    marked = true;
                }
            }
            idle = idle.saturating_add(1);
            if idle > 2_000_000 {
                sent = 0;
                marked = false;
                idle = 0;
            }
        }
    }
}
