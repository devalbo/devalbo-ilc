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
    /// The kebab-cased field name, as a range.
    ///
    /// COSMETIC FOR ENCODING and load-bearing for ASKING: with more than one
    /// field to collect, "which one is this?" is the question a prompt has to
    /// answer, and `help` is optional where a name always exists.
    pub name: Option<(usize, usize)>,
    /// The flag's own byte range, so its repeated fields can be re-walked.
    ///
    /// A CHOICE LIST CANNOT LIVE IN A FIXED STRUCT: an enum has as many values
    /// as the app declared, and this crate has no allocator at the point a flag
    /// is parsed. Keeping the span and reading it again on demand costs a second
    /// pass over a few dozen bytes and removes the arbitrary cap that storing
    /// them would need.
    pub span: (usize, usize),
    /// 1-based argument position, or 0 for flag-only.
    ///
    /// THE ORDER TO ASK IN, and it comes from the app rather than from the order
    /// fields happen to appear. A calculator's `left op right` reads in that
    /// order for the same reason it does on a command line.
    pub positional: u32,
    /// Byte range of the help text within the response, or `None`.
    ///
    /// A RANGE rather than a `&str` because this crate is `no_std` and the
    /// borrow would tie `Flag` to the buffer's lifetime for a value most callers
    /// use once. `help_of` resolves it.
    pub help: Option<(usize, usize)>,
}

/// Field numbers in `SpecFlag`. See `platform.proto`.
/// FIELD NUMBERS, GENERATED — not transcribed.
///
/// These were eleven hand-written numbers, and they are the riskiest set in this
/// crate: this decoder reads a spec produced by an APP's .proto, in another
/// language, from another build. A wrong enum value mislabels something; a wrong
/// field NUMBER makes the world read one field as another — `help` as `name`,
/// `positional` as `default` — and then collect the wrong input and hand the app
/// a request it will happily accept.
///
/// Nothing downstream can catch that. The frame checksums, the varints decode,
/// and the app takes a proto default for whatever went missing and reports
/// success.
use crate::proto_enums::platform::fields::{
    F_GET_COMMAND_SPEC_RESPONSE_COMMANDS as RESPONSE_COMMANDS,
    F_SPEC_COMMAND_FLAGS as COMMAND_FLAGS, F_SPEC_FLAG_DEFAULT_VALUE as FLAG_DEFAULT,
    F_SPEC_FLAG_ENUM_NUMBERS as FLAG_ENUM_NUMBERS, F_SPEC_FLAG_ENUM_VALUES as FLAG_ENUM_VALUES,
    F_SPEC_FLAG_FIELD as FLAG_FIELD, F_SPEC_FLAG_HELP as FLAG_HELP, F_SPEC_FLAG_KIND as FLAG_KIND,
    F_SPEC_FLAG_NAME as FLAG_NAME, F_SPEC_FLAG_POSITIONAL as FLAG_POSITIONAL,
    F_SPEC_FLAG_SOURCE as FLAG_SOURCE,
};

/// `SpecKind` — RE-EXPORTED, not retyped.
///
/// These were eight hand-written numbers with a comment pointing at
/// platform.proto, which is the arrangement that already cost a day here:
/// `TextOutlet` was transcribed in alphabetical rather than declared order, so a
/// badge writing to its own screen told every client it was writing to a serial
/// port. Nothing caught it — both values are legal, the frame checksummed, and
/// the enum name printed cleanly at the far end.
///
/// THIS ONE CROSSES THREE BOUNDARIES, which is why it matters more than most:
/// the number is chosen by an APP's .proto, encoded by the ENGINE into a command
/// spec, and read by a WORLD deciding which widget to show. A world that
/// disagreed about `7` would collect a string where a menu was meant, and the
/// app would take a proto default and report success.
pub use crate::proto_enums::platform::{
    SPEC_KIND_BOOL as KIND_BOOL, SPEC_KIND_BYTES as KIND_BYTES, SPEC_KIND_ENUM as KIND_ENUM,
    SPEC_KIND_INT32 as KIND_INT32, SPEC_KIND_INT64 as KIND_INT64,
    SPEC_KIND_STRING as KIND_STRING, SPEC_KIND_UINT32 as KIND_UINT32,
    SPEC_KIND_UINT64 as KIND_UINT64,
};

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

impl Flag {
    /// A blank flag, for sizing a fixed array before `flags_into` fills it.
    pub const EMPTY: Flag = Flag {
        field: 0,
        kind: 0,
        source: 0,
        help: None,
        default_value: None,
        name: None,
        positional: 0,
        span: (0, 0),
    };
}

