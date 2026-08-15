//! PSRAM on QMI chip select 1 — the 8 MB the badge needs to instantiate anything.
//!
//! WHY THIS IS REQUIRED, measured rather than assumed (EMBEDDED-PLAN Phase 0d).
//! Loading a component costs 81 KB once `deserialize_raw` leaves the artifact in
//! flash. *Instantiating* one costs 2911 KB — of which 2048 KB is the guest's
//! linear memory and **863 KB is Wasmtime's own structures**. That 863 KB alone
//! exceeds the RP2350's 520 KB of SRAM, so no guest-side build knob avoids this.
//!
//! RAW MMIO, NOT THE PAC, and that is deliberate. Every access below happens
//! while the QMI is in direct mode — which means **XIP is off and the flash this
//! firmware executes from is unreadable**. A single non-inlined call into
//! flash-resident code in that window is a hard fault with no message. Raw
//! `read_volatile`/`write_volatile` in one `#[inline(never)]` function placed in
//! RAM is what makes that impossible rather than merely unlikely; typed PAC
//! accessors are almost certainly inlined at `opt-level = "s"` with LTO, and
//! "almost certainly" is the wrong standard for a fault that cannot be printed.
//!
//! Register offsets and field positions were read out of `rp235x-pac`'s generated
//! code rather than transcribed from the datasheet, and the CS pin's function
//! select out of the pico-SDK's own GPIO table (`XIP_CS1n` is **F9** on GPIO 8;
//! the PAC's shared enum calls 9 "GPCK", which is the typical name and wrong for
//! this pin).
//!
//! **NOT YET RUN ON HARDWARE.** It compiles and its constants are sourced, which
//! is all that can be claimed until a badge answers. See `init` for what the
//! first run should print and how each failure mode reads.
//!
//! ONE THING THAT *IS* VERIFIED, statically, because it is the assumption whose
//! failure has no symptom — the RAM-resident functions really are in RAM:
//!
//! ```text
//! $ llvm-nm target/thumbv8m.main-none-eabihf/release/dlc-rp2350-bringup
//! 20000000 t dlc_rp2350_bringup::psram::detect
//! 20000076 t dlc_rp2350_bringup::psram::configure
//!
//! $ llvm-objdump -h …
//! 6 .data 00000134 20000000 10016f38 TEXT
//! ```
//!
//! Both live inside `.data`, whose VMA is RAM (`0x2000_0000`) and whose LMA is
//! flash — `cortex-m-rt` copies it at boot. `detect` is 118 bytes and `configure`
//! 190, which is the whole 308-byte section, so neither has a tail left behind.
//! **Re-run those two commands after touching this file**; `#[link_section]` is a
//! request, and inlining or outlining can move code out from under it.
//!
//! The `__Thumbv7ABSLongThunk__` veneers `nm` also shows for both are expected
//! and harmless: they live in flash and are executed on the way *in*, while XIP
//! is still up, because direct mode is entered inside the callee and exited
//! before it returns.

use core::ptr::{read_volatile, write_volatile};

// ---- register block bases (rp235x-pac `mod_cortex_m.rs`) --------------------
const QMI_BASE: usize = 0x400d_0000;
const XIP_CTRL_BASE: usize = 0x400c_8000;
const IO_BANK0_BASE: usize = 0x4002_8000;
const PADS_BANK0_BASE: usize = 0x4003_8000;

// ---- QMI register offsets ---------------------------------------------------
const DIRECT_CSR: usize = QMI_BASE + 0x00;
const DIRECT_TX: usize = QMI_BASE + 0x04;
const DIRECT_RX: usize = QMI_BASE + 0x08;
const M1_TIMING: usize = QMI_BASE + 0x20;
const M1_RFMT: usize = QMI_BASE + 0x24;
const M1_RCMD: usize = QMI_BASE + 0x28;
const M1_WFMT: usize = QMI_BASE + 0x2c;
const M1_WCMD: usize = QMI_BASE + 0x30;

// ---- DIRECT_CSR fields ------------------------------------------------------
const CSR_EN: u32 = 1 << 0;
const CSR_BUSY: u32 = 1 << 1;
const CSR_ASSERT_CS1N: u32 = 1 << 3;
const CSR_TXEMPTY: u32 = 1 << 11;
const CSR_CLKDIV_LSB: u32 = 22;

// ---- DIRECT_TX fields -------------------------------------------------------
const TX_IWIDTH_LSB: u32 = 16;
const TX_OE: u32 = 1 << 19;
const WIDTH_Q: u32 = 2;

