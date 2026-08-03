//! PHASE 1 GATE — does Pulley actually execute an ILC component?
//!
//! Everything after this assumes the answer is yes, so it is worth one binary.
//! The `wasmtime` CLI cannot answer it: the distributed build compiles FOR
//! pulley and then refuses to run the result ("Module was compiled for
//! architecture 'pulley32'"), because compiling for Pulley and executing Pulley
//! are separate features and the shipped binary has only the first.
//!
//! Run: cargo run -p dlc-platform-embedded --bin pulley-probe -- <component.wasm>
use dlc_platform_embedded::pulley::{pulley_engine, PulleyWidth};
use wasmtime::component::Component;

fn main() -> wasmtime::Result<()> {
    let path = std::env::args()
        .nth(1)
        .unwrap_or_else(|| "../../engine.component.wasm".to_string());
    let wasm = std::fs::read(&path)?;
    println!("component: {} bytes ({path})", wasm.len());

    for width in [PulleyWidth::Bits64, PulleyWidth::Bits32] {
        let engine = pulley_engine(width)?;
        match Component::new(&engine, &wasm) {
            Ok(component) => {
                // Compiling under a Pulley target is the load-bearing step: it
                // proves the interpreter accepts a real ILC component, imports
                // and all, before anything has to be wired up.
                let serialized = component.serialize()?;
                println!(
                    "{:>8}: OK — compiled to {} bytes of pulley bytecode",
                    width.triple(),
                    serialized.len()
                );
            }
            Err(e) => println!("{:>8}: FAIL — {e:#}", width.triple()),
        }
    }
    Ok(())
}
