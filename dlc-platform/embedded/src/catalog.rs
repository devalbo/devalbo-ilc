//! A well-known region holding SEVERAL components, so a tier can offer a choice.
//!
//! WHY THIS EXISTS RATHER THAN `include_bytes!`. Baking one artifact into the
//! firmware makes the firmware app-specific: a badge that runs hello and a badge
//! that runs tictactoe are two different builds of the same code, and swapping
//! apps means a toolchain rather than a file. This region inverts that — **one
//! loader firmware, and the apps are data** — which is what lets a host offer a
//! menu instead of a build flag.
//!
//! WHAT MAKES IT AFFORDABLE, and it is the same fact Phase 0c turned on:
//! `Component::deserialize_raw` borrows memory it only ever READS, so a payload
//! never has to be copied anywhere. On a chip whose flash is memory-mapped, a
//! catalog entry is already a `&'static [u8]` — enumerating twenty of them costs
//! twenty slices, not twenty megabytes. Nothing is read until one is chosen.
//!
//! THIS MODULE NAMES NO CHIP. It is handed a `&'static [u8]` and knows nothing
//! about where it came from; producing that slice from a fixed address is the
//! board's job, which is where the `unsafe` lives (see `rp2350/src/payload.rs`).
//!
//! # The format
//!
//! Entries back to back, each starting on a `SLOT_ALIGN` boundary:
//!
//! ```text
//! offset  size  field
//! 0       8     magic — MAGIC, and its absence is how the catalog ENDS
//! 8       4     payload length, u32 little-endian
//! 12      4     name length, u32 little-endian, <= NAME_MAX
//! 16      32    name, UTF-8, NUL-padded
//! 48      4     entry method id, u32 little-endian — 0 means DEFAULT_ENTRY_METHOD
//! 52      4     FNV-1a checksum of the payload, u32 LE — 0 means "not checksummed"
//! 56      4     flags, u32 LE — bit 0 = DEFAULT (run this one unattended)
//! 60      4     ENGINE TAG, u32 LE — 0 means "not recorded" (see ENGINE_TAG)
//! 64      len   the .cwasm itself
//! ```
//!
//! **The entry method is why a loader can be app-agnostic.** Decision 31 gives
//! every component ONE export, `execute(u32, list<u8>)`, so running a payload
//! means knowing which id to pass — and that is the app's fact, not the
//! firmware's. Carrying it here means a badge runs what it was given without a
//! rebuild, which is the entire point of the region.
//!
//! **There is no index block, deliberately.** A directory at the front would have
//! to be rewritten every time a payload is added, which on flash means erasing a
//! sector that holds live data. Terminating on a bad magic means appending a
//! payload touches only the sectors that payload occupies — and an erased flash
//! sector reads as `0xFF`, which is not the magic, so a blank region is an empty
//! catalog with no initialisation step.

use core::str;

/// Marks a slot as ours. The trailing byte is a FORMAT VERSION: bump it and old
/// firmware stops at the first entry it does not understand rather than reading a
/// length field that has moved.
pub const MAGIC: [u8; 8] = *b"ILCPAY\x01\x00";

/// Bytes from the start of an entry to the start of its payload.
///
/// 64 rather than the 16 the fields need, so that a `SLOT_ALIGN`-aligned entry
/// puts its payload on a 64-byte boundary — comfortably past the **16-byte
/// alignment `deserialize_raw` requires**, with room for the reserved field to
/// grow without moving the payload.
pub const HEADER_LEN: usize = 64;

/// Entries start on a 4 KB boundary — one flash sector on every part this
/// targets, so a payload can be replaced by erasing only its own sectors.
pub const SLOT_ALIGN: usize = 4096;

/// The longest name a slot can carry. Fixed rather than variable because a
/// fixed-size header is one read, and 32 bytes is more than "tictactoe" needs.
pub const NAME_MAX: usize = 32;

/// How many entries a scan will look at before giving up.
///
/// A guard against garbage, not a design limit: without it, flash that happens to
/// contain the magic and a zero length would spin forever on a board with no way
/// to say so.
pub const MAX_ENTRIES: usize = 16;

/// The method a payload runs when nothing says otherwise.
///
/// 10000 is the first id in the APP's band — everything below belongs to the
/// platform — so it is the first command a scaffolded app defines, and hello's
/// `greet`. A convention rather than a rule, which is exactly why the header can
/// override it.
pub const DEFAULT_ENTRY_METHOD: u32 = 10000;

