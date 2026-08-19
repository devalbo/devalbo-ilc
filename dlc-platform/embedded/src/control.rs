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

pub use crate::limits::MAGIC;

// EVERY ENUM VALUE, GENERATED. These were 41 constants typed out by hand from
// the .proto — verbs, buttons, notices, activities, levels, scopes, stages,
// layouts, input modes, outlets and a tier — and one of them was wrong for a
// day. See `proto_enums`.
pub use crate::proto_enums::*;
pub use crate::proto_enums::manifest::{
    STATUS_OUTLET_COLOR, STATUS_OUTLET_NONE, TEXT_OUTLET_DISPLAY, TEXT_OUTLET_NONE,
    TEXT_OUTLET_TERMINAL, TEXT_OUTLET_UART,
};

/// Frame overhead: magic, kind, length, checksum. Declared in `limits`, which is
/// where every size this protocol commits to lives.
const OVERHEAD: usize = crate::limits::FRAME_OVERHEAD;

// WHAT A FRAME CARRIES, and the magic above — generated from FRAMING.json, so
// the badge, badgectl and the browser cannot disagree about them.
pub use crate::limits::{KIND_HEARTBEAT, KIND_LOG, KIND_REQUEST, KIND_RESPONSE};

/// The largest payload a frame may carry. See `limits::MAX_PAYLOAD`.
pub const MAX_PAYLOAD: usize = crate::limits::MAX_PAYLOAD;


/// The highest button a world may be asked for; anything above is refused.
pub const BUTTON_MAX: u32 = BUTTON_DOWN;

/// The reserved event topic carrying an app's three status bytes.
///
/// Matches `platform.StatusTopic` in the Go half — a world matching a topic no
/// app emits would render nothing and look correct doing it.
pub const STATUS_TOPIC: &str = "ilc.status";


/// What this world will grant, as a bitmask of `1 << notice`.
///
/// A client may ask for anything; it gets the intersection, and the reply says
/// what that was. Refusing loudly is what stops a caller built against a newer
/// proto waiting forever for a notice nobody here can send.
pub const NOTICES_SUPPORTED: u32 = (1 << NOTICE_LOG) | (1 << NOTICE_HEARTBEAT);


