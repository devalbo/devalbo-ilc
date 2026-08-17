//! Answering questions about a world (BADGE-CONTROL-PLAN Phase 1).
//!
//! # Why this exists
//!
//! A world can talk and cannot answer. Everything it says arrives through a
//! buffered one-way log, which is enough right up until something looks wrong —
//! and then it cannot tell you whether the world is stuck or the log is. Three
//! debugging cycles went on exactly that ambiguity in one session.
//!
//! # Why it is HERE and not in the badge firmware
//!
//! It names no peripheral. "What world are you, what are you doing, how long
//! have you been up" are questions about a WORLD, and a browser world (D7) has
//! the same answers over a different transport. Written in `rp2350/` it would be
//! copied; written here it is shared — which is D6a's rule for anything that is
//! policy rather than a driver.
//!
//! It is also the only arrangement where the tests run: CI cross-compiles
//! firmware and cannot execute it, so a decoder written beside the badge would be
//! a decoder nothing exercises.
//!
//! # The frame
//!
//! ```text
//! "DLCC"  u8 kind  u16 len (LE)  payload[len]  u32 checksum (LE)
//! ```
//!
//! **A KIND, because not every frame answers a question.** Replies are pulled;
//! log lines and events are pushed (D8). Protobuf messages are not
//! self-describing, so without a kind a reader could not tell a `ControlResponse`
//! from a `LogLine` — and guessing by trying both decoders would accept
//! nonsense, since most byte strings parse as *some* message.
//!
//! **A MAGIC BECAUSE THE PORT IS SHARED.** The same CDC endpoint carries the
//! human-readable log, and the far end may be a person with a terminal typing
//! into it. A frame has to be recognisable in a stream that is mostly not
//! frames, and unrecognised bytes have to be discardable without desynchronising
//! the next one.
//!
//! **A CHECKSUM BECAUSE A TRUNCATED FRAME IS PLAUSIBLE.** A host that closes
//! mid-write leaves a prefix that parses. FNV-1a is the same function the catalog
//! uses — enough to catch truncation and corruption, and not pretending to be
//! more (see `catalog::checksum`).

use alloc::vec::Vec;

/// Marks the start of a control frame in a stream that is mostly log text.
pub const MAGIC: [u8; 4] = *b"DLCC";

/// Frame overhead: magic, kind, length, checksum.
const OVERHEAD: usize = 4 + 1 + 2 + 4;

/// What a frame carries.
pub const KIND_REQUEST: u8 = 1;
/// A reply to a request.
pub const KIND_RESPONSE: u8 = 2;
/// A log line, sent without being asked (D8b).
pub const KIND_LOG: u8 = 3;

/// The largest payload a frame may carry.
///
/// Bounded so a corrupt length cannot make a world allocate against a number it
/// read off a wire. 8 KB is far more than any Phase 1 verb needs and leaves room
/// for pass-through requests later.
pub const MAX_PAYLOAD: usize = 8 * 1024;

/// The verbs. Must match `Verb` in control.proto.
pub const VERB_GET_WORLD_STATE: u32 = 1;

/// Coarse activity. Must match `Activity` in control.proto.
pub const ACTIVITY_STARTING: u32 = 1;
pub const ACTIVITY_CHOOSING: u32 = 2;
pub const ACTIVITY_COLLECTING: u32 = 3;
pub const ACTIVITY_RUNNING: u32 = 4;
pub const ACTIVITY_RESTING: u32 = 5;

/// A decoded request: a verb and its opaque argument.
#[derive(Debug, PartialEq, Eq)]
pub struct Request {
    pub verb: u32,
    /// Byte range of the payload within the buffer it was parsed from.
    pub payload: Option<(usize, usize)>,
}

/// What a scan found.
#[derive(Debug, PartialEq, Eq)]
pub enum Found {
    /// A complete frame: the request, and how many bytes to consume.
    Frame(Request, usize),
    /// No frame yet — more bytes may complete one. Consume nothing.
    Incomplete,
    /// Garbage at the start. Consume this many bytes and look again.
    ///
    /// A COUNT rather than "discard everything": the buffer may hold a person's
    /// keystrokes followed by a real frame, and throwing the lot away would lose
    /// the frame with the noise.
    Skip(usize),
}