/// Every flag of every command in the response, in the order to ASK for them.
///
/// # Why this replaced `first_flag`
///
/// The badge collected exactly one field per command, which was right while the
/// only app took one. It is not a simplification a calculator survives:
/// `left op right` would have prompted for `left` and let the app take proto
/// defaults for the rest, computing `5 UNSPECIFIED 0` and reporting success.
///
/// # The order is the APP's, not this buffer's
///
/// Sorted by `positional` where the app declared one, which is the same order a
/// command line would take them in — so `5 + 3` reads as an expression rather
/// than as three unrelated prompts. Fields with no position keep their
/// declaration order, after the positional ones.
///
/// Returns how many were written. Any beyond `out.len()` are DROPPED, and the
/// caller is expected to say so: silently collecting four of five fields is the
/// same class of failure as collecting one of three.
pub fn flags_into(response: &[u8], out: &mut [Flag]) -> usize {
    let mut count = 0usize;
    let mut cursor = Cursor::new(response);
    while let Some((field, wire)) = cursor.tag() {
        if field == RESPONSE_COMMANDS && wire == 2 {
            let Some(command) = cursor.bytes() else {
                break;
            };
            let mut inner = Cursor::at(response, command);
            while let Some((field, wire)) = inner.tag() {
                if field == COMMAND_FLAGS && wire == 2 {
                    let Some(span) = inner.bytes() else {
                        break;
                    };
                    if let Some(flag) = parse_flag(response, span) {
                        if count < out.len() {
                            out[count] = flag;
                            count += 1;
                        }
                    }
                } else if inner.skip(wire).is_none() {
                    break;
                }
            }
        } else if cursor.skip(wire).is_none() {
            break;
        }
    }

    // INSERTION SORT, because this is at most a handful of entries and a sort
    // that allocates is not available here. Stable, so unpositioned fields keep
    // the order the app declared them in.
    for index in 1..count {
        let mut at = index;
        while at > 0 && rank(&out[at - 1]) > rank(&out[at]) {
            out.swap(at - 1, at);
            at -= 1;
        }
    }
    count
}

/// Where a flag sorts. Unpositioned fields go last, in declaration order.
fn rank(flag: &Flag) -> u32 {
    if flag.positional == 0 {
        u32::MAX
    } else {
        flag.positional
    }
}

/// How many choices an enum flag offers.
pub fn enum_count(response: &[u8], flag: &Flag) -> usize {
    let mut count = 0usize;
    let mut cursor = Cursor::at(response, flag.span);
    while let Some((field, wire)) = cursor.tag() {
        if field == FLAG_ENUM_VALUES && wire == 2 {
            if cursor.bytes().is_none() {
                break;
            }
            count += 1;
        } else if cursor.skip(wire).is_none() {
            break;
        }
    }
    count
}

/// The `index`-th choice: its name, and the WIRE NUMBER to encode.
///
/// # Why the number is not the index
///
/// It was, and that was a bug waiting for the first enum that is not numbered
/// densely from zero. `UNSPECIFIED = 0; OK = 1; FAILED = 5;` has FAILED at index
/// 2, so encoding the index sends 2 — a legal value that the app reads as OK.
/// Nothing rejects it, nothing logs it, and the app does the wrong thing and
/// reports success.
///
/// `enum_numbers` carries the real values, emitted from the same loop over the
/// descriptor that produced the names. Where a spec predates that field, the
/// index is used and is right for the common dense case — the fallback is the
/// old behaviour, not a new guess.
pub fn enum_choice<'a>(response: &'a [u8], flag: &Flag, index: usize) -> Option<(&'a str, i64)> {
    let mut names = 0usize;
    let mut name = None;
    let mut number = None;
    let mut numbers = 0usize;

    let mut cursor = Cursor::at(response, flag.span);
    while let Some((field, wire)) = cursor.tag() {
        match (field, wire) {
            (FLAG_ENUM_VALUES, 2) => {
                let span = cursor.bytes()?;
                if names == index {
                    name = core::str::from_utf8(response.get(span.0..span.1)?).ok();
                }
                names += 1;
            }
            // PACKED OR NOT. proto3 packs a repeated int32 by default, and a
            // hand-written encoder may not — accepting only one of them would be
            // a decoder that works until somebody writes a second generator.
            (FLAG_ENUM_NUMBERS, 2) => {
                let (start, end) = cursor.bytes()?;
                let mut inner = Cursor::at(response, (start, end));
                while let Some(value) = inner.varint() {
                    if numbers == index {
                        number = Some(value as i64);
                    }
                    numbers += 1;
                }
            }
            (FLAG_ENUM_NUMBERS, 0) => {
                let value = cursor.varint()?;
                if numbers == index {
                    number = Some(value as i64);
                }
                numbers += 1;
            }
            _ => cursor.skip(wire)?,
        }
    }

    Some((name?, number.unwrap_or(index as i64)))
}