/// Bit 0 of the flags word: **run this one when nobody chooses**.
///
/// WHY IT EXISTS. Before this, the app a badge ran unattended was whichever
/// entry happened to be FIRST in flash — an artifact of the order arguments were
/// typed into `payload-image`, not a decision anybody made. Swapping two
/// arguments silently changed what the badge does on power-up, and nothing on
/// the badge or in the image said which one was meant.
///
/// Marked rather than positional, so adding an app cannot displace the default
/// and the intent survives in the image itself.
pub const FLAG_DEFAULT: u32 = 1 << 0;

/// WHICH ENGINE A PAYLOAD WAS COMPILED FOR.
///
/// # The failure this exists to name
///
/// A `.cwasm` is bound to the exact Wasmtime that produced it. Bump the pin in
/// `Cargo.toml` and every payload already in the region becomes unloadable —
/// which is safe, because `deserialize_raw` refuses a mismatched artifact rather
/// than executing garbage, but it is not DIAGNOSABLE.
///
/// Without this field the bytes are intact, so the checksum verifies and the
/// payload-region stage reports `Verified`. The failure then surfaces two stages
/// later as a Wasmtime deserialize error, which reads like a firmware fault. The
/// one fact that explains it — "this was built for a different engine" — is
/// known at pack time and was being thrown away.
///
/// # Why a constant and not a computed hash
///
/// Wasmtime has `Engine::precompile_compatibility_hash`, which is exactly the
/// right answer and is gated behind `cranelift`/`winch`. The badge has NEITHER —
/// having no compiler is the point of the tier — so the firmware cannot compute
/// it. A shared constant is what both sides can agree on: the packer writes it,
/// the loader compares it, and `engine_tag_matches_the_pin` keeps it honest.
///
/// # What must change it
///
/// The Wasmtime version and the pointer width, because those are what decide
/// whether a `.cwasm` loads. Not the catalog format — `MAGIC` already carries
/// that — and not app-level facts, which belong to the app.
pub const ENGINE_TAG_SOURCE: &str = "wasmtime=46.0.1;pulley32";

/// The tag written into byte 60, derived from [`ENGINE_TAG_SOURCE`].
///
/// Never zero: 0 MEANS "not recorded", so an image built before this field
/// existed stays loadable rather than being rejected by a firmware that cannot
/// tell "old" from "wrong".
pub const ENGINE_TAG: u32 = {
    let bytes = ENGINE_TAG_SOURCE.as_bytes();
    let mut hash: u32 = 0x811c_9dc5;
    let mut i = 0;
    while i < bytes.len() {
        hash ^= bytes[i] as u32;
        hash = hash.wrapping_mul(0x0100_0193);
        i += 1;
    }
    if hash == 0 { 1 } else { hash }
};

/// Whether a payload's bytes are what were written.
///
/// **THIS MATTERS MORE NOW THAT THERE IS A MENU.** Without it, a corrupt payload
/// is discovered by Wasmtime refusing to deserialize it — after the user picked
/// it, and reported as a runtime error that reads like a firmware bug rather than
/// a bad file.
#[derive(Clone, Copy, PartialEq, Eq, Debug)]
pub enum Integrity {
    /// Checksum present and correct.
    Verified,
    /// No checksum recorded — an image built before the field existed. Runnable,
    /// because refusing it would break every payload already in the field, and
    /// `deserialize_raw` still gets the final say.
    Unverified,
    /// Checksum present and WRONG. The bytes are not what was written: a
    /// half-finished drag, a bad flash write, or a truncated UF2.
    Corrupt,
    /// Bytes intact, but built for a DIFFERENT ENGINE — see [`ENGINE_TAG`].
    ///
    /// Distinct from `Corrupt` because the remedy is different and a reader acts
    /// on it: corrupt means the file arrived damaged, this means it arrived
    /// perfectly and was compiled against another Wasmtime. One is "flash it
    /// again", the other is "repack it".
    WrongEngine,
}

/// One component in the region — a name and the bytes, borrowed in place.
#[derive(Clone, Copy)]
pub struct Payload {
    /// What a menu shows. Not parsed, not resolved — a label.
    pub name: &'static str,
    /// The `.cwasm`, still in flash. 64-byte aligned by construction, which is
    /// what makes it safe to hand to `MinimalHost::from_precompiled`.
    pub bytes: &'static [u8],
    /// Which command to run. The app's fact, carried with the app — see the
    /// module header for why this is not the firmware's business.
    pub entry_method: u32,
    /// Whether the bytes still check out.
    pub integrity: Integrity,
    /// Raw flags word; see [`FLAG_DEFAULT`].
    pub flags: u32,
}

