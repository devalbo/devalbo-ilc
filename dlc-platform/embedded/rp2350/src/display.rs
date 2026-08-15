//! The Tufty's 320×240 ST7789 — **8-bit PARALLEL, not SPI**.
//!
//! THE FIRST THING TO KNOW, because it sends people to the wrong crate: this
//! panel speaks the Intel 8080 parallel interface — eight data lines plus
//! WR/RD/RS/CS — and the popular `st7789` crates assume SPI. `board.rs` records
//! the pin map.
//!
//! # What is ours, and what is emphatically not
//!
//! **`mipidsi` drives the panel.** The ST7789 command set, its power-on
//! sequence, the MADCTL orientation bits and the colour-inversion quirk are a
//! maintained, hardware-tested crate's problem — not ours. This file first
//! existed as a hand-written driver, which was a mistake: ~300 lines
//! reimplementing `mipidsi::models::ST7789` and
//! `mipidsi::interface::ParallelInterface`, with the two constants most likely
//! to be wrong (MADCTL and inversion) hand-guessed from a datasheet and
//! untestable without hardware. Those are exactly the values a crate that has run
//! on real panels already knows.
//!
//! **What stays ours is [`SioBus`], and only because of an accident of layout.**
//! `LCD_DB0..DB7` are **GPIO32..39**, which are bits 0..7 of the RP2350's *high*
//! SIO bank — so a whole data byte is one register write. `mipidsi`'s own
//! `Generic8BitBus` would drive eight `OutputPin`s individually, which is
//! correct and about eight times the work per byte. `OutputBus` is a one-method
//! trait, so we keep the fast path and inherit everything else.
//!
//! # There is no Tufty 2350 crate
//!
//! Checked, not assumed: `pimoroni-tufty2040` exists and is the **RP2040** board
//! — it depends on `rp2040-hal` and `rp2040-boot2`, so it cannot build here. Its
//! pin map is useful reference and `board.rs` already carries the equivalent.
//!
//! # What is NOT verified
//!
//! **None of this has run on hardware.** Far less of it is now a guess, but the
//! wiring, [`ColorInversion`] and [`Orientation`] still are — see
//! [`Display::new`], where both are named rather than buried.

use embedded_graphics::pixelcolor::Rgb565;
use embedded_hal::digital::OutputPin;

use mipidsi::interface::{InterfaceKind, OutputBus, ParallelInterface};
use mipidsi::models::ST7789;
use mipidsi::options::{ColorInversion, Orientation, Rotation};
use mipidsi::{Builder, NoResetPin};

use rp235x_hal as hal;
use hal::gpio::{DynPinId, FunctionSio, Pin, PullDown, SioOutput};

/// The panel, in landscape.
pub const WIDTH: u16 = 320;
pub const HEIGHT: u16 = 240;

/// A configured output pin, type-erased so the eight data lines fit an array.
pub type OutPin = Pin<DynPinId, FunctionSio<SioOutput>, PullDown>;

/// Bits 0..7 of the HIGH SIO bank — GPIO32..39, the data bus.
const DATA_MASK: u32 = 0xFF;

/// The data bus as ONE register write.
///
/// **Why this exists rather than `Generic8BitBus`:** the Tufty wires DB0..DB7 to
/// GPIO32..39, which land as bits 0..7 of the RP2350's high SIO bank. So a byte
/// is a clear-then-set of one register instead of eight pin writes — and at
/// 153,600 bytes for a full-screen fill, that difference is the whole frame time.
///
/// **SET/CLR rather than writing `gpio_hi_out` wholesale**, which is a
/// correctness point and not a speed one: GPIO40..47 share that register
/// (`VBAT_SENSE`, `POWER_EN`, `LIGHT_SENSE`), so a full-register write would
/// stamp on pins this module does not own. SET/CLR touch only the bits named.
pub struct SioBus {
    /// Held, never read. Owning the pins is what makes the raw register writes
    /// sound: nothing else in the firmware can be driving these pads.
    _pins: [OutPin; 8],
}

impl SioBus {
    pub fn new(pins: [OutPin; 8]) -> Self {
        Self { _pins: pins }
    }
}

impl OutputBus for SioBus {
    type Word = u8;
    const KIND: InterfaceKind = InterfaceKind::Parallel8Bit;
    /// Writing a GPIO register cannot fail.
    type Error = core::convert::Infallible;

