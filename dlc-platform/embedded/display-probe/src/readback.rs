//! Ask the panel a question, and see whether anything answers.
//!
//! # Why this is the measurement that matters
//!
//! Every test so far has been WRITE-ONLY. We drove pins, sent Pimoroni's exact
//! init sequence, verified the pads toggle — and none of it could distinguish "the
//! controller received that" from "those bytes went into a wire connected to
//! nothing". `init` returning `Ok` never meant the panel agreed; it meant no pin
//! returned an error, and our pins are infallible.
//!
//! The ST7789 answers reads. `RDDID` (0x04) returns a dummy byte followed by
//! three ID bytes. If plausible values come back, the bus works in both
//! directions, the controller is powered, and it is processing commands — which
//! would move the fault to rendering. If `0x00` or `0xff` comes back, nothing is
//! listening, and every init sequence tried so far was written to a void.
//!
//! # Doing it by hand
//!
//! `mipidsi` has no read path, and this is deliberately raw: the point is to
//! depend on as little of our own stack as possible. Pins are driven straight
//! through SIO, including flipping the data bus to INPUT via the output-enable
//! registers — which is also the only place in this firmware that direction is
//! changed at runtime.
//!
//! Timing follows the 8080-II read cycle: RD low, wait, sample, RD high, wait.
//! Reads are much slower than writes on this controller (~150 ns access time),
//! so the delays here are generous rather than tuned.

use rp235x_hal as hal;

/// Bit mask for GPIO32..39 within the HIGH SIO bank.
const DATA_MASK: u32 = 0xFF;

/// What came back from the panel.
pub struct Ids {
    /// The bytes as read, dummy byte first.
    pub bytes: [u8; 4],
}

impl Ids {
    /// Does this look like a controller answering, rather than a floating bus?
    ///
    /// All-zero and all-ones are what an unconnected or unpowered bus reads as,
    /// and neither is a valid ID.
    pub fn plausible(&self) -> bool {
        let answer = &self.bytes[1..];
        !answer.iter().all(|b| *b == 0x00) && !answer.iter().all(|b| *b == 0xff)
    }
}

/// Send a command byte and read `n` bytes back.
///
/// # Safety
///
/// The caller must own GPIO27/28/30/31 and GPIO32..39 and have configured them
/// as SIO outputs. This drives them directly and flips the data bus to input and
/// back, so nothing else may touch those pads for the duration.
pub unsafe fn read_command(command: u8, cs: u8, rs: u8, wr: u8, rd: u8) -> Ids {
    let sio = unsafe { &*hal::pac::SIO::ptr() };
    let bit = |p: u8| 1u32 << p;

    let set = |mask: u32| sio.gpio_out_set().write(|w| unsafe { w.bits(mask) });
    let clr = |mask: u32| sio.gpio_out_clr().write(|w| unsafe { w.bits(mask) });

    // Idle: CS released, RD and WR high, data bus driving.
    set(bit(cs) | bit(rd) | bit(wr));
    sio.gpio_hi_oe_set().write(|w| unsafe { w.bits(DATA_MASK) });
    cortex_m::asm::delay(1000);

    // --- command phase: CS low, DC low, one write strobe -------------------
    clr(bit(cs));
    clr(bit(rs));
    sio.gpio_hi_out_clr().write(|w| unsafe { w.bits(DATA_MASK) });
    sio.gpio_hi_out_set()
        .write(|w| unsafe { w.bits(command as u32) });
    cortex_m::asm::delay(100);
    clr(bit(wr));
    cortex_m::asm::delay(100);
    set(bit(wr)); // latches on this edge
    cortex_m::asm::delay(100);

    // --- read phase: DC high, bus turned around ----------------------------
    set(bit(rs));
    // RELEASE THE BUS. Both the panel and the RP2350 driving it at once is a
    // short, so output-enable must drop before RD is asserted.
    sio.gpio_hi_oe_clr().write(|w| unsafe { w.bits(DATA_MASK) });
    cortex_m::asm::delay(1000);

    let mut bytes = [0u8; 4];
    for byte in bytes.iter_mut() {
        clr(bit(rd));
        // The controller needs ~150 ns to present data; this is far longer.
        cortex_m::asm::delay(2000);
        *byte = (sio.gpio_hi_in().read().bits() & DATA_MASK) as u8;
        set(bit(rd));
        cortex_m::asm::delay(2000);
    }

    // Put the bus back the way it was, so the caller's driver still works.
    sio.gpio_hi_oe_set().write(|w| unsafe { w.bits(DATA_MASK) });
    set(bit(cs));
    cortex_m::asm::delay(1000);

    Ids { bytes }
}
