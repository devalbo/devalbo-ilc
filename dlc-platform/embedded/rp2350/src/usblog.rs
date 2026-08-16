//! The bring-up log, over the USB-C cable that is already plugged in.
//!
//! # Why this exists
//!
//! The badge had two output channels and both failed us. The screen was the
//! thing under test, so it could not report on itself; the UART needs a 3.3 V
//! adapter clipped to two crocodile pads, which not everyone has. That left the
//! backlight — one bit — and four flash cycles were spent guessing because a
//! guess was cheaper than a measurement.
//!
//! Pimoroni's own firmware exposes a USB CDC serial port on this same cable. So
//! can we, and `usb-device` + `usbd-serial` do the protocol — this file is
//! plumbing, not a USB stack.
//!
//! # IT IS DEFAULT BEHAVIOUR, not a debug build
//!
//! No `cfg`, no feature flag: every badge build exposes this, including the
//! `BADGE_BEAT_MS=0` one that ships. It costs ~17 KB against a firmware whose
//! Wasmtime is a megabyte, and a badge that cannot say what it did is a badge
//! that gets diagnosed by reflashing — which is how four cycles went on guesses
//! that a single line of output would have settled.
//!
//! The rule this earns: **a device with no console is a device you debug by
//! rebuilding it.** Give it one before it needs one.
//!
//! # The shape, and why it is not "just print"
//!
//! USB needs polling to enumerate, and the bring-up is a straight-line sequence
//! that must not stall waiting for a host that may never attach. So the log is
//! **buffered while the badge runs** and **served afterwards**, from the same
//! idle loop the firmware already ends in. Nothing blocks, no output is lost to
//! a late-connecting host, and a run that ends in a hang still yields whatever
//! was written before it — which is exactly the case worth having.
//!
//! **KNOWN GAP, and it is the important one.** Serving happens in the idle loop,
//! so a run that HANGS never serves at all — and a hang is precisely when the
//! log matters most. Closing it means polling USB during the run (a static
//! device and a hook in the report's pause), so a host sees output live and a
//! stall still surrenders everything up to it. Not done yet; written down so the
//! limitation is a known one rather than a surprise.

use core::fmt::Write;

/// Everything printed during a run.
///
/// 8 KB: the report is a few hundred bytes, and the slack is for probes added
/// while chasing something. A full buffer TRUNCATES rather than wrapping — the
/// beginning of a bring-up log is the part that matters, and a wrap would eat it
/// to keep output nobody asked for.
pub struct LogBuffer {
    bytes: [u8; 8192],
    len: usize,
    truncated: bool,
}

impl LogBuffer {
    pub const fn new() -> Self {
        Self {
            bytes: [0; 8192],
            len: 0,
            truncated: false,
        }
    }

    pub fn as_bytes(&self) -> &[u8] {
        &self.bytes[..self.len]
    }

    pub fn truncated(&self) -> bool {
        self.truncated
    }
}

impl Write for LogBuffer {
    fn write_str(&mut self, s: &str) -> core::fmt::Result {
        for byte in s.bytes() {
            if self.len == self.bytes.len() {
                self.truncated = true;
                break;
            }
            self.bytes[self.len] = byte;
            self.len += 1;
        }
        Ok(())
    }
}

/// Write to two sinks at once.
///
/// The UART keeps working for anyone who has an adapter, and it is the only
/// channel alive before USB enumerates. Sending to both costs nothing and means
/// the two never disagree about what happened.
pub struct Tee<'a, A: Write, B: Write>(pub &'a mut A, pub &'a mut B);

impl<A: Write, B: Write> Write for Tee<'_, A, B> {
    fn write_str(&mut self, s: &str) -> core::fmt::Result {
        // Both, unconditionally: a failure on one must not silence the other.
        let first = self.0.write_str(s);
        let second = self.1.write_str(s);
        first.and(second)
    }
}
