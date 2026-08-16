//! The Tufty's ST7789, initialised the way THIS panel needs.
//!
//! # Why a model of our own rather than `mipidsi::models::ST7789`
//!
//! The stock model sends the minimum a generic ST7789 needs:
//!
//! ```text
//! sleep-out -> MADCTL -> invert -> pixel-format -> normal-mode -> display-on
//! ```
//!
//! and relies on the panel's power-on defaults for everything else. **This module
//! does not have usable defaults.** Pimoroni's driver — which demonstrably drives
//! this exact panel, because it is what the badge shipped running — additionally
//! programs the LCD's drive voltages before turning the display on:
//!
//! | command | value | what it sets |
//! | --- | --- | --- |
//! | `0xC3` VRHS | `0x0f` | LCD drive voltage |
//! | `0xC4` VDVS | `0x20` | ditto |
//! | `0xBB` VCOMS | `0x1b` | common voltage |
//! | `0xD0` PWCTRL1 | `0xa4,0xa1` | power control |
//! | `0xB0` RAMCTRL | `0x00,0xc0` | RAM / interface mode |
//! | `0xB2 B7 C0 C2 C6` | | porch, gate, VDV enable, frame rate |
//!
//! **A controller with no drive voltage accepts every command and shows
//! nothing**, which is exactly the failure this replaces: `init` returned `Ok`,
//! the backlight lit, and the panel stayed black. Nothing in the software could
//! detect it, because this bus is write-only — we never read the panel back, so
//! "success" only ever meant "no pin errored".
//!
//! Source: `modules/c/st7789/st7789.cpp` in <https://github.com/pimoroni/tufty2350>.
//!
//! # What is deliberately NOT copied
//!
//! **Gamma (`0xE0`/`0xE1`, 14 bytes each).** Gamma shapes colour response; it
//! does not decide whether anything appears. Left out until the panel is known to
//! light at all, so that a wrong gamma table cannot be confused with a dead
//! display. If colours come out flat or banded, this is the first thing to add.
//!
//! **MADCTL is `mipidsi`'s, not Pimoroni's `0x90`.** Letting the crate compute it
//! from `Orientation` keeps rotation a one-value change rather than a bitmask to
//! re-derive.

use embedded_graphics::pixelcolor::Rgb565;
use rp235x_hal as hal;
use hal::gpio::{DynPinId, FunctionSio, Pin, PullDown, SioOutput};
use mipidsi::interface::OutputBus;
use embedded_hal::delay::DelayNs;

use mipidsi::dcs::{
    BitsPerPixel, ExitSleepMode, InterfaceExt, PixelFormat, SetAddressMode, SetDisplayOn,
    SetInvertMode, SetPixelFormat,
};
use mipidsi::interface::{Interface, InterfaceKind};
use mipidsi::models::{Model, ModelInitError};
use mipidsi::options::ModelOptions;
use mipidsi::ConfigurationError;

/// An ST7789 with the Tufty 2350's power-on sequence.
pub struct TuftyST7789;

impl Model for TuftyST7789 {
    type ColorFormat = Rgb565;
    /// The controller's NATIVE framebuffer — portrait. `Builder::init` validates
    /// `display_size` against this, and passing the rotated 320x240 is what made
    /// the panel fail to initialise at all.
    const FRAMEBUFFER_SIZE: (u16, u16) = (240, 320);

