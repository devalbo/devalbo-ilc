//! The `wasmtime-platform.h` layer — what a `no_std` embedder owes Wasmtime.
//!
//! Wasmtime's docs put it plainly: an embedder must "implement the equivalent of
//! a C header file to indicate how to perform basic OS operations", and "if
//! they're not defined then a link-time error will be generated". So this file
//! is written by following link errors, one at a time, which is a far better
//! experience than discovering the same gaps at runtime on a device with one
//! UART.
//!
//! Everything here is `#[no_mangle] extern "C"` because Wasmtime looks these up
//! by C symbol name, not through Rust.
//!
//! **AND THAT IS THE HAZARD, not a detail.** A C symbol matches on NAME ALONE:
//! the linker never checks that the signature agrees, so a wrong one links
//! cleanly and fails somewhere else entirely. That is exactly what happened here
//! — see `SLOTS` below. When adding to this file, read the declaration in
//! wasmtime's `runtime/vm/sys/custom/capi.rs` and match it argument for argument;
//! it is the only thing keeping these honest.
use core::ffi::c_void;
use core::ptr;
use core::sync::atomic::{AtomicPtr, Ordering};

/// Wasmtime's thread-local slots — **and the `slot` argument is the bug fix.**
///
/// THE API CHANGED UNDER US, which is the whole lesson. This file was a correct
/// implementation of wasmtime 35's contract:
///
/// ```c
/// void *wasmtime_tls_get(void);              // 35
/// void  wasmtime_tls_set(void *ptr);
///
/// void *wasmtime_tls_get(size_t slot);       // 46
/// void  wasmtime_tls_set(size_t slot, void *ptr);
/// ```
///
/// `extern "C"` links by NAME ALONE, so the version bump changed the ABI and
/// nothing — not the compiler, not the linker, not a deprecation warning — said
/// so. Wasmtime's docs are accurate; they are simply not something the build
/// consults.
///
/// **The mechanism, which is dumber than it looks.** Wasmtime calls
/// `wasmtime_tls_set(0, ptr)`; on ARM `slot` lands in r0 and `ptr` in r1. The old
/// one-argument version read **r0** — so it stored `0` every time and threw the
/// real pointer away. The TLS slot was permanently null: `push` read null as the
/// previous head, `pop` read null again and asserted it equalled `self`. The
/// activation chain was never stored at all.
///
/// Not slot collision — slot 1 is `component-model-async`, which this build does
/// not enable, so wasmtime never passes it. Two slots are implemented here
/// because the signature has two, not because both are in use.
///
/// It surfaced as `traphandlers.rs:596: assertion failed: core::ptr::eq(head,
/// self)`, which is wasmtime correctly DETECTING a broken embedder contract — its
/// only fault being that the message names its own internals rather than the
/// symbol at fault. Loading a component never touches TLS; only instantiation
/// does, which is why this survived until a component was actually run.
///
/// SOUND HERE FOR A REASON, not by luck: the bring-up runs one Wasmtime instance
/// on one core with no preemption, so there is exactly one "thread" and a static
/// is what a TLS slot degenerates to. **The moment this firmware uses the
/// RP2350's second core for anything touching Wasmtime, this becomes wrong** —
/// and silently, which is why it is spelled out rather than assumed.
///
/// Atomics rather than `static mut`: not for thread-safety, but because with LTO
/// these inline into wasmtime's own code where a plain `static mut` is a normal
/// memory access the optimiser may keep in a register. `Relaxed` compiles to the
/// same load and store.
static SLOTS: [AtomicPtr<c_void>; 2] =
    [AtomicPtr::new(ptr::null_mut()), AtomicPtr::new(ptr::null_mut())];

#[no_mangle]
pub unsafe extern "C" fn wasmtime_tls_get(slot: usize) -> *mut c_void {
    // Wasmtime documents that no slot other than 0 or 1 is ever passed. Clamping
    // rather than indexing keeps a contract violation from becoming a panic in a
    // firmware whose panic handler is a halt.
    SLOTS[slot.min(1)].load(Ordering::Relaxed)
}

#[no_mangle]
pub unsafe extern "C" fn wasmtime_tls_set(slot: usize, ptr: *mut c_void) {
    SLOTS[slot.min(1)].store(ptr, Ordering::Relaxed);
}
