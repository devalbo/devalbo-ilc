//! Running an app command on someone else's behalf (BADGE-CONTROL-PLAN D2).
//!
//! # Why this cannot be answered where it is asked
//!
//! Every other verb reads a fact and returns it, so the USB interrupt answers
//! them itself. This one has to reach the ENGINE — a Wasmtime store, an
//! instance, a heap, and an async call that pumps its own executor — all of
//! which live on main's stack for the whole run. An interrupt cannot borrow
//! that, and should not: `execute` runs for as long as the app takes, and the
//! countdown takes eight seconds. Servicing USB from inside it is what keeps the
//! log alive; being called FROM it would stop the log dead.
//!
//! So the request is parked here, main picks it up at the top of the next turn,
//! and the answer is parked back. This module is the whole of that handover.
//!
//! # The reply is deferred, and that is visible to the caller
//!
//! A client sends the request and gets nothing until the app returns. That is
//! request/response with a long latency, not a notification — the answer is the
//! app's own bytes, and there is exactly one of them.
//!
//! ONE AT A TIME, enforced here rather than trusted. The protocol has no message
//! ids: a client pairs a reply with its request by there being only one in
//! flight. Accepting a second would silently answer the wrong question, so the
//! second is REFUSED, which a client can see.
//!
//! # What the app is told
//!
//! Nothing. It receives a method id and bytes, exactly as it would if a person
//! had spun a number and pressed B, and it has no way to ask which happened
//! (D6). That is the property that makes a test's result mean anything about the
//! app rather than about the harness.

use crate::shared::Shared;

/// Bigger than the 64 bytes a widget can collect, because a pass-through carries
/// an app's whole request rather than one field a person typed.
pub const REQUEST_MAX: usize = 256;

#[derive(Clone, Copy, PartialEq, Eq)]
enum State {
    /// Nothing outstanding.
    Idle,
    /// A client is waiting; main has not picked it up.
    Queued,
    /// Main is running it. A second request is still refused.
    Running,
}

struct Slot {
    state: State,
    method: u32,
    request: [u8; REQUEST_MAX],
    request_len: usize,
}

static SLOT: Shared<Slot> = Shared::new(Slot {
    state: State::Idle,
    method: 0,
    request: [0; REQUEST_MAX],
    request_len: 0,
});

/// Park a request for main. False if one is already outstanding or too large.
pub fn offer(method: u32, request: &[u8]) -> bool {
    if request.len() > REQUEST_MAX {
        return false;
    }
    SLOT.with(|slot| {
        if slot.state != State::Idle {
            return false;
        }
        slot.method = method;
        slot.request[..request.len()].copy_from_slice(request);
        slot.request_len = request.len();
        slot.state = State::Queued;
        true
    })
}

/// Whether a request is waiting.
///
/// Widgets poll this so they can stop asking for input that has already
/// arrived — see the module header.
pub fn waiting() -> bool {
    SLOT.try_with(|slot| slot.state == State::Queued).unwrap_or(false)
}

/// Take the request, if there is one. Marks it running.
pub fn take(into: &mut [u8]) -> Option<(u32, usize)> {
    SLOT.with(|slot| {
        if slot.state != State::Queued {
            return None;
        }
        let len = slot.request_len.min(into.len());
        into[..len].copy_from_slice(&slot.request[..len]);
        slot.state = State::Running;
        Some((slot.method, len))
    })
}

/// Release the slot. Called once the reply has been queued.
pub fn finished() {
    SLOT.with(|slot| slot.state = State::Idle);
}
