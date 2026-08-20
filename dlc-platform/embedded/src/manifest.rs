//! The environment manifest, embedded side — a world telling the engine what it
//! can actually do.
//!
//! # Why an embedded world needed this at all
//!
//! The badge advertised `ILC_STDOUT` through `wasi:cli/environment`, and briefly
//! `ILC_COLS`/`ILC_ROWS` too. That looked sufficient and is not, because of a
//! distinction the wasi keys cannot express:
//!
//! | | question | changes? |
//! | --- | --- | --- |
//! | **capability** | can this world show text at all? | settled at flash time |
//! | **allocation** | how many rows has the app got RIGHT NOW? | yes |
//!
//! The wasi environment is read ONCE, during `_initialize`, before any command
//! runs. An app cannot re-read it. So the moment a world takes screen back — a
//! menu, an alert, a notification band — the keys are stale and there is no way
//! to correct them: the app keeps formatting for rows it no longer has.
//!
//! The manifest is the half that moves. It is revision-stamped and re-sendable,
//! which gives an app both things it needs without a new capability: it can POLL
//! (`platform.Env()`, a cached read with no import) and it can be TOLD
//! (`platform.OnWorldManifestChange`).
//!
//! `ILC_STDOUT` stays as the startup bootstrap — what a boot log shows, and all a
//! world that cannot push a manifest would ever have. The budget keys are GONE:
//! they cost a const integer formatter and a lookup table to produce two values
//! nothing read, and an allocation belongs on the channel that can correct it.
//!
//! # Why it is hand-encoded
//!
//! There is no protobuf crate in this firmware, and adding one would pull an
//! allocator into a `no_std` image that deliberately has none. The message is a
//! handful of varints inside two nested length-delimited fields — less machinery
//! hand-written than as a dependency. Every field NUMBER is commented against
//! `platform.proto`, because the numbers are the contract and the names here are
//! decoration.
//!
//! The Go counterpart is `platform.Boot`; the browser's is
//! `dlc-platform/web/environment.ts`. Three host runtimes, three encoders, one
//! wire format — which is why the tests below pin BYTES rather than behaviour.
//!
//! # Why it lives in the library and not in the badge firmware
//!
//! Nothing here is board-specific, and a firmware crate's tests do not run: CI
//! cross-compiles those, it cannot execute them. Tests written next to the badge
//! would be comments (AGENTS.md §5). Here they run on the host on every push,
//! which for a hand-rolled wire encoder is the whole point.

/// THE METHOD IDS, GENERATED — not transcribed.
///
/// These were `= 2` and `= 1` written out here under comments naming the block
/// they came from, which is the same arrangement that put a wrong `TextOutlet`
/// on the wire for a day. The plugin now emits them for Rust as it already did
/// for Go and TypeScript; the badge needed them all along and was the one tier
/// still typing them.
///
/// Both are INHERITED BY EVERY APP through `RegisterAll`, which is what lets a
/// world drive an app it has never heard of.
pub use crate::proto_enums::platform::methods::{METHOD_ID_SET_WORLD_MANIFEST, METHOD_ID_VERSION};

/// `Availability` and `TextOutlet` — RE-EXPORTED, not retyped.
///
/// These were six hand-written numbers under comments naming the proto messages
/// they came from, which is the exact arrangement that already cost a day: a
/// `TextOutlet` transcribed in alphabetical rather than declared order made a
/// badge writing to its own screen tell every client it was writing to a serial
/// port. Both values legal, frame checksummed, enum name printed cleanly.
///
/// THE BOUNDARY IS THE POINT. This message is the world telling an APP what it
/// can do — a different language, a different build, often a different machine.
/// Nothing downstream can check the number against the name, so it has to be
/// right where it is written, and the only way to guarantee that is not to write
/// it.
///
/// `as u64` at the use sites: these are varints on the wire and the generator
/// emits `u32`, which is the enum's actual range.
pub use crate::proto_enums::manifest::{
    AVAILABILITY_ABSENT, TEXT_OUTLET_DISPLAY, TEXT_OUTLET_NONE, TEXT_OUTLET_UART,
};

const WIRE_VARINT: u8 = 0;
const WIRE_BYTES: u8 = 2;

/// Big enough for this message with room to grow a field or two.
///
/// A fixed buffer rather than a `Vec` because this crate has no allocator in its
/// `no_std` profile. `capacity_holds_the_largest_message` is what keeps the
/// number honest as fields are added.
const CAPACITY: usize = 64;

/// A byte buffer that writes forward and cannot overrun.
struct Buf {
    bytes: [u8; CAPACITY],
    len: usize,
}

impl Buf {
    const fn new() -> Self {
        Self { bytes: [0u8; CAPACITY], len: 0 }
    }

    /// Push one byte, SATURATING rather than panicking.
    ///
    /// An overrun would mean a truncated manifest, which the engine rejects as a
    /// decode error — loud and diagnosable at boot. A panic would instead take
    /// down a world whose entire job is to keep running and report why something
    /// failed. The capacity test means neither can actually happen.
    fn push(&mut self, byte: u8) {
        if self.len < CAPACITY {
            self.bytes[self.len] = byte;
            self.len += 1;
        }
    }

