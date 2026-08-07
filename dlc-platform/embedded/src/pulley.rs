//! Running an ILC component under Wasmtime's Pulley interpreter.
//!
//! WHY PULLEY. The badge (RP2350) has no JIT and no OS, so Cranelift is out.
//! Pulley is Wasmtime's portable bytecode interpreter, and it
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
    // NO `async_support(true)` — wasmtime 46 deprecates it as "no longer has any
    // effect", so the call was pure warning noise. Async is still what `wasi:io`
    // needs (it registers through `add_to_linker_async`, and calls go through
    // `call_async`); it simply no longer has to be asked for. See block_on.rs for
    // why driving those futures costs a loop rather than a runtime.
    // A machine with no OS has neither copy-on-write mappings nor signal
    // handlers, and Wasmtime's defaults assume both. Set here rather than in each
    // firmware because it is a property of the ENGINE this crate builds, not of
    // any one board — and because getting it wrong surfaces as "virtual memory
    // disabled at compile time -- cannot enable CoW" at *load* time, which reads
    // like a bad artifact and is a Config mistake.
    //
    // Harmless with `std`: they disable optimisations a laptop could have used,
    // and keeping one code path is worth more than that on a tier whose whole
    // purpose is to be the same everywhere.
    crate::no_vm::no_virtual_memory(&mut config);
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