impl Payload {
    /// Can this be run at all?
    pub fn runnable(&self) -> bool {
        // WrongEngine is refused for the same reason Corrupt is: it cannot load.
        // The difference is what the report says, not whether it runs.
        !matches!(self.integrity, Integrity::Corrupt | Integrity::WrongEngine)
    }

    /// Was this marked as the one to run unattended?
    pub fn is_default(&self) -> bool {
        self.flags & FLAG_DEFAULT != 0
    }
}

/// FNV-1a, 32-bit.
///
/// **Chosen for what it has to catch, not for strength.** Payloads are trusted by
/// construction — they come from our own build, not the network — so this is
/// looking for a truncated drag or a bad flash write, never an attacker. FNV-1a
/// is four lines, needs no table, and catches those. A CRC32 would cost a
/// kilobyte of table on a device counting them, and a cryptographic hash would be
/// answering a question nobody asked.
pub fn checksum(bytes: &[u8]) -> u32 {
    let mut hash: u32 = 0x811C_9DC5;
    for byte in bytes {
        hash ^= *byte as u32;
        hash = hash.wrapping_mul(0x0100_0193);
    }
    // Never return 0: that value MEANS "no checksum recorded", and a payload
    // whose real hash happened to be zero would silently become unverified.
    if hash == 0 {
        1
    } else {
        hash
    }
}

/// The payloads found in a region, in the order they appear.
///
/// Deliberately a fixed array rather than a `Vec`: scanning happens before the
/// heap is proven on a bring-up board, and an allocation here would be the first
/// thing to fault with nothing printed.
pub struct Catalog {
    slots: [Option<Payload>; MAX_ENTRIES],
    count: usize,
}

impl Catalog {
    /// How many payloads the region holds.
    pub fn len(&self) -> usize {
        self.count
    }

    pub fn is_empty(&self) -> bool {
        self.count == 0
    }

    /// The nth payload, for a selector driven by buttons or a serial character.
    pub fn get(&self, index: usize) -> Option<Payload> {
        self.slots.get(index).copied().flatten()
    }

    /// Every payload, for drawing the menu.
    ///
    /// **Includes corrupt ones.** A broken file that vanishes from the list is
    /// indistinguishable from one that was never installed, which sends someone
    /// to re-drag a payload that is already there. Showing it as broken is the
    /// only way the badge can say what it actually found.
    pub fn iter(&self) -> impl Iterator<Item = Payload> + '_ {
        self.slots[..self.count].iter().filter_map(|s| *s)
    }

    /// **Which app to run when nobody chooses** — the marked default if there is
    /// one, otherwise the first that works.
    ///
    /// Falling back to "first" keeps images built before the flag existed
    /// working, and keeps a single-app image from needing ceremony. What it no
    /// longer does is make argument order into a silent decision.
    pub fn default_choice(&self) -> Option<(usize, Payload)> {
        self.slots[..self.count]
            .iter()
            .enumerate()
            .filter_map(|(i, s)| s.map(|p| (i, p)))
            .find(|(_, p)| p.runnable() && p.is_default())
            .or_else(|| self.first_runnable())
    }

    /// The first payload that can actually run, for a badge with no menu.
    pub fn first_runnable(&self) -> Option<(usize, Payload)> {
        self.slots[..self.count]
            .iter()
            .enumerate()
            .filter_map(|(i, s)| s.map(|p| (i, p)))
            .find(|(_, p)| p.runnable())
    }
}