    fn tag(&mut self, field: u8, wire: u8) {
        self.varint(((field as u64) << 3) | wire as u64);
    }

    fn varint(&mut self, mut value: u64) {
        loop {
            let byte = (value & 0x7f) as u8;
            value >>= 7;
            if value == 0 {
                self.push(byte);
                return;
            }
            self.push(byte | 0x80);
        }
    }

    fn extend(&mut self, other: &[u8]) {
        for byte in other {
            self.push(*byte);
        }
    }

    fn as_slice(&self) -> &[u8] {
        &self.bytes[..self.len]
    }
}

/// An encoded `SetWorldManifestRequest`, ready to hand to `execute`.
pub struct Manifest {
    buf: Buf,
}

impl Manifest {
    pub fn as_bytes(&self) -> &[u8] {
        self.buf.as_slice()
    }
}

/// Encode the manifest a world wants the engine to hold.
///
/// `revision` MUST be non-zero — the engine refuses 0 rather than reading it as
/// "the first one", so a world that forgot could not be told apart from one that
/// is up to date. Re-send with an INCREMENTED revision when the allocation
/// changes; an unchanged revision is a deliberate no-op the engine skips.
///
/// `cols`/`rows` of zero mean UNMEASURED, never "no room" — an app reads unknown
/// as "wrap however you like". A world that knows its own text budget at compile
/// time, as the badge does, always sends real numbers.
/// What a world tells an app about itself.
///
/// A STRUCT because it was `encode(revision, outlet, cols, rows)` — four `u64`s
/// in a row, where swapping any two compiles cleanly and sends a world with the
/// wrong number of columns or, worse, the wrong revision. Named fields are the
/// same shape as the message they encode.
pub struct WorldManifest {
    pub revision: u64,
    /// `TextOutlet`.
    ///
    /// `u32`, WHICH IS THE ENUM'S RANGE. These were `u64` — the varint's type,
    /// not the value's — so an enum constant had to be cast at every use, and a
    /// cast is where a wrong number stops being visible. The counts below stay
    /// `u64` because they really are quantities.
    pub outlet: u32,
    /// Zero means UNMEASURED, never "no room".
    pub cols: u64,
    pub rows: u64,
    /// `StatusOutlet` — what `ILC_STATUS` used to say. A CAPABILITY.
    pub status: u32,
    /// `World` — what `ILC_WORLD` used to say. IDENTITY, and it travels in its
    /// own nested message so the difference is visible in the bytes.
    ///
    /// No tier: an app was BUILT for its tier and does not need telling, and
    /// `badge-normal` says rp2350 anyway.
    pub world: u32,
}

pub fn encode(env_in: WorldManifest) -> Manifest {
    let WorldManifest { revision, outlet, cols, rows, status, world } = env_in;
    // --- Filesystem ---------------------------------------------------------
    // ABSENT, stated rather than omitted. This world grants no root, and an app
    // that assumed one would have every write fail with an error it had no way
    // to anticipate. Saying so is what lets it take its fallback path instead.
    //
    // It also matches what the engine already believed: before a world sent any
    // manifest at all, `Env()` returned the zero value and `HasFilesystem()` was
    // false. Declaring ABSENT therefore changes nothing an app can observe,
    // which is exactly what makes it safe to START sending one.
    let mut fs = Buf::new();
    fs.tag(1, WIRE_VARINT); // Filesystem.availability
    fs.varint(AVAILABILITY_ABSENT as u64);

    // --- TextOut ------------------------------------------------------------
    let mut text = Buf::new();
    // No availability field: `outlet` already carries all three states — nobody
    // said, said no, and a live outlet — and a second encoding of one fact is
    // what this message exists to remove. Field 1 is reserved in the proto.
    text.tag(2, WIRE_VARINT); // TextOut.outlet
    text.varint(outlet as u64);
    // Zero is the proto default and encodes to nothing. That is the correct
    // spelling of "unmeasured": absent and zero must not be two distinct states,
    // or a reader would have to decide which one meant what.
    if cols != 0 {
        text.tag(3, WIRE_VARINT); // TextOut.cols
        text.varint(cols);
    }
    if rows != 0 {
        text.tag(4, WIRE_VARINT); // TextOut.rows
        text.varint(rows);
    }

    // --- WorldManifest --------------------------------------------------------
    let mut env = Buf::new();
    env.tag(1, WIRE_VARINT); // Environment.revision
    env.varint(revision);
    env.tag(2, WIRE_BYTES); // Environment.filesystem
    env.varint(fs.len as u64);
    env.extend(fs.as_slice());
    env.tag(4, WIRE_BYTES); // Environment.text_out  (field 3 is reserved)
    env.varint(text.len as u64);
    env.extend(text.as_slice());

    // WHAT THIS WORLD IS. These were `ILC_TIER`, `ILC_WORLD` and `ILC_STATUS` in
    // `wasi:cli/environment`, which is read once at `_initialize` and can never
    // be corrected — the same reason the text budget moved here.
    //
    // Zero is the proto default and encodes to nothing, which is the right
    // spelling of "this world did not say": an app reading UNSPECIFIED knows it
    // was not told, rather than being handed a plausible wrong answer.
    if status != 0 {
        env.tag(5, WIRE_VARINT); // Environment.status
        env.varint(status as u64);
    }

    // --- Identity -----------------------------------------------------------
    // NESTED, not flat, and that is the whole point: everything above is a
    // CAPABILITY an app acts on, and this is WHO the world is, which an app
    // should only ever report. The grouping is the rule — see `Identity` in
    // platform.proto.
    if world != 0 {
        let mut who = Buf::new();
        who.tag(1, WIRE_VARINT); // Identity.world
        who.varint(world as u64);
        env.tag(6, WIRE_BYTES); // Environment.identity
        env.varint(who.len as u64);
        env.extend(who.as_slice());
    }

    // --- SetWorldManifestRequest ----------------------------------------------
    let mut req = Buf::new();
    req.tag(1, WIRE_BYTES); // SetWorldManifestRequest.environment
    req.varint(env.len as u64);
    req.extend(env.as_slice());

    Manifest { buf: req }
}

