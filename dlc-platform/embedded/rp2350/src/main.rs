//! STEP 1 — does the badge boot with Wasmtime linked?
//!
//! Deliberately answers ONE question. Everything here exists to find out whether
//! Wasmtime `no_std` + Pulley + component-model links for `thumbv8m`, and
//! whether the board comes up with a PSRAM-backed heap and a usable UART. No
//! component is loaded yet — that is step 2, and keeping it out means a failure
//! now is a *linking* or *boot* failure rather than a mystery.
//!
//! WHAT WILL PROBABLY BREAK FIRST, in order:
//!
//!   1. Missing platform symbols. Wasmtime `no_std` expects the embedder to
//!      supply the equivalent of `wasmtime-platform.h` — virtual memory and
//!      friends. Those surface as LINK errors naming the symbol, which is the
//!      good kind of unimplemented: the list below is the work.
//!   2. The heap. 520 KB of SRAM cannot hold a component's linear memory, so the
//!      allocator must live in the 8 MB of PSRAM and be initialised before
//!      anything Wasmtime touches.
//!   3. Size. Wasmtime's `.text` on top of a 2.21 MB payload, in 16 MB of flash.
#![no_std]
#![no_main]

extern crate alloc;

mod board;
mod platform;

// Brought in for its #[panic_handler]; nothing calls it directly.
use panic_halt as _;

use embedded_alloc::LlffHeap as Heap;
use rp235x_hal as hal;

use hal::fugit::RateExtU32;
use hal::uart::{DataBits, StopBits, UartConfig};
use hal::Clock;

use core::fmt::Write;

#[global_allocator]
static HEAP: Heap = Heap::empty();

/// The RP2350 boot block. Without it the chip does not consider this a valid
/// image and will sit in the bootloader instead of telling you why.
#[link_section = ".start_block"]
#[used]
pub static IMAGE_DEF: hal::block::ImageDef = hal::block::ImageDef::secure_exe();

/// The crystal on the Tufty, in Hz. **CONFIRMED 2026-08-07** — see `board.rs`:
/// neither Pimoroni board header overrides `XOSC_HZ`, so the pico-SDK default of
/// 12 MHz is this board's value. A wrong one here shows up as garbled UART rather
/// than as an error, which is why it was worth checking rather than assuming.
const XTAL_HZ: u32 = board::XTAL_HZ;

/// Heap size for step 1. Deliberately SMALL and in SRAM: this step is not trying
/// to run a component, and starting with the simple allocator isolates "does it
/// boot" from "is PSRAM wired up". Step 2 moves this to PSRAM and grows it —
/// see the note at the bottom of this file.
const HEAP_BYTES: usize = 64 * 1024;
static mut HEAP_MEM: [u8; HEAP_BYTES] = [0; HEAP_BYTES];

