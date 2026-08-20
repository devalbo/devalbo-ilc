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
use core::alloc::{GlobalAlloc, Layout};
use core::sync::atomic::{AtomicUsize, Ordering};
use embedded_alloc::LlffHeap as Heap;

static HEAP: Heap = Heap::empty();

/// A pass-through allocator that remembers the BIGGEST single request.
///
/// WHY, and it is the only way to ask this question here. After
/// `deserialize_raw`, ~2.8 MB of the cost is instantiation — but "guest linear
/// memory" and "Wasmtime's own structures" are indistinguishable in a total.
/// The component API cannot help: a component instance exposes component-level
/// exports, not the core module's `memory`, so there is nothing to read a size
/// from. The allocator sees what neither can show: Wasmtime's `MallocMemory`
/// backs linear memory with a single `Vec`, so the guest's heap appears as one
/// large allocation and nothing else here comes close.
///
/// Diagnostic scaffolding, not a feature — but cheap enough to leave, and the
/// next person asking "where did the RAM go" would otherwise rebuild it.
struct Tracking;

static PEAK_ALLOC: AtomicUsize = AtomicUsize::new(0);

unsafe impl GlobalAlloc for Tracking {
    unsafe fn alloc(&self, layout: Layout) -> *mut u8 {
        PEAK_ALLOC.fetch_max(layout.size(), Ordering::Relaxed);
        unsafe { HEAP.alloc(layout) }
    }
    unsafe fn dealloc(&self, ptr: *mut u8, layout: Layout) {
        unsafe { HEAP.dealloc(ptr, layout) }
    }
    unsafe fn realloc(&self, ptr: *mut u8, layout: Layout, new_size: usize) -> *mut u8 {
        // GROWTH LIVES HERE, not in `alloc`. The guest's memory starts at the 2
        // pages its core module declares and grows by `memory.grow`, which is a
        // `Vec` realloc — so watching only `alloc` would report 128 KB and miss
        // the entire thing being measured.
        PEAK_ALLOC.fetch_max(new_size, Ordering::Relaxed);
        unsafe { HEAP.realloc(ptr, layout, new_size) }
    }
}

#[global_allocator]
static ALLOC: Tracking = Tracking;

/// The FALLBACK heap, used only if the PSRAM window below is not there.
///
/// **DELIBERATELY OVERSIZED, and the verdict is computed rather than pinned.**
/// This used to be pinned to the badge's 520 KB so that "does it fit in SRAM
/// alone" was asked honestly — which was the right question while the answer was
/// unknown, and the wrong instrument for finding a number. A pinned heap answers
/// with a yes or a panic, and the panic reports the allocation that failed rather
/// than the total needed; that is how this harness spent a run learning only that
/// 512 KB is not enough.
///
/// Giving it room and printing the PEAK answers "what does it cost", which is the
/// number an allocator has to be sized against. The fit is then one comparison
/// against `SRAM_BYTES`, computed and printed, so it cannot quietly stop being
/// checked.
const HEAP_BYTES: usize = 3 * 1024 * 1024;
static mut HEAP_MEM: [u8; HEAP_BYTES] = [0; HEAP_BYTES];

/// The MPS2's separate PSRAM window, used when it is really there.
///
/// 3 MB of `.bss` is as much heap as a 4 MB RAM region can give, and hello wants
/// more than that — so the measurement would have been capped by the emulator
/// rather than by the question. QEMU's MPS2 boards model a 16 MB PSRAM here,
/// which is enough room to find the real number.
///
/// **PROBED, NOT ASSUMED.** The docs do not state this region, so the firmware
/// writes a pattern at both ends and reads it back; a machine without it falls
/// back to `HEAP_MEM` and says so. Assuming it would turn "the region is not
/// modelled" into a bus fault at some later, unrelated line.
///
/// A pleasing accident rather than a plan: the emulator's spare RAM is called
/// PSRAM and the badge's is too, but they are different hardware and this says
/// nothing about the RP2350's.
const PSRAM_BASE: usize = 0x2100_0000;
const PSRAM_LEN: usize = 16 * 1024 * 1024;