#[cfg(test)]
mod tests {
    use super::*;

    /// Pin the BYTES, not the behaviour.
    ///
    /// The field numbers are a contract shared with Go and TypeScript, and a
    /// renamed constant that silently moved one would still compile and still
    /// look like it worked — right up until an engine read `cols` out of the
    /// `rows` slot. There is no protobuf decoder in this crate to check against,
    /// so the expected encoding is written out by hand.
    #[test]
    fn encodes_the_expected_wire_shape() {
        let m = encode(WorldManifest {
            revision: 1,
            outlet: TEXT_OUTLET_DISPLAY,
            cols: 40,
            rows: 12,
            status: 0,
            world: 0,
        });
        let want: &[u8] = &[
            0x0a, 0x0e, // field 1 (environment), 14 bytes
            0x08, 0x01, //   revision = 1
            0x12, 0x02, //   field 2 (filesystem), 2 bytes
            0x08, 0x01, //     availability = ABSENT
            0x22, 0x06, //   field 4 (text_out), 6 bytes
            0x10, 0x03, //     outlet = DISPLAY  (field 1 reserved, not sent)
            0x18, 0x28, //     cols = 40
            0x20, 0x0c, //     rows = 12
        ];
        assert_eq!(m.as_bytes(), want);
    }

    /// Zero cols/rows must VANISH rather than encode as an explicit zero, so
    /// "unmeasured" has exactly one spelling on the wire.
    #[test]
    fn unmeasured_budget_is_omitted() {
        let m = encode(WorldManifest {
            revision: 1,
            outlet: TEXT_OUTLET_UART,
            cols: 0,
            rows: 0,
            status: 0,
            world: 0,
        });
        assert!(!m.as_bytes().contains(&0x18), "cols tag present for an unmeasured width");
        assert!(!m.as_bytes().contains(&0x20), "rows tag present for an unmeasured height");
    }

    /// A revision above 127 crosses the varint continuation boundary — the one
    /// place a hand-rolled encoder classically gets it wrong, and a world that
    /// re-sends on every allocation change WILL get there.
    #[test]
    fn multibyte_revision_encodes_as_a_varint() {
        let m = encode(WorldManifest {
            revision: 300,
            outlet: TEXT_OUTLET_DISPLAY,
            cols: 40,
            rows: 12,
            status: 0,
            world: 0,
        });
        // 300 = 0xac 0x02 in LEB128.
        assert_eq!(&m.as_bytes()[2..5], &[0x08, 0xac, 0x02]);
    }

    /// Nested length prefixes must count the bytes they actually contain.
    ///
    /// This is the failure a shape test alone can miss once a field is added:
    /// the inner message grows, the outer prefix does not, and the engine
    /// decodes a truncated message or runs off the end.
    #[test]
    fn nested_lengths_match_their_contents() {
        let m = encode(WorldManifest {
            revision: 1,
            outlet: TEXT_OUTLET_DISPLAY,
            cols: 40,
            rows: 12,
            status: 0,
            world: 0,
        });
        let bytes = m.as_bytes();
        assert_eq!(bytes[0], 0x0a, "outer field 1, length-delimited");
        let env_len = bytes[1] as usize;
        assert_eq!(bytes.len(), 2 + env_len, "outer length must cover the rest exactly");
    }

    /// The buffer must never be the reason a manifest is truncated.
    #[test]
    fn capacity_holds_the_largest_message() {
        let m = encode(WorldManifest {
            revision: u32::MAX as u64,
            outlet: TEXT_OUTLET_DISPLAY,
            cols: 65535,
            rows: 65535,
            status: 0,
            world: 0,
        });
        assert!(m.as_bytes().len() < CAPACITY, "len {} vs capacity {}", m.as_bytes().len(), CAPACITY);
    }
}
