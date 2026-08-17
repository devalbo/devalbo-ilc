//! The last thing an app said it was doing (BADGE-CONTROL-PLAN D3a).
//!
//! # Why a global rather than store state
//!
//! It is read by the CONTROL CHANNEL, which runs from a USB interrupt and has no
//! access to a `Store` — and it is written from an `emit` import, which runs
//! inside a guest call. Those two contexts share nothing except statics.
//!
//! That is also the point: the value has to be readable WHILE the guest holds
//! the store, because "what is it doing?" is a question asked about a command
//! that has not returned. Anything reachable only between commands would answer
//! only when the answer no longer matters.
//!
//! # Why it is truncated rather than allocated
//!
//! A fixed buffer keeps this callable from an interrupt and from a guest call
//! without touching the allocator — the heap belongs to the guest, and an
//! activity update during a command must not perturb it. 64 characters is more
//! than "tick 3 of 5" needs and less than any tier would render.

use core::sync::atomic::{AtomicUsize, Ordering};

/// The longest activity string kept. Longer ones are truncated rather than
/// refused: a clipped label still says more than none.
const MAX: usize = 64;

static mut TEXT: [u8; MAX] = [0; MAX];

/// Published length. `Release` after the bytes, `Acquire` before them — the same
/// discipline the log buffer uses, and for the same reason: the reader is an
/// interrupt that can land between any two instructions.
static LEN: AtomicUsize = AtomicUsize::new(0);

/// Record what the app is doing.
pub fn set(what: &[u8]) {
    let n = what.len().min(MAX);
    // SAFETY: single core. The only writer is a guest's `emit`, and the only
    // reader publishes through LEN, so a reader never sees bytes this has not
    // finished writing.
    let text = unsafe { &mut *core::ptr::addr_of_mut!(TEXT) };
    text[..n].copy_from_slice(&what[..n]);
    LEN.store(n, Ordering::Release);
}

/// What the app last said, or empty.
pub fn get() -> &'static str {
    let len = LEN.load(Ordering::Acquire);
    // SAFETY: `len` was published after those bytes were written, so this reads
    // only initialised memory.
    let text = unsafe { &*core::ptr::addr_of!(TEXT) };
    core::str::from_utf8(&text[..len]).unwrap_or("")
}

/// Forget it. Called when an app is torn down, so a stale label does not outlive
/// the app that set it — a world reporting the previous app's activity is worse
/// than one reporting none.
pub fn clear() {
    LEN.store(0, Ordering::Release);
}

#[cfg(test)]
mod tests {
    use super::*;

    /// ONE TEST, because the subject is a GLOBAL and `cargo test` runs tests in
    /// parallel threads. Three separate tests raced each other and failed at
    /// random, which is worse than no test: an intermittent red trains people to
    /// re-run rather than to look.
    ///
    /// The alternative — a mutex around a static this crate uses from an
    /// interrupt — would add synchronisation to the firmware to serve the tests.
    #[test]
    fn records_truncates_and_clears() {
        set(b"tick 3 of 5");
        assert_eq!(get(), "tick 3 of 5");

        // A LONG LABEL IS CLIPPED, not refused: it still says more than nothing,
        // and refusing would make diagnostics depend on prose length.
        let long = [b'x'; MAX * 2];
        set(&long);
        assert_eq!(get().len(), MAX);

        // And clipping must not yield invalid UTF-8, which `get` would report as
        // empty — losing the whole label to save a byte.
        set(b"counting down from sixty, slowly, with some detail attached here");
        assert!(!get().is_empty());

        clear();
        assert_eq!(get(), "");
    }
}
