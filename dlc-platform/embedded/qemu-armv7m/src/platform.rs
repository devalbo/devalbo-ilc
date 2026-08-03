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

/// Wasmtime's thread-local slot, which on this chip is simply a static.
///
/// SOUND HERE FOR A REASON, not by luck: the bring-up runs one Wasmtime instance
/// on one core with no preemption, so there is exactly one "thread" and a static
/// is what a TLS slot degenerates to. **The moment this firmware uses the
/// RP2350's second core for anything touching Wasmtime, this becomes wrong** —
/// and silently, which is why it is spelled out rather than assumed.
static mut TLS: *mut c_void = ptr::null_mut();

#[no_mangle]
pub unsafe extern "C" fn wasmtime_tls_get() -> *mut c_void {
    TLS
}

#[no_mangle]
pub unsafe extern "C" fn wasmtime_tls_set(ptr: *mut c_void) {
    TLS = ptr;
}
