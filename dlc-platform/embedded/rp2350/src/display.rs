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

use mipidsi::models::ST7789;
use mipidsi::options::{ColorInversion, Orientation, Rotation};
use mipidsi::{Builder, NoResetPin};

use rp235x_hal as hal;
use hal::gpio::{DynPinId, FunctionSio, Pin, PullDown, SioOutput};

/// The panel as DRAWN — landscape, after the rotation below. This is what the
/// report and console lay out against.
pub const WIDTH: u16 = 320;
pub const HEIGHT: u16 = 240;

/// The panel as the CONTROLLER sees it. The ST7789 is natively 240x320 portrait,
/// and `mipidsi` validates `display_size` against exactly this — see
/// `Display::new`, where getting it backwards stopped the panel initialising.
const NATIVE_WIDTH: u16 = 240;
const NATIVE_HEIGHT: u16 = 320;

/// A configured output pin, type-erased so the eight data lines fit an array.
pub type OutPin = Pin<DynPinId, FunctionSio<SioOutput>, PullDown>;

/// The eight data pins, owned so nothing reconfigures them under the bus.
///
/// `SioBus` — an `OutputBus` impl that sat under `mipidsi::ParallelInterface` —
/// lived here until the panel came up. It was replaced wholesale by
/// `siobus::SioInterface`, which implements `Interface` directly so that
/// mipidsi's solid-fill fast path (a WR strobe with no data write between the
/// edges) cannot be reached. That fast path was dropping pixels on real
/// hardware; see `siobus.rs`.
/// The bus: our own `Interface` (padded strobes, no solid-fill fast path),
/// wrapped so CS is asserted per transaction.
///
/// NOT `mipidsi::ParallelInterface`. Its solid-fill fast path strobes WR without
/// re-driving the data, which is the narrowest pulse the CPU can emit — measured
/// on hardware as dropped pixels: rows walked sideways and the far end of the
/// framebuffer was never written. See `siobus.rs`.
pub type Bus = crate::cs::ChipSelect<crate::siobus::SioInterface<OutPin, OutPin>, OutPin>;

/// The Tufty's display: a `mipidsi` ST7789 over our own bus.
///
/// `NoResetPin` because the panel's reset is not on a GPIO this firmware owns —
/// `mipidsi` issues a software reset instead, which is what the hand-written
/// version did anyway without saying so.
pub type Panel = mipidsi::Display<Bus, ST7789, NoResetPin>;

/// The panel did not come up, and WHY.
///
/// It used to be a unit struct, on the reasoning that the error types involved
/// are all infallible so only the binary fact could matter. That was wrong twice
/// over: `init` also rejects a bad CONFIGURATION before touching any pin, and
/// discarding the reason cost a hardware session. The badge showed a blinking
/// backlight and no text, which looks exactly like a dead panel, while the real
/// answer — `InvalidDisplaySize` — was in a value thrown away by
/// `.map_err(|_| DisplayError)`.
///
/// A `&'static str` rather than the generic `InitError`, because the message has
/// to survive into a 40-column report and a UART line, and every variant this
/// can actually take is known at compile time.
#[derive(Debug)]
pub struct DisplayError(pub &'static str);

/// Everything the badge needs from a screen.
pub struct Display {
    panel: Panel,
    /// CS and RD are held LOW and HIGH respectively for the whole session and
    /// never toggled: nothing else is on this bus, and RD left floating would be
    /// an input the ST7789 may sample as a read strobe. Kept so they cannot be
    /// reconfigured elsewhere.
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
        // CS is NOT held low for the session: the wrapper asserts it per
        // transaction, matching Pimoroni. RD stays high — this driver never
        // reads, and a floating RD is an input the controller may sample.
        let _ = rd.set_high();

        // CS is bracketed per transaction by the wrapper; the data pins stay
        // owned here so nothing reconfigures them underneath the bus.
        let _data = data;
        let bus = crate::cs::ChipSelect::new(crate::siobus::SioInterface::new(rs, wr), cs);

        let panel = Builder::new(ST7789, bus)
            // NATIVE framebuffer, not the rotated presentation — `init` compares
            // this against MODEL::FRAMEBUFFER_SIZE, which for ST7789 is
            // (240, 320). Passing the landscape 320x240 made width exceed the
            // maximum and failed with InvalidDisplaySize on every boot, so the
            // panel never initialised at all. The `orientation` below is what
            // makes it landscape; WIDTH/HEIGHT stay the rotated dimensions
            // because that is what the drawing code lays out against.
            .display_size(NATIVE_WIDTH, NATIVE_HEIGHT)
            // Deg270, not Deg90 — landscape the OTHER way up. Confirmed on
            // hardware: Deg90 put the image upside down relative to the buttons.
            // The comment above this line used to say "try Deg270 if it comes up
            // upside down"; it did, so this is that.
            .orientation(Orientation::new().rotate(Rotation::Deg270))
            .invert_colors(ColorInversion::Inverted)
            .init(delay)
            .map_err(|e| {
                // NAME THE FAILURE. Which of these it is decides where to look,
                // and they point at completely different things.
                DisplayError(match e {
                    mipidsi::InitError::InvalidConfiguration(_) => "bad config (size/offset)",
                    mipidsi::InitError::ResetPin(_) => "reset pin",
                    mipidsi::InitError::Interface(_) => "bus/interface",
                })
            })?;

        Ok(Self { panel, _rd: rd })
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
