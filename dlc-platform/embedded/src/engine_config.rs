//! The settings a machine with no MMU requires — **the single source of truth
//! for both the compiler and the runtime.**
//!
//! WHY ITS OWN FILE, AND WHY IT IS `#[path]`-INCLUDED RATHER THAN IMPORTED.
//! A `.cwasm` records the settings of the compiler that produced it, and the
//! runtime refuses an artifact that disagrees — so these two lists must match
//! exactly. They lived in two crates and were kept in step by hand, which failed
//! exactly as you would expect:
//!
//! ```text
//! Module was compiled with a memory reservation of '10485760'
//! but '0' is expected for the host
//! ```
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

// ---------------------------------------------------------------------------
// The two configurations, and why one contains the other
// ---------------------------------------------------------------------------
//
// COMPILATION SETTINGS ARE RECORDED IN THE ARTIFACT. A `.cwasm` is only loadable
// by an engine configured compatibly, so producer and consumer are not two
// independent choices — they are one choice, written down twice unless something
// stops that.
//
// `spikes/browser-precompile` is what made this urgent. It built a working
// compiler THREE TIMES and produced an artifact the badge would reject each
// time, because it had copied some of the settings and not others:
//
//   default config              1,660,072 bytes  (CoW and guard pages the badge cannot honour)
//   + no_virtual_memory         1,586,584 bytes  (679 KB of .wasmtime.addrmap)
//   + generate_address_map(false)  900,720 bytes  identical to the real tool
//
// Every wrong version compiled cleanly and failed at LOAD, with a message that
// reads like a bad file rather than a config mistake. So the settings live here,
// as functions, and callers ask for a role rather than assembling a list.
//
// `for_artifact` is `for_runtime` PLUS one producer-only line. That containment
// is the invariant: anything a loader must agree with belongs in `for_runtime`,
// and only choices invisible to the loader may be added on top.

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

/// The engine a TARGET WITH NO MMU runs — the badge, and QEMU standing in for it.
///
/// Everything here is visible to a loader: get one wrong and a `.cwasm` compiled
/// elsewhere will not load, reporting an artifact problem that is really a
/// configuration disagreement.
pub fn for_runtime(config: &mut Config, target: &str) -> wasmtime::Result<()> {
    // `target()` is what makes a 64-bit laptop a faithful stand-in: the output is
    // Pulley bytecode for the named width regardless of what is running the
    // compiler. It is also why a browser can produce badge artifacts at all.
    config.target(target)?;
    config.wasm_component_model(true);
    no_virtual_memory(config);
    Ok(())
}

/// The engine that PRODUCES a `.cwasm` for such a target.
///
/// `for_runtime`, plus the one setting a loader cannot see. Callers that compile
/// should use this and nothing else — the point of the function is that there is
/// no list to assemble and therefore none to get partially right.
pub fn for_artifact(config: &mut Config, target: &str) -> wasmtime::Result<()> {
    for_runtime(config, target)?;

    // 44% OF THE ARTIFACT IS DEBUG METADATA. `.wasmtime.addrmap` alone was
    // 679 KB of hello's 1.57 MB — a wasm-offset-to-code-offset map that exists
    // for backtraces. On a device where the whole artifact must be ONE
    // contiguous allocation, that is the cheapest RAM win available.
    //
    // The cost is real: traps on the badge report addresses rather than wasm
    // locations. Worth it while the constraint is "does it load at all".
    //
    // INVISIBLE TO THE LOADER, which is what makes it safe to add here rather
    // than in `for_runtime`: an engine loading this artifact neither knows nor
    // cares whether the map was emitted.
    config.generate_address_map(false);
    Ok(())
}