/// Read a region and return what it holds.
///
/// SAFE, and that is the point of the signature: the caller has already done the
/// dangerous part by producing a `&'static [u8]` for a region that really exists.
/// Everything here is bounds-checked against that slice, so a corrupt or blank
/// catalog yields fewer entries — never a fault.
///
/// Stops at the first slot that does not begin with [`MAGIC`], which is also what
/// erased flash (`0xFF`) does.
pub fn scan(region: &'static [u8]) -> Catalog {
    let mut slots = [None; MAX_ENTRIES];
    let mut count = 0usize;
    let mut offset = 0usize;

    while count < MAX_ENTRIES {
        // The header must fit before anything in it can be trusted.
        let Some(header) = region.get(offset..offset + HEADER_LEN) else {
            break;
        };
        if header[..8] != MAGIC {
            break;
        }

        let len = u32::from_le_bytes([header[8], header[9], header[10], header[11]]) as usize;
        let name_len =
            u32::from_le_bytes([header[12], header[13], header[14], header[15]]) as usize;

        // A length that does not fit the region means a truncated or corrupt
        // write. STOP rather than skip: the next slot's position is computed from
        // this length, so continuing would be reading at an address derived from
        // known-bad data.
        let body = offset + HEADER_LEN;
        let Some(bytes) = region.get(body..body + len) else {
            break;
        };

        // A bad name is not worth stopping for — the payload is still loadable,
        // and a menu entry with no label beats a badge that refuses to boot.
        let name = header
            .get(16..16 + name_len.min(NAME_MAX))
            .and_then(|raw| str::from_utf8(raw).ok())
            .unwrap_or("<unnamed>");

        // Zero means "unset", so an image built before this field existed — or by
        // a tool that leaves the reserved bytes blank — still runs.
        let entry_method =
            match u32::from_le_bytes([header[48], header[49], header[50], header[51]]) {
                0 => DEFAULT_ENTRY_METHOD,
                id => id,
            };

        // INTEGRITY IS CHECKED HERE, at scan, rather than lazily at load — the
        // menu has to be able to SAY a file is broken, and it cannot do that from
        // a check that has not run. The cost is reading each payload once at
        // boot; at XIP speeds that is milliseconds, and it buys the difference
        // between "that app is corrupt" and a badge that dies after you pick it.
        let recorded =
            u32::from_le_bytes([header[52], header[53], header[54], header[55]]);
        let integrity = if recorded == 0 {
            Integrity::Unverified
        } else if recorded == checksum(bytes) {
            Integrity::Verified
        } else {
            Integrity::Corrupt
        };

        let flags = u32::from_le_bytes([header[56], header[57], header[58], header[59]]);

        // ENGINE TAG, checked only when the bytes themselves are sound. A corrupt
        // payload's tag proves nothing — the field could be damaged along with
        // everything else — and reporting the wrong engine for a truncated file
        // would send someone repacking a payload that simply needs reflashing.
        let engine = u32::from_le_bytes([header[60], header[61], header[62], header[63]]);
        let integrity = if integrity == Integrity::Verified && engine != 0 && engine != ENGINE_TAG {
            Integrity::WrongEngine
        } else {
            integrity
        };

        slots[count] = Some(Payload {
            name,
            bytes,
            entry_method,
            integrity,
            flags,
        });
        count += 1;

        // Next slot, rounded up to a sector. A zero-length payload would leave
        // `offset` unchanged and loop forever, so the round-up is `body + len`
        // — always past the header — rather than `offset + len`.
        let next = (body + len).div_ceil(SLOT_ALIGN) * SLOT_ALIGN;
        if next <= offset {
            break;
        }
        offset = next;
    }

    Catalog { slots, count }
}

#[cfg(test)]
mod tests {
    use super::*;
    use alloc::vec;
    use alloc::vec::Vec;

    /// Build a region the way the image tool does, so the format is tested from
    /// both ends rather than against itself.
    fn entry(name: &str, payload: &[u8]) -> Vec<u8> {
        entry_with_checksum(name, payload, checksum(payload))
    }

    fn entry_with_checksum(name: &str, payload: &[u8], sum: u32) -> Vec<u8> {
        entry_full(name, payload, sum, ENGINE_TAG)
    }

    fn entry_full(name: &str, payload: &[u8], sum: u32, engine: u32) -> Vec<u8> {
        let mut out = vec![0u8; HEADER_LEN];
        out[..8].copy_from_slice(&MAGIC);
        out[8..12].copy_from_slice(&(payload.len() as u32).to_le_bytes());
        out[12..16].copy_from_slice(&(name.len() as u32).to_le_bytes());
        out[16..16 + name.len()].copy_from_slice(name.as_bytes());
        out[52..56].copy_from_slice(&sum.to_le_bytes());
        out[60..64].copy_from_slice(&engine.to_le_bytes());
        out.extend_from_slice(payload);
        out.resize(out.len().div_ceil(SLOT_ALIGN) * SLOT_ALIGN, 0);
        out
    }

