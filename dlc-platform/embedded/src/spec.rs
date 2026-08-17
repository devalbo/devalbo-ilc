//! Reading what a command takes, from an app this firmware has never seen.
//!
//! # The other half of `request.rs`
//!
//! `request.rs` builds a request given a field number and a kind. This is where
//! those come from: the app answers `GetCommandSpec` (method 5) with the fields
//! it accepts, generated from its own `.proto`, and the world reads enough of
//! that answer to render a prompt and encode the reply.
//!
//! Before this, the badge hardcoded "field 1, a string, labelled `name?`" —
//! hello's question, asked of every app. A countdown was prompted for a name.
//!
//! # Why a hand-rolled decoder, and how little it does
//!
//! There is no protobuf runtime in this firmware and adding one would pull an
//! allocator into an image that deliberately has none. Decoding is harder than
//! encoding — nested messages, unknown fields to skip — but the world needs only
//! four values per flag, so this parses those and SKIPS EVERYTHING ELSE by wire
//! type.
//!
//! Skipping by wire type is what makes it forward-compatible: a field added to
//! `SpecFlag` next year is skipped by a firmware that has never heard of it,
//! rather than desynchronising the parse.

/// What the world needs to prompt for one input and encode the answer.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Flag {
    /// The proto field NUMBER — the wire, and what `request::encode_string_field`
    /// needs. A name is cosmetic.
    pub field: u32,
    /// `SpecKind`. 1 is STRING, which is the only kind a character picker can
    /// collect today.
    pub kind: u32,
    /// `SpecSource`. 3 is STDIN — "a person types this" — which is the app
    /// declaring that an input surface is wanted at all.
    pub source: u32,
    /// Byte range of the declared default, or `None`.
    ///
    /// The spinner starts here, so the first thing shown is what the app would
    /// have used anyway and confirming immediately means the same as not
    /// answering.
    pub default_value: Option<(usize, usize)>,
    /// Byte range of the help text within the response, or `None`.
    ///
    /// A RANGE rather than a `&str` because this crate is `no_std` and the
    /// borrow would tie `Flag` to the buffer's lifetime for a value most callers
    /// use once. `help_of` resolves it.
    pub help: Option<(usize, usize)>,
}

/// Field numbers in `SpecFlag`. See `platform.proto`.
const FLAG_NAME: u32 = 1;
const FLAG_FIELD: u32 = 2;
const FLAG_KIND: u32 = 3;
const FLAG_SOURCE: u32 = 4;
const FLAG_HELP: u32 = 5;
const FLAG_DEFAULT: u32 = 7;

/// Field numbers in `SpecCommand` and the response.
const COMMAND_FLAGS: u32 = 4;
const RESPONSE_COMMANDS: u32 = 1;

/// `SpecKind` values the world knows how to collect. See `platform.proto`.
pub const KIND_STRING: u32 = 1;
pub const KIND_BOOL: u32 = 2;
pub const KIND_INT32: u32 = 3;
pub const KIND_INT64: u32 = 4;
pub const KIND_UINT32: u32 = 5;
pub const KIND_UINT64: u32 = 6;

/// Whether this kind is a whole number, and so wants the spinner.
pub fn is_integer(kind: u32) -> bool {
    matches!(kind, KIND_INT32 | KIND_INT64 | KIND_UINT32 | KIND_UINT64)
}

/// The FIRST flag of the first command in a `GetCommandSpecResponse`.
///
/// One flag, deliberately: a badge has one screen and one keyboard, and
/// collecting a second value needs a mode that does not exist yet
/// (SESSION-AND-SURFACE-PLAN §4). A command with more than one input gets its
/// first collected and the rest defaulted, which is a no-op rather than an error
/// (Decision 33).
pub fn first_flag(response: &[u8]) -> Option<Flag> {
    let mut cursor = Cursor::new(response);
    while let Some((field, wire)) = cursor.tag() {
        if field == RESPONSE_COMMANDS && wire == 2 {
            let command = cursor.bytes()?;
            if let Some(flag) = first_flag_of_command(response, command) {
                return Some(flag);
            }
        } else {
            cursor.skip(wire)?;
        }
    }
    None
}