// ---- M1_TIMING fields -------------------------------------------------------
const T_CLKDIV_LSB: u32 = 0;
const T_RXDELAY_LSB: u32 = 8;
const T_MIN_DESELECT_LSB: u32 = 12;
const T_MAX_SELECT_LSB: u32 = 17;
const T_PAGEBREAK_LSB: u32 = 28;
const PAGEBREAK_1024: u32 = 2;

// ---- M1_RFMT / M1_WFMT fields ----------------------------------------------
const F_PREFIX_WIDTH_LSB: u32 = 0;
const F_ADDR_WIDTH_LSB: u32 = 2;
const F_SUFFIX_WIDTH_LSB: u32 = 4;
const F_DUMMY_WIDTH_LSB: u32 = 6;
const F_DATA_WIDTH_LSB: u32 = 8;
const F_PREFIX_LEN_LSB: u32 = 12;
const F_DUMMY_LEN_LSB: u32 = 16;
const PREFIX_LEN_8: u32 = 1;
const DUMMY_LEN_24: u32 = 6;

const XIP_CTRL_WRITABLE_M1: u32 = 1 << 11;

/// Where CS1 is mapped. CS0 is flash at `0x1000_0000`.
pub const PSRAM_XIP_BASE: usize = 0x1100_0000;

/// `XIP_CS1n` — **F9 on GPIO 8**, per the pico-SDK's RP2350 GPIO function table.
const FUNCSEL_XIP_CS1: u32 = 9;

/// APS6404L and compatibles answer `0x5D` to `read-id`. Anything else means the
/// device is absent, wired to a different pin, or not a PSRAM at all.
const KGD_APS6404: u8 = 0x5D;

/// The fastest an APS6404L will run.
const MAX_PSRAM_HZ: u32 = 133_000_000;

#[derive(Debug, Clone, Copy)]
pub struct Psram {
    pub base: *mut u8,
    pub len: usize,
}

#[derive(Debug, Clone, Copy)]
pub enum PsramError {
    /// No device answered. `kgd` is what came back — `0x00` or `0xFF` usually
    /// means nothing is driving the bus, i.e. the wrong CS pin.
    NotFound { kgd: u8, eid: u8 },
}

/// Bring PSRAM up on CS1 and return the window.
///
/// WHAT THE FIRST HARDWARE RUN SHOULD SHOW, because every failure here is silent
/// by nature and the symptoms are worth knowing in advance:
///
///   * `Ok(len = 8 MiB)` — the Tufty's part. Anything smaller means detection
///     read a real device but decoded its density differently than expected.
///   * `Err(NotFound { kgd: 0x00 })` or `0xFF` — nothing drove the bus. The CS
///     pin is the first suspect (`board::PSRAM_CS`), then whether this board
///     populates PSRAM at all.
///   * A hard fault with no output — this function was not actually in RAM, or
///     something it calls was not. That is what `#[link_section]` below prevents,
///     and the thing to re-check if it happens.
///
/// `sys_clk_hz` must be the *current* system clock: the QMI divisor is derived
/// from it, so calling this before `init_clocks_and_plls` settles, or passing a
/// stale value, produces a bus that is timed wrong rather than one that errors.
pub fn init(cs_pin: u8, sys_clk_hz: u32) -> Result<Psram, PsramError> {
    // Pin setup FIRST, and it is ordinary flash-resident code: it touches only
    // IO_BANK0 and PADS_BANK0, never the QMI, so XIP is still alive here.
    unsafe { configure_cs_pin(cs_pin) };

    // Timing computed HERE, in flash, and passed in — every instruction inside
    // the RAM-resident section is one that must be copied to RAM, so arithmetic
    // belongs outside it.
    //
    // divisor: round up so the bus never exceeds the part's 133 MHz.
    let divisor = sys_clk_hz.div_ceil(MAX_PSRAM_HZ);
    // rxdelay: one extra cycle above 100 MHz, where round-trip delay stops
    // fitting inside a single clock.
    let rxdelay = if sys_clk_hz / divisor > 100_000_000 { divisor + 1 } else { divisor };
    // MAX_SELECT is in units of 64 system clocks and must keep CS low for less
    // than 8 us — the APS6404L refreshes internally and a longer assertion
    // loses data. 8 us * f / 64.
    let max_select = (sys_clk_hz / 1_000_000 * 8) / 64;
    // MIN_DESELECT is in system clocks and must be at least 18 ns.
    let min_deselect = (sys_clk_hz / 1_000_000 * 18).div_ceil(1000);

    // INTERRUPTS OFF ACROSS THE WHOLE DIRECT-MODE WINDOW. An interrupt taken
    // while XIP is down would vector into unreadable flash. Disabled here rather
    // than inside the RAM function so the RAM function stays as small as
    // possible.
    cortex_m::interrupt::disable();
    let (kgd, eid) = unsafe { detect() };
    let result = if kgd == KGD_APS6404 {
        let len = decode_size(eid);
        unsafe { configure(divisor, rxdelay, max_select, min_deselect) };
        Ok(Psram { base: PSRAM_XIP_BASE as *mut u8, len })
    } else {
        Err(PsramError::NotFound { kgd, eid })
    };
    unsafe { cortex_m::interrupt::enable() };
    result
}

