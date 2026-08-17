//! Spike: can the badge's precompiler run with a wasm32 HOST?
//!
//! # The question, stated narrowly
//!
//! `docs/PAYLOAD-LOADING-PLAN.md` D5 wants a browser to turn a user's `.wasm`
//! into a `.cwasm` the badge can load, so nobody needs a toolchain. That rests
//! on one assumption nobody here has tested: **that Wasmtime + Cranelift build
//! and run when the host is wasm32.**
//!
//! This is NOT "can Wasmtime execute guests in a browser", which is a harder and
//! different question. Compiling to Pulley emits BYTECODE, not machine code —
//! no JIT, no executable pages, no signal handlers — so the usual blocker does
//! not apply. What is left is mundane and decisive: do the crates compile for
//! this host, and how big is the result.
//!
//! # Why size is half the answer
//!
//! A compiler that works and ships 30 MB to a page has answered the wrong
//! question. The plan should record the measurement, not an estimate.

// THE REAL CONFIG, INCLUDED — not a copy.
//
// The first version of this spike omitted it and produced a 1,660,072-byte
// artifact where the native precompiler makes 900,720. That is not a rounding
// difference, it is a DIFFERENT ARTIFACT: without these settings Wasmtime builds
// memory images assuming copy-on-write and guard pages, and the badge — which
// has no virtual memory — refuses them at load with "virtual memory disabled at
// compile time -- cannot enable CoW".
//
// So the spike would have proved the wrong thing: that a browser can compile
// SOMETHING, rather than that it can compile what this badge accepts.
//
// `#[path]` for the same reason `precompile/src/main.rs` uses it — no dependency,
// no feature unification, and still one list rather than two.
#[path = "../../../dlc-platform/embedded/src/engine_config.rs"]
mod engine_config;

/// Compile a component to Pulley bytecode for the badge.
///
/// Byte-for-byte the operation `dlc-platform/embedded/src/bin/precompile.rs`
/// performs on a laptop, with the same version pin and the same target — so a
/// green result here means the browser can produce artifacts the badge accepts,
/// not merely that some compiler ran.
pub fn precompile_pulley32(component: &[u8]) -> Result<Vec<u8>, String> {
    let mut config = wasmtime::Config::new();
    // THE WHOLE CONFIG FROM ONE PLACE — which is this spike's own finding turned
    // back on itself. Three earlier versions copied part of the list and built a
    // working compiler that emitted an artifact the badge rejects.
    engine_config::for_artifact(&mut config, "pulley32").map_err(|e| format!("config: {e}"))?;

    let engine = wasmtime::Engine::new(&config).map_err(|e| format!("engine: {e}"))?;
    engine
        .precompile_component(component)
        .map_err(|e| format!("precompile: {e}"))
}

/// What this build can produce, in the vocabulary of D5c/D5d.
///
/// The browser half of the INTERSECTION: a page advertises this, a badge
/// advertises what it accepts, and they either meet or the mismatch is named on
/// both sides.
pub const PRODUCES: &str = "wasmtime=46.0.1;pulley32";
