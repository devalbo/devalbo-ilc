//! AOT-compile a component for an embedded target, with a compiler whose
//! feature set matches the runtime that will load it.
//!
//!   cargo run -p dlc-precompile -- <component.wasm> <out.cwasm> [pulley32|pulley64]
/// SHARED WITH THE RUNTIME, not copied — see the file's own header for why it is
/// included by path rather than depended on as a crate.
#[path = "../../src/engine_config.rs"]
mod engine_config;

fn main() -> wasmtime::Result<()> {
    let mut args = std::env::args().skip(1);
    let input = args.next().expect("usage: <component.wasm> <out.cwasm> [width]");
    let output = args.next().expect("usage: <component.wasm> <out.cwasm> [width]");
    let target = args.next().unwrap_or_else(|| "pulley32".to_string());

    let mut config = wasmtime::Config::new();
    // ONE CALL. Target, component model, the no-MMU settings and the address-map
    // choice all come from `engine_config`, because compilation settings are
    // recorded in the ARTIFACT — a producer that assembles its own list will
    // eventually assemble a different one, and the failure lands at load time
    // looking like a bad file. See engine_config.rs.
    engine_config::for_artifact(&mut config, &target)?;

    let engine = wasmtime::Engine::new(&config)?;
    // NAME THE FILE. `?` on an io::Error prints "No such file or directory
    // (os error 2)" and nothing else — which sent a CI failure to the wrong
    // suspect entirely, because the message could equally have meant the input,
    // the output directory, or the compiler itself.
    let wasm = std::fs::read(&input)
        .map_err(|e| wasmtime::Error::msg(format!("reading {input}: {e}")))?;
    let bytes = engine.precompile_component(&wasm)?;
    // CREATE THE PARENT. Every caller so far happened to have one — `build/`
    // exists on any machine that has built before — so a fresh clone was the
    // first place this failed, which is precisely the machine least able to
    // guess why. Cheap, and it makes CWASM_OUT genuinely arbitrary.
    if let Some(dir) = std::path::Path::new(&output).parent() {
        std::fs::create_dir_all(dir)
            .map_err(|e| wasmtime::Error::msg(format!("creating {}: {e}", dir.display())))?;
    }
    std::fs::write(&output, &bytes)
        .map_err(|e| wasmtime::Error::msg(format!("writing {output}: {e}")))?;

    println!("{} -> {} ({} bytes) for {}", input, output, bytes.len(), target);
    Ok(())
}