/// Density from the extended ID, per the APS6404L datasheet.
fn decode_size(eid: u8) -> usize {
    const MIB: usize = 1024 * 1024;
    match eid >> 5 {
        0b000 => 2 * MIB,
        0b001 => 4 * MIB,
        0b010 => 8 * MIB,
        // 0x26 is the 8 MiB part's full EID; some report it without the density
        // bits above, so it is matched whole.
        _ if eid == 0x26 => 8 * MIB,
        // A device answered with the right KGD but an unfamiliar density. 1 MiB
        // is the family's floor and the safe assumption — under-reporting costs
        // heap, over-reporting corrupts it.
        _ => MIB,
    }
}

/// IE on, OD off, funcsel, and **ISO cleared** — that last one is RP2350-only.
///
/// Pads come out of reset ISOLATED on this chip, and a pad left isolated is
/// simply not connected: the QMI drives a pin that goes nowhere, detection reads
/// back `0xFF`, and nothing reports an error. RP2040 had no such bit, so code
/// ported from it is missing exactly this line.
unsafe fn configure_cs_pin(gpio: u8) {
    let pad = PADS_BANK0_BASE + 0x04 + 4 * gpio as usize;
    let ctrl = IO_BANK0_BASE + 8 * gpio as usize + 4;
    unsafe {
        const PAD_IE: u32 = 1 << 6;
        const PAD_OD: u32 = 1 << 7;
        const PAD_ISO: u32 = 1 << 8;
        let v = read_volatile(pad as *const u32);
        write_volatile(pad as *mut u32, (v | PAD_IE) & !(PAD_OD | PAD_ISO));
        write_volatile(ctrl as *mut u32, FUNCSEL_XIP_CS1);
    }
}

/// Read the device ID over direct mode. **Runs from RAM.**
///
/// Returns `(kgd, eid)`. Takes no arguments that need computing and returns
/// plain bytes, so the caller does the interpreting from flash.
#[inline(never)]
#[link_section = ".data.ram_func"]
unsafe fn detect() -> (u8, u8) {
    unsafe {
        // Divide the clock hard: detection happens before timing is configured,
        // so it must work at whatever the system clock is.
        write_volatile(DIRECT_CSR as *mut u32, (30 << CSR_CLKDIV_LSB) | CSR_EN);
        while read_volatile(DIRECT_CSR as *const u32) & CSR_BUSY != 0 {}

        // EXIT QPI FIRST, sent AS QUAD. A device left in QPI mode by a previous
        // boot will not understand a single-lane `read-id`, so a warm reset
        // would detect nothing while a cold one worked — a difference that looks
        // like flaky hardware.
        csr_set(CSR_ASSERT_CS1N);
        write_volatile(DIRECT_TX as *mut u32, TX_OE | (WIDTH_Q << TX_IWIDTH_LSB) | 0xf5);
        while read_volatile(DIRECT_CSR as *const u32) & CSR_BUSY != 0 {}
        let _ = read_volatile(DIRECT_RX as *const u32);
        csr_clear(CSR_ASSERT_CS1N);

        // read-id: 0x9f then six bytes. KGD is byte 5, EID byte 6.
        csr_set(CSR_ASSERT_CS1N);
        let mut kgd = 0u8;
        let mut eid = 0u8;
        let mut i = 0;
        while i < 7 {
            write_volatile(DIRECT_TX as *mut u32, if i == 0 { 0x9f } else { 0xff });
            while read_volatile(DIRECT_CSR as *const u32) & CSR_TXEMPTY == 0 {}
            while read_volatile(DIRECT_CSR as *const u32) & CSR_BUSY != 0 {}
            let rx = read_volatile(DIRECT_RX as *const u32) as u8;
            if i == 5 {
                kgd = rx;
            } else if i == 6 {
                eid = rx;
            }
            i += 1;
        }
        csr_clear(CSR_ASSERT_CS1N | CSR_EN);
        (kgd, eid)
    }
}

