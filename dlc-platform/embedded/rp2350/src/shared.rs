//! State shared between main and an interrupt, made safe by construction.
//!
//! # The three bugs this exists to retire
//!
//! All on 2026-08-17, all the same shape, all written by someone who knew
//! better and wrote a comment instead of a guarantee:
//!
//! | | shared thing | the two contexts | what it looked like |
//! | --- | --- | --- | --- |
//! | 1 | the log buffer | the report held `&mut`, the stdout echo took a second one | the log froze mid-boot and the badge looked hung |
//! | 2 | the outbound queue | `answer()` in the interrupt, `log_frame()` in main | both channels went silent |
//! | 3 | the USB device | `service()` from the interrupt AND from `pump()` | output stopped the instant a widget pumped |
//!
//! Every one was a `static mut`, reached through `addr_of_mut!`, with a comment
//! asserting the two callers could not overlap. Every assertion was false: an
//! interrupt preempts main wherever main happens to be, including halfway
//! through a `usb-device` call that is not reentrant.
//!
//! The problem is not carelessness. It is that the ad-hoc form makes the UNSAFE
//! thing easy — `addr_of_mut!` is three words — and the safe thing effortful. So
//! this makes the safe thing the only one available.
//!
//! # What it guarantees
//!
//! - **A critical section around every access.** The only way to reach the value
//!   is [`Shared::with`], which masks interrupts for the duration. Bugs 2 and 3
//!   cannot be written.
//! - **No two live borrows.** A reentrant `with` on the same cell PANICS instead
//!   of handing out a second `&mut`. Bug 1 becomes a loud failure at the moment
//!   it happens rather than corruption discovered an hour later.
//!
//! # What it costs
//!
//! Interrupts are masked for the body of each `with`. Those bodies are short —
//! a buffer append, a queue push, a USB poll — so the delay is well under a USB
//! frame and no host notices. **Do not put a screen repaint inside one**: a
//! full-frame draw is 153,600 bytes on a bit-banged bus, and masking interrupts
//! for that long would stall USB service badly enough to drop the connection.
//!
//! # Why not `critical_section::Mutex<RefCell<T>>`
//!
//! That is the idiomatic answer and it requires `T: Send`. The USB device, the
//! serial port and the display are not, and making them so would mean unsafe
//! impls asserting exactly the property this type is supposed to ENFORCE. This
//! is the same guarantee with the unsafe confined to one place that is read
//! carefully rather than sprinkled across the callers.
//!
//! It also avoids a dependency: `cortex_m::interrupt::free` is already here and
//! masks interrupts on this core, which is what the guarantee needs.

use core::cell::{Cell, UnsafeCell};

/// Set once a panic is being reported, and never cleared.
///
/// # Why every `Shared` has to know about panicking
///
/// `with` PANICS on a reentrant borrow, which is right during normal operation:
/// it names a real bug at the moment it happens. It is exactly wrong inside a
/// panic handler.
///
/// The handler's job is to REPORT, and reporting means writing to the log, which
/// frames a notice, which takes the notice cell. If the original panic happened
/// while that cell was borrowed — quite likely, since the panic probably came
/// from inside a `with` — the handler panics too. A panic inside a panic aborts
/// hard and says nothing, which is how a badge that had just been taught to
/// explain itself stayed silent anyway.
///
/// So during a panic, `with` SEIZES instead of refusing. The safety argument is
/// the one `seize` already rests on: a panic handler never returns, so no
/// abandoned borrow will resume.
pub static PANICKING: core::sync::atomic::AtomicBool = core::sync::atomic::AtomicBool::new(false);

/// A value reachable only inside a critical section.
pub struct Shared<T> {
    value: UnsafeCell<T>,
    /// Whether a borrow is live, to catch reentrancy.
    ///
    /// A `Cell` rather than an atomic because it is only ever touched with
    /// interrupts masked on a single core — the very condition that makes the
    /// simpler type correct here.
    borrowed: Cell<bool>,
}