    fn init<DELAY, DI>(
        &mut self,
        di: &mut DI,
        delay: &mut DELAY,
        options: &ModelOptions,
    ) -> Result<SetAddressMode, ModelInitError<DI::Error>>
    where
        DELAY: DelayNs,
        DI: Interface,
    {
        if !matches!(DI::KIND, InterfaceKind::Parallel8Bit | InterfaceKind::Serial4Line) {
            return Err(ModelInitError::InvalidConfiguration(
                ConfigurationError::UnsupportedInterface,
            ));
        }

        // The builder has already issued a software reset (there is no reset pin
        // on this board — checked against Pimoroni's pins.csv, which lists only
        // BACKLIGHT/CS/RS/WR/RD/DB0-7 for the display). It needs time to settle.
        delay.delay_us(150_000);

        // PIMORONI'S SEQUENCE, VERBATIM, in their order. Transcribed from
        // `common_init()` in modules/c/st7789/st7789.cpp — the driver that
        // demonstrably lights this panel, because it is what the badge shipped
        // running. Deviating "sensibly" from a known-good sequence is how you end
        // up unable to tell a wrong value from a wrong assumption.
        di.write_raw(0x3A, &[0x05])?; // COLMOD   16bpp
        di.write_raw(0xB2, &[0x0c, 0x0c, 0x00, 0x33, 0x33])?; // PORCTRL
        di.write_raw(0xC0, &[0x2c])?; // LCMCTRL
        di.write_raw(0xC2, &[0x01])?; // VDVVRHEN
        di.write_raw(0xC3, &[0x0f])?; // VRHS
        di.write_raw(0xC4, &[0x20])?; // VDVS
        di.write_raw(0xD0, &[0xa4, 0xa1])?; // PWCTRL1 — before FRCTRL2, as they have it
        di.write_raw(0xC6, &[0x0f])?; // FRCTRL2
        di.write_raw(0xB0, &[0x00, 0xc0])?; // RAMCTRL
        di.write_raw(0xB7, &[0x35])?; // GCTRL
        di.write_raw(0xBB, &[0x1b])?; // VCOMS

        // GAMMA. Skipped on the reasoning that it shapes colour rather than
        // deciding whether anything appears — which may well be true, and is
        // exactly the kind of reasoning that should not be applied while the
        // panel is still dark. Matching first, trimming later.
        di.write_raw(
            0xE0,
            &[
                0xF0, 0x00, 0x06, 0x04, 0x05, 0x05, 0x31, 0x44, 0x48, 0x36, 0x12, 0x12, 0x2B,
                0x34,
            ],
        )?; // GMCTRP1
        di.write_raw(
            0xE1,
            &[
                0xF0, 0x0B, 0x0F, 0x0F, 0x0D, 0x26, 0x31, 0x43, 0x47, 0x38, 0x14, 0x14, 0x2C,
                0x32,
            ],
        )?; // GMCTRN1

        di.write_command(SetInvertMode::new(options.invert_colors))?; // INVON
        di.write_command(ExitSleepMode)?; // SLPOUT
        delay.delay_us(100_000);

        // MADCTL is NOT in their common_init — they set it from the rotation
        // step. It has to happen somewhere, and `mipidsi` needs the value
        // returned, so it goes here, after sleep-out and before the display is
        // switched on.
        let madctl = SetAddressMode::from(options);
        di.write_command(madctl)?;

        di.write_raw(0x35, &[0x00])?; // TEON
        di.write_raw(0x44, &[0x00, 0x00])?; // STE

        di.write_command(SetDisplayOn)?;
        // DISPON needs settling time before pixels are pushed.
        delay.delay_us(120_000);

        Ok(madctl)
    }
}


/// A configured output pin, type-erased so eight fit an array.
pub type OutPin = Pin<DynPinId, FunctionSio<SioOutput>, PullDown>;

const DATA_MASK: u32 = 0xFF;

/// The data bus as ONE register write — DB0..DB7 are GPIO32..39, which are bits
/// 0..7 of the RP2350's high SIO bank. Verified on hardware: writing 0xA5 and
/// reading the pad register back returns 0xA5.
pub struct SioBus {
    _pins: [OutPin; 8],
}

impl SioBus {
    pub fn new(pins: [OutPin; 8]) -> Self {
        Self { _pins: pins }
    }
}

impl OutputBus for SioBus {
    type Word = u8;
    const KIND: mipidsi::interface::InterfaceKind = mipidsi::interface::InterfaceKind::Parallel8Bit;
    type Error = core::convert::Infallible;

    #[inline(always)]
    fn set_value(&mut self, value: u8) -> Result<(), Self::Error> {
        // SAFETY: this struct owns GPIO32..39; SET/CLR touch only those bits.
        let sio = unsafe { &*hal::pac::SIO::ptr() };
        sio.gpio_hi_out_clr().write(|w| unsafe { w.bits(DATA_MASK) });
        sio.gpio_hi_out_set().write(|w| unsafe { w.bits(value as u32) });
        Ok(())
    }
}


/// The panel AS DRAWN, after the Deg90 rotation — what layout code measures
/// against. The controller's own framebuffer is 240x320 portrait; these are the
/// rotated dimensions and the two must not be confused (passing the rotated pair
/// to `display_size` is what stopped the panel initialising at all).
pub const WIDTH_LANDSCAPE: u16 = 320;
pub const HEIGHT_LANDSCAPE: u16 = 240;
