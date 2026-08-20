//! WHICH BUILD OF THE APP IS RUNNING — asked once, and kept for as long as it is.
//!
//! # Why the world tracks this at all
//!
//! Because the world is the only thing that can. A loader runs payloads it was
//! never built for, so "which app" is already a fact it discovers rather than
//! knows — and "which BUILD of that app" is the half that makes a report
//! reproducible. `hello 0.1.0` and `hello 0.2.0` are the same catalog entry,
//! the same size band, and different software.
//!
//! The firmware has the same problem one layer down and solves it the same way
//! (`WorldState.build_id`): a version that never changes cannot answer "is this
//! running what I just built", and that is the question that costs afternoons.
//!
//! # Why it is read once rather than per request
//!
//! An app's version is fixed for the life of an instance — it is compiled in.
//! Asking again per `GetWorldState` would spend a guest call on an answer that
//! cannot have changed, from an interrupt that must not enter the store.
//!
//! # Why a global, and why truncated
//!
//! Identical reasoning to `activity`: the reader is the control channel, running
//! from a USB interrupt with no access to a `Store`; the writer is the session
//! phase, running with the guest suspended. Statics are the only thing those two
//! contexts share, and a fixed buffer keeps both sides off the allocator — the
//! heap belongs to the guest.

use core::sync::atomic::{AtomicUsize, Ordering};

/// The longest version string kept. A semver with a build tag fits comfortably;
/// anything longer is truncated rather than refused, because a clipped version
/// still identifies more than none.
const MAX: usize = 48;

static mut TEXT: [u8; MAX] = [0; MAX];

/// Published length. `Release` after the bytes, `Acquire` before them — the same
/// discipline `activity` uses, and for the same reason: the reader is an
/// interrupt that can land between any two instructions.
static LEN: AtomicUsize = AtomicUsize::new(0);

/// Record what the running app calls itself.
pub fn set(version: &[u8]) {
    let n = version.len().min(MAX);
    // SAFETY: single core. Written by the session phase with the guest
    // suspended; read through LEN, so a reader never sees bytes this has not
    // finished writing.
    let text = unsafe { &mut *core::ptr::addr_of_mut!(TEXT) };
    text[..n].copy_from_slice(&version[..n]);
    LEN.store(n, Ordering::Release);
}

/// What the running app calls itself, or empty when nothing is running or the
/// app did not say.
pub fn get() -> &'static str {
    let len = LEN.load(Ordering::Acquire);
    // SAFETY: `len` was published after those bytes were written.
    let text = unsafe { &*core::ptr::addr_of!(TEXT) };
    core::str::from_utf8(&text[..len]).unwrap_or("")
}

/// Forget it, when the instance goes.
///
/// A WORLD REPORTING THE PREVIOUS APP'S VERSION IS WORSE THAN ONE REPORTING
/// NONE — the same rule `activity::clear` exists for. An empty answer says "no
/// instance"; a stale one says something false about the instance that is
/// running.
pub fn clear() {
    LEN.store(0, Ordering::Release);
}