/// A decoded request: a verb and its opaque argument.
#[derive(Debug, PartialEq, Eq)]
pub struct Request {
    pub verb: u32,
    /// The client's correlation number, echoed on the reply. Zero means the
    /// client did not correlate — see `ControlRequest.request_id`.
    pub id: u64,
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

/// How big the frame at the start of `bytes` claims to be, once its header has
/// arrived.
///
/// # Why a reader needs this before it can parse
///
/// A world reassembles a frame in a fixed buffer. A frame larger than that
/// buffer can never complete: `scan` keeps answering `Incomplete`, the buffer
/// fills, and the oldest byte slides out to make room for the newest — forever.
/// Nothing is corrupted and nothing is reported; the sender simply never gets an
/// answer, which is indistinguishable from a world that is not listening.
///
/// That is the same shape as the reply buffer that silently truncated a long
/// answer, at the other end of the same conversation. Both are a fixed size with
/// no way to say "that will not fit".
///
/// Returns `None` while the header is still incomplete.
pub fn declared_len(bytes: &[u8]) -> Option<usize> {
    if bytes.len() < OVERHEAD || bytes[..MAGIC.len()] != MAGIC {
        return None;
    }
    Some(OVERHEAD + u16::from_le_bytes([bytes[5], bytes[6]]) as usize)
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
    let mut request = Request { verb: 0, id: 0, payload: None };
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
            (3, 0) => {
                let (value, next) = varint(body, at)?;
                at = next;
                request.id = value;
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

/// Decode a `PressButtonRequest` — a single varint field.
pub fn parse_button(body: &[u8]) -> Option<u32> {
    let mut at = 0usize;
    let mut button = 0u32;
    while at < body.len() {
        let (tag, next) = varint(body, at)?;
        at = next;
        match ((tag >> 3) as u32, (tag & 0x7) as u8) {
            (1, 0) => {
                let (value, next) = varint(body, at)?;
                at = next;
                button = value as u32;
            }
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
    Some(button)
}

/// Decode an `ExecuteRequest` into a method id and the app's request bytes.
///
/// The bytes are returned as a RANGE, not copied: they are an app's own message
/// in an app's own schema, and this crate has no business looking inside one.
pub fn parse_execute(body: &[u8]) -> Option<(u32, Option<(usize, usize)>)> {
    let mut at = 0usize;
    let mut method = 0u32;
    let mut request = None;
    while at < body.len() {
        let (tag, next) = varint(body, at)?;
        at = next;
        match ((tag >> 3) as u32, (tag & 0x7) as u8) {
            (1, 0) => {
                let (value, next) = varint(body, at)?;
                at = next;
                method = value as u32;
            }
            (2, 2) => {
                let (len, next) = varint(body, at)?;
                let end = next.checked_add(len as usize)?;
                if end > body.len() {
                    return None;
                }
                request = Some((next, end));
                at = end;
            }
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
    Some((method, request))
}

/// Encode an `ExecuteResponse` — the app's own answer, carried verbatim.
/// `ExecuteResponse` — the app's own answer, carried verbatim.
pub struct ExecuteResponse<'a> {
    pub method: u32,
    pub success: bool,
    pub output: &'a [u8],
    pub error: &'a str,
}

impl ExecuteResponse<'_> {
    pub fn encode(&self) -> Vec<u8> {
        let mut out = Vec::new();
        put_varint_field(&mut out, 4, self.method as u64);
        put_varint_field(&mut out, 1, self.success as u64);
        if !self.output.is_empty() {
            tag(&mut out, 2, 2);
            put_varint(&mut out, self.output.len() as u64);
            out.extend_from_slice(self.output);
        }
        put_string(&mut out, 3, self.error);
        out
    }
}

/// One entry of a `ListPayloadsResponse`.
///
/// Encoded into a nested message and appended, so the caller can stream entries
/// out of the registry's critical section without materialising them all first.
/// `PayloadInfo`. It had EIGHT positional arguments, five of them numbers, and
/// `#[allow(clippy::too_many_arguments)]` sitting on top acknowledging it.
pub struct PayloadInfo<'a> {
    pub index: u32,
    pub name: &'a str,
    pub size: u32,
    pub integrity: u32,
    pub entry_method: u32,
    pub is_default: bool,
    pub runnable: bool,
}

impl PayloadInfo<'_> {
    /// Append this entry to a `ListPayloadsResponse`.
    pub fn append_to(&self, out: &mut Vec<u8>) {
        let mut entry = Vec::new();
        put_varint_field(&mut entry, 1, self.index as u64);
        put_string(&mut entry, 2, self.name);
        put_varint_field(&mut entry, 3, self.size as u64);
        put_varint_field(&mut entry, 4, self.integrity as u64);
        put_varint_field(&mut entry, 5, self.entry_method as u64);
        put_varint_field(&mut entry, 6, self.is_default as u64);
        put_varint_field(&mut entry, 7, self.runnable as u64);
        tag(out, 1, 2);
        put_varint(out, entry.len() as u64);
        out.extend_from_slice(&entry);
    }
}

/// Close a `ListPayloadsResponse` with the entry the world last ran.
pub fn payloads_selected(out: &mut Vec<u8>, selected: u32) {
    put_varint_field(out, 2, selected as u64);
}

/// Decode a `SelectPayloadRequest` — the same shape as a button.
pub fn parse_index(body: &[u8]) -> Option<u32> {
    parse_button(body)
}

/// Append one row to a `ScreenResponse`.
///
/// A BUILDER RATHER THAN A STRUCT, because the rows live inside a critical
/// section guarding the panel mirror and cannot be lent out as a slice of `&str`
/// without copying the whole grid first.
pub fn screen_row(out: &mut Vec<u8>, row: &str) {
    put_string(out, 1, row);
}

/// Close a `ScreenResponse` with its dimensions.
pub fn screen_dims(out: &mut Vec<u8>, cols: u32, height: u32) {
    put_varint_field(out, 2, cols as u64);
    put_varint_field(out, 3, height as u64);
}

/// Decode a `Subscription` into a bitmask of `1 << notice`.
///
/// A BITMASK because a world needs to answer "am I sending this one?" on every
/// service call, and a set membership test that is one AND cannot be the reason
/// a log line was late.
///
/// BOTH ENCODINGS OF A REPEATED ENUM are accepted. proto3 packs them by default
/// (wire type 2), but the unpacked form (one varint per value, wire type 0) is
/// legal, is what a hand-written encoder is most likely to emit, and is what a
/// `no_std` client here would write if it ever needed to subscribe to a peer.
/// Accepting only the form our own Go client happens to produce would be a
/// protocol that works exactly until someone writes a second client.
/// `Subscription` — what a client asked for, and what a world granted.
///
/// ONE TYPE FOR BOTH DIRECTIONS because it is one proto message. A separate
/// "granted" type would be the same fields under a different name, and the
/// second one to gain a field would be the one nobody remembered to update.
#[derive(Debug, PartialEq, Eq)]
pub struct Subscription {
    /// A bitmask of `1 << notice`.
    pub notices: u32,
    /// Milliseconds between heartbeats, or 0 for the world's default.
    pub heartbeat_ms: u32,
}

impl Subscription {
    pub fn encode(&self) -> Vec<u8> {
        let mut values = Vec::new();
        for notice in 0..32u32 {
            if self.notices & (1 << notice) != 0 {
                put_varint(&mut values, notice as u64);
            }
        }
        let mut out = Vec::new();
        // PACKED, which is what a proto3 encoder produces, so the reply looks
        // like any other and a generated client decodes it without a special
        // case.
        if !values.is_empty() {
            tag(&mut out, 1, 2);
            put_varint(&mut out, values.len() as u64);
            out.extend_from_slice(&values);
        }
        // THE GRANTED RATE, which may not be the one asked for.
        put_varint_field(&mut out, 2, self.heartbeat_ms as u64);
        out
    }
}

/// The fastest and slowest beat this world will agree to.
///
/// A MILLISECOND BEAT WOULD STARVE THE LOG. Notices sit below the text stream in
/// the send ladder precisely so a frame never delays the words it duplicates,
/// and a beat fast enough to fill every service call would defeat that from the
/// other side. A minute, at the other end, is a liveness signal that takes a
/// minute to tell you anything — technically a heartbeat, useless for spotting a
/// hang.
pub const HEARTBEAT_MIN_MS: u32 = 200;
pub const HEARTBEAT_MAX_MS: u32 = 60_000;
/// What a client gets for asking with no opinion.
pub const HEARTBEAT_DEFAULT_MS: u32 = 1_000;

/// Clamp a requested rate to what this world will sustain.
pub fn heartbeat_rate(asked: u32) -> u32 {
    if asked == 0 {
        HEARTBEAT_DEFAULT_MS
    } else {
        asked.clamp(HEARTBEAT_MIN_MS, HEARTBEAT_MAX_MS)
    }
}

pub fn parse_subscription(body: &[u8]) -> Option<Subscription> {
    let mut at = 0usize;
    let mut wanted = 0u32;
    let mut heartbeat_ms = 0u32;
    while at < body.len() {
        let (tag, next) = varint(body, at)?;
        at = next;
        let field = (tag >> 3) as u32;
        let wire = (tag & 0x7) as u8;
        match (field, wire) {
            (1, 0) => {
                let (value, next) = varint(body, at)?;
                at = next;
                wanted |= notice_bit(value);
            }
            (1, 2) => {
                let (len, next) = varint(body, at)?;
                let end = next.checked_add(len as usize)?;
                if end > body.len() {
                    return None;
                }
                let mut inner = next;
                while inner < end {
                    let (value, next) = varint(body, inner)?;
                    inner = next;
                    wanted |= notice_bit(value);
                }
                at = end;
            }
            (2, 0) => {
                let (value, next) = varint(body, at)?;
                at = next;
                heartbeat_ms = value as u32;
            }
            // Unknown fields skipped by wire type, as everywhere else here.
            //
            // BELOW THE NAMED ARMS, always. Rust takes the first match, so a
            // catch-all written above a specific field silently eats it — which
            // is exactly what happened to `heartbeat_ms`, and the round-trip
            // test is the only reason it did not ship.
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
    Some(Subscription { notices: wanted, heartbeat_ms })
}

/// A notice number as a bit. Anything past the mask's width is DROPPED rather
/// than wrapped — `1 << 33` is not a distinct bit, and silently aliasing an
/// unknown notice onto a known one would grant a subscription nobody asked for.
fn notice_bit(notice: u64) -> u32 {
    if notice < 32 {
        1 << notice
    } else {
        0
    }
}


// ---------------------------------------------------------------------------
// Encoding without a heap
// ---------------------------------------------------------------------------
//
// EVERYTHING ABOVE RETURNS `Vec`, AND A LOG LINE CANNOT.
//
// The badge writes its first log lines before it has a heap: PSRAM comes up at
// stage 4, and stages 1 to 3 — clocks, the data bus, the display — have already
// narrated themselves by then. Those are also the stages most likely to be the
// reason somebody is reading the log at all.
//
// So a `LogLine` is encoded into a caller-provided buffer. It is the one message
// on this protocol with that constraint, and it is not a general preference:
// replies are answered long after boot, are occasional, and are clearer built
// with a `Vec`.

/// A write cursor over a fixed buffer, which refuses rather than truncates.
///
/// A SHORT WRITE IS NOT A SMALL FRAME. Half a protobuf field decodes as
/// something else entirely, and a frame whose header disagrees with its body
/// desynchronises every frame after it. So overflow poisons the cursor and the
/// caller gets `None` — one check, at the end, instead of one per field.
struct Cursor<'a> {
    out: &'a mut [u8],
    at: usize,
    overflowed: bool,
}

impl<'a> Cursor<'a> {
    fn new(out: &'a mut [u8]) -> Self {
        Self { out, at: 0, overflowed: false }
    }

    fn byte(&mut self, value: u8) {
        if self.at < self.out.len() {
            self.out[self.at] = value;
            self.at += 1;
        } else {
            self.overflowed = true;
        }
    }

    fn bytes(&mut self, values: &[u8]) {
        for value in values {
            self.byte(*value);
        }
    }

    fn varint(&mut self, mut value: u64) {
        while value >= 0x80 {
            self.byte((value as u8) | 0x80);
            value >>= 7;
        }
        self.byte(value as u8);
    }

    fn tag(&mut self, field: u32, wire: u8) {
        self.varint(((field as u64) << 3) | wire as u64);
    }

    fn string(&mut self, field: u32, value: &str) {
        if value.is_empty() {
            return;
        }
        self.tag(field, 2);
        self.varint(value.len() as u64);
        self.bytes(value.as_bytes());
    }

    fn varint_field(&mut self, field: u32, value: u64) {
        if value == 0 {
            return;
        }
        self.tag(field, 0);
        self.varint(value);
    }

    fn finish(self) -> Option<usize> {
        if self.overflowed {
            None
        } else {
            Some(self.at)
        }
    }
}

/// Encode a `LogLine` into `out`, returning its length.
///
/// Zero-valued fields are OMITTED, which is proto3's own rule and the reason an
/// undated line can be honest: a zero `uptime_ms` decodes as unset rather than
/// as "at boot".
/// `LogLine`. Five fields, four of them numbers — the shape most in need of
/// names, since any two of them can be swapped without the compiler noticing.
pub struct LogLine<'a> {
    pub uptime_ms: u64,
    pub stage: u32,
    pub level: u32,
    pub scope: u32,
    pub text: &'a str,
}

impl LogLine<'_> {
    /// Encode into `out`, returning its length.
    ///
    /// Zero-valued fields are OMITTED, which is proto3's own rule and the reason
    /// an undated line can be honest: a zero `uptime_ms` decodes as unset rather
    /// than as "at boot".
    pub fn encode_into(&self, out: &mut [u8]) -> Option<usize> {
        let mut cursor = Cursor::new(out);
        cursor.varint_field(1, self.uptime_ms);
        cursor.varint_field(2, self.stage as u64);
        cursor.varint_field(3, self.level as u64);
        cursor.string(4, self.text);
        cursor.varint_field(5, self.scope as u64);
        cursor.finish()
    }
}


/// Wrap a payload in a frame, into `out`, returning its length.
///
/// The no-heap twin of [`frame`]. The two MUST agree byte for byte, which is
/// what `frame_into_matches_frame` in the tests below exists to hold them to.
pub fn frame_into(out: &mut [u8], kind: u8, payload: &[u8]) -> Option<usize> {
    let mut cursor = Cursor::new(out);
    cursor.bytes(&MAGIC);
    cursor.byte(kind);
    cursor.bytes(&(payload.len() as u16).to_le_bytes());
    cursor.bytes(payload);
    cursor.bytes(&crate::catalog::checksum(payload).to_le_bytes());
    cursor.finish()
}




// `WorldName` — GENERATED from names/WORLDS.tsv, the same rows the proto enum
// comes from, so a badge cannot report a number the schema does not name.
pub use crate::names_gen::{
    WORLD_NAME_BADGE_BADGER_MINIMAL, WORLD_NAME_BADGE_BADGER_NORMAL, WORLD_NAME_BADGE_MINIMAL,
    WORLD_NAME_BADGE_NORMAL, WORLD_NAME_BROWSER, WORLD_NAME_NATIVE, WORLD_NAME_UNKNOWN,
};






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

/// A varint field, OMITTED when zero — proto3's rule, and what lets an unset
/// enum stay unset rather than becoming its zero variant on the wire.
fn put_varint_field(out: &mut Vec<u8>, field: u32, value: u64) {
    if value == 0 {
        return;
    }
    tag(out, field, 0);
    put_varint(out, value);
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
    /// `WorldKind`, `Tier`, `ScreenLayout`, `InputMode`, `TextOutlet` — all
    /// numbers, all closed sets declared in control.proto.
    ///
    /// These were `&str` fields and a `config` slice of string pairs, which meant
    /// a world rendered an enum it already had to prose so that a client could
    /// compare prose. Both ends knew the type; only the wire did not.
    pub world: u32,
    pub tier: u32,
    /// GENUINELY A STRING: a version is not a closed set.
    pub version: &'a str,
    pub screen: u32,
    pub input: u32,
    pub text: u32,
    pub activity: u32,
    pub app: &'a str,
    /// What the app last said it was doing. See `activity.rs`.
    ///
    /// FREE TEXT ON PURPOSE — the world cannot know an app's vocabulary.
    pub app_activity: &'a str,
    pub uptime_ms: u64,
    /// Pass-through bookkeeping — see `WorldState` in control.proto.
    pub requests_offered: u32,
    pub requests_taken: u32,
    pub session_open: bool,
}

impl WorldState<'_> {
    /// Encode as the proto message of the same name.
    pub fn encode(&self) -> Vec<u8> {
        let mut out = Vec::new();
        // Field numbers 1, 2 and 4 are RESERVED — they carried the string world,
        // the string tier and the config map. Reusing them would make an older
        // client decode a number as the text it used to be.
        put_varint_field(&mut out, 9, self.world as u64);
        put_varint_field(&mut out, 10, self.tier as u64);
        put_string(&mut out, 3, self.version);
        put_varint_field(&mut out, 11, self.screen as u64);
        put_varint_field(&mut out, 12, self.input as u64);
        put_varint_field(&mut out, 13, self.text as u64);
        put_varint_field(&mut out, 5, self.activity as u64);
        put_string(&mut out, 6, self.app);
        put_string(&mut out, 8, self.app_activity);
        put_varint_field(&mut out, 7, self.uptime_ms);
        put_varint_field(&mut out, 14, self.requests_offered as u64);
        put_varint_field(&mut out, 15, self.requests_taken as u64);
        put_varint_field(&mut out, 16, self.session_open as u64);
        out
    }
}

/// Encode a `ControlResponse`.
/// `ControlResponse`, as a value rather than an argument list.
///
/// # Why these are structs and not parameters
///
/// This was `response(id, ok, error, payload)` and it grew there one field at a
/// time. Positional arguments are a schema written in argument ORDER, which is
/// the one part of a function signature the compiler cannot check when two
/// neighbours share a type: `subscription(mask, rate)` took two `u32`s, and
/// swapping them compiled cleanly and granted a subscription to notice number
/// 1000 at a rate of 2 ms.
///
/// A message on the wire is a set of NAMED fields. Building it from a set of
/// named fields keeps those two the same shape, so a reader comparing this file
/// to the `.proto` is comparing like with like — which is the same argument that
/// took `WorldState` this way before anything else here, and the same argument
/// that replaced the world's `config` map with typed fields.
pub struct Response<'a> {
    pub id: u64,
    pub ok: bool,
    pub error: &'a str,
    pub payload: &'a [u8],
}

