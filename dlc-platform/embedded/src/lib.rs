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
extern crate alloc;

pub mod block_on;
pub mod cli_bindings;
pub mod host;
pub mod minimal;
pub mod uart;
pub mod pulley;
