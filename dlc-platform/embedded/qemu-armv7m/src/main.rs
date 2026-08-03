//! Does Pulley run our component on a 32-bit ARM core?
//!
//! The question a 64-bit laptop cannot answer, because a host executes only its
//! own Pulley pointer width. QEMU's Cortex-M33 answers it, and the RAM figure it
//! prints is the first real evidence for Phase 0's gate.
#![no_std]
#![no_main]

extern crate alloc;

mod platform;
use panic_semihosting as _;

use core::fmt::Write;
use cortex_m_rt::entry;
use cortex_m_semihosting::{debug, hio};
use embedded_alloc::LlffHeap as Heap;

#[global_allocator]
static HEAP: Heap = Heap::empty();

/// THE BADGE'S SRAM, not the emulator's.
///
/// QEMU offers 4 MB, which would make this test pass for the wrong reason. The
/// RP2350 has 520 KB and its PSRAM is an 8 MB extension reached over QSPI — so
/// pinning the heap here is how "does it fit in SRAM alone" gets asked honestly.
/// Raise it to model PSRAM once the answer to the smaller question is known.
const HEAP_BYTES: usize = 3072 * 1024;
static mut HEAP_MEM: [u8; HEAP_BYTES] = [0; HEAP_BYTES];

#[entry]
fn main() -> ! {
    unsafe {
        let ptr = &raw mut HEAP_MEM as *mut u8;
        HEAP.init(ptr as usize, HEAP_BYTES);
    }
    let mut out = hio::hstdout().unwrap();
    let _ = writeln!(out, "=== ILC on an emulated 32-bit ARM (QEMU mps2-an385) ===");
    let _ = writeln!(out, "heap: {} KB (pinned to the RP2350's SRAM)", HEAP_BYTES / 1024);

    let mut config = wasmtime::Config::new();
    // pulley32 — the badge's width, and the whole point of running here.
    if let Err(_) = config.target("pulley32") {
        let _ = writeln!(out, "config.target(pulley32): FAILED");
        debug::exit(debug::EXIT_FAILURE);
    }
    config.wasm_component_model(true);

    match wasmtime::Engine::new(&config) {
        Ok(engine) => {
            let _ = writeln!(out, "engine: created for pulley32 on a 32-bit core");

            // THE REAL QUESTION. A precompiled component lives in flash; loading
            // it builds runtime structures in RAM, and 512 KB is what the badge
            // has before PSRAM. This is the first honest measurement of Phase 0's
            // gate — on the right pointer width, with the right heap size.
            //
            // `deserialize`, not `new`: a no_std Wasmtime has no compiler, so the
            // .cwasm is AOT-built by `make embedded-cwasm`. Unsafe because
            // precompiled bytes are trusted by construction — they came from our
            // own build, not from the network.
            // GITIGNORED — run `make qemu-payload` first on a fresh clone.
            // Not committed: 1.6 MB, derived twice over (engine -> component ->
            // .cwasm), and version-locked to the Wasmtime that produced it, so a
            // stored copy would go stale silently.
            const CWASM: &[u8] = include_bytes!("../hello.pulley32.cwasm");
            let _ = writeln!(out, "payload: {} bytes of pulley32 in flash", CWASM.len());

            match unsafe { wasmtime::component::Component::deserialize(&engine, CWASM) } {
                Ok(_component) => {
                    let _ = writeln!(out, "component: DESERIALIZED — it fits");
                    let _ = writeln!(out, "RESULT: PASS");
                    debug::exit(debug::EXIT_SUCCESS);
                }
                Err(e) => {
                    // Printing the error costs code size, and is worth it: "it
                    // failed" sent this investigation down a memory rabbit hole
                    // for three builds.
                    let _ = writeln!(out, "component: deserialize FAILED: {e}");
                    let _ = writeln!(out, "RESULT: FAIL");
                    debug::exit(debug::EXIT_FAILURE);
                }
            }
        }
        Err(_) => {
            let _ = writeln!(out, "engine: creation FAILED");
            debug::exit(debug::EXIT_FAILURE);
        }
    }
    loop {}
}
