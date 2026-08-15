//! **The portable name profile** — one set of rules every ILC tier can honour.
//!
//! WHY THIS IS STRICT, and why it REJECTS rather than fixes. A name is written on
//! one tier and read on another: an app persists `saves/Game1.json` on a badge,
//! and a user opens it over USB on Windows. Every layer in that path has its own
//! rules, and the failure mode is not an error — it is a file that quietly has a
//! different name than the one that was asked for.
//!
//! That is not hypothetical here. `fatview.rs` mounted a payload called
//! `hello.pulley32` as `HELLO.PU.CWA`, because an 8.3 directory entry has no dot
//! — the extension is the last three bytes, positionally. Nothing failed. The
//! name was simply wrong, and would have stayed wrong until somebody noticed.
//!
//! **So the profile is the INTERSECTION of what the tiers accept, and violating
//! it is an error at the boundary** — at `dlc-precompile` for payload names, at
//! `wasi:filesystem` for app paths. Sanitising is reserved for the one place a
//! name is purely cosmetic (a synthetic 8.3 view), and even there it is a
//! last-resort net under a check that should already have passed.
//!
//! # What each tier imposes, and who is the binding constraint
//!
//! | tier | storage | the rule it adds |
//! | --- | --- | --- |
//! | CLI / native | POSIX | case-SENSITIVE; `/` and NUL illegal |
//! | browser | OPFS | no `/`; `.` and `..` reserved |
//! | badge | FAT over USB | case-INSENSITIVE; Windows reserved names; no trailing dot or space |
//!
//! **The badge is the binding constraint, and case is the subtle one.** POSIX
//! treats `Save.json` and `save.json` as two files; FAT and Windows treat them as
//! one. An app that creates both works on a laptop and silently loses data on a
//! badge — so the profile forbids uppercase entirely rather than trying to
//! detect the collision. Lowercase is a rule an app can follow; "do not create
//! two names that differ only in case" is a rule it cannot check locally.

use core::fmt;

// THE RULES ARE GENERATED, the reasons are here. `cmd/gen-names` emits
// `names_gen.rs` and its Go twin from `dlc-platform/names/RULES.json`, so this
// implementation and the app-side one cannot disagree about a character class, a
// limit, or a reserved name. Before codegen they were written twice and
// `VECTORS.tsv` existed to DETECT the drift; now the drift cannot occur, and the
// vectors check the remaining question — whether the rules mean what was
// intended.
pub use crate::names_gen::{COMPONENT_MAX, PATH_MAX, SHORT_STEM_LEN};
use crate::names_gen::{
    bad_edge_first, bad_edge_last, portable_name_char, short_name_char, RESERVED_DEVICES,
};

/// Why a name was refused. Carried rather than collapsed into a bool because the
/// message is what makes the rule learnable — an app author sees which rule and
/// why it exists, not "invalid path".
#[derive(Clone, Copy, PartialEq, Eq, Debug)]
pub enum NameError {
    /// Nothing, or a path made only of separators.
    Empty,
    /// Longer than [`COMPONENT_MAX`] or [`PATH_MAX`].
    TooLong,
    /// A byte outside the portable set: not `a-z`, `0-9`, `-`, `_`, or `.`.
    ///
    /// **Uppercase lands here on purpose** — see the module header. FAT is
    /// case-insensitive and POSIX is not, so allowing both cases means an app can
    /// create two files that become one on the badge.
    IllegalCharacter,
    /// `.` or `..`, which every filesystem reserves for traversal.
    ReservedTraversal,
    /// A name Windows refuses regardless of extension: `con`, `prn`, `aux`,
    /// `nul`, `com1`..`com9`, `lpt1`..`lpt9`.
    ///
    /// Only matters because the badge's files are read on a PC — which is exactly
    /// the kind of constraint that is invisible until someone plugs the thing in.
    ReservedDevice,
    /// Starts with `.` (hidden on POSIX, and unrepresentable in 8.3), or starts
    /// or ends with `-`, or ends with `.`.
    BadEdge,
    /// An absolute path, a `\` separator, or an empty component from `//`.
    BadStructure,
}

