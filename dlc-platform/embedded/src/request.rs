//! Building a request for an app whose schema this firmware has never seen.
//!
//! # Why this is possible at all
//!
//! The standing rule is that a loader cannot touch an app's protobuf — it is why
//! the badge prints `stdout` rather than rendering `result.output`. That is true
//! for RESPONSES and false for REQUESTS, and the asymmetry is the whole basis of
//! WORLD-INPUT-PLAN D1.
//!
//! Rendering a response needs the schema: a field number does not say what a
//! value MEANS or how to show it. Building a request does not. Encoding a string
//! into field 1 is a tag, a length and the bytes:
//!
//! ```text
//! 0x0A  0x05  h e l l o
//! ```
//!
//! So a world that has been told "field 1, string" — which the app advertises
//! through `GetCommandSpec` — can produce a request the guest decodes correctly,
//! for an app it was never built for.
//!
//! # Why it lives here and not in the firmware
//!
//! A firmware crate's tests do not run: CI cross-compiles those, it cannot
//! execute them, so a test written next to the badge is a comment (AGENTS.md
//! §5). Here it runs on the host on every push, which for a hand-rolled wire
//! encoder is the only arrangement worth having — the same reasoning that moved
//! `manifest.rs` out of `rp2350/`.

/// Protobuf wire type 2: length-delimited.
const WIRE_BYTES: u8 = 2;

/// Encode `value` as a string in `field`, into `out`.
///
/// Returns the written prefix, or `None` if `out` is too small. **A refusal
/// rather than a truncation**: a short buffer would produce a length prefix that
/// disagrees with the bytes after it, and the guest would fail to decode a
/// request that looks structurally plausible — which is a far worse thing to
/// debug than a world that declined to send.
pub fn encode_string_field<'a>(field: u8, value: &str, out: &'a mut [u8]) -> Option<&'a [u8]> {
    let bytes = value.as_bytes();
    let mut len = 0usize;

    // The tag: field number in the upper bits, wire type in the lower three.
    // Fields above 15 need a multi-byte varint tag; refused rather than
    // mis-encoded, because a silently wrong tag writes into the wrong field and
    // still decodes.
    if field == 0 || field > 15 {
        return None;
    }
    push(out, &mut len, (field << 3) | WIRE_BYTES)?;

    // The length, as a varint. Values above 127 need continuation bytes.
    let mut remaining = bytes.len();
    loop {
        let byte = (remaining & 0x7f) as u8;
        remaining >>= 7;
        if remaining == 0 {
            push(out, &mut len, byte)?;
            break;
        }
        push(out, &mut len, byte | 0x80)?;
    }

    for byte in bytes {
        push(out, &mut len, *byte)?;
    }
    Some(&out[..len])
}

/// Encode `value` as a varint in `field`.
///
/// The other half of the pair: `GetCommandSpecRequest.method_id` is a `uint32`,
/// so asking the engine what a command takes needs this rather than the string
/// encoder above.
pub fn encode_varint_field<'a>(field: u8, value: u64, out: &'a mut [u8]) -> Option<&'a [u8]> {
    let mut len = 0usize;
    if field == 0 || field > 15 {
        return None;
    }
    // Wire type 0: varint.
    push(out, &mut len, field << 3)?;
    let mut remaining = value;
    loop {
        let byte = (remaining & 0x7f) as u8;
        remaining >>= 7;
        if remaining == 0 {
            push(out, &mut len, byte)?;
            break;
        }
        push(out, &mut len, byte | 0x80)?;
    }
    Some(&out[..len])
}

fn push(out: &mut [u8], len: &mut usize, byte: u8) -> Option<()> {
    if *len >= out.len() {
        return None;
    }
    out[*len] = byte;
    *len += 1;
    Some(())
}

#[cfg(test)]
mod tests {
    use super::*;

    /// The bytes are the contract. Pinned by hand because there is no protobuf
    /// decoder in this crate to check against, and because a self-consistently
    /// wrong encoding passes every test that is not this one.
    #[test]
    fn encodes_a_string_into_field_one() {
        let mut out = [0u8; 32];
        let got = encode_string_field(1, "alice", &mut out).expect("fits");
        assert_eq!(got, &[0x0a, 0x05, b'a', b'l', b'i', b'c', b'e']);
    }

    /// An empty value is still a SET FIELD, not an omitted one.
    ///
    /// It matters for D3c: an app distinguishes "nobody gave me a name" from
    /// "somebody gave me an empty one" only if the two are different on the
    /// wire — and today they are not, because proto3 scalars have no presence.
    /// Recorded here so the limitation is known rather than discovered.
    #[test]
    fn an_empty_string_still_writes_the_field() {
        let mut out = [0u8; 8];
        let got = encode_string_field(1, "", &mut out).expect("fits");
        assert_eq!(got, &[0x0a, 0x00]);
    }

    /// Lengths above 127 cross the varint continuation boundary — the classic
    /// place a hand-rolled encoder goes wrong, and reachable here because
    /// `MAX_LEN` is 32 today and nothing stops a future caller passing more.
    #[test]
    fn a_long_value_uses_a_multibyte_length() {
        let value = "x".repeat(300);
        let mut out = [0u8; 512];
        let got = encode_string_field(1, &value, &mut out).expect("fits");
        // 300 = 0xac 0x02 in LEB128.
        assert_eq!(&got[..3], &[0x0a, 0xac, 0x02]);
        assert_eq!(got.len(), 3 + 300);
    }

    /// A buffer too small REFUSES rather than truncating.
    #[test]
    fn a_short_buffer_refuses() {
        let mut out = [0u8; 4];
        assert!(encode_string_field(1, "alice", &mut out).is_none());
    }

    /// A varint field, for asking the engine about a method by id.
    #[test]
    fn encodes_a_varint_field() {
        let mut out = [0u8; 16];
        // method 10000 = 0x90 0x4e in LEB128, in field 1.
        let got = encode_varint_field(1, 10_000, &mut out).expect("fits");
        assert_eq!(got, &[0x08, 0x90, 0x4e]);
    }

    /// Field 0 does not exist and fields above 15 need a wider tag. Both are
    /// refused rather than encoded into the wrong place.
    #[test]
    fn out_of_range_fields_are_refused() {
        let mut out = [0u8; 32];
        assert!(encode_string_field(0, "a", &mut out).is_none());
        assert!(encode_string_field(16, "a", &mut out).is_none());
        assert!(encode_string_field(15, "a", &mut out).is_some());
    }
}
