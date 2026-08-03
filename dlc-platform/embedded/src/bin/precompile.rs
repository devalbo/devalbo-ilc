//! AOT-compile a component for the badge, using THIS crate's Wasmtime.
//!
//! WHY NOT `wasmtime compile`. Wasmtime version-locks serialized artifacts, and
//! the CLI in devbox is not the crate the firmware links: 46.0.1 versus 46.0.2
//! was enough to fail with "compilation settings are not compatible with the
//! native host" — a message that says nothing about versions and sent this
//! investigation into three builds of memory tuning first.
//!
//! Compiling through the same dependency the runtime uses makes the mismatch
//! impossible rather than merely unlikely.
//!
//!   cargo run --bin precompile -- <component.wasm> <out.cwasm> [pulley32|pulley64]
use dlc_platform_embedded::pulley::{pulley_engine, PulleyWidth};
use wasmtime::component::Component;

fn main() -> wasmtime::Result<()> {
    let mut args = std::env::args().skip(1);
    let input = args.next().expect("usage: precompile <component.wasm> <out.cwasm> [width]");
    let output = args.next().expect("usage: precompile <component.wasm> <out.cwasm> [width]");
    let width = match args.next().as_deref() {
        Some("pulley64") => PulleyWidth::Bits64,
        _ => PulleyWidth::Bits32,
    };

    let wasm = std::fs::read(&input)?;
    let engine = pulley_engine(width)?;
    let component = Component::new(&engine, &wasm)?;
    let bytes = component.serialize()?;
    std::fs::write(&output, &bytes)?;

    println!(
        "{} ({} bytes) -> {} ({} bytes) for {}",
        input,
        wasm.len(),
        output,
        bytes.len(),
        width.triple()
    );
    Ok(())
}