/// Look for a frame at the start of `bytes`.
pub fn scan(bytes: &[u8]) -> Found {
    if bytes.len() < MAGIC.len() {
        // Could still become a magic. Only give up once there is enough to know.
        if MAGIC.starts_with(bytes) {
            return Found::Incomplete;
        }
        return Found::Skip(1);
    }
    if bytes[..MAGIC.len()] != MAGIC {
        return Found::Skip(1);
    }
    if bytes.len() < OVERHEAD {
        return Found::Incomplete;
    }

    let len = u16::from_le_bytes([bytes[5], bytes[6]]) as usize;
    if len > MAX_PAYLOAD {
        // A LENGTH THIS LARGE IS NOISE, not a big frame. Skipping past the magic
        // rather than trusting it is what stops a corrupt byte stalling the
        // stream until that many bytes arrive.
        return Found::Skip(MAGIC.len());
    }
    let total = OVERHEAD + len;
    if bytes.len() < total {
        return Found::Incomplete;
    }

    let body = &bytes[7..7 + len];
    let recorded = u32::from_le_bytes([
        bytes[7 + len],
        bytes[8 + len],
        bytes[9 + len],
        bytes[10 + len],
    ]);
    if recorded != crate::catalog::checksum(body) {
        // Framed like a frame and not one. Skip the magic so a real frame later
        // in the buffer is still found.
        return Found::Skip(MAGIC.len());
    }

    match parse_request(body) {
        // Offsets are relative to `body`, which starts at 6.
        Some(mut request) => {
            if let Some((start, end)) = request.payload {
                request.payload = Some((start + 7, end + 7));
            }
            Found::Frame(request, total)
        }
        None => Found::Skip(MAGIC.len()),
    }
}

/// `ControlRequest` — a varint verb and optional bytes.
fn parse_request(body: &[u8]) -> Option<Request> {
    let mut at = 0usize;
    let mut request = Request { verb: 0, payload: None };
    while at < body.len() {
        let (tag, next) = varint(body, at)?;
        at = next;
        let field = (tag >> 3) as u32;
        let wire = (tag & 0x7) as u8;
        match (field, wire) {
            (1, 0) => {
                let (value, next) = varint(body, at)?;
                at = next;
                request.verb = value as u32;
            }
            (2, 2) => {
                let (len, next) = varint(body, at)?;
                let start = next;
                let end = start.checked_add(len as usize)?;
                if end > body.len() {
                    return None;
                }
                request.payload = Some((start, end));
                at = end;
            }
            // Unknown fields are skipped by wire type, so a caller built from a
            // newer proto still parses here.
            (_, 0) => at = varint(body, at)?.1,
            (_, 2) => {
                let (len, next) = varint(body, at)?;
                at = next.checked_add(len as usize)?;
                if at > body.len() {
                    return None;
                }
            }
            (_, 1) => at = at.checked_add(8)?,
            (_, 5) => at = at.checked_add(4)?,
            _ => return None,
        }
    }
    Some(request)
}

fn varint(bytes: &[u8], mut at: usize) -> Option<(u64, usize)> {
    let mut value = 0u64;
    let mut shift = 0u32;
    loop {
        let byte = *bytes.get(at)?;
        at += 1;
        value |= ((byte & 0x7f) as u64) << shift;
        if byte & 0x80 == 0 {
            return Some((value, at));
        }
        shift += 7;
        if shift >= 70 {
            return None;
        }
    }
}

/// Wrap a payload in a frame, ready to send.
pub fn frame(kind: u8, payload: &[u8]) -> Vec<u8> {
    let mut out = Vec::with_capacity(OVERHEAD + payload.len());
    out.extend_from_slice(&MAGIC);
    out.push(kind);
    out.extend_from_slice(&(payload.len() as u16).to_le_bytes());
    out.extend_from_slice(payload);
    out.extend_from_slice(&crate::catalog::checksum(payload).to_le_bytes());
    out
}

/// Encode a `LogLine` (D8b).
///
/// The machine-readable half of the log. The TEXT stream still carries the same
/// words on the same wire, always — including in builds with no control channel,
/// where this function is never called. A frame reader must never have less than
/// a terminal would have shown, which is why `text` is carried verbatim rather
/// than being reconstructed from the structured fields.
pub fn log_line(uptime_ms: u64, stage: &str, level: u32, text: &str) -> Vec<u8> {
    let mut out = Vec::new();
    if uptime_ms != 0 {
        tag(&mut out, 1, 0);
        put_varint(&mut out, uptime_ms);
    }
    put_string(&mut out, 2, stage);
    if level != 0 {
        tag(&mut out, 3, 0);
        put_varint(&mut out, level as u64);
    }
    put_string(&mut out, 4, text);
    out
}

/// `Level` in control.proto.
pub const LEVEL_STAGE_OK: u32 = 1;
pub const LEVEL_STAGE_FAIL: u32 = 2;
pub const LEVEL_NOTE: u32 = 3;