/// Write a pattern at both ends of the window and read it back.
///
/// Both ends, because a partial or mirrored mapping would pass a single-word test
/// and then corrupt the heap in a way that looks like an allocator bug.
unsafe fn psram_present() -> bool {
    let lo = PSRAM_BASE as *mut u32;
    let hi = (PSRAM_BASE + PSRAM_LEN - 4) as *mut u32;
    core::ptr::write_volatile(lo, 0xDEAD_BEEF);
    core::ptr::write_volatile(hi, 0x1234_5678);
    core::ptr::read_volatile(lo) == 0xDEAD_BEEF && core::ptr::read_volatile(hi) == 0x1234_5678
}

/// What the RP2350 actually has, and therefore what the numbers below mean.
/// The badge adds 8 MB of PSRAM over QSPI; this is the SRAM that needs no driver.
const SRAM_BYTES: usize = 520 * 1024;

/// The payload, aligned — and the alignment is load-bearing, not tidiness.
///
/// Wasmtime parses a `.cwasm` as an ELF, and `object`'s header reads require the
/// buffer be aligned to the header type. `deserialize` never had to care: it
/// COPIES into a fresh allocation, and an allocator hands back aligned memory.
/// Pointing at flash means the alignment is whatever the linker gave us, and
/// `include_bytes!` alone promises 1.
#[repr(C, align(16))]
struct Aligned<T: ?Sized>(T);

/// THE BADGE'S OWN ARTIFACT, borrowed rather than copied. `build.rs` places
/// `build/hello.pulley32.cwasm` into OUT_DIR, so the emulator and the board
/// demonstrably run the same bytes — which is what makes a green run here mean
/// anything about the board. Run `make badge-cwasm` on a fresh clone; the
/// artifact is gitignored because it is derived and version-locked to the
/// Wasmtime that produced it.
static CWASM: &Aligned<[u8]> =
    &Aligned(*include_bytes!(concat!(env!("OUT_DIR"), "/payload.cwasm")));

