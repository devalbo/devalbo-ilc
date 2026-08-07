//! The settings a machine with no MMU requires — **the single source of truth
//! for both the compiler and the runtime.**
//!
//! WHY ITS OWN FILE, AND WHY IT IS `#[path]`-INCLUDED RATHER THAN IMPORTED.
//! A `.cwasm` records the settings of the compiler that produced it, and the
//! runtime refuses an artifact that disagrees — so these two lists must match
//! exactly. They lived in two crates and were kept in step by hand, which failed
//! exactly as you would expect:
//!
//!     Module was compiled with a memory reservation of '10485760'
//!     but '0' is expected for the host
//!
//! The obvious fix — have `precompile/` depend on this crate — is wrong, and the
//! reason is the point of `precompile/Cargo.toml`: that crate builds wasmtime
//! with `cranelift` and deliberately **without `runtime`**, because a runtime
//! engine requires target == host and a compile-only one does not. A dependency
//! edge would unify the features and drag `runtime` back in, breaking
//! cross-compilation to `pulley32` from a 64-bit machine.
//!
//! So `precompile/src/main.rs` includes this file with `#[path]`. No dependency,
//! no feature unification, and still one list rather than two.

use wasmtime::Config;

/// Apply the settings that a target with no virtual memory demands.
pub fn no_virtual_memory(config: &mut Config) {
    // No copy-on-write mappings and no host signal handlers. Getting these wrong
    // surfaces at *load* time as "virtual memory disabled at compile time --
    // cannot enable CoW", which reads like a bad artifact and is a Config
    // mistake.
    config.memory_init_cow(false);
    config.signals_based_traps(false);

    // AND THE THREE THAT ONLY BITE AT INSTANTIATION, which is why they survived
    // until a component was instantiated rather than merely loaded.
    //
    // Without virtual memory, linear memory comes from `MallocMemory`, and that
    // backend refuses anything it cannot express — a reservation it cannot make
    // lazily, and guard pages it cannot map. Its own checks, in order:
    //
    //   guard_size() > 0    -> "only compatible if guard pages aren't used"
    //   reservation() > 0   -> "only compatible with no ahead-of-time memory reservation"
    //   memory_init_cow     -> "cannot be used with CoW images"
    //
    // The defaults assume a 64-bit host with address space to spare, so all three
    // must be said explicitly. `reservation_for_growth` is not checked but is
    // ADDED to the initial allocation, so leaving it would make every guest
    // memory start larger than the guest asked for — expensive in exactly the
    // place this tier can least afford it.
    config.memory_reservation(0);
    config.memory_guard_size(0);
    config.memory_reservation_for_growth(0);
}
