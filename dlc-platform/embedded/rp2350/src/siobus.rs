//! The parallel bus, bit-banged, with strobes that cannot be missed.
//!
//! # Why not PIO, after all that
//!
//! Pimoroni drive this panel from PIO and porting their program was the plan.
//! It cannot be done through `rp235x-hal` 0.3: **PIO reaches GPIO32..47 only
//! through the RP2350's `GPIOBASE` window**, the HAL does not model it, and the
//! type system says so plainly (`Gpio32: ValidFunction<FunctionPio1>` is not
//! implemented). Their C code sets that register directly; doing the same here
//! would mean raw register pokes behind the HAL's back, in the one layer that
//! most needs to be trustworthy.
//!
//! So this keeps the CPU driving the bus and fixes the thing that was ACTUALLY
//! wrong with it.
//!
//! # What was actually wrong
//!
//! Not the pins, and not the init sequence. `mipidsi`'s `ParallelInterface` has
//! a fast path for solid fills: write the byte once, then toggle WR in a tight
//! loop, relying on the data lines holding their value.
//!
//! ```rust,ignore
//! for _ in 1..(count * N) {
//!     self.wr.set_low()?;      // two pin writes, nothing between the edges
//!     self.wr.set_high()?;
//! }
//! ```
//!
//! That is the shortest strobe this code can emit, against a controller wanting
//! roughly 15 ns either side of the edge. A missed strobe is a **dropped pixel**,
//! and dropped pixels produce exactly the two symptoms seen on the badge: rows
//! walking sideways ("one-pixel jumps partway through rectangles") because the
//! stream is now misaligned, and the far end of the framebuffer never written
//! ("top half static") because the stream ran out early.
//!
//! One mechanism, both symptoms — and it explains why short command sequences
//! always worked while large fills degraded.
//!
//! # What this does instead
//!
//! Implements `Interface` directly rather than `OutputBus`, so the fast path is
//! ours to not have. **Every pixel writes its data**, and every strobe carries
//! explicit padding. Slower per byte and immaterial at this size: a full-screen
//! fill is 153,600 bytes, which is milliseconds either way.

use mipidsi::interface::{Interface, InterfaceKind};
use rp235x_hal as hal;

/// GPIO32..39 as bits 0..7 of the HIGH SIO bank — verified on hardware by
/// writing 0xA5 and reading it back off the pads.
const DATA_MASK: u32 = 0xFF;

/// Nothing here can fail: these are register writes and infallible pins.
#[derive(Debug)]
pub struct SioError;

/// The bus: eight data pins on SIO, plus DC and WR.
pub struct SioInterface<DC, WR> {
    dc: DC,
    wr: WR,
}

impl<DC: embedded_hal::digital::OutputPin, WR: embedded_hal::digital::OutputPin>
    SioInterface<DC, WR>
{
    /// The data pins must already be configured as SIO outputs; this drives them
    /// through the SET/CLR registers and never touches their configuration.
    pub fn new(dc: DC, mut wr: WR) -> Self {
        let _ = wr.set_high();
        Self { dc, wr }
    }

    /// One byte: present the data, then strobe.
    ///
    /// **The padding is the point.** `nop`s rather than a timer because the
    /// requirement is tens of nanoseconds and any timer call would cost more
    /// than the delay it is trying to produce. At 150 MHz an instruction is
    /// ~6.7 ns, so four either side clears the ST7789's ~15 ns comfortably.
    #[inline(always)]
    fn write_byte(&mut self, byte: u8) {
        // SAFETY: this type owns GPIO32..39 for the life of the bus, and SET/CLR
        // touch only those bits — the neighbours in this register (GPIO40..47:
        // VBAT_SENSE, POWER_EN, LIGHT_SENSE) are left alone. POWER_EN in
        // particular MUST NOT be disturbed: it powers this very panel.
        let sio = unsafe { &*hal::pac::SIO::ptr() };
        sio.gpio_hi_out_clr().write(|w| unsafe { w.bits(DATA_MASK) });
        sio.gpio_hi_out_set()
            .write(|w| unsafe { w.bits(byte as u32) });

        // Data settles before the strobe falls.
        cortex_m::asm::nop();
        cortex_m::asm::nop();
        let _ = self.wr.set_low();
        cortex_m::asm::nop();
        cortex_m::asm::nop();
        cortex_m::asm::nop();
        cortex_m::asm::nop();
        // The panel latches on THIS edge.
        let _ = self.wr.set_high();
        cortex_m::asm::nop();
        cortex_m::asm::nop();
        cortex_m::asm::nop();
        cortex_m::asm::nop();
    }
}

impl<DC: embedded_hal::digital::OutputPin, WR: embedded_hal::digital::OutputPin> Interface
    for SioInterface<DC, WR>
{
    type Word = u8;
    type Error = SioError;
    const KIND: InterfaceKind = InterfaceKind::Parallel8Bit;

    fn send_command(&mut self, command: u8, args: &[u8]) -> Result<(), Self::Error> {
        let _ = self.dc.set_low();
        self.write_byte(command);
        let _ = self.dc.set_high();
        for arg in args {
            self.write_byte(*arg);
        }
        Ok(())
    }

    fn send_pixels<const N: usize>(
        &mut self,
        pixels: impl IntoIterator<Item = [Self::Word; N]>,
    ) -> Result<(), Self::Error> {
        let _ = self.dc.set_high();
        for pixel in pixels {
            for word in pixel {
                self.write_byte(word);
            }
        }
        Ok(())
    }

    fn send_repeated_pixel<const N: usize>(
        &mut self,
        pixel: [Self::Word; N],
        count: u32,
    ) -> Result<(), Self::Error> {
        // NO FAST PATH. This is the whole reason the file exists: re-driving the
        // data every time costs one register write per byte and removes the
        // dependence on a strobe narrower than the datasheet allows.
        let _ = self.dc.set_high();
        for _ in 0..count {
            for word in pixel {
                self.write_byte(word);
            }
        }
        Ok(())
    }
}
