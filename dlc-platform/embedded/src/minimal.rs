//! The MINIMAL host — what the badge can actually provide.
//!
//! `wasmtime-wasi` is `std`, so the badge cannot use it. This is the shape that
//! replaces it: satisfy every import the component declares, but implement only
//! the ones it actually CALLS, and let the rest trap if they are ever reached.
//!
//! That is not a shortcut — it is how a capability-injected engine is supposed to
//! degrade. A tier that has no filesystem provides one that traps; an app that
//! never writes never notices. What makes it safe is that a trap is loud: the
//! command fails with a backtrace naming the interface, rather than returning a
//! plausible wrong answer.
//!
//! Proven on the host under `pulley64` first, deliberately. Everything here is
//! `std`-free in shape so Phase 2 is a copy rather than a redesign — the only
//! `std` left is `Vec`/`String` from `alloc`, which bare metal has.

use alloc::string::String;
use alloc::vec::Vec;

use wasmtime::component::{Component, HasSelf, Linker, Resource, ResourceTable};
use wasmtime_wasi_io::IoView;

use wasmtime_wasi_io::streams::{DynInputStream, DynOutputStream};

// Aliases so the 28 refusals below read as a table rather than as paths.
use crate::cli_bindings::wasi::filesystem::types::{
    Advice as FsAdvice, Descriptor as FsDescriptor, DescriptorFlags as FsDescriptorFlags,
    DescriptorStat as FsDescriptorStat, DescriptorType as FsDescriptorType,
    DirectoryEntryStream as FsDirEntryStream, ErrorCode as FsErrorCode,
    MetadataHashValue as FsMetadataHashValue, NewTimestamp as FsNewTimestamp,
    OpenFlags as FsOpenFlags, PathFlags as FsPathFlags,
};

use crate::uart::{boxed, boxed_input, BufferSink};
use wasmtime::{Engine, Result, Store};

use crate::pulley::{pulley_engine, PulleyWidth};

/// What the badge will hold: no WASI context at all, just what the host chooses
/// to collect.
#[derive(Default)]
pub struct MinimalState {
    pub table: ResourceTable,
    pub events: Vec<(String, Vec<u8>)>,
    /// Everything the guest wrote to stdout.
    ///
    /// SHARED with the stream handed to the guest, rather than a plain `Vec` the
    /// stream never touched — see `SharedBuffer` for the bug that was.
    pub stdout: crate::uart::SharedBuffer,
    /// What this tier tells the app about itself. See the `environment` impl for
    /// why capability advertisement lives here rather than in a new capability.
    pub environment: Vec<(String, String)>,
    /// A monotonic counter standing in for a hardware timer, used only when no
    /// real clock has been installed.
    ticks: u64,
    /// THE REAL CLOCK, in microseconds, or `None` on a host that has not
    /// supplied one.
    ///
    /// A function pointer rather than a peripheral because this struct is
    /// threaded through every WASI impl and shared by three very different
    /// hosts — TIMER0 on the badge, SysTick under QEMU, the OS on a laptop. It
    /// is the smallest thing all three can provide.
    pub clock: Option<crate::clock::Clock>,
    /// A deterministic PRNG standing in for the hardware RNG.
    ///
    /// NOT for anything that needs real entropy. It exists because TinyGo calls
    /// `get-random-u64` from `_initialize` — before any command runs — so a
    /// component will not instantiate without *some* answer. On the badge this
    /// becomes the RP2350's hardware RNG; the shape does not change.
    seed: u64,
}

/// stdout: where a tier decides what "printing" physically means.
impl crate::cli_bindings::wasi::cli::stdout::Host for MinimalState {
    fn get_stdout(&mut self) -> Resource<DynOutputStream> {
        // A CLONE OF THE SHARED BUFFER, not a fresh one: the handle the guest
        // gets must write where the host reads.
        self.table
            .push(boxed(self.stdout.clone()))
            .expect("the resource table is ours and unbounded")
    }
}

/// stderr: a separate stream on a machine that has two, the same one on a badge
/// that has a single UART. Kept distinct here so the choice stays the host's.
impl crate::cli_bindings::wasi::cli::stderr::Host for MinimalState {
    fn get_stderr(&mut self) -> Resource<DynOutputStream> {
        self.table
            .push(boxed(BufferSink::default()))
            .expect("the resource table is ours and unbounded")
    }
}