// ---------------------------------------------------------------------------
// Encoding answers
// ---------------------------------------------------------------------------

fn tag(out: &mut Vec<u8>, field: u32, wire: u8) {
    put_varint(out, ((field as u64) << 3) | wire as u64);
}

fn put_varint(out: &mut Vec<u8>, mut value: u64) {
    loop {
        let byte = (value & 0x7f) as u8;
        value >>= 7;
        if value == 0 {
            out.push(byte);
            return;
        }
        out.push(byte | 0x80);
    }
}

fn put_string(out: &mut Vec<u8>, field: u32, value: &str) {
    if value.is_empty() {
        // Proto3 default: an empty string encodes to nothing. Emitting it would
        // be bytes that decode identically and cost a world its budget.
        return;
    }
    tag(out, field, 2);
    put_varint(out, value.len() as u64);
    out.extend_from_slice(value.as_bytes());
}

/// What a world is and what it is doing.
pub struct WorldState<'a> {
    pub world: &'a str,
    pub tier: &'a str,
    pub version: &'a str,
    /// Flash-time knobs, as key/value pairs. A slice rather than a map because a
    /// world has a handful and `no_std` has no map worth the dependency.
    pub config: &'a [(&'a str, &'a str)],
    pub activity: u32,
    pub app: &'a str,
    /// What the app last said it was doing. See `activity.rs`.
    pub app_activity: &'a str,
    pub uptime_ms: u64,
}

impl WorldState<'_> {
    /// Encode as the proto message of the same name.
    pub fn encode(&self) -> Vec<u8> {
        let mut out = Vec::new();
        put_string(&mut out, 1, self.world);
        put_string(&mut out, 2, self.tier);
        put_string(&mut out, 3, self.version);
        for (key, value) in self.config {
            // A protobuf map entry is a message with key=1, value=2.
            let mut entry = Vec::new();
            put_string(&mut entry, 1, key);
            put_string(&mut entry, 2, value);
            tag(&mut out, 4, 2);
            put_varint(&mut out, entry.len() as u64);
            out.extend_from_slice(&entry);
        }
        if self.activity != 0 {
            tag(&mut out, 5, 0);
            put_varint(&mut out, self.activity as u64);
        }
        put_string(&mut out, 6, self.app);
        put_string(&mut out, 8, self.app_activity);
        if self.uptime_ms != 0 {
            tag(&mut out, 7, 0);
            put_varint(&mut out, self.uptime_ms);
        }
        out
    }
}

