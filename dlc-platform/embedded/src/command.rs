//! The engine boundary's return type, on its own so both hosts can share it.
//!
//! It lived in `host.rs` until `host.rs` became `std`-only. `minimal.rs` — the
//! badge's host — lifts the same record, so leaving it there would have made the
//! portable host depend on the one that cannot be compiled for a board.

use alloc::string::String;
use alloc::vec::Vec;

/// The result of one command — the WIT `command-result`, in Rust.
///
/// DERIVED, not hand-lifted: `command-result` is a WIT *record*, so wasmtime
/// needs the shape to map field-by-field. Reading it as a tuple fails with
/// "expected tuple found record", which is the canonical ABI refusing to guess.
#[derive(wasmtime::component::ComponentType, wasmtime::component::Lift)]
#[component(record)]
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CommandResult {
    pub success: bool,
    pub output: Vec<u8>,
    pub error: Option<String>,
}