/// stdin: closed, because a badge has no keyboard (see uart.rs).
impl crate::cli_bindings::wasi::cli::stdin::Host for MinimalState {
    fn get_stdin(&mut self) -> Resource<DynInputStream> {
        self.table
            .push(boxed_input())
            .expect("the resource table is ours and unbounded")
    }
}

/// No argv — but the environment is how a TIER ADVERTISES WHAT IT IS.
///
/// A badge is not launched from a shell, so there is no inherited environment to
/// pass through and the table is whatever the host decides to say. That makes it
/// the right place for capability advertisement, and the reason is D6: a fact the
/// app needs does not justify a new capability when an interface the world
/// ALREADY DECLARES can carry it. `wasi:cli/environment` is imported by every
/// component here whether or not it is called, so this costs nothing.
///
/// **What belongs here is what the app cannot otherwise find out.** Every tier
/// provides `wasi:cli/stdout` — it must, because TinyGo acquires it during
/// `_initialize` — so its presence says nothing about whether anyone will ever
/// SEE the bytes. On a badge with a screen they are read; piped to a buffer that
/// is dropped, they are not. Only the host knows which, so only the host can say.
///
/// **Advertise what is true TODAY, not what is planned.** The badge's stdout
/// reaches a UART, and it reaches a screen when Phase 3 wires the TFT — so the
/// value changes then, and an app that adapts its output gets it right in both
/// eras. Claiming `display` now would be the one kind of lie this interface makes
/// expensive: silent, and believed.
impl crate::cli_bindings::wasi::cli::environment::Host for MinimalState {
    fn get_environment(&mut self) -> Vec<(String, String)> {
        self.environment.clone()
    }
    fn get_arguments(&mut self) -> Vec<String> {
        Vec::new()
    }
    fn initial_cwd(&mut self) -> Option<String> {
        None
    }
}

/// A monotonic tick. On the badge this is the RP2350's timer; here it counts
/// calls, which is enough for anything that only needs "later than last time".
impl crate::cli_bindings::wasi::clocks::monotonic_clock::Host for MinimalState {
    /// NANOSECONDS, which is what `wasi:clocks` specifies. The installed clock
    /// counts microseconds because that is what the hardware timer does, so the
    /// conversion happens here — once, rather than in every caller.
    fn now(&mut self) -> u64 {
        match self.clock {
            Some(clock) => clock().saturating_mul(1_000),
            // No clock installed: the old counting behaviour, which satisfies
            // anything that only needs "later than last time" and satisfies
            // nothing that needs a duration. Kept so a host without a timer
            // still instantiates — TinyGo reads the clock during `_initialize`.
            None => {
                self.ticks += 1;
                self.ticks
            }
        }
    }

    fn resolution(&mut self) -> u64 {
        // A microsecond, in nanoseconds. Honest about the timer underneath
        // rather than claiming nanosecond precision it does not have.
        if self.clock.is_some() {
            1_000
        } else {
            1
        }
    }

    fn subscribe_instant(&mut self, when: u64) -> Resource<wasmtime_wasi_io::poll::DynPollable> {
        // `when` is an absolute nanosecond instant on the same clock `now`
        // reports, so the conversion is the same one.
        self.deadline_pollable(when / 1_000)
    }

    fn subscribe_duration(&mut self, when: u64) -> Resource<wasmtime_wasi_io::poll::DynPollable> {
        crate::clock::note_sleep();
        // A DURATION from now, not an instant — the difference mattered exactly
        // once, when this ignored its argument entirely and every sleep returned
        // immediately.
        let Some(clock) = self.clock else {
            return self.ready_pollable();
        };
        self.deadline_pollable(clock().saturating_add(when / 1_000))
    }
}

/// Wall-clock time, which this board only really has once the RTC is wired.
///
/// ZERO is deliberate and worth stating: an app that needs the date will read
/// the epoch and be obviously wrong, rather than subtly wrong. tictactoe does
/// not use it; notes does, which is why notes takes its clock from the HOST as
/// input rather than asking the engine (§6.4a).
impl crate::cli_bindings::wasi::clocks::wall_clock::Host for MinimalState {
    fn now(&mut self) -> crate::cli_bindings::wasi::clocks::wall_clock::Datetime {
        crate::cli_bindings::wasi::clocks::wall_clock::Datetime { seconds: 0, nanoseconds: 0 }
    }
    fn resolution(&mut self) -> crate::cli_bindings::wasi::clocks::wall_clock::Datetime {
        crate::cli_bindings::wasi::clocks::wall_clock::Datetime { seconds: 1, nanoseconds: 0 }
    }
}

