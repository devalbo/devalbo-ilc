//! The inherited embedded runtime for ILC apps (docs/EMBEDDED-PLAN.md D3).
//!
//! What every embedded app gets rather than writes: the Pulley runtime, the WASI
//! host implementations, and the capability imports. An app's own
//! `hosts/embedded/` holds only its slot — what to draw, and what a button means.
//!
//! THIS CRATE NAMES NO CHIP, and that is checked rather than claimed: nothing
//! under `src/` mentions RP2350, Cortex-M, or a HAL. Anything that does lives in
//! a sibling directory named for its target — `rp2350/` for the badge's chip,
//! `qemu-armv7m/` for the emulator. So "embedded" here means the portable half,
//! and the moment it runs on a second chip that claim stops being a hope.
//! **`no_std` UNLESS `std` IS ASKED FOR** (Cargo.toml explains the two profiles).
//! The badge and the QEMU harness build this crate with `--no-default-features`;
//! the laptop tools keep the default. Everything below the `cfg` is what the
//! board gets.
#![cfg_attr(not(feature = "std"), no_std)]

extern crate alloc;

pub mod block_on;
pub mod catalog;
pub mod clock;
pub mod cli_bindings;
pub mod command;
pub mod fatview;
pub mod engine_config;
pub mod manifest;
pub mod minimal;
pub mod names;
#[rustfmt::skip]
mod names_gen;
pub mod uart;
pub mod pulley;
pub mod request;
pub mod spec;

/// The `wasmtime-wasi` host — a laptop's, and the one Phase 2 replaces.
///
/// Gated because `wasmtime-wasi` is `std`. `minimal.rs` is the badge's answer to
/// the same question and is always compiled, which is the point: the portable
/// host is the default and the convenient one is the exception.
#[cfg(feature = "std")]
pub mod host;