    /// THE CONSTANT MUST TRACK THE PIN, and nothing else enforces that.
    ///
    /// `ENGINE_TAG_SOURCE` names the Wasmtime version by hand because the badge
    /// cannot compute a compatibility hash — it has no compiler. A hand-written
    /// version string is exactly the kind of thing that goes stale on a
    /// dependency bump, and a STALE tag is worse than no tag: every payload
    /// would claim compatibility with an engine that can no longer load it,
    /// which is the original bug wearing a badge that says it was checked.
    #[test]
    fn engine_tag_matches_the_pin() {
        let manifest = include_str!("../Cargo.toml");
        let pin = manifest
            .lines()
            .find(|line| line.trim_start().starts_with("wasmtime = "))
            .expect("no wasmtime dependency line in Cargo.toml");
        let version = pin
            .split_once("version = \"=")
            .or_else(|| pin.split_once("\"="))
            .map(|(_, rest)| rest.split('"').next().unwrap_or(""))
            .expect("wasmtime pin is not an exact `=x.y.z` version");
        assert!(
            ENGINE_TAG_SOURCE.contains(version),
            "ENGINE_TAG_SOURCE is {ENGINE_TAG_SOURCE:?} but Cargo.toml pins wasmtime {version:?} \
             — bump the constant, or every payload will claim an engine that cannot load it"
        );
    }

    /// A payload built for another engine is NAMED, not called corrupt.
    ///
    /// The two need different remedies: corrupt means the file arrived damaged
    /// and should be reflashed; this means it arrived perfectly and must be
    /// repacked. Collapsing them sends people to reflash a file that is fine.
    #[test]
    fn a_foreign_engine_is_named_rather_than_called_corrupt() {
        let image = entry_full("hello", b"payload", checksum(b"payload"), ENGINE_TAG ^ 0xFFFF);
        let catalog = scan(leak(image));
        let found = catalog.get(0).unwrap();
        assert_eq!(found.integrity, Integrity::WrongEngine);
        assert!(!found.runnable(), "a payload for another engine cannot load");
    }

    /// Zero means "not recorded", so images predating this field still run.
    ///
    /// Refusing them would break every payload already in the field for a
    /// firmware that cannot tell "old" from "wrong" — and `deserialize_raw`
    /// still gets the final say, exactly as it did before the field existed.
    #[test]
    fn an_untagged_payload_still_runs() {
        let image = entry_full("hello", b"payload", checksum(b"payload"), 0);
        let catalog = scan(leak(image));
        let found = catalog.get(0).unwrap();
        assert_eq!(found.integrity, Integrity::Verified);
        assert!(found.runnable());
    }

    /// A corrupt payload reports CORRUPT even when its tag is also wrong.
    ///
    /// Damaged bytes can damage the tag too, so the tag proves nothing here —
    /// and "repack it" would be the wrong advice for a truncated file.
    #[test]
    fn corruption_outranks_a_foreign_engine() {
        let image = entry_full("hello", b"payload", 0xDEAD_BEEF, ENGINE_TAG ^ 0xFFFF);
        let catalog = scan(leak(image));
        assert_eq!(catalog.get(0).unwrap().integrity, Integrity::Corrupt);
    }

    #[test]
    fn a_good_payload_verifies() {
        let catalog = scan(leak(entry("hello", b"payload")));
        assert_eq!(catalog.get(0).unwrap().integrity, Integrity::Verified);
        assert!(catalog.get(0).unwrap().runnable());
    }

    #[test]
    fn a_corrupt_payload_is_named_rather_than_hidden() {
        // THE POINT: a broken file that vanishes from the list is
        // indistinguishable from one never installed, and sends someone to
        // re-drag a payload that is already there.
        let mut image = entry_with_checksum("hello", b"payload", 0xDEAD_BEEF);
        image.extend_from_slice(&entry("good", b"other"));
        let catalog = scan(leak(image));

        assert_eq!(catalog.len(), 2, "a corrupt entry must not end the scan");
        assert_eq!(catalog.get(0).unwrap().integrity, Integrity::Corrupt);
        assert!(!catalog.get(0).unwrap().runnable());
        // ...and the good one after it is still reachable, which is the whole
        // reason corruption is a per-entry fact rather than a scan-stopping one.
        assert_eq!(catalog.get(1).unwrap().integrity, Integrity::Verified);
        assert_eq!(catalog.first_runnable().unwrap().0, 1);
    }

    #[test]
    fn an_unchecksummed_payload_still_runs() {
        // Images built before the field existed must not become unbootable.
        let catalog = scan(leak(entry_with_checksum("old", b"payload", 0)));
        assert_eq!(catalog.get(0).unwrap().integrity, Integrity::Unverified);
        assert!(catalog.get(0).unwrap().runnable());
    }

