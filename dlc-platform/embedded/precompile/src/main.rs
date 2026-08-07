//! AOT-compile a component for an embedded target, with a compiler whose
//! feature set matches the runtime that will load it.
//!
//!   cargo run -p dlc-precompile -- <component.wasm> <out.cwasm> [pulley32|pulley64]
/// SHARED WITH THE RUNTIME, not copied — see the file's own header for why it is
/// included by path rather than depended on as a crate.
#[path = "../../src/no_vm.rs"]
mod no_vm;

fn main() -> wasmtime::Result<()> {
    let mut args = std::env::args().skip(1);
    let input = args.next().expect("usage: <component.wasm> <out.cwasm> [width]");
    let output = args.next().expect("usage: <component.wasm> <out.cwasm> [width]");
    let target = args.next().unwrap_or_else(|| "pulley32".to_string());

    let mut config = wasmtime::Config::new();
    config.target(&target)?;
    config.wasm_component_model(true);
    // Everything a target with no MMU requires, from the one list the runtime
    // also uses. Compilation settings are recorded in the artifact, so a
    // disagreement here is a load failure there.
    no_vm::no_virtual_memory(&mut config);
    // 44% OF THE ARTIFACT IS DEBUG METADATA. `.wasmtime.addrmap` alone was
    // 679 KB of hello's 1.57 MB — a wasm-offset-to-code-offset map that exists
    // for backtraces. On a device where the whole artifact must be ONE
    // contiguous allocation, that is the cheapest RAM win available.
    //
    // The cost is real: traps on the badge report addresses rather than wasm
    // locations. Worth it while the constraint is "does it load at all".
    config.generate_address_map(false);

    let engine = wasmtime::Engine::new(&config)?;
    let wasm = std::fs::read(&input)?;
    let bytes = engine.precompile_component(&wasm)?;
    std::fs::write(&output, &bytes)?;

    println!("{} -> {} ({} bytes) for {}", input, output, bytes.len(), target);
    Ok(())
}