/// No preopens, so the engine is granted no filesystem at all.
///
/// A REAL ANSWER, not a placeholder. "This tier has no filesystem" is a state
/// the architecture already expects (§6.4a): the engine degrades rather than
/// failing, and an app that needs storage says so. The badge gets a RAM-backed
/// filesystem in D5 and littlefs after that; until then, empty is honest.
impl crate::cli_bindings::wasi::filesystem::preopens::Host for MinimalState {
    fn get_directories(&mut self) -> Vec<(Resource<FsDescriptor>, String)> {
        Vec::new()
    }
}

impl crate::cli_bindings::wasi::filesystem::types::Host for MinimalState {
    /// Which stream error was a filesystem error. Nothing here produces one.
    fn filesystem_error_code(
        &mut self,
        _err: Resource<wasmtime_wasi_io::streams::Error>,
    ) -> Option<FsErrorCode> {
        None
    }
}

/// Every descriptor method, and every one of them refuses.
///
/// UNREACHABLE BY CONSTRUCTION rather than by hope: `get_directories` hands out
/// nothing, so the guest never holds a descriptor to call these on. They exist
/// because the trait requires them — the interface is imported whole or not at
/// all — and they return `ACCESS` so that if one ever IS reached, the failure
/// names a permission problem rather than a panic.
///
/// This is what D5 replaces. A RAM-backed filesystem changes only this block;
/// the engine, the app, and every other interface stay exactly as they are.
impl crate::cli_bindings::wasi::filesystem::types::HostDescriptor for MinimalState {
    fn read_via_stream(&mut self, _: Resource<FsDescriptor>, _: u64) -> Result<Resource<DynInputStream>, FsErrorCode> { Err(FsErrorCode::Access) }
    fn write_via_stream(&mut self, _: Resource<FsDescriptor>, _: u64) -> Result<Resource<DynOutputStream>, FsErrorCode> { Err(FsErrorCode::Access) }
    fn append_via_stream(&mut self, _: Resource<FsDescriptor>) -> Result<Resource<DynOutputStream>, FsErrorCode> { Err(FsErrorCode::Access) }
    fn advise(&mut self, _: Resource<FsDescriptor>, _: u64, _: u64, _: FsAdvice) -> Result<(), FsErrorCode> { Err(FsErrorCode::Access) }
    fn sync_data(&mut self, _: Resource<FsDescriptor>) -> Result<(), FsErrorCode> { Err(FsErrorCode::Access) }
    fn get_flags(&mut self, _: Resource<FsDescriptor>) -> Result<FsDescriptorFlags, FsErrorCode> { Err(FsErrorCode::Access) }
    fn get_type(&mut self, _: Resource<FsDescriptor>) -> Result<FsDescriptorType, FsErrorCode> { Err(FsErrorCode::Access) }
    fn set_size(&mut self, _: Resource<FsDescriptor>, _: u64) -> Result<(), FsErrorCode> { Err(FsErrorCode::Access) }
    fn set_times(&mut self, _: Resource<FsDescriptor>, _: FsNewTimestamp, _: FsNewTimestamp) -> Result<(), FsErrorCode> { Err(FsErrorCode::Access) }
    fn read(&mut self, _: Resource<FsDescriptor>, _: u64, _: u64) -> Result<(Vec<u8>, bool), FsErrorCode> { Err(FsErrorCode::Access) }
    fn write(&mut self, _: Resource<FsDescriptor>, _: Vec<u8>, _: u64) -> Result<u64, FsErrorCode> { Err(FsErrorCode::Access) }
    fn read_directory(&mut self, _: Resource<FsDescriptor>) -> Result<Resource<FsDirEntryStream>, FsErrorCode> { Err(FsErrorCode::Access) }
    fn sync(&mut self, _: Resource<FsDescriptor>) -> Result<(), FsErrorCode> { Err(FsErrorCode::Access) }
    fn create_directory_at(&mut self, _: Resource<FsDescriptor>, _: String) -> Result<(), FsErrorCode> { Err(FsErrorCode::Access) }
    fn stat(&mut self, _: Resource<FsDescriptor>) -> Result<FsDescriptorStat, FsErrorCode> { Err(FsErrorCode::Access) }
    fn stat_at(&mut self, _: Resource<FsDescriptor>, _: FsPathFlags, _: String) -> Result<FsDescriptorStat, FsErrorCode> { Err(FsErrorCode::Access) }
    fn set_times_at(&mut self, _: Resource<FsDescriptor>, _: FsPathFlags, _: String, _: FsNewTimestamp, _: FsNewTimestamp) -> Result<(), FsErrorCode> { Err(FsErrorCode::Access) }
    fn link_at(&mut self, _: Resource<FsDescriptor>, _: FsPathFlags, _: String, _: Resource<FsDescriptor>, _: String) -> Result<(), FsErrorCode> { Err(FsErrorCode::Access) }
    fn open_at(&mut self, _: Resource<FsDescriptor>, _: FsPathFlags, _: String, _: FsOpenFlags, _: FsDescriptorFlags) -> Result<Resource<FsDescriptor>, FsErrorCode> { Err(FsErrorCode::Access) }
    fn readlink_at(&mut self, _: Resource<FsDescriptor>, _: String) -> Result<String, FsErrorCode> { Err(FsErrorCode::Access) }
    fn remove_directory_at(&mut self, _: Resource<FsDescriptor>, _: String) -> Result<(), FsErrorCode> { Err(FsErrorCode::Access) }
    fn rename_at(&mut self, _: Resource<FsDescriptor>, _: String, _: Resource<FsDescriptor>, _: String) -> Result<(), FsErrorCode> { Err(FsErrorCode::Access) }
    fn symlink_at(&mut self, _: Resource<FsDescriptor>, _: String, _: String) -> Result<(), FsErrorCode> { Err(FsErrorCode::Access) }
    fn unlink_file_at(&mut self, _: Resource<FsDescriptor>, _: String) -> Result<(), FsErrorCode> { Err(FsErrorCode::Access) }
    fn is_same_object(&mut self, _: Resource<FsDescriptor>, _: Resource<FsDescriptor>) -> bool { false }
    fn metadata_hash(&mut self, _: Resource<FsDescriptor>) -> Result<FsMetadataHashValue, FsErrorCode> { Err(FsErrorCode::Access) }
    fn metadata_hash_at(&mut self, _: Resource<FsDescriptor>, _: FsPathFlags, _: String) -> Result<FsMetadataHashValue, FsErrorCode> { Err(FsErrorCode::Access) }
    fn drop(&mut self, _: Resource<FsDescriptor>) -> wasmtime::Result<()> { Ok(()) }
}

