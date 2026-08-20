//! WHERE THE WORLD HAS GOT TO, published as it happens.
//!
//! # The question this exists to answer
//!
//! "Is the badge stuck, and if so, in what?" The bring-up already announced
//! every stage — but only into the LOG, which is a stream a client reads if it
//! was listening at the time. A world that hung before the log was up, or that
//! hung with a client not yet attached, announced its progress to nobody.
//!
//! That is not the rare case. It is the normal one for the failure people
//! actually hit: a board that reboots and comes back wrong is silent precisely
//! because it never reached the part that speaks.
//!
//! So progress is also a VALUE, not only an event. Two words, written as the
//! world enters each step, readable at any later moment by anything that can
//! read a `u32`.
//!
//! # Why two levels
//!
//! A `Phase` is a step of the world's life that a person would name unprompted —
//! bring the board up, see what is installed, open a session, run it. A `Stage`
//! is a step within one. The pair is what makes "is this a new high-level thing,
//! or part of one that already exists?" answerable, in the firmware and on the
//! wire alike; see the `Phase` comment in control.proto.
//!
//! # Why atomics, and nothing else
//!
//! Because the caller may be an interrupt, and because the heap does not exist
//! for the first three stages. Anything richer — a struct behind a lock, a
//! formatted string — would be unavailable in exactly the window this is for.
//! Two `u32`s can be read from an interrupt, mid-panic, before PSRAM, with no
//! allocation and no borrow.

use core::sync::atomic::{AtomicU32, Ordering};

use dlc_platform_embedded::control;

/// The phase the world is in. One of `control::PHASE_*`.
static PHASE: AtomicU32 = AtomicU32::new(control::PHASE_UNSPECIFIED);

/// The stage within it, or `STAGE_UNSPECIFIED` between stages.
static STAGE: AtomicU32 = AtomicU32::new(control::STAGE_UNSPECIFIED);

/// HOW THE LAST THING WENT, published for the same reason the phase is.
///
/// The verdict already existed — as a colour on the panel and a word at the end
/// of the log — and both of those reach a PERSON. A client had to scrape
/// `verdict: OK` out of prose, which is the parse-the-text problem this channel
/// exists to remove, and fragile in the usual way: the word comes from
/// `Status::name`, so renaming a state breaks every reader silently.
static VERDICT: AtomicU32 = AtomicU32::new(control::VERDICT_UNSPECIFIED);

/// How many transitions the world has taken that this file says are impossible.
///
/// ON THE WIRE, not only in the log, because a fault nobody was watching for is
/// the one worth catching. A client that sees a non-zero count knows to go
/// looking for the line that explains it.
static FAULTS: AtomicU32 = AtomicU32::new(0);

/// Enter a phase, and say so if that was not a thing the world could do.
///
/// Returns the rejected `(from, to)` when the transition is not in the table, so
/// the caller can report it with the log it already holds. **The move still
/// happens.** A bookkeeping fault is a firmware bug worth shouting about, and
/// refusing it would strand the world in the phase it was leaving — turning a
/// wrong label into a hang, which is a far worse trade on a board whose whole
/// problem is being hard to observe.
///
/// CLEARS THE STAGE, because a stage belongs to the phase it was announced in.
/// Carrying the last one across a boundary would report `payloads / psram` — a
/// pair that never existed and reads as a firmware fault rather than a
/// transition.
#[must_use = "an invalid transition should be reported, not dropped"]
pub fn enter(phase: u32) -> Option<(u32, u32)> {
    let from = PHASE.load(Ordering::Acquire);
    STAGE.store(control::STAGE_UNSPECIFIED, Ordering::Relaxed);
    PHASE.store(phase, Ordering::Release);
    // THE TABLE LIVES IN THE SHARED CRATE, because the shape of a world's life
    // is not a fact about this board (D6a) — and because that is the only place
    // it can be tested: CI cross-compiles this firmware and cannot run it.
    if control::phase_may_follow(from, phase) {
        return None;
    }
    FAULTS.fetch_add(1, Ordering::Relaxed);
    Some((from, phase))
}

/// How many impossible transitions have been taken. Zero on a healthy world.
pub fn faults() -> u32 {
    FAULTS.load(Ordering::Relaxed)
}

/// Begin a stage. Called by `Report::stage`, so no caller has to remember to.
pub fn begin(stage: u32) {
    STAGE.store(stage, Ordering::Release);
}

/// The phase and stage, for anything that has to report them.
pub fn get() -> (u32, u32) {
    (PHASE.load(Ordering::Acquire), STAGE.load(Ordering::Acquire))
}

/// Record how the last thing went. Called by `Report::finish`, so no caller has
/// to remember to — the same arrangement that keeps the stage honest.
pub fn verdict(code: u32) {
    VERDICT.store(code, Ordering::Release);
}

/// The last verdict, or `VERDICT_UNSPECIFIED` before there is one.
pub fn last_verdict() -> u32 {
    VERDICT.load(Ordering::Acquire)
}
