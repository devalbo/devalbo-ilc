//! The Pulley column of the wasm-parity check.
//!
//! Reads the SAME golden vectors as the native and jco columns, runs them
//! through the shared component under Wasmtime's Pulley interpreter, and prints
//! the SAME line format so `verify-parity.sh` can diff three streams instead of
//! two:
//!
//! ```text
//! <success>\t<base64(output)>\t<error>
//! EVENT\t<topic>\t<base64(payload)>
//! ```
//!
//! Why this exists: the badge runs an interpreter, and until now nothing outside
//! the badge did. A Pulley-only divergence — a miscompiled loop, a component-model
//! lowering the interpreter treats differently — would have been discovered on
//! hardware, over a UART, with no way to bisect it. Here it is a red CI step.
//!
//! It runs the plain `.wasm` rather than a `.cwasm`: the compile happens in-process
//! with the same settings, so the artifact under test is the interpreter's
//! execution of our component. The AOT path has its own check (`dlc build`, and
//! `picotool` on the firmware); conflating them would leave a failure ambiguous.

use std::collections::VecDeque;
use std::sync::{Arc, Mutex};

use anyhow::{Context, Result};
use base64::Engine as _;
use wasmtime::component::{Component, Linker, ResourceTable, Val};
use wasmtime::{Config, Engine, Store};
use wasmtime_wasi::{DirPerms, FilePerms, WasiCtx, WasiCtxBuilder, WasiCtxView, WasiView};

/// Events recorded in emission order, drained after each vector so the stream
/// interleaves with results exactly as the jco harness does. The interleaving is
/// the point: comparing events as a separate set would miss *which* command
/// emitted them.
type Events = Arc<Mutex<VecDeque<(String, Vec<u8>)>>>;

struct Host {
    wasi: WasiCtx,
    table: ResourceTable,
}

impl WasiView for Host {
    fn ctx(&mut self) -> WasiCtxView<'_> {
        WasiCtxView {
            ctx: &mut self.wasi,
            table: &mut self.table,
        }
    }
}

fn main() -> Result<()> {
    let mut args = std::env::args().skip(1);
    let component_path = args.next().context("usage: dlc-parity-pulley <component.wasm> <vectors.json> <root-dir>")?;
    let vectors_path = args.next().context("missing <vectors.json>")?;
    let root = args.next().context("missing <root-dir>")?;

    // PULLEY, not the host ISA. Without this the check would silently exercise
    // Cranelift on x86/aarch64 and prove nothing about the badge — the failure
    // mode being a green check that tests the wrong engine.
    let mut config = Config::new();
    config.target("pulley64")?;
    config.wasm_component_model(true);
    let engine = Engine::new(&config)?;

    let component = Component::from_file(&engine, &component_path)
        .map_err(|e| anyhow::anyhow!("loading {component_path}: {e}"))?;

    let events: Events = Arc::new(Mutex::new(VecDeque::new()));

    let mut linker: Linker<Host> = Linker::new(&engine);
    wasmtime_wasi::p2::add_to_linker_sync(&mut linker)?;

    // The one custom import. Fire-and-forget by WIT declaration, so the host
    // returns nothing and cannot make the guest wait (Decision 33).
    let mut events_iface = linker.instance("devalbo:ilc/events")?;
    let recorder = events.clone();
    events_iface.func_new("emit", move |_store, _ty, params, _results| {
        // wasmtime::bail!, not anyhow::bail!. Wasmtime 46 has its OWN Error type
        // and a host closure must return it; the two names are one character
        // apart and the mismatch reads as a nonsense type error.
        let topic = match &params[0] {
            Val::String(s) => s.clone(),
            other => wasmtime::bail!("emit: topic was not a string: {other:?}"),
        };
        let payload = match &params[1] {
            Val::List(items) => bytes_of(items).map_err(wasmtime::Error::msg)?,
            other => wasmtime::bail!("emit: payload was not a list: {other:?}"),
        };
        recorder.lock().unwrap().push_back((topic, payload));
        Ok(())
    })?;

    // The filesystem root the runner granted us — the Pulley equivalent of the
    // native run's cwd and the jco harness's preopen (§5.2: the host binds the
    // root, the engine just uses `os`). Vectors like `new` write real files, so
    // without this the trees could not be compared.
    let wasi = WasiCtxBuilder::new()
        .preopened_dir(&root, "/", DirPerms::all(), FilePerms::all())?
        .build();

    let mut store = Store::new(
        &engine,
        Host {
            wasi,
            table: ResourceTable::new(),
        },
    );

    let instance = linker.instantiate(&mut store, &component)?;
    let execute = instance
        .get_func(&mut store, "execute")
        .context("the component exports no `execute` — wrong world?")?;

    let raw = std::fs::read(&vectors_path).map_err(|e| anyhow::anyhow!("reading {vectors_path}: {e}"))?;
    let vectors: serde_json::Value = serde_json::from_slice(&raw)?;
    let vectors = vectors.as_array().context("vectors file is not a JSON array")?;

    let b64 = base64::engine::general_purpose::STANDARD;

    for v in vectors {
        let method = v["method"].as_u64().context("vector has no numeric `method`")? as u32;
        let request = hex_decode(v["request"].as_str().unwrap_or(""))?;

        let params = [
            Val::U32(method),
            Val::List(request.into_iter().map(Val::U8).collect()),
        ];
        let mut results = [Val::Bool(false)];
        execute.call(&mut store, &params, &mut results)?;

        let (success, output, error) = unpack_result(&results[0])?;
        println!("{success}\t{}\t{error}", b64.encode(&output));

        // Drained per vector, exactly where the jco harness drains, so the two
        // streams interleave identically.
        let mut queue = events.lock().unwrap();
        while let Some((topic, payload)) = queue.pop_front() {
            println!("EVENT\t{topic}\t{}", b64.encode(&payload));
        }
    }

    Ok(())
}

