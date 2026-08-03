//! PHASE 1 — run a real ILC component under Pulley and call a command.
//!
//! The point is not the output; it is that every hard part of the component
//! boundary is exercised here, on a machine with a debugger, before any of it
//! goes near a UART: WASI 0.2 wired, the custom `devalbo:ilc/events` import
//! bound, and `execute(u32, list<u8>) -> command-result` lifted across the
//! canonical ABI.
//!
//!   cargo run --bin run-component -- <component.wasm> <method_id> [request-hex]
//!
//! With no arguments it runs dlc's own engine and asks for `version` (method 1),
//! which every ILC app inherits and which needs no request bytes.
use std::path::PathBuf;

use dlc_platform_embedded::host::EngineHost;
use dlc_platform_embedded::pulley::PulleyWidth;

fn main() -> wasmtime::Result<()> {
    let mut args = std::env::args().skip(1);
    let path = args.next().unwrap_or_else(|| "../../engine.component.wasm".into());
    let method: u32 = args.next().unwrap_or_else(|| "1".into()).parse().unwrap_or(1);
    let request = match args.next() {
        Some(hex) => (0..hex.len())
            .step_by(2)
            .map(|i| u8::from_str_radix(&hex[i..i + 2], 16).unwrap_or(0))
            .collect::<Vec<u8>>(),
        None => Vec::new(),
    };

    let bytes = std::fs::read(&path)?;
    // A throwaway root, granted the way every ILC host grants one.
    let root = PathBuf::from(std::env::var("ILC_ROOT").unwrap_or_else(|_| "/tmp/ilc-pulley-root".into()));
    std::fs::create_dir_all(&root)?;

    println!("component: {} bytes ({path})", bytes.len());
    println!("root:      {}", root.display());

    // pulley64 on a 64-bit dev machine: Pulley bytecode is pointer-width
    // specific and a host executes only its own width. The badge runs pulley32,
    // which this machine can compile for but never run.
    let mut host = EngineHost::new(&bytes, PulleyWidth::Bits64, &root)?;
    println!("instantiated under pulley64 — WASI wired, events bound\n");

    let result = host.execute(method, &request)?;
    println!("execute(method={method}, {} request bytes)", request.len());
    println!("  success: {}", result.success);
    if let Some(err) = &result.error {
        println!("  error:   {err}");
    }
    println!("  output:  {} bytes", result.output.len());
    if let Ok(text) = std::str::from_utf8(&result.output) {
        let printable: String = text.chars().filter(|c| !c.is_control()).collect();
        if !printable.trim().is_empty() {
            println!("  as text: {printable}");
        }
    }
    for (topic, payload) in host.drain_events() {
        println!("  event:   {topic} ({} bytes)", payload.len());
    }
    Ok(())
}
