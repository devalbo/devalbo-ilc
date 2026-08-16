//! Chip select around every transaction, the way Pimoroni does it.
//!
//! # The difference this removes
//!
//! Their driver brackets each transaction:
//!
//! ```cpp
//! gpio_put(dc, 0);   // command mode
//! gpio_put(cs, 0);   // assert
//! write_blocking(&command, 1);
//! if (data) { gpio_put(dc, 1); write_blocking(data, len); }
//! gpio_put(cs, 1);   // release
//! ```
//!
//! Ours held CS low for the whole session, on the reasoning that nothing else is
//! on the bus so there is nothing to select between. That reasoning is about
//! ARBITRATION, and CS on this controller is not only an arbitration signal —
//! the rising edge is what tells it a transaction has ended. A controller that
//! never sees CS rise can sit waiting for the rest of a command that already
//! finished.
//!
//! Whether that is what this panel does, I do not know. What I do know is that
//! it is a difference from a driver that demonstrably works, and I introduced it
//! for a reason ("nothing else is on this bus") that was never about the thing
//! CS actually does here.
//!
//! # Why a wrapper rather than a patch to the bus
//!
//! `mipidsi`'s `ParallelInterface` owns DC and WR but knows nothing about CS —
//! by design, since CS is a board-level concern. Wrapping `Interface` puts the
//! bracketing at exactly the boundary Pimoroni brackets: one assert/release per
//! command, including the pixel writes.

use embedded_hal::digital::OutputPin;
use mipidsi::interface::{Interface, InterfaceKind};

/// Any `Interface`, with CS asserted for the duration of each transaction.
pub struct ChipSelect<I: Interface, CS: OutputPin> {
    inner: I,
    cs: CS,
}

impl<I: Interface, CS: OutputPin> ChipSelect<I, CS> {
    /// Takes CS already configured as an output. Leaves it HIGH (released),
    /// which is the idle state their driver returns to after every transaction.
    pub fn new(inner: I, mut cs: CS) -> Self {
        let _ = cs.set_high();
        Self { inner, cs }
    }
}

impl<I: Interface, CS: OutputPin> Interface for ChipSelect<I, CS> {
    type Word = I::Word;
    type Error = I::Error;
    const KIND: InterfaceKind = I::KIND;

    fn send_command(&mut self, command: u8, args: &[u8]) -> Result<(), Self::Error> {
        let _ = self.cs.set_low();
        let result = self.inner.send_command(command, args);
        // RELEASED EVEN ON FAILURE. Leaving CS asserted after an error would
        // hold the bus against every later transaction, turning one failure into
        // a dead display — which is a far worse symptom than the original fault.
        let _ = self.cs.set_high();
        result
    }

    fn send_pixels<const N: usize>(
        &mut self,
        pixels: impl IntoIterator<Item = [Self::Word; N]>,
    ) -> Result<(), Self::Error> {
        let _ = self.cs.set_low();
        let result = self.inner.send_pixels(pixels);
        let _ = self.cs.set_high();
        result
    }

    fn send_repeated_pixel<const N: usize>(
        &mut self,
        pixel: [Self::Word; N],
        count: u32,
    ) -> Result<(), Self::Error> {
        let _ = self.cs.set_low();
        let result = self.inner.send_repeated_pixel(pixel, count);
        let _ = self.cs.set_high();
        result
    }
}