/// A `list<u8>` as bytes. Shared by the `emit` import and `command-result` so the
/// two cannot disagree about what a payload is.
fn bytes_of(items: &[Val]) -> std::result::Result<Vec<u8>, String> {
    items
        .iter()
        .map(|v| match v {
            Val::U8(b) => Ok(*b),
            other => Err(format!("expected a u8 in list<u8>, found {other:?}")),
        })
        .collect()
}

/// `command-result` is a WIT record: `{ success: bool, output: list<u8>, error: string }`.
/// Read by FIELD NAME rather than by position — a field reordering in the WIT
/// would otherwise silently shift the comparison instead of failing here.
fn unpack_result(val: &Val) -> Result<(bool, Vec<u8>, String)> {
    let fields = match val {
        Val::Record(fields) => fields,
        other => anyhow::bail!("execute returned {other:?}, expected a record"),
    };
    let mut success = false;
    let mut output = Vec::new();
    let mut error = String::new();
    for (name, value) in fields {
        match (name.as_str(), value) {
            ("success", Val::Bool(b)) => success = *b,
            // `option<string>`, so None is the SUCCESS case and prints as the
            // empty field the other two columns print. Matching Val::String here
            // rejected every vector with "unexpected field on command-result".
            ("error", Val::Option(opt)) => {
                error = match opt.as_deref() {
                    Some(Val::String(s)) => s.clone(),
                    None => String::new(),
                    other => anyhow::bail!("error was not an option<string>: {other:?}"),
                }
            }
            ("output", Val::List(items)) => output = bytes_of(items).map_err(anyhow::Error::msg)?,
            (other, _) => anyhow::bail!("unexpected field on command-result: {other}"),
        }
    }
    Ok((success, output, error))
}

fn hex_decode(s: &str) -> Result<Vec<u8>> {
    if s.len() % 2 != 0 {
        anyhow::bail!("odd-length hex request: {s}");
    }
    (0..s.len())
        .step_by(2)
        .map(|i| u8::from_str_radix(&s[i..i + 2], 16).map_err(Into::into))
        .collect()
}