/// THE OPEN QUESTION after `deserialize_raw`: what does *instantiation* cost?
///
/// Loading builds the code structures; instantiating additionally allocates the
/// component's linear memory, its resource tables, and one store — and none of
/// that had been measured when PSRAM was being planned around a number that
/// turned out to be avoidable. This is that measurement, on the badge's pointer
/// width — and with room to spare rather than at the badge's SRAM size, because
/// the goal is the cost, not a pass/fail (see `HEAP_BYTES`).
///
/// Through `MinimalHost` — the SAME host the badge will run, not a stand-in.
/// `dlc-platform-embedded` builds without `std` precisely so this harness and the
/// firmware share one implementation; a locally-written linker here would measure
/// the harness rather than the thing being shipped.
fn measure_instantiation(out: &mut impl Write, payload: &'static [u8]) {
    use dlc_platform_embedded::manifest;
use dlc_platform_embedded::minimal::MinimalHost;
    use dlc_platform_embedded::pulley::PulleyWidth;

    let before = HEAP.used();
    // SAFETY: `payload` is our own build's .cwasm, 16-byte aligned by the
    // `Aligned` wrapper, and `'static` in flash — the three things
    // `from_precompiled` asks for.
    let mut host = match unsafe { MinimalHost::from_precompiled(payload, PulleyWidth::Bits32) } {
        Ok(h) => h,
        Err(e) => {
            let _ = writeln!(out, "instantiate: FAILED: {e:?}");
            let _ = writeln!(out, "RESULT: FAIL");
            debug::exit(debug::EXIT_FAILURE);
            return;
        }
    };
    let instantiated = HEAP.used() - before;
    let _ = writeln!(
        out,
        "heap after instantiate: {} KB",
        instantiated / 1024
    );

    // AND THEN RUN IT. Instantiation proves the imports link; a command proves
    // the interpreter executes our engine at 32 bits, which is the claim the
    // whole embedded tier rests on. hello's `greet` is method 10000.
    // FIRST, THE MANIFEST — and this is the only place it is proved.
    //
    // The encoder in `dlc_platform_embedded::manifest` is hand-rolled protobuf,
    // because a `no_std` firmware has no allocator to give a real one. Its unit
    // tests pin the BYTES, which catches a wrong field number but cannot catch a
    // wrong understanding of the format: bytes that are self-consistently wrong
    // pass a byte test and are refused by an engine.
    //
    // Here a REAL ENGINE decodes them, on a 32-bit core, through the badge's own
    // host. That is the difference between "these are the bytes I meant to
    // write" and "these are the bytes an engine accepts".
    let env = manifest::encode(manifest::WorldManifest {
        revision: 1,
        outlet: manifest::TEXT_OUTLET_UART,
        // The harness prints over semihosting to a terminal of unknown size.
        // Zero is UNMEASURED, which is the honest answer and the one an app reads
        // as "wrap however you like".
        cols: 0,
        rows: 0,
        // NO STATUS INDICATOR: this is an emulated core with a serial console and
        // nothing to light up. Saying so explicitly is what lets an app skip work
        // it knows is invisible (Decision 33).
        status: dlc_platform_embedded::control::STATUS_OUTLET_NONE,
        // AND NO WORLD DECLARED. This harness is not a host slot anybody ships;
        // `undefined` is the honest answer and the common one.
        world: 0,
    });
    match host.execute(manifest::METHOD_ID_SET_WORLD_MANIFEST, env.as_bytes()) {
        Ok(r) if r.success => {
            let _ = writeln!(out, "set-environment: success");
        }
        Ok(r) => {
            let _ = writeln!(
                out,
                "set-environment: REFUSED: {}",
                r.error.as_deref().unwrap_or("no reason")
            );
        }
        Err(e) => {
            let _ = writeln!(out, "set-environment: FAILED: {e:?}");
        }
    }

    match host.execute(10000, &[]) {
        Ok(r) => {
            let _ = writeln!(out, "execute(10000): success={}", r.success);
            // STDOUT, not `output`. A command's return value is PROTOBUF, and
            // rendering it needs the app's schema — which no generic host has.
            // `hello` looked readable only because its encoded response happened
            // to be valid UTF-8 (leading field tag 0x0a reads as a newline, which
            // is where that stray blank line came from); an app with a numeric
            // field printed nothing at all. Found by running badge-selftest here.
            let printed = host.stdout();
            if let Ok(s) = core::str::from_utf8(&printed) {
                if !s.is_empty() {
                    let _ = writeln!(out, "stdout:\n{s}");
                }
            }
            if let Ok(s) = core::str::from_utf8(&r.output) {
                if !s.is_empty() {
                    let _ = writeln!(out, "output(raw): {s}");
                }
            }
            let peak = HEAP.used() - before;
            let _ = writeln!(out, "heap after execute: {} KB", peak / 1024);
            // THE SPLIT the total cannot show: the largest single request is the
            // guest's linear memory, because MallocMemory backs it with one Vec.
            // Everything else Wasmtime allocates here is orders smaller.
            let biggest = PEAK_ALLOC.load(Ordering::Relaxed);
            let _ = writeln!(
                out,
                "largest single alloc: {} KB (guest linear memory) -> {} KB is everything else",
                biggest / 1024,
                peak.saturating_sub(biggest) / 1024
            );
            // THE VERDICT, computed. What matters is not whether this run passed
            // but whether the badge has the RAM, and that is one comparison —
            // stated here so it cannot quietly stop being checked.
            let _ = writeln!(
                out,
                "verdict: {} KB needed vs {} KB of RP2350 SRAM -> {}",
                peak / 1024,
                SRAM_BYTES / 1024,
                if peak <= SRAM_BYTES { "FITS, no PSRAM required" } else { "PSRAM REQUIRED" }
            );
        }
        Err(e) => {
            let _ = writeln!(out, "execute(10000): FAILED: {e:?}");
            let _ = writeln!(out, "RESULT: FAIL");
            debug::exit(debug::EXIT_FAILURE);
        }
    }
}

