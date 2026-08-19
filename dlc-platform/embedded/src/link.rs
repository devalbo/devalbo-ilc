//! Where control frames come from and go (BADGE-CONTROL-PLAN D9).
//!
//! # Why this is two methods and not a driver
//!
//! Everything above this — framing, verbs, replies, subscriptions, the
//! heartbeat — is written once and names no peripheral. `control::scan` takes a
//! byte slice and returns a frame, and its tests run on a laptop. What was
//! coupled is the thing that FEEDS it: `usblog.rs` happened to read from a CDC
//! endpoint, and every layer above inherited that by accident rather than by
//! decision.
//!
//! So the seam is the smallest thing that can separate them: something that
//! produces bytes and something that consumes them.
//!
//! # The two properties that make one medium look like another
//!
//! **Never blocks.** This is called from an interrupt and from loops that are
//! waiting for a person to press a button. A link that blocked would stall the
//! world in order to talk about the world.
//!
//! **Partial transfers are normal.** `send` reports how much went and the caller
//! retries the rest. That single rule is what makes a 20-byte BLE characteristic
//! and a 64-byte USB endpoint the same problem — without it, every layer above
//! would need to know the medium's packet size.
//!
//! # Why no error type
//!
//! A link that cannot send right now and a link that is broken produce the same
//! number here: zero. That is not carelessness about failure, it is an
//! observation about the caller — it retries on the next service call either
//! way, and a badge whose host has unplugged is indistinguishable from one whose
//! host is briefly busy, right up until it is not. What DOES distinguish them is
//! time, which the caller already has and this layer does not.
//!
//! # What this deliberately does not have
//!
//! No `attached()`, no `flush()`, no packet size. Each is expressible and none
//! has a caller: the badge's replay-on-open reads CDC's DTR line directly,
//! because "a terminal opened the port" is genuinely a CDC fact and the replay
//! is a property of the TEXT stream rather than of control frames. A trait
//! method with no consumer is a guess about the second implementation, made
//! before there is one (D6).

/// One medium that carries control frames.
pub trait Link {
    /// Take whatever has arrived into `into`, returning how many bytes.
    ///
    /// Zero means nothing is waiting, which is the common case and not an error.
    fn receive(&mut self, into: &mut [u8]) -> usize;

    /// Send what fits, returning how much went.
    ///
    /// A short write is normal. The caller keeps the remainder and offers it
    /// again — see the module note on why that is the rule that unifies media.
    fn send(&mut self, bytes: &[u8]) -> usize;
}

/// A link that hands back whatever was sent into it.
///
/// # What it is for
///
/// Proving that the layers above are actually transport-independent, rather
/// than believing it because they were written to be. Everything the badge does
/// with frames can be exercised here, on a laptop, in CI — where the badge's own
/// `Link` cannot go, because CI has no USB and no board.
///
/// It also stands in for the second medium before there is one: a bug that shows
/// up under a link with a tiny `send` window is a bug BLE would have found, and
/// finding it here is cheaper than finding it on a radio.
#[cfg(feature = "std")]
pub struct Loopback {
    /// Bytes waiting to be received.
    inbound: alloc::collections::VecDeque<u8>,
    /// Everything ever sent, in order.
    pub sent: alloc::vec::Vec<u8>,
    /// The most bytes `send` will accept at once, or 0 for no limit.
    ///
    /// THE POINT OF THE WHOLE TYPE, really: a medium with a small window is what
    /// exposes a caller that assumed its write went whole.
    pub window: usize,
}

#[cfg(feature = "std")]
impl Loopback {
    pub fn new() -> Self {
        Self {
            inbound: alloc::collections::VecDeque::new(),
            sent: alloc::vec::Vec::new(),
            window: 0,
        }
    }

    /// A link that accepts at most `window` bytes per `send`.
    pub fn with_window(window: usize) -> Self {
        Self { window, ..Self::new() }
    }

    /// Queue bytes for the world to receive.
    pub fn deliver(&mut self, bytes: &[u8]) {
        self.inbound.extend(bytes.iter().copied());
    }
}

#[cfg(feature = "std")]
impl Default for Loopback {
    fn default() -> Self {
        Self::new()
    }
}

#[cfg(feature = "std")]
impl Link for Loopback {
    fn receive(&mut self, into: &mut [u8]) -> usize {
        let mut taken = 0;
        while taken < into.len() {
            match self.inbound.pop_front() {
                Some(byte) => {
                    into[taken] = byte;
                    taken += 1;
                }
                None => break,
            }
        }
        taken
    }

    fn send(&mut self, bytes: &[u8]) -> usize {
        let room = if self.window == 0 {
            bytes.len()
        } else {
            bytes.len().min(self.window)
        };
        self.sent.extend_from_slice(&bytes[..room]);
        room
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use alloc::vec::Vec;

    #[test]
    fn a_short_window_makes_a_caller_retry() {
        // The property every layer above depends on, asserted rather than
        // assumed: a link may take less than it was offered, and the sender is
        // responsible for the rest.
        let mut link = Loopback::with_window(4);
        let message = b"the quick brown fox";
        let mut at = 0;
        while at < message.len() {
            at += link.send(&message[at..]);
        }
        assert_eq!(link.sent, message);
    }

    #[test]
    fn receive_takes_what_fits_and_leaves_the_rest() {
        let mut link = Loopback::new();
        link.deliver(b"abcdef");

        let mut small = [0u8; 4];
        assert_eq!(link.receive(&mut small), 4);
        assert_eq!(&small, b"abcd");

        // The remainder is still there — a link does not drop what a caller had
        // no room for, or a frame split across two reads could never be
        // reassembled.
        let mut rest = [0u8; 4];
        assert_eq!(link.receive(&mut rest), 2);
        assert_eq!(&rest[..2], b"ef");

        // And an empty link is zero, not an error.
        assert_eq!(link.receive(&mut rest), 0);
    }

    #[test]
    fn a_frame_survives_a_link_that_delivers_one_byte_at_a_time() {
        // THE BLE CASE, on a laptop. A frame written whole and delivered in
        // fragments must still scan — which is the claim `control::scan`'s
        // `Incomplete` result exists to support, exercised here through the seam
        // rather than by slicing a buffer in a test.
        let framed = crate::control::frame(crate::control::KIND_REQUEST, b"\x08\x01");
        let mut link = Loopback::new();
        link.deliver(&framed);

        let mut buffered: Vec<u8> = Vec::new();
        let mut one = [0u8; 1];
        loop {
            let got = link.receive(&mut one);
            if got == 0 {
                break;
            }
            buffered.extend_from_slice(&one[..got]);
            match crate::control::scan(&buffered) {
                crate::control::Found::Frame(request, consumed) => {
                    assert_eq!(consumed, framed.len());
                    assert_eq!(request.verb, crate::control::VERB_GET_WORLD_STATE);
                    return;
                }
                // Every byte but the last leaves the scanner waiting, which is
                // exactly what it should do.
                crate::control::Found::Incomplete => continue,
                crate::control::Found::Skip(n) => panic!("skipped {n} of a good frame"),
            }
        }
        panic!("a frame delivered one byte at a time never completed");
    }
}