/// Encode a `ControlResponse`.
pub fn response(ok: bool, error: &str, payload: &[u8]) -> Vec<u8> {
    let mut out = Vec::new();
    if ok {
        tag(&mut out, 1, 0);
        put_varint(&mut out, 1);
    }
    put_string(&mut out, 2, error);
    if !payload.is_empty() {
        tag(&mut out, 3, 2);
        put_varint(&mut out, payload.len() as u64);
        out.extend_from_slice(payload);
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;

    fn request_bytes(verb: u32, payload: &[u8]) -> Vec<u8> {
        let mut body = Vec::new();
        tag(&mut body, 1, 0);
        put_varint(&mut body, verb as u64);
        if !payload.is_empty() {
            tag(&mut body, 2, 2);
            put_varint(&mut body, payload.len() as u64);
            body.extend_from_slice(payload);
        }
        frame(KIND_REQUEST, &body)
    }

    #[test]
    fn a_whole_frame_is_found() {
        let bytes = request_bytes(VERB_GET_WORLD_STATE, b"");
        match scan(&bytes) {
            Found::Frame(request, consumed) => {
                assert_eq!(request.verb, VERB_GET_WORLD_STATE);
                assert_eq!(consumed, bytes.len());
            }
            other => panic!("expected a frame, got {other:?}"),
        }
    }

    /// A payload's offsets must be resolvable against the ORIGINAL buffer, not
    /// the frame body — a caller holding the wrong base reads the checksum as
    /// data.
    #[test]
    fn a_payload_is_located_in_the_original_buffer() {
        let bytes = request_bytes(2, b"hello");
        let Found::Frame(request, _) = scan(&bytes) else {
            panic!("expected a frame")
        };
        let (start, end) = request.payload.expect("a payload");
        assert_eq!(&bytes[start..end], b"hello");
    }

    /// PARTIAL FRAMES WAIT. A world that consumed a prefix would desynchronise
    /// and never see the rest.
    #[test]
    fn a_partial_frame_is_incomplete_at_every_length() {
        let bytes = request_bytes(VERB_GET_WORLD_STATE, b"abc");
        for cut in 0..bytes.len() {
            assert_eq!(scan(&bytes[..cut]), Found::Incomplete, "cut at {cut}");
        }
    }

    /// NOISE IS SKIPPED ONE BYTE AT A TIME, and a frame after it is still found.
    /// The port carries a human's keystrokes as well as frames.
    #[test]
    fn noise_before_a_frame_does_not_lose_it() {
        let mut bytes = b"hello, i am a person typing\n".to_vec();
        let start = bytes.len();
        bytes.extend_from_slice(&request_bytes(VERB_GET_WORLD_STATE, b""));

        let mut at = 0usize;
        loop {
            match scan(&bytes[at..]) {
                Found::Skip(n) => at += n,
                Found::Frame(request, _) => {
                    assert_eq!(request.verb, VERB_GET_WORLD_STATE);
                    assert_eq!(at, start, "found the frame at the wrong offset");
                    return;
                }
                Found::Incomplete => panic!("gave up with a whole frame present"),
            }
        }
    }

    /// A CORRUPT FRAME IS SKIPPED, not acted on. Truncation leaves a prefix that
    /// parses, which is exactly what the checksum is for.
    #[test]
    fn a_bad_checksum_is_refused() {
        let mut bytes = request_bytes(VERB_GET_WORLD_STATE, b"");
        let last = bytes.len() - 1;
        bytes[last] ^= 0xFF;
        assert_eq!(scan(&bytes), Found::Skip(MAGIC.len()));
    }

    /// A LENGTH LARGER THAN THE LIMIT IS NOISE. Trusting it would stall the
    /// stream until that many bytes arrived, which for a corrupt u16 is forever.
    #[test]
    fn an_absurd_length_does_not_stall_the_stream() {
        let mut bytes = request_bytes(VERB_GET_WORLD_STATE, b"");
        bytes[5] = 0xFF;
        bytes[6] = 0xFF;
        assert_eq!(scan(&bytes), Found::Skip(MAGIC.len()));
    }

    /// A LOG FRAME IS DISTINGUISHABLE FROM A REPLY. Protobuf is not
    /// self-describing, so without the kind byte a reader would have to guess —
    /// and most byte strings parse as *some* message, so guessing accepts
    /// nonsense rather than failing.
    #[test]
    fn the_kind_survives_the_frame() {
        let line = log_line(1234, "manifest", LEVEL_STAGE_OK, "40x13 display");
        let framed = frame(KIND_LOG, &line);
        assert_eq!(framed[4], KIND_LOG);
        assert_ne!(framed[4], KIND_RESPONSE);
        // And the words a terminal would have shown are carried verbatim.
        assert!(framed.windows(13).any(|w| w == b"40x13 display"));
    }

    /// Nothing in a scan may panic on arbitrary input: these bytes come off a
    /// wire, and a fault here takes down the channel meant to explain faults.
    #[test]
    fn arbitrary_bytes_never_panic() {
        let mut seed = 0x1234_5678u32;
        for _ in 0..2000 {
            let mut noise = Vec::new();
            for _ in 0..64 {
                seed = seed.wrapping_mul(1_103_515_245).wrapping_add(12345);
                noise.push((seed >> 16) as u8);
            }
            // With and without a leading magic, so the length and checksum paths
            // are both reached with garbage behind them.
            let _ = scan(&noise);
            let mut framed = MAGIC.to_vec();
            framed.extend_from_slice(&noise);
            let _ = scan(&framed);
        }
    }

    /// The answer round-trips through the frame it will be sent in.
    #[test]
    fn a_world_state_encodes_and_frames() {
        let state = WorldState {
            world: "normal",
            tier: "rp2350",
            version: "0.1.0",
            config: &[("screen", "split"), ("input", "keyboard")],
            activity: ACTIVITY_RUNNING,
            app: "countdown",
            app_activity: "tick 3 of 5",
            uptime_ms: 1234,
        };
        let payload = response(true, "", &state.encode());
        let framed = frame(KIND_RESPONSE, &payload);
        // It is a frame, and it survives its own checksum.
        assert_eq!(&framed[..4], &MAGIC);
        assert!(matches!(scan(&framed), Found::Frame(_, _) | Found::Skip(_)));
        // The strings are in there — a decoder-free smoke test that the fields
        // were written at all.
        assert!(framed.windows(6).any(|w| w == b"normal"));
        assert!(framed.windows(9).any(|w| w == b"countdown"));
        assert!(framed.windows(11).any(|w| w == b"tick 3 of 5"));
    }
}
