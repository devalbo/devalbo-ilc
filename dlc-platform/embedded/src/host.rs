//! The ILC host, in Rust — everything a component needs in order to run.
//!
//! This is the inherited runtime (EMBEDDED-PLAN D3): an app's own
//! `hosts/embedded/` holds only its slot, and everything here it gets for free.
//!
//! `std` TODAY, `no_std` LATER, and the split is deliberate. Phase 1 solves the
//! component boundary — WASI wiring, the custom `events` import, lifting
//! `execute(u32, list<u8>)` — on a machine with a debugger and a filesystem.
//! Phase 2 swaps `wasmtime-wasi` for hand-written implementations over UART and
//! RAM. What does NOT change between them is this file's shape, which is the
//! whole reason for doing it in this order.

use std::sync::{Arc, Mutex};

use wasmtime::component::{Component, Linker, ResourceTable};
use wasmtime::{Result, Store};
use wasmtime_wasi::{WasiCtx, WasiCtxBuilder, WasiCtxView, WasiView};

use crate::pulley::{pulley_engine, PulleyWidth};

/// What the engine announced while a command ran.
///
/// Events are FIRE-AND-FORGET (Decision 33): the engine cannot tell whether
/// anyone listened, because on some tier nobody does. Collecting them here is a
/// host choice, and on the badge the same hook drives the screen.
#[derive(Clone, Debug, Default)]
pub struct EventLog(Arc<Mutex<Vec<(String, Vec<u8>)>>>);

impl EventLog {
    pub fn drain(&self) -> Vec<(String, Vec<u8>)> {
        std::mem::take(&mut *self.0.lock().unwrap())
    }
}

/// Store state: WASI's resources plus whatever the host is collecting.
struct HostState {
    table: ResourceTable,
    ctx: WasiCtx,
    events: EventLog,
}

impl WasiView for HostState {
    fn ctx(&mut self) -> WasiCtxView<'_> {
        WasiCtxView { ctx: &mut self.ctx, table: &mut self.table }
    }
}

// `CommandResult` moved to `command.rs` when this module became std-only — the
// badge's host lifts the same record and must not depend on this one. Re-exported
// so callers keep the path they had.
pub use crate::command::CommandResult;

/// A live ILC engine: one component instance, ready for many commands.
///
/// PERSISTENT on purpose. Decision 31 keeps `execute` callable many times on one
/// instance rather than one-shot, because a reactive UI needs it — and because a
/// badge that re-instantiated per keypress would spend its life booting.
pub struct EngineHost {
    store: Store<HostState>,
    execute: wasmtime::component::Func,
    events: EventLog,
}

impl EngineHost {
    /// Instantiate a component under Pulley, with a filesystem root granted.
    ///
    /// The root is GRANTED, never assumed — the same rule the native and web
    /// hosts follow (`AGENTS.md` §3·5). Here it is a real directory; on the
    /// badge it will be RAM (D5), and the engine cannot tell the difference,
    /// which is the point.
    pub fn new(component_bytes: &[u8], width: PulleyWidth, root: &std::path::Path) -> Result<Self> {
        let engine = pulley_engine(width)?;
        let component = Component::new(&engine, component_bytes)?;

        let mut linker: Linker<HostState> = Linker::new(&engine);
        wasmtime_wasi::p2::add_to_linker_sync(&mut linker)?;

        // The custom capability. Decision 33 chose flat scalars + bytes exactly
        // so this is a plain function on both substrates — here it is a Rust
        // closure, on the badge it is the same signature over a native call.
        let events = EventLog::default();
        linker.instance("devalbo:ilc/events")?.func_wrap(
            "emit",
            |mut caller: wasmtime::StoreContextMut<'_, HostState>,
             (topic, payload): (String, Vec<u8>)| {
                caller.data_mut().events.0.lock().unwrap().push((topic, payload));
                Ok(())
            },
        )?;

        let ctx = WasiCtxBuilder::new()
            .inherit_stdout()
            .inherit_stderr()
            // The engine writes relative to this, exactly as it writes relative
            // to a browser's OPFS preopen.
            .preopened_dir(root, "/", wasmtime_wasi::DirPerms::all(), wasmtime_wasi::FilePerms::all())?
            .build();

        let mut store = Store::new(
            &engine,
            HostState { table: ResourceTable::new(), ctx, events: events.clone() },
        );
        let instance = linker.instantiate(&mut store, &component)?;
        let execute = instance
            .get_func(&mut store, "execute")
            .ok_or_else(|| wasmtime::Error::msg("component exports no `execute`"))?;

        Ok(Self { store, execute, events })
    }

    /// Run one command. The boundary is Decision 28's: a permanent `method_id`
    /// plus flat proto-encoded bytes, and nothing here knows what either means.
    pub fn execute(&mut self, method: u32, request: &[u8]) -> Result<CommandResult> {
        let typed = self
            .execute
            .typed::<(u32, Vec<u8>), (CommandResult,)>(&self.store)?;
        let (result,) = typed.call(&mut self.store, (method, request.to_vec()))?;
        Ok(result)
    }

    /// Events emitted since the last drain, in order.
    pub fn drain_events(&self) -> Vec<(String, Vec<u8>)> {
        self.events.drain()
    }
}
