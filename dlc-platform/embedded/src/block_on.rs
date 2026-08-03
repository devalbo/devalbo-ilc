//! A one-function executor.
//!
//! `wasi:io` is registered through `add_to_linker_async`, so the store must have
//! async support and calls go through `call_async` — which needs *something* to
//! drive the future. On a laptop that would be tokio; on a badge there is no
//! runtime and no reason for one.
//!
//! THIS IS ONLY SOUND BECAUSE EVERY POLLABLE THIS HOST OWNS IS ALWAYS READY
//! (see uart.rs). Nothing here ever waits on a timer, an interrupt, or a socket,
//! so a future that returns `Pending` has nothing to wait *for* — spinning until
//! it resolves is not a busy-wait, it is the future making progress on the next
//! poll. Add a capability that genuinely blocks and this must be replaced with a
//! real executor, or the firmware will spin forever with no clue why.
use core::future::Future;
use core::pin::pin;
use core::task::{Context, Poll, RawWaker, RawWakerVTable, Waker};

static VTABLE: RawWakerVTable = RawWakerVTable::new(
    |_| RawWaker::new(core::ptr::null(), &VTABLE),
    |_| {},
    |_| {},
    |_| {},
);

/// Drive a future to completion by polling it.
pub fn block_on<F: Future>(future: F) -> F::Output {
    let waker = unsafe { Waker::from_raw(RawWaker::new(core::ptr::null(), &VTABLE)) };
    let mut cx = Context::from_waker(&waker);
    let mut future = pin!(future);
    loop {
        if let Poll::Ready(value) = future.as_mut().poll(&mut cx) {
            return value;
        }
    }
}