    #[test]
    fn a_truncated_payload_is_detected() {
        // The realistic corruption: a drag interrupted part-way, so the header is
        // right and the bytes are short. The length still fits the region, so the
        // scan cannot catch it — only the checksum can.
        let full = vec![0xABu8; 4096];
        let mut image = entry_with_checksum("hello", &full[..2048], checksum(&full));
        image.extend_from_slice(&entry("after", b"z"));
        let catalog = scan(leak(image));
        assert_eq!(catalog.get(0).unwrap().integrity, Integrity::Corrupt);
        assert_eq!(catalog.get(1).unwrap().name, "after");
    }

    #[test]
    fn the_checksum_never_collides_with_no_checksum() {
        // 0 MEANS "unrecorded", so a payload whose real hash is 0 must not
        // silently become unverified.
        for len in 0..64usize {
            let bytes = vec![0u8; len];
            assert_ne!(checksum(&bytes), 0);
        }
    }

    fn leak(v: Vec<u8>) -> &'static [u8] {
        Vec::leak(v)
    }

    #[test]
    fn blank_flash_is_an_empty_catalog() {
        // Erased flash is 0xFF, and it must read as "no payloads" rather than as
        // a corrupt entry — there is no initialisation step for a new board.
        let catalog = scan(leak(vec![0xFF; SLOT_ALIGN * 2]));
        assert!(catalog.is_empty());
    }

    #[test]
    fn finds_entries_in_order() {
        let mut image = entry("hello", b"first");
        image.extend_from_slice(&entry("tictactoe", b"second"));
        let catalog = scan(leak(image));

        assert_eq!(catalog.len(), 2);
        assert_eq!(catalog.get(0).unwrap().name, "hello");
        assert_eq!(catalog.get(0).unwrap().bytes, b"first");
        assert_eq!(catalog.get(1).unwrap().name, "tictactoe");
        assert_eq!(catalog.get(1).unwrap().bytes, b"second");
    }

    #[test]
    fn payloads_land_16_byte_aligned() {
        // THE PROPERTY `deserialize_raw` DEPENDS ON. It is a consequence of
        // HEADER_LEN and SLOT_ALIGN both being multiples of 16, which is exactly
        // the kind of invariant that survives until someone adds a field.
        let mut image = entry("a", b"x");
        image.extend_from_slice(&entry("b", &[0u8; 9000]));
        let region = leak(image);
        let base = region.as_ptr() as usize;
        for payload in scan(region).iter() {
            let offset = payload.bytes.as_ptr() as usize - base;
            assert_eq!(offset % 16, 0, "payload {} is not aligned", payload.name);
        }
    }

    #[test]
    fn a_truncated_entry_stops_the_scan() {
        // A write interrupted by a power cut. The first entry is still good, and
        // the second must not be reported from a length that runs off the end.
        let mut image = entry("good", b"payload");
        let mut bad = entry("truncated", b"");
        bad[8..12].copy_from_slice(&(u32::MAX / 2).to_le_bytes());
        image.extend_from_slice(&bad);
        let catalog = scan(leak(image));

        assert_eq!(catalog.len(), 1);
        assert_eq!(catalog.get(0).unwrap().name, "good");
    }

    #[test]
    fn a_zero_length_payload_does_not_loop() {
        // The guard in `scan`: a zero length must still advance a whole slot.
        let mut image = entry("empty", b"");
        image.extend_from_slice(&entry("after", b"z"));
        let catalog = scan(leak(image));

        assert_eq!(catalog.len(), 2);
        assert_eq!(catalog.get(1).unwrap().name, "after");
    }

    #[test]
    fn an_unset_entry_method_falls_back_to_the_app_band() {
        // `entry` leaves bytes 48..52 zero, which is what a tool that predates
        // the field writes. Such an image must still run.
        let catalog = scan(leak(entry("hello", b"x")));
        assert_eq!(catalog.get(0).unwrap().entry_method, DEFAULT_ENTRY_METHOD);
    }

    #[test]
    fn an_explicit_entry_method_is_carried() {
        // THE PROPERTY THAT MAKES A LOADER APP-AGNOSTIC: the id travels with the
        // app, so firmware that never heard of it can still run it.
        let mut image = entry("other", b"x");
        image[48..52].copy_from_slice(&10007u32.to_le_bytes());
        assert_eq!(scan(leak(image)).get(0).unwrap().entry_method, 10007);
    }

    #[test]
    fn garbage_after_the_last_entry_is_ignored() {
        let mut image = entry("only", b"payload");
        image.extend_from_slice(&[0x5A; SLOT_ALIGN]);
        assert_eq!(scan(leak(image)).len(), 1);
    }
}