/// Reset the device, put it in QPI mode, and program the M1 window. **Runs from RAM.**
#[inline(never)]
#[link_section = ".data.ram_func"]
unsafe fn configure(divisor: u32, rxdelay: u32, max_select: u32, min_deselect: u32) {
    unsafe {
        write_volatile(DIRECT_CSR as *mut u32, (30 << CSR_CLKDIV_LSB) | CSR_EN);
        while read_volatile(DIRECT_CSR as *const u32) & CSR_BUSY != 0 {}

        // reset-enable, reset, then enter-QPI — each its own CS assertion,
        // because the device latches a command on the rising edge.
        let cmds = [0x66u32, 0x99, 0x35];
        let mut i = 0;
        while i < 3 {
            csr_set(CSR_ASSERT_CS1N);
            write_volatile(DIRECT_TX as *mut u32, cmds[i]);
            while read_volatile(DIRECT_CSR as *const u32) & CSR_BUSY != 0 {}
            let _ = read_volatile(DIRECT_RX as *const u32);
            csr_clear(CSR_ASSERT_CS1N);
            i += 1;
        }
        // Leave direct mode before the window is programmed; from here the QMI
        // serves CS1 automatically.
        write_volatile(DIRECT_CSR as *mut u32, 0);

        write_volatile(
            M1_TIMING as *mut u32,
            (PAGEBREAK_1024 << T_PAGEBREAK_LSB)
                | (max_select << T_MAX_SELECT_LSB)
                | (min_deselect << T_MIN_DESELECT_LSB)
                | (rxdelay << T_RXDELAY_LSB)
                | (divisor << T_CLKDIV_LSB),
        );

        // 0xEB — quad read, everything on four lanes, 24 dummy cycles.
        write_volatile(
            M1_RFMT as *mut u32,
            (WIDTH_Q << F_PREFIX_WIDTH_LSB)
                | (WIDTH_Q << F_ADDR_WIDTH_LSB)
                | (WIDTH_Q << F_SUFFIX_WIDTH_LSB)
                | (WIDTH_Q << F_DUMMY_WIDTH_LSB)
                | (WIDTH_Q << F_DATA_WIDTH_LSB)
                | (PREFIX_LEN_8 << F_PREFIX_LEN_LSB)
                | (DUMMY_LEN_24 << F_DUMMY_LEN_LSB),
        );
        write_volatile(M1_RCMD as *mut u32, 0xeb);

        // 0x38 — quad write. No dummy cycles: writes do not turn the bus around.
        write_volatile(
            M1_WFMT as *mut u32,
            (WIDTH_Q << F_PREFIX_WIDTH_LSB)
                | (WIDTH_Q << F_ADDR_WIDTH_LSB)
                | (WIDTH_Q << F_SUFFIX_WIDTH_LSB)
                | (WIDTH_Q << F_DUMMY_WIDTH_LSB)
                | (WIDTH_Q << F_DATA_WIDTH_LSB)
                | (PREFIX_LEN_8 << F_PREFIX_LEN_LSB),
        );
        write_volatile(M1_WCMD as *mut u32, 0x38);

        // XIP is READ-ONLY until this bit is set, and a heap that cannot be
        // written is not a heap. Without it the failure is a store that silently
        // does nothing, which reads as memory corruption.
        let ctrl = XIP_CTRL_BASE as *mut u32;
        write_volatile(ctrl, read_volatile(ctrl) | XIP_CTRL_WRITABLE_M1);
    }
}

// Read-modify-write of DIRECT_CSR. `#[inline(always)]` because these are called
// from the RAM-resident functions above and must not become a call into flash.
#[inline(always)]
unsafe fn csr_set(bits: u32) {
    unsafe { write_volatile(DIRECT_CSR as *mut u32, read_volatile(DIRECT_CSR as *const u32) | bits) }
}

#[inline(always)]
unsafe fn csr_clear(bits: u32) {
    unsafe {
        write_volatile(DIRECT_CSR as *mut u32, read_volatile(DIRECT_CSR as *const u32) & !bits)
    }
}