/// The other resource in `wasi:filesystem/types`. Same reasoning as the
/// descriptor: nothing hands one out, so nothing can call these.
impl crate::cli_bindings::wasi::filesystem::types::HostDirectoryEntryStream for MinimalState {
    fn read_directory_entry(
        &mut self,
        _: Resource<FsDirEntryStream>,
    ) -> Result<Option<crate::cli_bindings::wasi::filesystem::types::DirectoryEntry>, FsErrorCode> {
        Err(FsErrorCode::Access)
    }
    fn drop(&mut self, _: Resource<FsDirEntryStream>) -> wasmtime::Result<()> {
        Ok(())
    }
}

impl IoView for MinimalState {
    fn table(&mut self) -> &mut ResourceTable {
        &mut self.table
    }
}

impl MinimalState {
    /// A pollable that is always ready.
    ///
    /// Built through `wasmtime_wasi_io::poll::subscribe`, which is the only way
    /// to make one: `DynPollable` is a struct holding a future factory, not a
    /// trait object, so it cannot be cast into. Subscribing to a resource that
    /// implements `Pollable` is the supported route — and every `Pollable` this
    /// host owns resolves immediately (see uart.rs), which is what keeps the
    /// async requirement affordable on bare metal.
    fn ready_pollable(&mut self) -> Resource<wasmtime_wasi_io::poll::DynPollable> {
        let stream = self
            .table
            .push(crate::uart::ClosedStream)
            .expect("the resource table is ours and unbounded");
        wasmtime_wasi_io::poll::subscribe(&mut self.table, stream)
            .expect("subscribing to an always-ready pollable cannot fail")
    }