#[hal::entry]
fn main() -> ! {
    // The heap FIRST: anything below may allocate, and an allocation before the
    // allocator exists is a hard fault with no message.
    unsafe {
        let ptr = &raw mut HEAP_MEM as *mut u8;
        HEAP.init(ptr as usize, HEAP_BYTES);
    }

    let mut pac = hal::pac::Peripherals::take().unwrap();
    let mut watchdog = hal::Watchdog::new(pac.WATCHDOG);
    let clocks = hal::clocks::init_clocks_and_plls(
        XTAL_HZ,
        pac.XOSC,
        pac.CLOCKS,
        pac.PLL_SYS,
        pac.PLL_USB,
        &mut pac.RESETS,
        &mut watchdog,
    )
    .unwrap();

    let sio = hal::Sio::new(pac.SIO);
    let pins = hal::gpio::Pins::new(pac.IO_BANK0, pac.PADS_BANK0, sio.gpio_bank0, &mut pac.RESETS);

    // UART0 on GPIO0/1 — **CONFIRMED 2026-08-07, and for a better reason than it
    // was chosen.** The Tufty declares no default UART at all, so there was no
    // convention to be right about; GPIO0/1 are `CL0`/`CL1`, two of the four
    // crocodile-clip pads, which is where a serial adapter can physically attach.
    // See `board.rs`.
    let uart_pins = (
        pins.gpio0.into_function::<hal::gpio::FunctionUart>(),
        pins.gpio1.into_function::<hal::gpio::FunctionUart>(),
    );
    let mut uart = hal::uart::UartPeripheral::new(pac.UART0, uart_pins, &mut pac.RESETS)
        .enable(
            UartConfig::new(115_200.Hz(), DataBits::Eight, None, StopBits::One),
            clocks.peripheral_clock.freq(),
        )
        .unwrap();

    let _ = writeln!(uart, "\r\n=== ILC badge bring-up (step 1) ===");
    let _ = writeln!(uart, "board:   RP2350B @ {} Hz", clocks.system_clock.freq().to_Hz());
    let _ = writeln!(uart, "heap:    {} bytes (SRAM; PSRAM comes in step 2)", HEAP_BYTES);

    // THE POINT OF STEP 1. Touching Wasmtime is what forces the linker to
    // resolve it — a crate that is merely a dependency gets dropped, and the
    // build would "pass" while proving nothing.
    //
    // `Config::new()` allocates, so this exercises the heap too. If the firmware
    // reaches the line after this, Wasmtime linked and runs on this chip.
    let mut config = wasmtime::Config::new();
    config.target("pulley32").ok();
    config.wasm_component_model(true);

    match wasmtime::Engine::new(&config) {
        Ok(_engine) => {
            let _ = writeln!(uart, "wasmtime: ENGINE CREATED — pulley32 + component-model");
            let _ = writeln!(uart, "step 1:  PASS");
        }
        Err(_) => {
            // No `{e}`: formatting the error pulls in more of core::fmt than a
            // bring-up needs, and the interesting fact is binary.
            let _ = writeln!(uart, "wasmtime: engine creation FAILED");
            let _ = writeln!(uart, "step 1:  FAIL");
        }
    }

    loop {
        cortex_m::asm::wfi();
    }
}

// STEP 2 STARTS HERE, and this note is the handover:
//
//   * Embed a component with `include_bytes!` and hand it to
//     **`Component::deserialize_raw`, NOT `deserialize`** — PRECOMPILED, because
//     a no_std Wasmtime has no Cranelift. Build it with `make embedded-cwasm`.
//
//     `deserialize` copies the artifact into a fresh allocation, which is where
//     "PSRAM is a prerequisite" came from: 890 KB of contiguous heap that 520 KB
//     of SRAM can never provide. `deserialize_raw` takes externally-owned memory
//     the runtime only reads, so the artifact stays in flash where XIP already
//     makes it addressable — Pulley bytecode is interpreted, never executed
//     natively, so it has no reason to be in RAM. **Measured in QEMU 2026-08-07:
//     81 KB of heap instead of 890 KB, at the badge's real SRAM size.** The
//     payload must be 16-byte aligned; see the `Aligned` wrapper in
//     `qemu-armv7m/src/main.rs`, because `include_bytes!` alone promises 1.
//
//   * PSRAM is therefore **no longer the gate, and is still probably wanted**.
//     What has been measured is *loading*; instantiation additionally needs the
//     component's linear memory, which is unmeasured. Measure it before building
//     an allocator — the whole point of the above is that the number to size
//     against is now a different, smaller one. The chip select is `board::PSRAM_CS`
//     = **GPIO8** (the plan's GPIO47 was another Pimoroni board's pin), and
//     `embassy-rp`'s `psram` module is prior art worth reading first.
//   * Implement the WASI 0.2 interfaces the engine imports (EMBEDDED-PLAN D4).
//     `wasi:random` is not deferrable: TinyGo calls `get-random-u64` from
//     `_initialize`, so the component will not instantiate without it.