#[entry]
fn main() -> ! {
    // The probe must precede the heap, and the heap must precede everything: an
    // allocation before the allocator exists is a hard fault with no message.
    let (heap_base, heap_len, heap_where) = unsafe {
        if psram_present() {
            (PSRAM_BASE, PSRAM_LEN, "MPS2 PSRAM window")
        } else {
            (&raw mut HEAP_MEM as *mut u8 as usize, HEAP_BYTES, "SRAM .bss")
        }
    };
    unsafe { HEAP.init(heap_base, heap_len) };

    let mut out = hio::hstdout().unwrap();
    let _ = writeln!(out, "=== ILC on an emulated 32-bit ARM (QEMU mps2-an385) ===");
    // The heap size here is the EMULATOR's headroom, not a claim about the badge.
    // What gets compared against the RP2350 is the peak below.
    let _ = writeln!(out, "heap: {} KB in {}", heap_len / 1024, heap_where);

    let mut config = wasmtime::Config::new();
    // pulley32 — the badge's width, and the whole point of running here.
    if let Err(_) = config.target("pulley32") {
        let _ = writeln!(out, "config.target(pulley32): FAILED");
        debug::exit(debug::EXIT_FAILURE);
    }
    config.wasm_component_model(true);
    // THE SHARED LIST, not a local copy — `dlc-platform-embedded`'s `engine_config`, the
    // same function `pulley_engine` and the AOT compiler call.
    //
    // This site is why that function exists. It carried its own two settings and
    // was right until the artifact gained three more, at which point the harness
    // rejected the very payload it had just been taught to produce:
    // "compiled with a memory reservation of '0' but '10485760' is expected for
    // the host". Three places had to agree; now there is one.
    dlc_platform_embedded::engine_config::no_virtual_memory(&mut config);

    match wasmtime::Engine::new(&config) {
        Ok(engine) => {
            let _ = writeln!(out, "engine: created for pulley32 on a 32-bit core");

            // THE REAL QUESTION, and it is no longer the one this harness first
            // asked. `deserialize` COPIES the artifact into a fresh allocation,
            // so loading a 1.59 MB payload needed 1.59 MB of contiguous heap and
            // failed at the badge's 520 KB — which is where "PSRAM is a
            // prerequisite" came from.
            //
            // `deserialize_raw` does not copy. It takes externally-owned memory
            // and, per engine.rs, "the memory provided is guaranteed to only be
            // immutably [read] by the runtime" — so the payload can stay in flash,
            // where XIP already makes it directly addressable. Pulley bytecode is
            // INTERPRETED, never executed natively, so there is no reason it has
            // to live in RAM at all.
            //
            // What is measured below is therefore the real number: the RAM cost
            // of the runtime structures alone, with the artifact left where it
            // was. FALSIFY by swapping this back to `deserialize` — at 512 KB it
            // must go red with "out of memory", which is what it did before.
            //
            // Unsafe because precompiled bytes are trusted by construction: they
            // came from our own build, not from the network. `deserialize_raw`
            // adds one obligation on top — the memory must outlive the component
            // and never be modified — and a `static` in flash satisfies both by
            // being immutable and 'static.
            let payload: &'static [u8] = &CWASM.0;
            let _ = writeln!(out, "payload: {} bytes of pulley32", payload.len());
            // PRINTED, NOT ASSUMED. The whole claim is that this lives in flash;
            // RAM starts at 0x20000000 here, so an address below that is the
            // evidence. A linker change that quietly moved it into .data would
            // otherwise make this test pass while proving the opposite.
            let _ = writeln!(out, "payload at: {:p} (flash is below 0x20000000)", payload.as_ptr());
            let before = HEAP.used();

            let raw = core::ptr::NonNull::from(payload);
            match unsafe { wasmtime::component::Component::deserialize_raw(&engine, raw) } {
                Ok(_component) => {
                    let _ = writeln!(out, "component: DESERIALIZED — it fits");
                    let loaded = HEAP.used() - before;
                    let _ = writeln!(
                        out,
                        "heap after load: {} KB (artifact stayed in flash)",
                        loaded / 1024
                    );
                    // Drop it: the measurement below instantiates through the
                    // real host, and holding two copies of the runtime structures
                    // would inflate the number this whole harness exists to
                    // report.
                    drop(_component);
                    measure_instantiation(&mut out, payload);
                    let _ = writeln!(out, "RESULT: PASS");
                    debug::exit(debug::EXIT_SUCCESS);
                }
                Err(e) => {
                    // Printing the error costs code size, and is worth it: "it
                    // failed" sent this investigation down a memory rabbit hole
                    // for three builds.
                    let _ = writeln!(out, "component: deserialize FAILED: {e:?}");
                    let _ = writeln!(out, "RESULT: FAIL");
                    debug::exit(debug::EXIT_FAILURE);
                }
            }
        }
        Err(e) => {
            let _ = writeln!(out, "engine: creation FAILED: {e:?}");
            debug::exit(debug::EXIT_FAILURE);
        }
    }
    loop {}
}