// SAFETY: every access goes through `with`, which masks interrupts. On a single
// core with no threads, that is mutual exclusion — nothing else can be running.
unsafe impl<T> Sync for Shared<T> {}

impl<T> Shared<T> {
    pub const fn new(value: T) -> Self {
        Self {
            value: UnsafeCell::new(value),
            borrowed: Cell::new(false),
        }
    }

    /// Run `f` with exclusive access.
    ///
    /// PANICS on a reentrant call for the same cell. That is a programming error
    /// — two live `&mut` to one value — and panicking names it where it happens.
    /// The alternative is what bug 1 did: hand out the second borrow, corrupt a
    /// published length, and present as a frozen log an hour later.
    /// `#[track_caller]` so the panic names the CALL SITE, not this file.
    ///
    /// The first report of this said `at src/shared.rs:108` — the line the
    /// `panic!` is written on, which is the same line for every cell and every
    /// caller in the firmware. True, and useless: it identifies the guard rather
    /// than what tripped it.
    #[track_caller]
    pub fn with<R>(&self, f: impl FnOnce(&mut T) -> R) -> R {
        // CAPTURED OUT HERE, because `#[track_caller]` does not cross a closure
        // boundary: the `panic!` below lives inside the one handed to
        // `interrupt::free`, which is its own function as far as the attribute is
        // concerned. The first attempt at this reported `shared.rs` either way —
        // the guard's own line, identical for every cell and every caller.
        let caller = core::panic::Location::caller();
        cortex_m::interrupt::free(|_| {
            if self.borrowed.get() {
                // A PANIC IS ALREADY IN PROGRESS: the handler is trying to say
                // why, and refusing it a borrow buys nothing. See `PANICKING`.
                if !PANICKING.load(core::sync::atomic::Ordering::Relaxed) {
                    panic!("shared value borrowed twice, taken again at {caller}");
                }
            }
            self.borrowed.set(true);
            // SAFETY: interrupts are masked and `borrowed` proves no other
            // borrow is live, so this reference is unique for its lifetime.
            let result = f(unsafe { &mut *self.value.get() });
            self.borrowed.set(false);
            result
        })
    }

    /// Hand the value over unconditionally, abandoning any live borrow.
    ///
    /// # Only for a panic handler
    ///
    /// A panic leaves whatever it interrupted half-done, including a `with` that
    /// never reached its `borrowed.set(false)`. From then on `try_with` answers
    /// `None` forever and `with` panics again — so the one piece of code that
    /// most needs the USB device, the handler trying to REPORT the panic, is the
    /// one piece that cannot have it.
    ///
    /// Sound exactly where nothing else can be running: a `#[panic_handler]`
    /// never returns, so no abandoned borrow will resume. Calling it anywhere
    /// else hands out a second `&mut` and is undefined behaviour.
    ///
    /// # Safety
    ///
    /// The caller must guarantee no other borrow will ever be used again — in
    /// practice, that the program is on its way to a halt.
    pub unsafe fn seize<R>(&self, f: impl FnOnce(&mut T) -> R) -> R {
        self.borrowed.set(true);
        // SAFETY: the caller has promised nothing else will resume.
        f(unsafe { &mut *self.value.get() })
    }

    /// Run `f` only if no borrow is live, returning `None` otherwise.
    ///
    /// For a caller that would rather SKIP than panic — an interrupt that finds
    /// main mid-update has nothing useful to do with the value and every reason
    /// not to bring the board down over it.
    pub fn try_with<R>(&self, f: impl FnOnce(&mut T) -> R) -> Option<R> {
        cortex_m::interrupt::free(|_| {
            if self.borrowed.get() {
                return None;
            }
            self.borrowed.set(true);
            // SAFETY: as above.
            let result = f(unsafe { &mut *self.value.get() });
            self.borrowed.set(false);
            Some(result)
        })
    }
}