/// Resolve a flag's help text against the buffer it was parsed from.
pub fn help_of<'a>(response: &'a [u8], flag: &Flag) -> Option<&'a str> {
    let (start, end) = flag.help?;
    core::str::from_utf8(response.get(start..end)?).ok()
}

/// Resolve a flag's declared default, as text.
pub fn default_of<'a>(response: &'a [u8], flag: &Flag) -> Option<&'a str> {
    let (start, end) = flag.default_value?;
    core::str::from_utf8(response.get(start..end)?).ok()
}

/// A flag's default as a number, or 0.
///
/// The spec carries defaults as STRINGS — one representation for every kind,
/// parsed by whoever needs a number. Anything unparseable reads as 0, which is
/// the same answer as no default at all and keeps a malformed spec from being a
/// failure the world has to report.
pub fn default_number(response: &[u8], flag: &Flag) -> i64 {
    let Some(text) = default_of(response, flag) else {
        return 0;
    };
    let (negative, digits) = match text.strip_prefix('-') {
        Some(rest) => (true, rest),
        None => (false, text),
    };
    let mut value: i64 = 0;
    for byte in digits.bytes() {
        if !byte.is_ascii_digit() {
            return 0;
        }
        value = match value.checked_mul(10).and_then(|v| v.checked_add((byte - b'0') as i64)) {
            Some(v) => v,
            None => return 0,
        };
    }
    if negative {
        -value
    } else {
        value
    }
}

fn first_flag_of_command(base: &[u8], command: (usize, usize)) -> Option<Flag> {
    let mut cursor = Cursor::at(base, command);
    while let Some((field, wire)) = cursor.tag() {
        if field == COMMAND_FLAGS && wire == 2 {
            let flag = cursor.bytes()?;
            return parse_flag(base, flag);
        }
        cursor.skip(wire)?;
    }
    None
}

fn parse_flag(base: &[u8], span: (usize, usize)) -> Option<Flag> {
    let mut cursor = Cursor::at(base, span);
    let mut flag = Flag { field: 0, kind: 0, source: 0, help: None, default_value: None };
    while let Some((field, wire)) = cursor.tag() {
        match (field, wire) {
            (FLAG_FIELD, 0) => flag.field = cursor.varint()? as u32,
            (FLAG_KIND, 0) => flag.kind = cursor.varint()? as u32,
            (FLAG_SOURCE, 0) => flag.source = cursor.varint()? as u32,
            (FLAG_HELP, 2) => flag.help = Some(cursor.bytes()?),
            (FLAG_DEFAULT, 2) => flag.default_value = Some(cursor.bytes()?),
            (FLAG_NAME, 2) => {
                cursor.bytes()?;
            }
            // UNKNOWN FIELDS ARE SKIPPED BY WIRE TYPE, which is what keeps a
            // firmware readable by a spec that grew fields after it shipped.
            _ => cursor.skip(wire)?,
        }
    }
    // A flag with no field number is not usable: encoding into field 0 writes a
    // request the guest cannot decode.
    if flag.field == 0 {
        return None;
    }
    Some(flag)
}

/// A position in a byte slice, tracking absolute offsets so spans survive.
struct Cursor<'a> {
    bytes: &'a [u8],
    at: usize,
    end: usize,
}

impl<'a> Cursor<'a> {
    fn new(bytes: &'a [u8]) -> Self {
        Self { bytes, at: 0, end: bytes.len() }
    }

    fn at(bytes: &'a [u8], span: (usize, usize)) -> Self {
        Self { bytes, at: span.0, end: span.1 }
    }

    fn byte(&mut self) -> Option<u8> {
        if self.at >= self.end {
            return None;
        }
        let byte = self.bytes[self.at];
        self.at += 1;
        Some(byte)
    }

    fn varint(&mut self) -> Option<u64> {
        let mut value = 0u64;
        let mut shift = 0u32;
        loop {
            let byte = self.byte()?;
            value |= ((byte & 0x7f) as u64) << shift;
            if byte & 0x80 == 0 {
                return Some(value);
            }
            shift += 7;
            // A varint longer than 10 bytes is malformed. Refusing beats
            // shifting past 64 and silently producing a plausible number.
            if shift >= 70 {
                return None;
            }
        }
    }