impl fmt::Display for NameError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        let text = match self {
            NameError::Empty => "empty",
            NameError::TooLong => "too long",
            NameError::IllegalCharacter => {
                "illegal character (use lowercase a-z, 0-9, '-', '_', '.')"
            }
            NameError::ReservedTraversal => "'.' and '..' are reserved",
            NameError::ReservedDevice => "reserved device name on Windows",
            NameError::BadEdge => "must not start with '.' or '-', or end with '.' or '-'",
            NameError::BadStructure => "must be relative, '/'-separated, with no empty components",
        };
        f.write_str(text)
    }
}

/// Check ONE path component — a filename or a directory name, no separators.
pub fn check_component(name: &str) -> Result<(), NameError> {
    if name.is_empty() {
        return Err(NameError::Empty);
    }
    if name.len() > COMPONENT_MAX {
        return Err(NameError::TooLong);
    }
    if name == "." || name == ".." {
        return Err(NameError::ReservedTraversal);
    }

    for byte in name.bytes() {
        if !portable_name_char(byte) {
            return Err(NameError::IllegalCharacter);
        }
    }

    // Edges. A leading dot is hidden on POSIX and has no 8.3 representation; a
    // trailing dot or space is silently STRIPPED by Windows, which is the same
    // class of bug as the 8.3 mangling this profile exists to prevent.
    if bad_edge_first(name.as_bytes()[0]) || bad_edge_last(name.as_bytes()[name.len() - 1]) {
        return Err(NameError::BadEdge);
    }

    // Windows matches a device name against the stem, so `con.txt` is refused too.
    let stem = name.split('.').next().unwrap_or(name);
    if RESERVED_DEVICES.contains(&stem) {
        return Err(NameError::ReservedDevice);
    }

    Ok(())
}

/// Check a whole relative path — `/`-separated, no leading slash.
///
/// **Relative only.** A tier decides where an app's files live (a directory on a
/// laptop, a FAT subdirectory on the badge), so an absolute path is a claim the
/// app is not entitled to make. This mirrors what the Go platform's `SafeJoin`
/// enforces for the same reason: containment belongs to the host.
pub fn check_path(path: &str) -> Result<(), NameError> {
    if path.is_empty() {
        return Err(NameError::Empty);
    }
    if path.len() > PATH_MAX {
        return Err(NameError::TooLong);
    }
    // A leading `/` is absolute; a `\` is a Windows separator that POSIX reads as
    // a legal filename character, so the same string names different things on
    // different tiers.
    if path.starts_with('/') || path.contains('\\') {
        return Err(NameError::BadStructure);
    }

    let mut any = false;
    for component in path.split('/') {
        // `//` and a trailing `/` both produce an empty component.
        if component.is_empty() {
            return Err(NameError::BadStructure);
        }
        check_component(component)?;
        any = true;
    }
    if !any {
        return Err(NameError::Empty);
    }
    Ok(())
}

/// Is this a name the profile accepts? For callers that only need the answer.
pub fn is_portable(path: &str) -> bool {
    check_path(path).is_ok()
}


// ---------------------------------------------------------------------------
// SET-LEVEL validation: collisions no per-name check can see
// ---------------------------------------------------------------------------
//
// Every rule above judges ONE name in isolation, and a whole class of bug is
// invisible from there: two names that are each perfectly legal and still cannot
// coexist. The host does not report it — one file simply becomes the other, or
// shadows it in a listing.
//
// TWO WAYS THAT HAPPENS HERE:
//
//   1. **Case folding.** `Save.json` and `save.json` are two files on Linux and
//      one on FAT, Windows, and a default-formatted macOS. The portable profile
//      forbids uppercase outright, so it cannot happen there — but an app that
//      opted into `posix` (a declared, narrower world) can hit it, which is
//      exactly the trade that profile makes.
//
//   2. **8.3 truncation**, and this one is live rather than hypothetical. The
//      badge's USB volume renders a catalog name as its first EIGHT characters,
//      so `board-state-1` and `board-state-2` both appear as `BOARD-ST` — two
//      payloads, one filename, and the second unreachable through the drive.
//
// **The truncation used here is the SAME FUNCTION the volume renders with**
// (`fatview` calls `short_stem_83`). A validator that reimplemented the rule
// would check something the badge does not do, which is worse than not checking.

/// How two names collide.
#[derive(Clone, Copy, PartialEq, Eq, Debug)]
pub enum Collision {
    /// Identical once case is folded. One file on any case-insensitive host.
    CaseFold,
    /// Identical once truncated to an 8.3 short name — the badge's USB view.
    ShortName,
}

