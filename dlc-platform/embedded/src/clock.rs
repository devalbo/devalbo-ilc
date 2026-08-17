//! Real time, and waiting for it.
//!
//! # What was here before
//!
//! `monotonic_clock::now` counted CALLS and `subscribe_duration` threw its
//! argument away and returned an always-ready pollable. So `time.Sleep(1s)` in a
//! guest returned immediately, and anything that unfolded over time could not.
//!
//! That was a stub, not a limit — the comment above it already said "on the badge
//! this is the RP2350's timer". This is that timer.
//!
//! # Why a function pointer rather than a peripheral
//!
//! This crate is shared by the badge, the QEMU harness and the laptop tools, and
//! each has a different clock: TIMER0, SysTick, the OS. A `fn() -> u64` is the
//! smallest thing all three can supply and costs no generics on `MinimalState`,
//! which is threaded through every WASI impl.
//!
//! MICROSECONDS, because that is what the RP2350's timer counts natively and
//! converting at the source is one place rather than three.
//!
//! # Why waiting is a spin, and why that is not as bad as it sounds
//!
//! [`crate::block_on`] drives futures by polling in a loop. A deadline future
//! that returns `Pending` therefore returns control to that loop, which is WORLD
//! CODE — the firmware can repaint a screen and sample buttons there while the
//! guest waits.
//!
//! It is still a busy-wait: the core spins rather than sleeping, and on a battery
//! that costs. Accepted for now because a badge running one app has nothing else
//! to do with the cycles, and because the alternative is an interrupt-driven
//! executor — which is where a shell would start becoming an operating system
//! (SESSION-AND-SURFACE-PLAN D6).
//!
//! **Providing a clock is not scheduling.** Every other tier already implements
//! `wasi:clocks` properly; this one was the outlier.

use alloc::boxed::Box;
use core::future::Future;
use core::pin::Pin;
use core::task::{Context, Poll};

/// Microseconds since some fixed point. Monotonic; the origin does not matter.
pub type Clock = fn() -> u64;

/// The clock this firmware installed, if any.
///
/// A GLOBAL rather than a constructor argument because TinyGo reads the clock
/// during `_initialize` — inside instantiation, before any host code could set a
/// field on the store. The clock has to be in place before the guest exists.
///
/// SAFETY: written once at boot, on a single core, before any guest runs.
static mut INSTALLED: Option<Clock> = None;

/// Install the clock. Call at boot, before instantiating anything.
pub fn install(clock: Clock) {
    unsafe { INSTALLED = Some(clock) };
}

/// The installed clock, or `None` on a host that has no timer.
pub fn installed() -> Option<Clock> {
    unsafe { INSTALLED }
}

/// How many times a guest has asked to wait.
///
/// DIAGNOSTIC, and it earned its place immediately: a countdown that should have
/// taken five seconds finished instantly, and the first question — does the
/// guest even call `subscribe_duration`? — could not be answered from outside.
/// Zero here means the sleep never reached the host at all, which is a different
/// bug from a deadline that resolves too early.
static SLEEPS: core::sync::atomic::AtomicU32 = core::sync::atomic::AtomicU32::new(0);

pub fn note_sleep() {
    SLEEPS.fetch_add(1, core::sync::atomic::Ordering::Relaxed);
}

pub fn sleeps() -> u32 {
    SLEEPS.load(core::sync::atomic::Ordering::Relaxed)
}

/// A future that is not ready until the clock passes a deadline.
///
/// Returns `Pending` rather than looping internally, and the difference is the
/// whole point: looping inside would keep control inside the guest's import call,
/// where the world can do nothing. Returning `Pending` hands control back to
/// `block_on`, which is where a repaint can happen.
pub struct Deadline {
    clock: Clock,
    deadline_us: u64,
}

impl Deadline {
    pub fn new(clock: Clock, deadline_us: u64) -> Self {
        Self { clock, deadline_us }
    }
}

impl Future for Deadline {
    type Output = ();

    fn poll(self: Pin<&mut Self>, cx: &mut Context<'_>) -> Poll<()> {
        if (self.clock)() >= self.deadline_us {
            return Poll::Ready(());
        }
        // WAKE IMMEDIATELY. There is no interrupt to schedule a wake-up from, so
        // the only way this future makes progress is by being polled again — and
        // `block_on`'s waker is a no-op, so the loop must not be relying on it.
        // Calling it anyway keeps the contract honest for any future executor
        // that does listen.
        cx.waker().wake_by_ref();
        Poll::Pending
    }
}

/// A pollable that becomes ready at a deadline.
pub struct DeadlinePollable {
    clock: Clock,
    deadline_us: u64,
}

impl DeadlinePollable {
    pub fn new(clock: Clock, deadline_us: u64) -> Self {
        Self { clock, deadline_us }
    }
}

#[async_trait::async_trait]
impl wasmtime_wasi_io::poll::Pollable for DeadlinePollable {
    async fn ready(&mut self) {
        Deadline::new(self.clock, self.deadline_us).await
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use core::sync::atomic::{AtomicU64, Ordering};

    static FAKE_NOW: AtomicU64 = AtomicU64::new(0);
    fn fake_clock() -> u64 {
        FAKE_NOW.load(Ordering::Relaxed)
    }

    fn poll_once(future: &mut Deadline) -> Poll<()> {
        let waker = futures_task_noop();
        let mut cx = Context::from_waker(&waker);
        // SAFETY: the future is on the stack and not moved after pinning.
        let pinned = unsafe { Pin::new_unchecked(future) };
        pinned.poll(&mut cx)
    }

    fn futures_task_noop() -> core::task::Waker {
        use core::task::{RawWaker, RawWakerVTable};
        static VTABLE: RawWakerVTable = RawWakerVTable::new(
            |_| RawWaker::new(core::ptr::null(), &VTABLE),
            |_| {},
            |_| {},
            |_| {},
        );
        unsafe { core::task::Waker::from_raw(RawWaker::new(core::ptr::null(), &VTABLE)) }
    }

    /// A deadline in the future is PENDING — which is what returns control to
    /// the world so it can repaint while the guest waits.
    #[test]
    fn a_future_deadline_is_pending() {
        FAKE_NOW.store(0, Ordering::Relaxed);
        let mut future = Deadline::new(fake_clock, 1_000);
        assert_eq!(poll_once(&mut future), Poll::Pending);
    }

    /// And it becomes ready once the clock passes it. The guest's sleep ENDS,
    /// which is the whole behaviour that did not exist before.
    #[test]
    fn a_passed_deadline_is_ready() {
        FAKE_NOW.store(0, Ordering::Relaxed);
        let mut future = Deadline::new(fake_clock, 1_000);
        assert_eq!(poll_once(&mut future), Poll::Pending);
        FAKE_NOW.store(1_000, Ordering::Relaxed);
        assert_eq!(poll_once(&mut future), Poll::Ready(()));
    }

    /// A deadline already in the past is ready at once, rather than waiting a
    /// full wrap of the clock. `>=` not `>`, and the test says which.
    #[test]
    fn a_past_deadline_does_not_wait() {
        FAKE_NOW.store(5_000, Ordering::Relaxed);
        let mut future = Deadline::new(fake_clock, 1_000);
        assert_eq!(poll_once(&mut future), Poll::Ready(()));
    }
}
