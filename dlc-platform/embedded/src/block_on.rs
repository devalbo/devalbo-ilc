//! A one-function executor.
//!
//! `wasi:io` is registered through `add_to_linker_async`, so the store must have
//! async support and calls go through `call_async` — which needs *something* to
//! drive the future. On a laptop that would be tokio; on a badge there is no
//! runtime and no reason for one.
//!
//! IT USED TO BE SOUND ONLY BECAUSE EVERY POLLABLE WAS ALWAYS READY (see
//! uart.rs), and this comment warned that adding a capability which genuinely
//! blocks would leave the firmware spinning forever with no clue why.
//!
//! `clock.rs` added exactly that capability, deliberately. A guest sleep now
//! returns `Pending` until its deadline, so this loop DOES spin on real time —
//! a busy-wait, on a core that has nothing else to run.
//!
//! **Which is why the loop takes a pump.** Every iteration is world code running
//! while the guest is suspended mid-import: the one place a screen can be
//! repainted and buttons sampled during a command. Without it a sleeping guest
//! would freeze the badge; with it, the world stays alive and the app's output
//! appears as it is produced.
//!
//! This is a PUMP, not a scheduler. Single-threaded, cooperative by
//! construction, and there is only ever one thing to run — which is the line
//! between a shell and an operating system (SESSION-AND-SURFACE-PLAN D6).
use core::future::Future;
use core::pin::pin;
use core::task::{Context, Poll, RawWaker, RawWakerVTable, Waker};

static VTABLE: RawWakerVTable = RawWakerVTable::new(
    |_| RawWaker::new(core::ptr::null(), &VTABLE),
    |_| {},
    |_| {},
    |_| {},
);

/// What the world does while a guest waits.
///
/// A `static` rather than a parameter because the call site is deep inside
/// `MinimalHost::execute`, which is shared by every host — a laptop tool has
/// nothing to pump and should not have to say so. Installed once at boot by a
/// firmware that has a screen.
///
/// SAFETY: written once before any guest runs, on a single core, and only read
/// afterwards.
static mut PUMP: Option<fn()> = None;

/// Install the function called on every poll while a guest is suspended.
///
/// Call it before running anything. A firmware that does not is unchanged: the
/// loop spins as it always did.
pub fn set_pump(pump: fn()) {
    unsafe { PUMP = Some(pump) };
}

/// Drive a future to completion by polling it.
pub fn block_on<F: Future>(future: F) -> F::Output {
    let waker = unsafe { Waker::from_raw(RawWaker::new(core::ptr::null(), &VTABLE)) };
    let mut cx = Context::from_waker(&waker);
    let mut future = pin!(future);
    loop {
        if let Poll::Ready(value) = future.as_mut().poll(&mut cx) {
            return value;
        }
        // BETWEEN POLLS, not before the first: a future that is ready
        // immediately — which is still almost all of them — must not pay for a
        // repaint it does not need.
        if let Some(pump) = unsafe { PUMP } {
            pump();
        }
    }
}
