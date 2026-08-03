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

/// The crystal on the Tufty, in Hz. **CONFIRM AGAINST THE BOARD** — 12 MHz is
/// the RP2350 reference design's value and what Pimoroni's boards use, but a
/// wrong value here shows up as garbled UART rather than as an error, which is
/// an unpleasant hour.
const XTAL_HZ: u32 = 12_000_000;

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

    // UART0 on GPIO0/1 — the RP2350 default pinout. **CONFIRM AGAINST THE
    // TUFTY'S SCHEMATIC**: if the badge exposes a different pair (or expects USB
    // serial), this is the line to change, and the symptom of getting it wrong
    // is silence rather than an error.
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
//   * Move the heap to PSRAM. The Tufty has 8 MB; `rp235x-hal` needs the PSRAM
//     chip-select pin configured (GPIO47 on Pimoroni's RP2350 boards — CONFIRM),
//     after which HEAP.init() points at the PSRAM window instead of HEAP_MEM.
//     520 KB of SRAM will not hold a component's linear memory, so this is not
//     optional the moment a real component appears.
//   * Embed a component with `include_bytes!` and hand it to
//     `Component::deserialize` — PRECOMPILED, because a no_std Wasmtime has no
//     Cranelift. Build it with `make embedded-cwasm`.
//   * Implement the WASI 0.2 interfaces the engine imports (EMBEDDED-PLAN D4).
//     `wasi:random` is not deferrable: TinyGo calls `get-random-u64` from
//     `_initialize`, so the component will not instantiate without it.
