//! Presses that did not come from a finger (BADGE-CONTROL-PLAN D3).
//!
//! # Why a queue and not a flag
//!
//! Every widget here debounces by edge: it remembers whether a button was down
//! last time it looked, and acts when it was not and now is. Debouncing is a
//! world-level concern and stays one — so an injected press has to produce an
//! EDGE, not a level, or it either does nothing or repeats forever.
//!
//! A flag that a client sets and clears cannot do that reliably. Set it and
//! clear it quickly and the widget's 40 ms poll misses it entirely; hold it and
//! the widget sees one press and then a stuck button. Both failures depend on
//! timing the client cannot see.
//!
//! So a press is a LATCH THAT THE FIRST READER CONSUMES. `taken` clears the bit
//! and reports whether it was set, which means exactly one poll observes the
//! button down and the next observes it up. One press, one edge, whatever the
//! poll rate, with no timing agreement between the two sides.
//!
//! # Why it merges with the pin rather than replacing it
//!
//! A widget reads `pin.is_low() || buttons::taken(...)`. The badge stays usable
//! by hand while a client is driving it, and — more importantly — an app cannot
//! tell the two apart, which is D6's rule: driving a world must be
//! indistinguishable from using it, or a test proves something about the harness
//! instead of about the app.
//!
//! # What it is not
//!
//! Not a press-and-hold, and not a chord. Both are expressible — a duration
//! field, a bitmask — and neither has a caller yet, so neither is here (D6's
//! rule about capabilities without consumers).

use core::sync::atomic::{AtomicU32, Ordering};

use dlc_platform_embedded::control;

/// One bit per button, set by the control channel and cleared by whoever looks.
///
/// AN ATOMIC RATHER THAN A `Shared`: the control channel writes from the USB
/// interrupt and widgets read from main, and the whole operation is a
/// test-and-clear that the hardware does in one instruction. There is no
/// invariant spanning two fields here, which is the thing `Shared` exists to
/// protect.
static PENDING: AtomicU32 = AtomicU32::new(0);

/// Queue a press. Returns false if the world has no such button.
///
/// REFUSING IS PART OF THE CONTRACT. A caller built against a newer proto that
/// asks for a button this board does not have gets told so, rather than having
/// its press silently dropped and waiting for an effect that will never come.
pub fn press(button: u32) -> bool {
    if button == 0 || button > control::BUTTON_MAX {
        return false;
    }
    PENDING.fetch_or(1 << button, Ordering::Relaxed);
    true
}

/// Whether a press is waiting for this button, consuming it if so.
pub fn taken(button: u32) -> bool {
    let bit = 1 << button;
    PENDING.fetch_and(!bit, Ordering::Relaxed) & bit != 0
}
