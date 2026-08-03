//! Running an ILC component under Wasmtime's Pulley interpreter.
//!
//! WHY PULLEY. The badge (RP2350) has no JIT and no OS, so Cranelift is out.
//! Pulley is Wasmtime's portable bytecode interpreter, and — unlike WAMR — it
//! supports the Component Model, which is what lets the embedded tier run the
//! same artifact as the browser instead of a separate wasip1 build.
//!
//! WHY AOT. A `no_std` Wasmtime has no compiler, so the component is compiled
//! ahead of time (`wasmtime compile --target pulley32`) on a real machine and
//! the resulting `.cwasm` is flashed. This module therefore takes PRECOMPILED
//! bytes on the badge — and, on a laptop with `cranelift` available, can compile
//! them itself, which is what makes Phase 1 runnable without hardware.

// wasmtime 46 has its OWN error type, which does not implement anyhow's
// StdError — so this uses `wasmtime::Result` rather than fighting the
// conversion. Errors from here are already descriptive.
use wasmtime::{Config, Engine, Result};

/// An engine configured to interpret rather than compile.
///
/// `target("pulley32")` is the whole trick: it tells Wasmtime to produce and run
/// Pulley bytecode for a 32-bit target — the badge's shape — even though this is
/// a 64-bit laptop. That is what makes a laptop a faithful stand-in.
pub fn pulley_engine(pointer_width: PulleyWidth) -> Result<Engine> {
    let mut config = Config::new();
    config.target(pointer_width.triple())?;
    config.wasm_component_model(true);
    // Required by `wasi:io`, which is registered through `add_to_linker_async`.
    // See block_on.rs for why this costs a loop rather than a runtime.
    config.async_support(true);
    Engine::new(&config)
}

/// Pulley is pointer-width specific, and the badge is 32-bit.
#[derive(Clone, Copy, Debug)]
pub enum PulleyWidth {
    /// The RP2350 and every other 32-bit MCU.
    Bits32,
    /// A development host, where a 64-bit interpreter is the closer analogue of
    /// native execution.
    Bits64,
}

impl PulleyWidth {
    pub fn triple(self) -> &'static str {
        match self {
            PulleyWidth::Bits32 => "pulley32",
            PulleyWidth::Bits64 => "pulley64",
        }
    }
}