    #[inline(always)]
    fn set_value(&mut self, value: u8) -> Result<(), Self::Error> {
        // SAFETY: this struct owns GPIO32..39, and SET/CLR affect only the bits
        // named, so the neighbours in this register are untouched.
        let sio = unsafe { &*hal::pac::SIO::ptr() };
        sio.gpio_hi_out_clr().write(|w| unsafe { w.bits(DATA_MASK) });
        sio.gpio_hi_out_set()
            .write(|w| unsafe { w.bits(value as u32) });
        Ok(())
    }
}

/// The bus plus the two lines `mipidsi` drives per transaction.
pub type Bus = ParallelInterface<SioBus, OutPin, OutPin>;

/// The Tufty's display: a `mipidsi` ST7789 over our own bus.
///
/// `NoResetPin` because the panel's reset is not on a GPIO this firmware owns —
/// `mipidsi` issues a software reset instead, which is what the hand-written
/// version did anyway without saying so.
pub type Panel = mipidsi::Display<Bus, ST7789, NoResetPin>;

/// The panel did not come up.
///
/// Deliberately opaque: `mipidsi::InitError` is generic over the interface and
/// reset-pin error types, and neither can actually occur here — writing a GPIO
/// register is infallible and there is no reset pin. What the firmware needs is
/// the binary fact, which it prints.
#[derive(Debug)]
pub struct DisplayError;

/// Everything the badge needs from a screen.
pub struct Display {
    panel: Panel,
    /// CS and RD are held LOW and HIGH respectively for the whole session and
    /// never toggled: nothing else is on this bus, and RD left floating would be
    /// an input the ST7789 may sample as a read strobe. Kept so they cannot be
    /// reconfigured elsewhere.
    _cs: OutPin,
    _rd: OutPin,
}

impl Display {
    /// Take the pins and bring the panel up.
    ///
    /// **THE TWO SETTINGS MOST LIKELY TO NEED CHANGING ON REAL HARDWARE, named
    /// here rather than buried:**
    ///
    /// - [`Orientation`] — the ST7789 is natively 240×320 portrait and this panel
    ///   is mounted landscape. If the image is mirrored or upside down, change
    ///   the [`Rotation`]; `mipidsi` computes MADCTL from it, so this is a
    ///   one-value edit rather than a bitmask to re-derive.
    /// - [`ColorInversion`] — most ST7789 panels are IPS and need inversion on.
    ///   If every colour comes out as its opposite (the OK green reading as
    ///   magenta), flip this to `ColorInversion::Normal`.
    pub fn new(
        cs: OutPin,
        rs: OutPin,
        wr: OutPin,
        mut rd: OutPin,
        data: [OutPin; 8],
        delay: &mut impl embedded_hal::delay::DelayNs,
    ) -> Result<Self, DisplayError> {
        let mut cs = cs;
        // Assert for the whole session, before any traffic.
        let _ = cs.set_low();
        let _ = rd.set_high();

        let bus = ParallelInterface::new(SioBus::new(data), rs, wr);

        let panel = Builder::new(ST7789, bus)
            .display_size(WIDTH, HEIGHT)
            // Landscape. Try Rotation::Deg270 if the image comes up upside down.
            .orientation(Orientation::new().rotate(Rotation::Deg90))
            .invert_colors(ColorInversion::Inverted)
            .init(delay)
            .map_err(|_| DisplayError)?;

        Ok(Self {
            panel,
            _cs: cs,
            _rd: rd,
        })
    }

    /// Flood the panel with one colour — **the minimal world's entire output**,
    /// and the badge's status light.
    ///
    /// `Status::rgb565` has carried this mapping since before there was a driver
    /// to use it; `clear` is what finally shows it.
    pub fn fill(&mut self, color: u16) {
        use embedded_graphics::pixelcolor::raw::RawU16;
        use embedded_graphics::prelude::*;
        // RGB565 taken as raw bits, because `Status::rgb565` already speaks the
        // panel's native format — converting through a colour type would be a
        // round trip to nowhere.
        let _ = self.panel.clear(Rgb565::from(RawU16::new(color)));
    }

    /// The `embedded-graphics` target, for text.
    ///
    /// Handing out the panel rather than wrapping every drawing operation: the
    /// console lives in `main.rs`, and `DrawTarget` is the seam that makes fonts
    /// somebody else's problem.
    pub fn target(&mut self) -> &mut Panel {
        &mut self.panel
    }
}