/// A name reduced to the 8-character stem a FAT short name carries.
///
/// **THE SHARED TRUTH FOR TRUNCATION.** `fatview::short_name` calls this to build
/// the directory entry, and `check_set` calls it to predict a collision, so the
/// two cannot disagree. Uppercased because short names are, and sanitised because
/// an 8.3 field has no dots, spaces, or bytes above ASCII.
pub fn short_stem_83(name: &str) -> [u8; SHORT_STEM_LEN] {
    let mut stem = [b' '; SHORT_STEM_LEN];
    for (index, byte) in name.bytes().take(SHORT_STEM_LEN).enumerate() {
        stem[index] = short_name_char(byte);
    }
    stem
}

/// The name a payload appears under **on the USB volume**, written into `out`.
///
/// **ONE NAME FOR ONE THING.** A user who mounts the badge sees `HELLO.CWA`; a
/// menu that called the same payload `hello` would be a second name for it, and
/// "which of these is the file I dragged on?" is not a question a badge should
/// make anyone ask. So the picker shows what the filesystem shows.
///
/// Safe to use as an identifier precisely because [`check_set`] refuses a catalog
/// whose short names collide — the truncation is lossy, and enforced unique.
///
/// Returns the filled prefix of `out`, which needs 12 bytes: 8 + `.` + 3.
pub fn display_filename<'a>(name: &str, out: &'a mut [u8; 12]) -> &'a str {
    let stem = short_stem_83(name);
    // Trailing spaces are padding in a directory entry, not part of the name.
    let width = stem.iter().rposition(|b| *b != b' ').map_or(0, |i| i + 1);
    out[..width].copy_from_slice(&stem[..width]);
    out[width] = b'.';
    out[width + 1..width + 4].copy_from_slice(b"CWA");
    core::str::from_utf8(&out[..width + 4]).unwrap_or("")
}

/// Are these two names the same file on a case-insensitive host?
fn folds_together(a: &str, b: &str) -> bool {
    a.len() == b.len()
        && a.bytes()
            .zip(b.bytes())
            .all(|(x, y)| x.to_ascii_lowercase() == y.to_ascii_lowercase())
}

