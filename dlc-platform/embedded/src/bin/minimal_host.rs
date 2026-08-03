//! Can a component run with the host the BADGE can actually provide?
//!
//! No `wasmtime-wasi` — every import trapped except `wasi:random` (which
//! `_initialize` demands) and `devalbo:ilc/events`. If this works here, the
//! firmware version is a port rather than an experiment.
use dlc_platform_embedded::minimal::MinimalHost;
use dlc_platform_embedded::pulley::PulleyWidth;

fn main() -> wasmtime::Result<()> {
    let mut args = std::env::args().skip(1);
    let path = args.next().unwrap_or_else(|| "../../engine.component.wasm".into());
    let method: u32 = args.next().unwrap_or_else(|| "1".into()).parse().unwrap_or(1);

    let bytes = std::fs::read(&path)?;
    println!("component: {} bytes ({path})", bytes.len());
    println!("host:      hand-written — NO wasmtime-wasi, NO trap stubs\n");

    let mut host = MinimalHost::new(&bytes, PulleyWidth::Bits64)?;
    println!("instantiated — stdio over a byte sink, clocks ticking, filesystem absent by design");

    let result = host.execute(method, &[])?;
    println!("\nexecute(method={method})");
    println!("  success: {}", result.success);
    if let Some(err) = &result.error {
        println!("  error:   {err}");
    }
    if let Ok(text) = std::str::from_utf8(&result.output) {
        let printable: String = text.chars().filter(|c| !c.is_control()).collect();
        if !printable.trim().is_empty() {
            println!("  as text: {printable}");
        }
    }
    for (topic, payload) in host.events() {
        println!("  event:   {topic} ({} bytes)", payload.len());
    }
    Ok(())
}
