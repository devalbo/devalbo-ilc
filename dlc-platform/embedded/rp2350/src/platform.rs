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
use core::ffi::c_void;
use core::ptr;
use core::sync::atomic::{AtomicPtr, Ordering};

/// Wasmtime's thread-local slot, which on this chip is simply a static.
///
/// SOUND HERE FOR A REASON, not by luck: the bring-up runs one Wasmtime instance
/// on one core with no preemption, so there is exactly one "thread" and a static
/// is what a TLS slot degenerates to. **The moment this firmware uses the
/// RP2350's second core for anything touching Wasmtime, this becomes wrong** —
/// and silently, which is why it is spelled out rather than assumed.
/// **The `slot` argument is a bug fix — see `qemu-armv7m/src/platform.rs` for the
/// full account.**
///
/// Wasmtime 35 declared `wasmtime_tls_get(void)` / `wasmtime_tls_set(void*)`;
/// wasmtime 46 declares `wasmtime_tls_get(size_t slot)` /
/// `wasmtime_tls_set(size_t slot, void *ptr)`. This file implemented the 35 form,
/// correctly, and `extern "C"` links by name — so the ABI changed with no error
/// anywhere. On ARM the caller then puts `slot` in r0 and `ptr` in r1, and the
/// one-argument version read r0: it stored `0` forever and discarded every
/// pointer.
///
/// It cannot show up until a component is instantiated, because loading never
/// touches TLS. On the emulator it appeared as `assertion failed:
/// core::ptr::eq(head, self)` deep inside wasmtime. On a badge with one UART it
/// would have been a halt with no message.
///
/// **Check every symbol here against wasmtime's `runtime/vm/sys/custom/capi.rs`.**
/// The linker will not.
///
/// Atomics rather than `static mut` because with LTO these inline into wasmtime's
/// own code, where a plain static is an access the optimiser may cache in a
/// register. `Relaxed` compiles to the same load and store.
static SLOTS: [AtomicPtr<c_void>; 2] =
    [AtomicPtr::new(ptr::null_mut()), AtomicPtr::new(ptr::null_mut())];

#[no_mangle]
pub unsafe extern "C" fn wasmtime_tls_get(slot: usize) -> *mut c_void {
    // Wasmtime documents that no slot other than 0 or 1 is passed. Clamping keeps
    // a contract violation from becoming a panic in a firmware whose panic
    // handler is a halt.
    SLOTS[slot.min(1)].load(Ordering::Relaxed)
}

#[no_mangle]
pub unsafe extern "C" fn wasmtime_tls_set(slot: usize, ptr: *mut c_void) {
    SLOTS[slot.min(1)].store(ptr, Ordering::Relaxed);
}
