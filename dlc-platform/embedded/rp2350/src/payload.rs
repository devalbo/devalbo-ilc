//! What this badge can run: the built-in default, plus whatever is in flash.
//!
//! THE BOARD'S HALF of `dlc_platform_embedded::catalog`. That module knows the
//! format and names no chip; this one knows where the region physically is and
//! owns the single `unsafe` that turns an address into a slice.
//!
//! # Adding an app without a toolchain
//!
//! The point of the region, and the reason it beats `include_bytes!`: hold BOOT,
//! tap RESET, and drag a payload UF2 onto the `RP2350` drive. The bootloader
//! writes each UF2 block to the flash address that block names, so a payload UF2
//! built for [`board::PAYLOAD_BASE`] lands in the region and **leaves the
//! firmware alone** — different sectors entirely. Reset, and the scan below finds
//! it.
//!
//! **What you drag must be a UF2.** The BOOTSEL drive is a synthetic FAT12
//! volume, not storage: its only real operation is parsing UF2 blocks. A `.cwasm`
//! dropped on it is accepted by the Finder and discarded by the bootloader, with
//! no error anywhere. `make badge-payload` produces the UF2; see the README.

use dlc_platform_embedded::catalog::{self, Payload, MAX_ENTRIES};

// Only `region()` needs it, and that is gated the same way — so with the region
// off this import is unused. Newly visible: `BADGE_REGION=off` had never actually
// been built until the payload path in check-embedded.sh was fixed.
#[cfg(payload_region)]
use crate::board;

/// 16-byte alignment for the built-in payload, which `deserialize_raw` requires
/// and `include_bytes!` does not provide — it promises 1. Catalog entries get
/// theirs from the format instead (a 4 KB-aligned slot plus a 64-byte header).
#[cfg(has_builtin_payload)]
#[repr(C, align(16))]
struct Aligned<T: ?Sized>(T);

/// The payload compiled into this firmware — **empty unless `BADGE_PAYLOAD` was
/// set at build time**, which is the "empty loader" configuration.
///
/// `OUT_DIR` rather than a path in the tree: `build.rs` always writes this file,
/// so the include always resolves and a fresh clone builds with no setup step.
#[cfg(has_builtin_payload)]
static BUILTIN: &Aligned<[u8]> =
    &Aligned(*include_bytes!(concat!(env!("OUT_DIR"), "/default.cwasm")));

/// Everything runnable, in the order a menu should show it.
///
/// A fixed array and no allocation, because this runs BEFORE the heap is known
/// good on a bring-up board — an allocation here would fault with nothing
/// printed, which is the failure mode this whole firmware is arranged to avoid.
pub struct Payloads {
    slots: [Option<Payload>; MAX_ENTRIES + 1],
    count: usize,
}

impl Payloads {
    pub fn len(&self) -> usize {
        self.count
    }

    pub fn is_empty(&self) -> bool {
        self.count == 0
    }

    pub fn get(&self, index: usize) -> Option<Payload> {
        self.slots.get(index).copied().flatten()
    }

    /// For printing the menu now, and drawing it on the TFT in Phase 3.
    pub fn iter(&self) -> impl Iterator<Item = Payload> + '_ {
        self.slots[..self.count].iter().filter_map(|s| *s)
    }

    /// **Which app runs when nobody chooses.** The marked default if there is
    /// one, else the first that works — see `catalog::FLAG_DEFAULT` for why this
    /// is not simply slot 0.
    pub fn default_choice(&self) -> Option<(usize, Payload)> {
        self.slots[..self.count]
            .iter()
            .enumerate()
            .filter_map(|(i, s)| s.map(|p| (i, p)))
            .find(|(_, p)| p.runnable() && p.is_default())
            .or_else(|| {
                self.slots[..self.count]
                    .iter()
                    .enumerate()
                    .filter_map(|(i, s)| s.map(|p| (i, p)))
                    .find(|(_, p)| p.runnable())
            })
    }
}

/// Find everything this badge could run.
///
/// The built-in comes FIRST when there is one, so a badge flashed with a default
/// boots into it even after payloads are dragged onto the region — dropping a
/// file changes what is *available*, never what is *default*. A selector picks
/// anything else by index.
pub fn discover() -> Payloads {
    let mut slots = [None; MAX_ENTRIES + 1];
    let mut count = 0usize;

    // An empty built-in is the explicit "no default" case, not a zero-length app.
    #[cfg(has_builtin_payload)]
    if !BUILTIN.0.is_empty() {
        slots[count] = Some(Payload {
            name: "built-in",
            bytes: &BUILTIN.0,
            // No header to read one from, so the convention applies: the first id
            // in the app's band. A baked-in payload whose entry is elsewhere wants
            // the region, which carries the id per app.
            entry_method: catalog::DEFAULT_ENTRY_METHOD,
            // VERIFIED BY CONSTRUCTION. This payload is part of the firmware
            // image, so it cannot rot independently of the code reading it — a
            // corrupt one means a corrupt firmware, which fails earlier and
            // louder than a checksum here would.
            integrity: catalog::Integrity::Verified,
            // A BAKED-IN PAYLOAD IS THE DEFAULT, by definition: the firmware was
            // built to run it. Marking it means the unattended choice stays this
            // one even after apps are dragged onto the region.
            flags: catalog::FLAG_DEFAULT,
        });
        count += 1;
    }

    #[cfg(payload_region)]
    for found in catalog::scan(region()).iter() {
        slots[count] = Some(found);
        count += 1;
    }

    Payloads { slots, count }
}

/// The catalog region, as bytes.
///
/// # Safety, and it is the only `unsafe` in this file
///
/// Sound because of three facts that are checked elsewhere and stated here:
///
/// 1. **The region is mapped.** XIP presents flash at `0x10000000`, and
///    `PAYLOAD_BASE + PAYLOAD_LEN` is exactly the end of this part's 16 MB —
///    the PSRAM window begins where this ends, so nothing here reads past it.
/// 2. **Nothing else may write it.** `memory.x` caps FLASH at 1 MB, so the
///    linker cannot place firmware here; it would fail to link instead.
/// 3. **`'static` is honest.** This is memory-mapped flash that outlives every
///    component, which is exactly what `deserialize_raw` asks of a payload.
///
/// The contents are NOT trusted to be well-formed — `catalog::scan` bounds-checks
/// everything against this slice, so a half-written drag produces fewer entries
/// rather than a fault.
#[cfg(payload_region)]
fn region() -> &'static [u8] {
    // SAFETY: the three facts above.
    unsafe { core::slice::from_raw_parts(board::PAYLOAD_BASE as *const u8, board::PAYLOAD_LEN) }
}

/// What the banner prints, so the serial log says which firmware this is.
pub const MODE: &str = if cfg!(has_builtin_payload) {
    if cfg!(payload_region) {
        "built-in default + flash region"
    } else {
        "built-in only (region disabled)"
    }
} else if cfg!(payload_region) {
    "loader — flash region only"
} else {
    "NONE — no built-in, no region"
};
