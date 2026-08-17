//! What this badge can run, and which of it was chosen.
//!
//! # Why a registry rather than passing the catalog around
//!
//! `payload::discover()` returns a `Payloads` that lives on main's stack for the
//! whole run. The control channel answers from the USB INTERRUPT and has no way
//! to reach a stack frame — so the facts a client asks about are published here
//! once, at the moment they are discovered, and read from there.
//!
//! It holds `catalog::Payload` values rather than a copy of their fields. They
//! are `Copy` and every reference in them is `'static` (a name in flash, bytes in
//! flash), so this is a snapshot that cannot go stale or dangle — the entries
//! genuinely are static for the life of the board.
//!
//! # The pending selection
//!
//! `VERB_SELECT_PAYLOAD` cannot interrupt a running app, because a menu only
//! exists between sessions. So a selection is REMEMBERED and consumed by the next
//! menu, which then resolves to it immediately instead of waiting out its
//! timeout.
//!
//! That is worth stating plainly because it makes the verb asynchronous in a way
//! a caller can misread: the reply means "noted", not "running". A caller that
//! wants to know an app is up asks `VERB_GET_WORLD_STATE` afterwards, which is
//! the question that actually means that.

use dlc_platform_embedded::catalog::{Integrity, Payload, MAX_ENTRIES};

use crate::shared::Shared;

struct Registry {
    slots: [Option<Payload>; MAX_ENTRIES + 1],
    count: usize,
    /// The entry the world last ran.
    selected: usize,
    /// An index a client asked for, not yet acted on.
    pending: Option<usize>,
}

static REGISTRY: Shared<Registry> = Shared::new(Registry {
    slots: [None; MAX_ENTRIES + 1],
    count: 0,
    selected: 0,
    pending: None,
});

/// Publish what was found. Called once, after `discover`.
pub fn publish(payloads: &crate::payload::Payloads) {
    REGISTRY.with(|registry| {
        registry.count = payloads.len().min(registry.slots.len());
        for index in 0..registry.count {
            registry.slots[index] = payloads.get(index);
        }
    });
}

/// Record which entry is being run.
pub fn selected(index: usize) {
    REGISTRY.with(|registry| registry.selected = index);
}

/// Ask for an entry by index. False if there is no such entry.
///
/// VALIDATED HERE, against what was actually discovered, so a client learns
/// immediately that index 4 does not exist rather than watching a menu resolve
/// to something else and having to work out why.
pub fn request(index: usize) -> bool {
    REGISTRY.with(|registry| {
        if index >= registry.count {
            return false;
        }
        registry.pending = Some(index);
        true
    })
}

/// Take any requested entry. The menu calls this instead of waiting.
pub fn take_request() -> Option<usize> {
    REGISTRY.with(|registry| registry.pending.take())
}

/// Hand each entry to `f`. Returns false if the registry was busy.
///
/// A CALLBACK for the same reason the screen mirror uses one: the caller is the
/// USB interrupt encoding a reply, and it has nowhere to put a copy.
pub fn each(mut f: impl FnMut(usize, &Payload)) -> Option<usize> {
    REGISTRY.try_with(|registry| {
        for index in 0..registry.count {
            if let Some(payload) = registry.slots[index].as_ref() {
                f(index, payload);
            }
        }
        registry.selected
    })
}

/// `Integrity` in control.proto.
///
/// WRITTEN OUT, not cast. This enum's discriminants are a local detail and the
/// proto numbers are a wire contract; a `as u32` here would silently re-map every
/// value the day somebody reorders the Rust enum.
pub const fn integrity_code(integrity: Integrity) -> u32 {
    match integrity {
        Integrity::Verified => 1,
        Integrity::Unverified => 2,
        Integrity::Corrupt => 3,
        Integrity::WrongEngine => 4,
    }
}