/// Check a SET of names for pairs that cannot coexist.
///
/// Returns the two offending indices and how they collide, so a caller can name
/// both — "these two collide" is actionable and "invalid" is not.
///
/// `O(n^2)`, deliberately: a catalog holds 16 entries and an app's directory
/// holds tens, so a hash set would cost allocation on a target that has none for
/// a saving nobody can measure.
pub fn check_set(names: &[&str]) -> Result<(), (usize, usize, Collision)> {
    for (i, a) in names.iter().enumerate() {
        for (j, b) in names.iter().enumerate().skip(i + 1) {
            // Case first: it is the stronger statement about the same pair, and
            // reporting "these differ only in case" is more useful than
            // "these truncate alike" when both are true.
            if folds_together(a, b) {
                return Err((i, j, Collision::CaseFold));
            }
            if short_stem_83(a) == short_stem_83(b) {
                return Err((i, j, Collision::ShortName));
            }
        }
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn the_menu_name_is_the_filesystem_name() {
        // The badge shows what a PC would show, so there is only ever one name
        // for a payload.
        let mut buf = [0u8; 12];
        assert_eq!(display_filename("hello", &mut buf), "HELLO.CWA");
        let mut buf = [0u8; 12];
        assert_eq!(display_filename("tictactoe", &mut buf), "TICTACTO.CWA");
        // Sanitisation applies here too — the same rule the volume renders with.
        let mut buf = [0u8; 12];
        assert_eq!(display_filename("a.b", &mut buf), "A_B.CWA");
    }

    #[test]
    fn distinct_names_do_not_collide() {
        assert_eq!(check_set(&["hello", "tictactoe", "notes"]), Ok(()));
        assert_eq!(check_set(&[]), Ok(()));
        assert_eq!(check_set(&["only"]), Ok(()));
    }

    #[test]
    fn truncation_collision_is_the_one_that_is_live() {
        // NOT HYPOTHETICAL. The badge renders a catalog name as its first eight
        // characters, so these two payloads appear on the USB volume as one file
        // and the second is unreachable. Nothing fails; a file is just missing.
        assert_eq!(
            check_set(&["board-state-1", "board-state-2"]),
            Err((0, 1, Collision::ShortName))
        );
        // Eight characters is the exact boundary, so it is worth pinning.
        assert_eq!(check_set(&["abcdefgh1", "abcdefgh2"]), Err((0, 1, Collision::ShortName)));
        assert_eq!(check_set(&["abcdefg1", "abcdefg2"]), Ok(()));
    }

    #[test]
    fn sanitisation_can_create_a_collision_that_the_names_did_not_have() {
        // Two DIFFERENT legal names becoming one short name because the bytes that
        // distinguished them were replaced. This is why the validator must call
        // the renderer's own function rather than compare the original strings.
        assert_eq!(check_set(&["a.b", "a_b"]), Err((0, 1, Collision::ShortName)));
    }

    #[test]
    fn case_folding_collides_where_a_profile_allows_case() {
        // The portable profile forbids uppercase, so this cannot arise there — it
        // is the trade an app makes by declaring the narrower `posix` world.
        assert_eq!(
            check_set(&["save.json", "Save.json"]),
            Err((0, 1, Collision::CaseFold))
        );
    }

    #[test]
    fn case_is_reported_in_preference_to_truncation() {
        // Both are true of this pair; "these differ only in case" is the more
        // useful thing to be told.
        assert_eq!(check_set(&["readme", "README"]), Err((0, 1, Collision::CaseFold)));
    }

    #[test]
    fn the_offending_pair_is_named() {
        // Indices rather than a bool, so a caller can print both names. "Invalid"
        // is not actionable; "these two collide" is.
        assert_eq!(
            check_set(&["alpha", "beta", "board-state-1", "gamma", "board-state-2"]),
            Err((2, 4, Collision::ShortName))
        );
    }

    #[test]
    fn ordinary_names_pass() {
        for name in ["hello", "save.json", "board-state", "a", "logs/day-1.txt", "x_9.dat"] {
            assert!(check_path(name).is_ok(), "{name} should be portable");
        }
    }

    #[test]
    fn uppercase_is_refused_because_fat_is_case_insensitive() {
        // THE RULE MOST LIKELY TO SURPRISE, and the one with the worst failure
        // mode: on a laptop these are two files, on the badge they are one, and
        // the app silently loses data.
        assert_eq!(check_path("Save.json"), Err(NameError::IllegalCharacter));
        assert_eq!(check_path("save.json"), Ok(()));
    }

    #[test]
    fn windows_device_names_are_refused() {
        // Only matters because the badge's files get read on a PC — invisible
        // until someone plugs it in, which is why it is a rule and not a comment.
        assert_eq!(check_path("con"), Err(NameError::ReservedDevice));
        assert_eq!(check_path("com1.txt"), Err(NameError::ReservedDevice));
        assert_eq!(check_path("console"), Ok(()), "only the exact stem is reserved");
    }

    #[test]
    fn traversal_and_absolute_paths_are_refused() {
        assert_eq!(check_path(".."), Err(NameError::ReservedTraversal));
        assert_eq!(check_path("a/../b"), Err(NameError::ReservedTraversal));
        assert_eq!(check_path("/etc/passwd"), Err(NameError::BadStructure));
        assert_eq!(check_path("a\\b"), Err(NameError::BadStructure));
        assert_eq!(check_path("a//b"), Err(NameError::BadStructure));
        assert_eq!(check_path("a/"), Err(NameError::BadStructure));
    }

    #[test]
    fn edges_windows_would_silently_strip_are_refused() {
        // Windows drops a trailing dot or space without telling anyone, so the
        // name that comes back is not the name that went in.
        assert_eq!(check_path("save."), Err(NameError::BadEdge));
        assert_eq!(check_path(".hidden"), Err(NameError::BadEdge));
        assert_eq!(check_path("-flag"), Err(NameError::BadEdge));
    }

    #[test]
    fn spaces_and_unicode_are_refused() {
        // Both are legal on POSIX and OPFS and neither survives 8.3, so they are
        // exactly the kind of name that works until it is read on the badge.
        assert_eq!(check_path("my save.json"), Err(NameError::IllegalCharacter));
        assert_eq!(check_path("café.txt"), Err(NameError::IllegalCharacter));
    }

    #[test]
    fn the_payload_name_that_started_this() {
        // `hello.pulley32` mounted as HELLO.PU.CWA. It is a legal PATH — the
        // profile does not ban dots — so the fix is not a stricter rule here but
        // the 8.3 sanitiser in fatview, and this test records which is which.
        assert_eq!(check_path("hello.pulley32"), Ok(()));
    }

    #[test]
    fn length_limits_are_enforced_at_both_levels() {
        let long = "a".repeat(COMPONENT_MAX + 1);
        assert_eq!(check_path(&long), Err(NameError::TooLong));

        let deep = core::iter::repeat("ab").take(100).collect::<Vec<_>>().join("/");
        assert_eq!(check_path(&deep), Err(NameError::TooLong));
    }
}

/// **THE SHARED SPECIFICATION, read by this implementation and by Go's.**
///
/// Apps are Go, this host is Rust, the browser host is JS — no library spans
/// them, so "consistent across all worlds" has to mean one file that every
/// implementation is tested against. `dlc-platform/names_test.go` reads the same
/// rows. Neither side is the authority; the file is.
///
/// A new rule goes in the file FIRST, and both implementations go red until they
/// agree. Two validators that agree today drift silently, and the drift shows up
/// as an app that writes a name one tier can read and another mangles.
#[cfg(test)]
mod shared_spec {
    use super::*;
    use alloc::string::ToString;

    const VECTORS: &str = include_str!("../../names/VECTORS.tsv");
    const COLLISIONS: &str = include_str!("../../names/COLLISIONS.tsv");
    const WORLDS: &str = include_str!("../../names/WORLDS.tsv");

    fn kind_name(error: NameError) -> &'static str {
        match error {
            NameError::Empty => "empty",
            NameError::TooLong => "too-long",
            NameError::IllegalCharacter => "illegal-character",
            NameError::ReservedTraversal => "reserved-traversal",
            NameError::ReservedDevice => "reserved-device",
            NameError::BadEdge => "bad-edge",
            NameError::BadStructure => "bad-structure",
        }
    }

    #[test]
    fn every_shared_vector_agrees() {
        let mut checked = 0;
        for (number, line) in VECTORS.lines().enumerate() {
            if line.starts_with('#') || !line.contains('\t') {
                continue;
            }
            let (path, want) = line.split_once('\t').expect("checked above");
            let want = want.trim();

            match (check_path(path), want) {
                (Ok(()), "ok") => {}
                (Err(e), expected) if kind_name(e) == expected => {}
                (got, expected) => panic!(
                    "line {}: {path:?} -> {}, the shared spec says {expected}",
                    number + 1,
                    match got {
                        Ok(()) => "ok".to_string(),
                        Err(e) => kind_name(e).to_string(),
                    }
                ),
            }
            checked += 1;
        }
        // A spec file that silently stopped being read would make this pass while
        // checking nothing.
        assert!(checked >= 30, "only {checked} vectors checked");
    }

    #[test]
    fn every_shared_collision_agrees() {
        let mut checked = 0;
        for (number, line) in COLLISIONS.lines().enumerate() {
            if line.starts_with('#') || !line.contains('\t') {
                continue;
            }
            let (names, want) = line.split_once('\t').expect("checked above");
            let want = want.trim();
            let set: Vec<&str> = names.split_whitespace().collect();

            let got = match check_set(&set) {
                Ok(()) => "ok",
                Err((_, _, Collision::CaseFold)) => "case-fold",
                Err((_, _, Collision::ShortName)) => "short-name",
            };
            assert_eq!(
                got,
                want,
                "line {}: {set:?} -> {got}, the shared spec says {want}",
                number + 1
            );
            checked += 1;
        }
        assert!(checked >= 10, "only {checked} collision vectors checked");
    }

    #[test]
    fn the_world_registry_is_understood() {
        // The badge only implements two of these worlds, but it must RECOGNISE
        // every id — an unrecognised one is `unknown`, which fails closed, and
        // that only works if the registry is the thing being read.
        let mut saw_undefined = false;
        let mut saw_unknown = false;
        let mut worlds = 0;
        for line in WORLDS.lines() {
            if line.starts_with('#') || !line.contains('\t') {
                continue;
            }
            let (id, _) = line.split_once('\t').expect("checked above");
            match id {
                "undefined" => saw_undefined = true,
                "unknown" => saw_unknown = true,
                _ => worlds += 1,
            }
        }
        assert!(saw_undefined && saw_unknown, "both non-worlds must be in the registry");
        assert!(worlds >= 4, "the registry should name the real worlds");
    }
}