    /// A pollable that becomes ready when the clock passes `deadline_us`.
    ///
    /// Falls back to always-ready with no clock installed: a host that cannot
    /// measure time must not hang a guest that asks to wait. The sleep becomes a
    /// no-op, which is what happened everywhere before this existed.
    fn deadline_pollable(&mut self, deadline_us: u64) -> Resource<wasmtime_wasi_io::poll::DynPollable> {
        let Some(clock) = self.clock else {
            return self.ready_pollable();
        };
        let pollable = self
            .table
            .push(crate::clock::DeadlinePollable::new(clock, deadline_us))
            .expect("the resource table is ours and unbounded");
        wasmtime_wasi_io::poll::subscribe(&mut self.table, pollable)
            .expect("subscribing to a deadline pollable cannot fail")
    }

    fn next_random(&mut self) -> u64 {
        // xorshift64*: a few instructions, no dependencies, adequate for seeding
        // a map's hash. Named for what it is so nobody mistakes it for entropy.
        let mut x = if self.seed == 0 { 0x9E3779B97F4A7C15 } else { self.seed };
        x ^= x >> 12;
        x ^= x << 25;
        x ^= x >> 27;
        self.seed = x;
        x.wrapping_mul(0x2545F4914F6CDD1D)
    }
}

/// An engine with the smallest host a component will accept.
pub struct MinimalHost {
    store: Store<MinimalState>,
    execute: wasmtime::component::Func,
}

impl MinimalHost {
    /// Compile a plain `.wasm` and instantiate it — **the laptop's path**.
    ///
    /// `Component::new` runs Cranelift, which a `no_std` Wasmtime does not have,
    /// so this is `std`-only by necessity rather than by choice.
    #[cfg(feature = "std")]
    pub fn new(component_bytes: &[u8], width: PulleyWidth) -> Result<Self> {
        let engine = pulley_engine(width)?;
        let component = Component::new(&engine, component_bytes)?;
        // No advertisement: a laptop harness is not a tier telling an app what it
        // can do, and an empty environment is what these tools measured against.
        Self::from_component(engine, component, &[])
    }

    /// Instantiate an AOT artifact **without moving it out of flash** — the badge's path.
    ///
    /// `deserialize_raw` rather than `deserialize`, and the difference is the
    /// whole reason the badge can load anything at all: `deserialize` copies into
    /// a fresh allocation the size of the artifact (890 KB for hello), which
    /// 520 KB of SRAM can never provide. `deserialize_raw` borrows, and the
    /// runtime only ever reads — so interpreted Pulley bytecode can stay in the
    /// flash the linker already put it in. Measured in QEMU at 81 KB of heap
    /// against 890 KB for the copying path.
    ///
    /// # Safety
    ///
    /// `cwasm` must be a `.cwasm` this project produced (precompiled bytes are
    /// trusted by construction — they are not parsed defensively), **16-byte
    /// aligned** because Wasmtime reads it as an ELF, and `'static` so it
    /// outlives the component as `deserialize_raw` requires. A `static` in flash
    /// satisfies the last two; `include_bytes!` alone does not satisfy alignment.
    pub unsafe fn from_precompiled(cwasm: &'static [u8], width: PulleyWidth) -> Result<Self> {
        // SAFETY: forwarded to the caller by this function's own contract.
        unsafe { Self::from_precompiled_advertising(cwasm, width, &[]) }
    }

    /// As [`Self::from_precompiled`], but telling the app what this tier is.
    ///
    /// The pairs become `wasi:cli/environment`, which is where a tier advertises
    /// what it can actually do — see the `environment` impl above for why that is
    /// the right vehicle rather than a new capability. An empty table is a host
    /// that says nothing about itself, which is what the laptop harnesses want.
    ///
    /// # Safety
    ///
    /// Identical to [`Self::from_precompiled`].
    pub unsafe fn from_precompiled_advertising(
        cwasm: &'static [u8],
        width: PulleyWidth,
        environment: &[(&str, &str)],
    ) -> Result<Self> {
        let engine = pulley_engine(width)?;
        // SAFETY: forwarded to the caller by this function's own contract.
        let component =
            unsafe { Component::deserialize_raw(&engine, core::ptr::NonNull::from(cwasm))? };
        Self::from_component(engine, component, environment)
    }

