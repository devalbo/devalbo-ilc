//! Pack `.cwasm` files into a catalog image for the badge's well-known region.
//!
//! ```text
//! cargo run --bin payload-image -- <out.bin> <name>=<file.cwasm> [<name>=<file> ...]
//! ```
//!
//! The OTHER HALF of `catalog.rs`, and deliberately in the same crate: a format
//! whose writer and reader live in different repositories drifts, and the drift
//! shows up as a badge that reads garbage. Here, `catalog::scan`'s tests build
//! their fixtures the way this tool does, and both use the same constants.
//!
//! What comes out is a raw image to be flashed at the region's base address —
//! `picotool uf2 convert -t bin -o <base>` turns it into something flashable.
//! It is NOT a UF2 itself, because the base address is the board's business and
//! this tool names no chip.
use dlc_platform_embedded::names;
use dlc_platform_embedded::catalog;
use dlc_platform_embedded::catalog::{
    DEFAULT_ENTRY_METHOD, HEADER_LEN, MAGIC, MAX_ENTRIES, NAME_MAX, SLOT_ALIGN,
};

fn main() -> std::io::Result<()> {
    let mut args = std::env::args().skip(1);
    let Some(output) = args.next() else {
        eprintln!("usage: payload-image <out.bin> <name>=<file.cwasm> ...");
        std::process::exit(2);
    };

    let entries: Vec<String> = args.collect();
    if entries.is_empty() {
        eprintln!("usage: payload-image <out.bin> <name>=<file.cwasm> ...");
        std::process::exit(2);
    }
    // The scanner stops at MAX_ENTRIES, so writing more would produce an image
    // whose tail is silently unreachable.
    if entries.len() > MAX_ENTRIES {
        eprintln!("too many payloads: {} (the catalog scans {MAX_ENTRIES})", entries.len());
        std::process::exit(2);
    }

    // COLLISION CHECK BEFORE ANY BYTES ARE WRITTEN. Two payloads whose names
    // truncate alike render as ONE file on the badge's USB volume, and the second
    // is unreachable — nothing fails, a payload is simply missing. Caught here
    // because this is the last point a human is present to retype an argument.
    let names: Vec<&str> = entries
        .iter()
        .map(|entry| {
            let (label, _) = entry.split_once('=').unwrap_or((entry.as_str(), ""));
            label.split_once('@').map(|(n, _)| n).unwrap_or(label)
        })
        .collect();
    if let Err((a, b, kind)) = names::check_set(&names) {
        eprintln!(
            "payload names {:?} and {:?} collide ({}) — they would be one file on the badge",
            names[a],
            names[b],
            match kind {
                names::Collision::CaseFold => "differ only in case",
                names::Collision::ShortName => "same first 8 characters, once shortened for FAT",
            }
        );
        std::process::exit(2);
    }

    // `DEFAULT=<name>` picks the unattended app. Read from the environment rather
    // than argv so it cannot be confused with a payload argument.
    let default_name = std::env::var("DEFAULT").ok().filter(|v| !v.trim().is_empty());
    if let Some(want) = default_name.as_deref() {
        if !names.contains(&want) {
            eprintln!("DEFAULT={want:?} names no payload in this image: {names:?}");
            std::process::exit(2);
        }
    }

    let mut image: Vec<u8> = Vec::new();
    for (index, entry) in entries.iter().enumerate() {
        let (label, path) = entry
            .split_once('=')
            .unwrap_or_else(|| panic!("expected <name>[@<method>]=<file>, got {entry:?}"));
        // `<name>@<method>` carries the app's entry point INTO the image, which is
        // what lets one loader firmware run apps it was not built for — Decision
        // 31 gives every component a single `execute(u32, ...)`, so running one
        // means knowing its id. Omitted means the app band's first id.
        let (name, method) = match label.split_once('@') {
            Some((name, id)) => (
                name,
                id.parse::<u32>()
                    .unwrap_or_else(|_| panic!("{id:?} is not a method id")),
            ),
            None => (label, DEFAULT_ENTRY_METHOD),
        };
        // Refused rather than truncated: a name is what a menu shows and what a
        // person types, so silently shortening it makes the badge disagree with
        // the command that built it.
        assert!(
            name.len() <= NAME_MAX,
            "name {name:?} is {} bytes; the header holds {NAME_MAX}",
            name.len()
        );
        // THE PORTABLE PROFILE, ENFORCED HERE because this is the last place a
        // human is present. A payload name becomes a filename on a USB drive read
        // on Windows, macOS and Linux, and the badge cannot refuse it later — it
        // can only mangle it, which is the failure this profile exists to stop.
        // Rejecting now costs a retyped argument; rejecting never costs a file
        // whose name is not the one anybody chose.
        if let Err(why) = names::check_component(name) {
            eprintln!("payload name {name:?} is not portable: {why}");
            std::process::exit(2);
        }
        let payload = std::fs::read(path)?;

        let mut header = vec![0u8; HEADER_LEN];
        header[..8].copy_from_slice(&MAGIC);
        header[8..12].copy_from_slice(&(payload.len() as u32).to_le_bytes());
        header[12..16].copy_from_slice(&(name.len() as u32).to_le_bytes());
        header[16..16 + name.len()].copy_from_slice(name.as_bytes());
        header[48..52].copy_from_slice(&method.to_le_bytes());
        // Recorded here so the BADGE can tell a truncated drag from a bad build.
        // Cheap to write, and the only way corruption becomes a named condition
        // rather than a confusing failure at instantiation.
        header[52..56].copy_from_slice(&catalog::checksum(&payload).to_le_bytes());
        // WHICH ONE RUNS UNATTENDED, marked rather than positional. `DEFAULT=`
        // names it; with none given, the first entry is marked so a single-app
        // image needs no ceremony and the intent is still recorded in the image.
        let mut flags = 0u32;
        if default_name.as_deref() == Some(name) || (default_name.is_none() && index == 0) {
            flags |= catalog::FLAG_DEFAULT;
        }
        header[56..60].copy_from_slice(&flags.to_le_bytes());
        // Bytes 52..64 stay zero — the reserved field. A hash goes here the day
        // the badge wants to verify a payload it did not just receive.

        image.extend_from_slice(&header);
        image.extend_from_slice(&payload);
        // Pad to the next slot. The LAST entry is padded too, so that appending
        // another image to this one produces a valid catalog — which is how a
        // second app gets added without rebuilding the first.
        image.resize(image.len().div_ceil(SLOT_ALIGN) * SLOT_ALIGN, 0);

        println!(
            "  {name}: {} bytes from {path} (execute {method}){}",
            payload.len(),
            if flags & catalog::FLAG_DEFAULT != 0 { "  [default]" } else { "" }
        );
    }

    std::fs::write(&output, &image)?;
    println!(
        "{output}: {} payloads, {} bytes ({} KB)",
        entries.len(),
        image.len(),
        image.len() / 1024
    );
    Ok(())
}