    fn tag(&mut self) -> Option<(u32, u8)> {
        let tag = self.varint()?;
        Some(((tag >> 3) as u32, (tag & 0x7) as u8))
    }

    /// A length-delimited field, as an absolute span.
    fn bytes(&mut self) -> Option<(usize, usize)> {
        let len = self.varint()? as usize;
        let start = self.at;
        let end = start.checked_add(len)?;
        if end > self.end {
            return None;
        }
        self.at = end;
        Some((start, end))
    }

    fn skip(&mut self, wire: u8) -> Option<()> {
        match wire {
            0 => {
                self.varint()?;
            }
            1 => self.at = self.at.checked_add(8)?,
            2 => {
                self.bytes()?;
            }
            5 => self.at = self.at.checked_add(4)?,
            // Groups (3, 4) are proto2 and never appear here. Refusing stops the
            // parse rather than guessing a length.
            _ => return None,
        }
        if self.at > self.end {
            return None;
        }
        Some(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use alloc::vec::Vec;

    fn varint(out: &mut Vec<u8>, mut value: u64) {
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

    fn tag(out: &mut Vec<u8>, field: u32, wire: u8) {
        varint(out, ((field as u64) << 3) | wire as u64);
    }

    fn delimited(out: &mut Vec<u8>, field: u32, body: &[u8]) {
        tag(out, field, 2);
        varint(out, body.len() as u64);
        out.extend_from_slice(body);
    }

    /// Build the response a real engine sends for one command with one flag.
    fn response(field: u32, kind: u32, source: u32, help: &str, name: &str) -> Vec<u8> {
        let mut flag = Vec::new();
        delimited(&mut flag, FLAG_NAME, name.as_bytes());
        tag(&mut flag, FLAG_FIELD, 0);
        varint(&mut flag, field as u64);
        tag(&mut flag, FLAG_KIND, 0);
        varint(&mut flag, kind as u64);
        tag(&mut flag, FLAG_SOURCE, 0);
        varint(&mut flag, source as u64);
        delimited(&mut flag, FLAG_HELP, help.as_bytes());

        let mut command = Vec::new();
        delimited(&mut command, 1, b"count"); // SpecCommand.name
        delimited(&mut command, COMMAND_FLAGS, &flag);

        let mut out = Vec::new();
        delimited(&mut out, RESPONSE_COMMANDS, &command);
        out
    }

    #[test]
    fn reads_the_field_kind_and_help() {
        let bytes = response(1, KIND_STRING, 3, "count down from this number", "from");
        let flag = first_flag(&bytes).expect("a flag");
        assert_eq!(flag.field, 1);
        assert_eq!(flag.kind, KIND_STRING);
        assert_eq!(flag.source, 3);
        assert_eq!(help_of(&bytes, &flag), Some("count down from this number"));
    }

    /// A field number other than 1 must survive — the whole point is not
    /// hardcoding hello's shape.
    #[test]
    fn a_field_other_than_one_is_carried() {
        let bytes = response(7, KIND_STRING, 1, "help", "other");
        assert_eq!(first_flag(&bytes).unwrap().field, 7);
    }

    /// An app with no inputs yields nothing, and the world skips the prompt.
    #[test]
    fn no_flags_means_no_prompt() {
        let mut command = Vec::new();
        delimited(&mut command, 1, b"tick");
        let mut out = Vec::new();
        delimited(&mut out, RESPONSE_COMMANDS, &command);
        assert_eq!(first_flag(&out), None);
    }

    /// An empty response — an app that registered no spec at all.
    #[test]
    fn an_empty_response_is_not_a_parse_error() {
        assert_eq!(first_flag(&[]), None);
    }

    /// UNKNOWN FIELDS ARE SKIPPED, so a spec that grows stays readable by a
    /// firmware that shipped before the growth.
    #[test]
    fn unknown_fields_are_skipped() {
        let mut flag = Vec::new();
        tag(&mut flag, 99, 0); // a varint field from the future
        varint(&mut flag, 12345);
        delimited(&mut flag, 98, b"a bytes field from the future");
        tag(&mut flag, FLAG_FIELD, 0);
        varint(&mut flag, 3);
        tag(&mut flag, FLAG_KIND, 0);
        varint(&mut flag, KIND_STRING as u64);

        let mut command = Vec::new();
        delimited(&mut command, COMMAND_FLAGS, &flag);
        let mut out = Vec::new();
        delimited(&mut out, RESPONSE_COMMANDS, &command);

        let parsed = first_flag(&out).expect("still parses");
        assert_eq!(parsed.field, 3);
        assert_eq!(parsed.kind, KIND_STRING);
    }

    /// TRUNCATED INPUT MUST NOT PANIC. These bytes come off a wire; a firmware
    /// that indexed past the end would fault on a malformed response instead of
    /// declining to prompt.
    #[test]
    fn truncated_input_declines_rather_than_panics() {
        let full = response(1, KIND_STRING, 3, "help", "from");
        for cut in 0..full.len() {
            let _ = first_flag(&full[..cut]);
        }
    }

    /// The declared default reaches the spinner as a number.
    #[test]
    fn a_default_is_read_as_a_number() {
        let mut flag = Vec::new();
        tag(&mut flag, FLAG_FIELD, 0);
        varint(&mut flag, 1);
        tag(&mut flag, FLAG_KIND, 0);
        varint(&mut flag, KIND_INT32 as u64);
        delimited(&mut flag, FLAG_DEFAULT, b"5");
        let mut command = Vec::new();
        delimited(&mut command, COMMAND_FLAGS, &flag);
        let mut out = Vec::new();
        delimited(&mut out, RESPONSE_COMMANDS, &command);

        let parsed = first_flag(&out).expect("a flag");
        assert!(is_integer(parsed.kind));
        assert_eq!(default_number(&out, &parsed), 5);
    }

    /// A missing or unparseable default reads as 0 — the same answer as none,
    /// rather than a failure the world would have to report.
    #[test]
    fn a_bad_default_is_zero_not_an_error() {
        for text in [&b"not a number"[..], b"", b"12x", b"-"] {
            let mut flag = Vec::new();
            tag(&mut flag, FLAG_FIELD, 0);
            varint(&mut flag, 1);
            delimited(&mut flag, FLAG_DEFAULT, text);
            let mut command = Vec::new();
            delimited(&mut command, COMMAND_FLAGS, &flag);
            let mut out = Vec::new();
            delimited(&mut out, RESPONSE_COMMANDS, &command);
            let parsed = first_flag(&out).expect("a flag");
            assert_eq!(default_number(&out, &parsed), 0, "{text:?}");
        }
    }

    /// Negative defaults survive: a signed field may legitimately start below 0.
    #[test]
    fn a_negative_default_is_read() {
        let mut flag = Vec::new();
        tag(&mut flag, FLAG_FIELD, 0);
        varint(&mut flag, 1);
        delimited(&mut flag, FLAG_DEFAULT, b"-40");
        let mut command = Vec::new();
        delimited(&mut command, COMMAND_FLAGS, &flag);
        let mut out = Vec::new();
        delimited(&mut out, RESPONSE_COMMANDS, &command);
        let parsed = first_flag(&out).expect("a flag");
        assert_eq!(default_number(&out, &parsed), -40);
    }

    /// Which kinds get the spinner. Written out so adding a kind to the proto
    /// without deciding its widget is a test failure rather than a silent skip.
    #[test]
    fn integer_kinds_are_recognised() {
        for kind in [KIND_INT32, KIND_INT64, KIND_UINT32, KIND_UINT64] {
            assert!(is_integer(kind), "kind {kind}");
        }
        for kind in [KIND_STRING, KIND_BOOL, 7, 8, 0] {
            assert!(!is_integer(kind), "kind {kind}");
        }
    }

    /// A flag with no field number is refused: encoding into field 0 produces a
    /// request the guest cannot decode.
    #[test]
    fn a_flag_without_a_field_number_is_refused() {
        let mut flag = Vec::new();
        delimited(&mut flag, FLAG_HELP, b"help but no number");
        let mut command = Vec::new();
        delimited(&mut command, COMMAND_FLAGS, &flag);
        let mut out = Vec::new();
        delimited(&mut out, RESPONSE_COMMANDS, &command);
        assert_eq!(first_flag(&out), None);
    }
}