/// Resolve a flag's name against the buffer it was parsed from.
pub fn name_of<'a>(response: &'a [u8], flag: &Flag) -> Option<&'a str> {
    let (start, end) = flag.name?;
    core::str::from_utf8(response.get(start..end)?).ok()
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
    let mut flag = Flag {
        field: 0,
        kind: 0,
        source: 0,
        help: None,
        default_value: None,
        name: None,
        positional: 0,
        span,
    };
    while let Some((field, wire)) = cursor.tag() {
        match (field, wire) {
            (FLAG_FIELD, 0) => flag.field = cursor.varint()? as u32,
            (FLAG_KIND, 0) => flag.kind = cursor.varint()? as u32,
            (FLAG_SOURCE, 0) => flag.source = cursor.varint()? as u32,
            (FLAG_HELP, 2) => flag.help = Some(cursor.bytes()?),
            (FLAG_DEFAULT, 2) => flag.default_value = Some(cursor.bytes()?),
            (FLAG_NAME, 2) => flag.name = Some(cursor.bytes()?),
            (FLAG_POSITIONAL, 0) => flag.positional = cursor.varint()? as u32,
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

    // -----------------------------------------------------------------------
    // More than one field (the calculator case)
    // -----------------------------------------------------------------------

    /// A command with several flags, as `left op right` would arrive.
    fn multi(flags: &[(u32, u32, &str, u32)]) -> Vec<u8> {
        let mut command = Vec::new();
        delimited(&mut command, 1, b"calc");
        for (field, kind, name, positional) in flags {
            let mut flag = Vec::new();
            delimited(&mut flag, FLAG_NAME, name.as_bytes());
            tag(&mut flag, FLAG_FIELD, 0);
            varint(&mut flag, *field as u64);
            tag(&mut flag, FLAG_KIND, 0);
            varint(&mut flag, *kind as u64);
            if *positional != 0 {
                tag(&mut flag, FLAG_POSITIONAL, 0);
                varint(&mut flag, *positional as u64);
            }
            delimited(&mut command, COMMAND_FLAGS, &flag);
        }
        let mut out = Vec::new();
        delimited(&mut out, RESPONSE_COMMANDS, &command);
        out
    }

    fn empty_flag() -> Flag {
        Flag::EMPTY
    }

    #[test]
    fn every_field_of_a_command_is_collected() {
        // The failure this replaces: a calculator prompted for `left` only, and
        // the app took proto defaults for the rest — computing `5 UNSPECIFIED 0`
        // and reporting success.
        let response = multi(&[(1, 4, "left", 1), (2, 7, "op", 2), (3, 4, "right", 3)]);
        let mut flags = [empty_flag(); 8];
        let count = flags_into(&response, &mut flags);
        assert_eq!(count, 3);
        assert_eq!(flags[0].field, 1);
        assert_eq!(flags[1].field, 2);
        assert_eq!(flags[2].field, 3);
    }

    #[test]
    fn the_order_asked_is_the_order_the_app_declared() {
        // Field numbers deliberately at odds with the positions: an expression
        // reads `left op right` whatever order the proto happens to number them.
        let response = multi(&[(3, 4, "right", 3), (1, 4, "left", 1), (2, 7, "op", 2)]);
        let mut flags = [empty_flag(); 8];
        let count = flags_into(&response, &mut flags);
        assert_eq!(count, 3);
        assert_eq!(name_of(&response, &flags[0]), Some("left"));
        assert_eq!(name_of(&response, &flags[1]), Some("op"));
        assert_eq!(name_of(&response, &flags[2]), Some("right"));
    }

    #[test]
    fn unpositioned_fields_keep_their_declared_order_and_come_last() {
        let response = multi(&[(1, 1, "alpha", 0), (2, 1, "beta", 0), (3, 1, "gamma", 1)]);
        let mut flags = [empty_flag(); 8];
        let count = flags_into(&response, &mut flags);
        assert_eq!(count, 3);
        assert_eq!(name_of(&response, &flags[0]), Some("gamma"), "positional leads");
        // Stability matters: without it two unpositioned fields could swap on a
        // rebuild and a world would ask for them in a different order each time.
        assert_eq!(name_of(&response, &flags[1]), Some("alpha"));
        assert_eq!(name_of(&response, &flags[2]), Some("beta"));
    }

    #[test]
    fn more_fields_than_room_are_dropped_not_wrapped() {
        let response = multi(&[(1, 1, "a", 1), (2, 1, "b", 2), (3, 1, "c", 3)]);
        let mut flags = [empty_flag(); 2];
        assert_eq!(flags_into(&response, &mut flags), 2);
    }

    #[test]
    fn one_field_still_works() {
        // The countdown, unchanged: the case that used to be the only one.
        let response = response(1, 3, 3, "count down from this number", "from");
        let mut flags = [empty_flag(); 8];
        assert_eq!(flags_into(&response, &mut flags), 1);
        assert_eq!(flags[0].field, 1);
        assert_eq!(name_of(&response, &flags[0]), Some("from"));
    }

    // -----------------------------------------------------------------------
    // Enum choices, and why an ordinal is not a value
    // -----------------------------------------------------------------------

    /// A flag offering choices, with the wire numbers the app declared.
    fn with_choices(names: &[&str], numbers: &[i64], packed: bool) -> Vec<u8> {
        let mut flag = Vec::new();
        delimited(&mut flag, FLAG_NAME, b"op");
        tag(&mut flag, FLAG_FIELD, 0);
        varint(&mut flag, 2);
        tag(&mut flag, FLAG_KIND, 0);
        varint(&mut flag, KIND_ENUM as u64);
        for name in names {
            delimited(&mut flag, FLAG_ENUM_VALUES, name.as_bytes());
        }
        if !numbers.is_empty() {
            if packed {
                let mut body = Vec::new();
                for number in numbers {
                    varint(&mut body, *number as u64);
                }
                delimited(&mut flag, FLAG_ENUM_NUMBERS, &body);
            } else {
                for number in numbers {
                    tag(&mut flag, FLAG_ENUM_NUMBERS, 0);
                    varint(&mut flag, *number as u64);
                }
            }
        }
        let mut command = Vec::new();
        delimited(&mut command, 1, b"calc");
        delimited(&mut command, COMMAND_FLAGS, &flag);
        let mut out = Vec::new();
        delimited(&mut out, RESPONSE_COMMANDS, &command);
        out
    }

    fn only_flag(response: &[u8]) -> Flag {
        let mut flags = [Flag::EMPTY; 4];
        assert_eq!(flags_into(response, &mut flags), 1);
        flags[0]
    }

    #[test]
    fn a_sparse_enum_encodes_its_declared_number_not_its_position() {
        // THE BUG THIS EXISTS FOR. `failed` sits at index 2 and its value is 5.
        // Encoding the index sends 2, which is `ok` — a legal value, so nothing
        // rejects it and the app cheerfully does the opposite of what was asked.
        let response = with_choices(&["unspecified", "ok", "failed"], &[0, 1, 5], true);
        let flag = only_flag(&response);
        assert_eq!(enum_count(&response, &flag), 3);
        assert_eq!(enum_choice(&response, &flag, 2), Some(("failed", 5)));
    }

    #[test]
    fn both_encodings_of_the_numbers_are_read() {
        // proto3 packs a repeated int32 by default; a hand-written encoder may
        // not. Accepting one is a decoder that works until a second generator.
        for packed in [true, false] {
            let response = with_choices(&["a", "b", "c"], &[0, 3, 9], packed);
            let flag = only_flag(&response);
            assert_eq!(enum_choice(&response, &flag, 1), Some(("b", 3)), "packed={packed}");
            assert_eq!(enum_choice(&response, &flag, 2), Some(("c", 9)), "packed={packed}");
        }
    }

    #[test]
    fn a_spec_without_numbers_falls_back_to_the_position() {
        // An older spec, from before `enum_numbers` existed. The index is right
        // for the dense case, which is what those specs describe — the fallback
        // is the OLD behaviour, not a new guess.
        let response = with_choices(&["zero", "one", "two"], &[], true);
        let flag = only_flag(&response);
        assert_eq!(enum_choice(&response, &flag, 2), Some(("two", 2)));
    }

    #[test]
    fn asking_past_the_end_gives_nothing() {
        let response = with_choices(&["a", "b"], &[0, 1], true);
        let flag = only_flag(&response);
        assert_eq!(enum_choice(&response, &flag, 2), None);
    }
}
