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

/// The largest app request this world will run. Declared in the shared `limits`,
/// beside the frame size it has to fit inside.
pub const REQUEST_MAX: usize = dlc_platform_embedded::limits::MAX_REQUEST;

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
    /// The client's correlation number, held until the reply is built.
    ///
    /// KEPT HERE because nothing else sees both ends: the interrupt knows which
    /// request arrived, main knows what the app answered, and they are a turn
    /// apart. Without it the reply would carry no id and a client with two
    /// questions in flight could not tell which had been answered.
    id: u64,
    request: [u8; REQUEST_MAX],
    request_len: usize,
}

static SLOT: Shared<Slot> = Shared::new(Slot {
    state: State::Idle,
    method: 0,
    id: 0,
    request: [0; REQUEST_MAX],
    request_len: 0,
});

/// Whether a session is live and could run a request.
///
/// # Why a request can arrive with nobody to answer it
///
/// `rest()` never returns: once a session ends the badge serves its log forever
/// and no turn will ever run again. A request offered then was accepted, queued,
/// and waited for — the client timed out after thirty seconds and was told "is
/// BADGE_CONTROL=on in this build?", which is both wrong and the most misleading
/// thing the tool could have said.
///
/// REFUSING IS THE ANSWER, not queueing hopefully. A world that cannot do a
/// thing should say so while somebody is still listening.
static SESSION: core::sync::atomic::AtomicBool = core::sync::atomic::AtomicBool::new(false);

/// Called when an app is instantiated, and again when the session ends.
pub fn session_open(open: bool) {
    SESSION.store(open, core::sync::atomic::Ordering::Release);
}

/// Whether a request could be run at all right now.
pub fn session_is_open() -> bool {
    SESSION.load(core::sync::atomic::Ordering::Acquire)
}

/// How many requests have been offered, accepted or not.
///
/// A DIAGNOSTIC, because "the client sent one and nothing happened" has three
/// causes that look identical from the far end of a cable: it never arrived, it
/// arrived and was refused, or it arrived and nobody collected it. A counter
/// main can print separates the first from the other two.
pub static OFFERED: core::sync::atomic::AtomicU32 = core::sync::atomic::AtomicU32::new(0);
pub static TAKEN: core::sync::atomic::AtomicU32 = core::sync::atomic::AtomicU32::new(0);

/// Park a request for main. False if one is already outstanding or too large.
pub fn offer(method: u32, id: u64, request: &[u8]) -> bool {
    OFFERED.fetch_add(1, core::sync::atomic::Ordering::Relaxed);
    if request.len() > REQUEST_MAX {
        return false;
    }
    if !session_is_open() {
        return false;
    }
    SLOT.with(|slot| {
        if slot.state != State::Idle {
            return false;
        }
        slot.method = method;
        slot.id = id;
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
        TAKEN.fetch_add(1, core::sync::atomic::Ordering::Relaxed);
        Some((slot.method, len))
    })
}

/// What the reply being built is answering: the method, and the client's id.
pub fn answering() -> (u32, u64) {
    SLOT.with(|slot| (slot.method, slot.id))
}

/// Release the slot. Called once the reply has been queued.
pub fn finished() {
    SLOT.with(|slot| slot.state = State::Idle);
}