    /// Everything both paths share: the linker, and the one instantiation.
    fn from_component(
        engine: Engine,
        component: Component,
        environment: &[(&str, &str)],
    ) -> Result<Self> {
        let mut linker: Linker<MinimalState> = Linker::new(&engine);

        // SHADOWING ON, and it is not laziness — it is the only order that works.
        //
        // `define_unknown_imports_as_traps` skips FUNCTIONS that are already
        // defined, but it creates each INSTANCE unconditionally — so defining
        // `devalbo:ilc/events` first and then stubbing fails with "map entry
        // `devalbo:ilc/events` defined twice". Stubbing first and overriding
        // after is therefore the order, and overriding is exactly what
        // shadowing permits.
        linker.allow_shadowing(true);
        // NO TRAP STUBS. This was tried in both orders and neither works once a
        // resource is involved: `define_unknown_imports_as_traps` defines
        // `wasi:cli/stdout` with a stubbed `get-stdout`, and a later real
        // definition does not displace the RESOURCE TYPE it registered — the
        // failure appears as "resource type mismatch" on `get-stdout`, pointing
        // at the wrong file entirely. Removing the stubs made stdout link
        // immediately and moved the error on to the next unimplemented import,
        // which is how it was found.
        //
        // So the badge implements EVERY import it declares. That is not a
        // hardship — it is what a capability-injected host is: a tier states
        // what it can do, including the parts it does cheaply. Where a stub is
        // wanted (no filesystem, say), it is written deliberately rather than
        // generated, so its failure mode is chosen rather than inherited.
        wasmtime_wasi_io::add_to_linker_async(&mut linker)?;
        //
        // `wasi:random` is not optional and not deferrable: TinyGo's
        // `_initialize` calls `get-random-u64` before any command runs, so a
        // component whose random traps never instantiates at all. That fact cost
        // a spike to learn and is the single most useful thing to know before
        // wiring a board.
        // VERSIONED NAME. WASI interfaces are `wasi:random/random@0.2.0` in the
        // component's import list, and an unversioned key silently defines a
        // DIFFERENT instance — the override appears to work and the stub still
        // runs, which surfaces as an instantiation failure rather than as a
        // linking error.
        //
        // AND THE WHOLE INTERFACE, not just the function that is called.
        // Overriding an instance REPLACES it, discarding the stubs inside — so a
        // partial override fails with "instance export `get-random-bytes` has
        // the wrong type". Shadowing is per-instance, not per-function.
        {
            let mut random = linker.instance("wasi:random/random@0.2.0")?;
            random.func_wrap(
                "get-random-u64",
                |mut caller: wasmtime::StoreContextMut<'_, MinimalState>, ()| {
                    Ok((caller.data_mut().next_random(),))
                },
            )?;
            random.func_wrap(
                "get-random-bytes",
                |mut caller: wasmtime::StoreContextMut<'_, MinimalState>, (len,): (u64,)| {
                    let mut out = Vec::with_capacity(len as usize);
                    while (out.len() as u64) < len {
                        out.extend_from_slice(&caller.data_mut().next_random().to_le_bytes());
                    }
                    out.truncate(len as usize);
                    Ok((out,))
                },
            )?;
        }

        // The app's own capability, overriding its stub. On the badge this drives
        // the screen.
        linker.instance("devalbo:ilc/events")?.func_wrap(
            "emit",
            |mut caller: wasmtime::StoreContextMut<'_, MinimalState>,
             (topic, payload): (String, Vec<u8>)| {
                // THE RESERVED ACTIVITY TOPIC IS TAKEN AS IT PASSES.
                //
                // This runs while the guest is suspended inside `emit` — the only
                // moment the value is worth having. Events are drained AFTER a
                // command returns, and an activity report delivered then
                // describes something that has already finished.
                if topic == "ilc.activity" {
                    crate::activity::set(&payload);
                }
                caller.data_mut().events.push((topic, payload));
                Ok(())
            },
        )?;

        // ---- stdio, which TinyGo acquires at init -------------------------
        //
        // `wasi:io/{error,poll,streams}` comes from `wasmtime-wasi-io` — 15
        // functions across two resources, a `pollable`, and a `stream-error`
        // variant that would otherwise be hand-written. The crate is
        // no_std-capable, so the badge inherits the same implementation.
        //
        // ASYNC, because `Pollable::ready` is. That is only affordable here
        // because every stream this host owns is ALWAYS ready (see uart.rs), so
        // the executor never has to do anything but poll once.

        // What remains ours: handing out the handle, which is where a tier
        // chooses what stdout physically IS. On the badge, a UART.
        //
        // Through GENERATED bindings, not `func_wrap`: `get-stdout` returns
        // `own<output-stream>`, a resource defined in `wasi:io/streams`, and a
        // hand-written wrapper can only name `Resource<DynOutputStream>` —
        // which wasmtime rejects, because it matches resource types by
        // registered identity rather than structurally. `bindgen!`'s `with` map
        // is what says "this IS wasmtime-wasi-io's resource" (cli_bindings.rs).
        crate::cli_bindings::wasi::cli::stdout::add_to_linker::<_, HasSelf<_>>(
            &mut linker,
            |state| state,
        )?;
        crate::cli_bindings::wasi::cli::stderr::add_to_linker::<_, HasSelf<_>>(
            &mut linker,
            |state| state,
        )?;
        crate::cli_bindings::wasi::cli::stdin::add_to_linker::<_, HasSelf<_>>(
            &mut linker,
            |state| state,
        )?;
        crate::cli_bindings::wasi::cli::environment::add_to_linker::<_, HasSelf<_>>(
            &mut linker,
            |state| state,
        )?;
        crate::cli_bindings::wasi::clocks::monotonic_clock::add_to_linker::<_, HasSelf<_>>(
            &mut linker,
            |state| state,
        )?;
        crate::cli_bindings::wasi::clocks::wall_clock::add_to_linker::<_, HasSelf<_>>(
            &mut linker,
            |state| state,
        )?;
        crate::cli_bindings::wasi::filesystem::types::add_to_linker::<_, HasSelf<_>>(
            &mut linker,
            |state| state,
        )?;
        crate::cli_bindings::wasi::filesystem::preopens::add_to_linker::<_, HasSelf<_>>(
            &mut linker,
            |state| state,
        )?;

        let state = MinimalState {
            environment: environment
                .iter()
                .map(|(k, v)| ((*k).into(), (*v).into()))
                .collect(),
            ..MinimalState::default()
        };
        // THE CLOCK GOES IN BEFORE THE GUEST EXISTS. TinyGo reads it during
        // `_initialize`, which runs inside instantiation below — set it after
        // and a guest's first look at the time would find the counting stub.
        let mut state = state;
        state.clock = crate::clock::installed();
        let mut store = Store::new(&engine, state);
        let instance =
            crate::block_on::block_on(linker.instantiate_async(&mut store, &component))?;
        let execute = instance
            .get_func(&mut store, "execute")
            .ok_or_else(|| wasmtime::Error::msg("component exports no `execute`"))?;
        Ok(Self { store, execute })
    }

    pub fn execute(&mut self, method: u32, request: &[u8]) -> Result<crate::command::CommandResult> {
        let typed = self
            .execute
            .typed::<(u32, Vec<u8>), (crate::command::CommandResult,)>(&self.store)?;
        let (result,) = crate::block_on::block_on(
            typed.call_async(&mut self.store, (method, request.to_vec())),
        )?;
        // NO `post_return_async` — wasmtime 46 deprecates it as "no longer needs
        // to be called; this function has no effect". It was required once,
        // between an async call and the next one, and the runtime now handles it.
        // `execute` stays callable many times on one instance either way, which
        // is what Decision 31 requires.
        Ok(result)
    }

    pub fn events(&mut self) -> Vec<(String, Vec<u8>)> {
        core::mem::take(&mut self.store.data_mut().events)
    }

    /// Everything the guest wrote to `wasi:cli/stdout`, drained.
    ///
    /// A THIRD CHANNEL, and worth naming as one: a command's return value is the
    /// answer, `events` are what a slot renders, and this is what the guest
    /// *printed* — TinyGo's `println`, a panic message, anything the engine emits
    /// on its way. A host that drops it silently is the reason "it ran but said
    /// nothing" is hard to debug on a board with one UART.
    ///
    /// Drained rather than borrowed so a caller can print after each command
    /// without re-printing the last one.
    pub fn stdout(&mut self) -> Vec<u8> {
        self.store.data().stdout.take()
    }
}