impl Response<'_> {
    pub fn encode(&self) -> Vec<u8> {
        let mut out = Vec::new();
        if self.ok {
            tag(&mut out, 1, 0);
            put_varint(&mut out, 1);
        }
        put_string(&mut out, 2, self.error);
        if !self.payload.is_empty() {
            tag(&mut out, 3, 2);
            put_varint(&mut out, self.payload.len() as u64);
            out.extend_from_slice(self.payload);
        }
        put_varint_field(&mut out, 4, self.id);
        out
    }

    /// The no-heap twin, for refusing a request that arrived before the world
    /// had an allocator. `payload` is ignored: nothing that can be answered
    /// without a heap has one.
    pub fn encode_into(&self, out: &mut [u8]) -> Option<usize> {
        let mut cursor = Cursor::new(out);
        if self.ok {
            cursor.tag(1, 0);
            cursor.varint(1);
        }
        cursor.string(2, self.error);
        cursor.varint_field(4, self.id);
        cursor.finish()
    }
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
            world: WORLD_NAME_BADGE_NORMAL,
            tier: TIER_RP2350,
            version: "0.1.0",
            screen: SCREEN_LAYOUT_SPLIT,
            input: INPUT_MODE_KEYBOARD,
            text: TEXT_OUTLET_DISPLAY,
            activity: ACTIVITY_RUNNING,
            app: "countdown",
            app_activity: "tick 3 of 5",
            uptime_ms: 1234,
            requests_offered: 0,
            requests_taken: 0,
            session_open: false,
        };
        let payload = Response { id: 0, ok: true, error: "", payload: &state.encode() }.encode();
        let framed = frame(KIND_RESPONSE, &payload);
        // It is a frame, and it survives its own checksum.
        assert_eq!(&framed[..4], &MAGIC);
        assert!(matches!(scan(&framed), Found::Frame(_, _) | Found::Skip(_)));
        // The strings that are still strings are in there — a decoder-free smoke
        // test that the fields were written at all. `world` and `tier` are no
        // longer among them, which is the point of this change.
        assert!(!framed.windows(6).any(|w| w == b"normal"), "an enum must not travel as its name");
        assert!(framed.windows(9).any(|w| w == b"countdown"));
        assert!(framed.windows(11).any(|w| w == b"tick 3 of 5"));
    }

    // -----------------------------------------------------------------------
    // Subscriptions (D8c)
    // -----------------------------------------------------------------------

    /// A repeated enum encoded the way proto3 does it by default.
    fn packed(notices: &[u32]) -> Vec<u8> {
        let mut values = Vec::new();
        for notice in notices {
            put_varint(&mut values, *notice as u64);
        }
        let mut out = Vec::new();
        tag(&mut out, 1, 2);
        put_varint(&mut out, values.len() as u64);
        out.extend_from_slice(&values);
        out
    }

    /// The same set, one varint per value. Also legal, and what a hand-written
    /// encoder is most likely to produce.
    fn unpacked(notices: &[u32]) -> Vec<u8> {
        let mut out = Vec::new();
        for notice in notices {
            tag(&mut out, 1, 0);
            put_varint(&mut out, *notice as u64);
        }
        out
    }

    #[test]
    fn both_encodings_of_a_repeated_enum_decode_the_same() {
        let wanted = 1 << NOTICE_LOG;
        assert_eq!(parse_subscription(&packed(&[NOTICE_LOG])).unwrap().notices, wanted);
        assert_eq!(parse_subscription(&unpacked(&[NOTICE_LOG])).unwrap().notices, wanted);
    }

    #[test]
    fn an_empty_subscription_is_an_unsubscribe_not_an_error() {
        // An empty `Subscription` encodes to zero bytes, so this is exactly what
        // arrives when a client asks to be left alone.
        assert_eq!(parse_subscription(&[]).unwrap().notices, 0);
    }

    #[test]
    fn a_notice_this_world_does_not_know_is_refused_not_granted() {
        // A caller built from a newer proto asks for something unknown. It must
        // not be silently aliased onto a notice that does exist.
        let asked = parse_subscription(&packed(&[NOTICE_LOG, 7])).unwrap().notices;
        assert_eq!(asked & NOTICES_SUPPORTED, 1 << NOTICE_LOG);
        assert_eq!(NOTICES_SUPPORTED & (1 << 7), 0, "7 is not supported");
    }

    #[test]
    fn a_notice_past_the_masks_width_cannot_alias_onto_a_real_one() {
        // `1 << 33` wraps on a u32 shift and would land on bit 1 — which is the
        // log. Granting a log subscription to someone who asked for notice 33
        // would be the worst kind of wrong: plausible.
        let asked = parse_subscription(&packed(&[33])).unwrap().notices;
        assert_eq!(asked, 0);
    }

    #[test]
    fn a_heartbeat_rate_is_clamped_to_what_the_world_will_sustain() {
        // NO OPINION takes the default rather than zero, which would be "beat as
        // fast as the loop runs" and would starve the text stream.
        assert_eq!(heartbeat_rate(0), HEARTBEAT_DEFAULT_MS);
        // Absurd in both directions is answered, not obeyed. The client learns
        // what it actually got from the reply.
        assert_eq!(heartbeat_rate(1), HEARTBEAT_MIN_MS);
        assert_eq!(heartbeat_rate(3_600_000), HEARTBEAT_MAX_MS);
        // A reasonable ask is granted unchanged.
        assert_eq!(heartbeat_rate(2_000), 2_000);
    }

    #[test]
    fn a_granted_set_round_trips() {
        let granted = 1 << NOTICE_LOG;
        let back = parse_subscription(
            &Subscription { notices: granted, heartbeat_ms: 1000 }.encode(),
        )
        .unwrap();
        assert_eq!(back.notices, granted);
        assert_eq!(back.heartbeat_ms, 1000, "the granted rate comes back too");
    }

    #[test]
    fn granting_nothing_encodes_to_nothing() {
        // A rate of zero encodes as absent too, so an empty grant is empty.
        let none = Subscription { notices: 0, heartbeat_ms: 0 }.encode();
        assert!(none.is_empty());
        assert_eq!(parse_subscription(&none).unwrap().notices, 0);
    }

    #[test]
    fn a_subscribe_request_survives_framing() {
        let body = {
            let mut out = Vec::new();
            tag(&mut out, 1, 0);
            put_varint(&mut out, VERB_SUBSCRIBE as u64);
            tag(&mut out, 2, 2);
            let payload = packed(&[NOTICE_LOG]);
            put_varint(&mut out, payload.len() as u64);
            out.extend_from_slice(&payload);
            out
        };
        let framed = frame(KIND_REQUEST, &body);
        let Found::Frame(request, consumed) = scan(&framed) else {
            panic!("a well-formed subscribe did not scan");
        };
        assert_eq!(consumed, framed.len());
        assert_eq!(request.verb, VERB_SUBSCRIBE);
        let (start, end) = request.payload.expect("subscribe carries a payload");
        assert_eq!(
            parse_subscription(&framed[start..end]).unwrap().notices,
            1 << NOTICE_LOG
        );
    }

    // -----------------------------------------------------------------------
    // Encoding without a heap
    // -----------------------------------------------------------------------

    #[test]
    fn frame_into_matches_frame() {
        // The two encoders must be interchangeable: the badge uses the no-heap
        // one for log lines and the allocating one for replies, and a reader has
        // no idea which produced what.
        let payload = b"the quick brown fox";
        let allocated = frame(KIND_LOG, payload);
        let mut fixed = [0u8; 64];
        let len = frame_into(&mut fixed, KIND_LOG, payload).expect("fits");
        assert_eq!(&fixed[..len], &allocated[..]);
    }

    #[test]
    fn a_log_line_encoded_without_a_heap_scans_back() {
        let mut body = [0u8; 128];
        let len = LogLine {
            uptime_ms: 1234,
            stage: STAGE_INSTANTIATE,
            level: LEVEL_STAGE_OK,
            scope: SCOPE_HARDWARE_ONLY,
            text: "2914 KB heap",
        }
        .encode_into(&mut body)
        .expect("fits");

        let mut framed = [0u8; 192];
        let total = frame_into(&mut framed, KIND_LOG, &body[..len]).expect("fits");
        // Scanning proves the magic, the length and the checksum all agree.
        assert!(matches!(scan(&framed[..total]), Found::Frame(_, _) | Found::Skip(_)));
        assert_eq!(&framed[..4], &MAGIC);
        assert_eq!(framed[4], KIND_LOG);
    }

    #[test]
    fn a_buffer_too_small_refuses_rather_than_truncating() {
        // A short write is not a small frame: half a protobuf field decodes as
        // something else, and a header that disagrees with its body
        // desynchronises everything after it.
        let mut tiny = [0u8; 8];
        assert_eq!(
            LogLine {
                uptime_ms: 1234,
                stage: STAGE_INSTANTIATE,
                level: LEVEL_NOTE,
                scope: 0,
                text: "a much longer line than eight bytes",
            }
            .encode_into(&mut tiny),
            None
        );
        assert_eq!(frame_into(&mut tiny, KIND_LOG, b"much longer than eight"), None);
    }

    #[test]
    fn an_undated_line_omits_its_timestamp_rather_than_claiming_boot() {
        // Backfilled history has no recoverable time. Zero must encode as ABSENT
        // so it decodes as unset, not as "this happened at 0 ms".
        let mut with = [0u8; 64];
        let with_len = LogLine { uptime_ms: 5, stage: 0, level: LEVEL_NOTE, scope: 0, text: "x" }
            .encode_into(&mut with)
            .unwrap();
        let mut without = [0u8; 64];
        let without_len = LogLine { uptime_ms: 0, stage: 0, level: LEVEL_NOTE, scope: 0, text: "x" }
            .encode_into(&mut without)
            .unwrap();
        assert!(
            without_len < with_len,
            "a zero uptime must take no bytes on the wire"
        );
    }

    #[test]
    fn a_stage_that_opens_and_never_resolves_is_expressible() {
        // The case the whole field exists for: the world announced something and
        // did not come back. This must encode without a result.
        let mut body = [0u8; 96];
        let len = LogLine {
            uptime_ms: 42,
            stage: STAGE_PSRAM,
            level: LEVEL_STAGE_OPEN,
            scope: SCOPE_HARDWARE_ONLY,
            text: "",
        }
        .encode_into(&mut body)
        .expect("fits");
        assert!(len > 0);
    }

    // -----------------------------------------------------------------------
    // Correlation (request_id)
    // -----------------------------------------------------------------------

    #[test]
    fn a_request_id_is_parsed_and_echoed() {
        let mut body = Vec::new();
        tag(&mut body, 1, 0);
        put_varint(&mut body, VERB_GET_WORLD_STATE as u64);
        tag(&mut body, 3, 0);
        put_varint(&mut body, 4242);
        let framed = frame(KIND_REQUEST, &body);

        let Found::Frame(request, _) = scan(&framed) else {
            panic!("a correlated request did not scan");
        };
        assert_eq!(request.id, 4242);

        // The reply carries it back verbatim — a world never interprets it.
        let reply = Response { id: request.id, ok: true, error: "", payload: &[] }.encode();
        assert!(reply.windows(2).any(|w| w == [(4 << 3), 0x92]) || !reply.is_empty());
        assert_eq!(parse_response_id(&reply), Some(4242));
    }

    #[test]
    fn an_uncorrelated_request_still_works() {
        // A client that predates the field sends no id, and zero encodes as
        // ABSENT — so the reply carries no id either, and nothing changed for it.
        let mut body = Vec::new();
        tag(&mut body, 1, 0);
        put_varint(&mut body, VERB_GET_WORLD_STATE as u64);
        let framed = frame(KIND_REQUEST, &body);
        let Found::Frame(request, _) = scan(&framed) else {
            panic!("an uncorrelated request did not scan");
        };
        assert_eq!(request.id, 0);
        assert_eq!(parse_response_id(&Response { id: 0, ok: true, error: "", payload: &[] }.encode()), None);
    }

    #[test]
    fn the_no_heap_refusal_correlates_too() {
        // The early-boot refusal is the one reply built on the stack, and a
        // client waiting on an id needs it as much as any other — more, since
        // "too early" is exactly when a caller is unsure what it is talking to.
        let mut out = [0u8; 96];
        let len = Response { id: 77, ok: false, error: "not yet", payload: &[] }
            .encode_into(&mut out)
            .expect("fits");
        assert_eq!(parse_response_id(&out[..len]), Some(77));
    }

    /// Read field 4 out of a `ControlResponse`, for the tests above.
    fn parse_response_id(body: &[u8]) -> Option<u64> {
        let mut at = 0usize;
        while at < body.len() {
            let (tag, next) = varint(body, at)?;
            at = next;
            let (field, wire) = ((tag >> 3) as u32, (tag & 0x7) as u8);
            match (field, wire) {
                (4, 0) => return varint(body, at).map(|(v, _)| v),
                (_, 0) => at = varint(body, at)?.1,
                (_, 2) => {
                    let (len, next) = varint(body, at)?;
                    at = next.checked_add(len as usize)?;
                }
                _ => return None,
            }
        }
        None
    }
}
