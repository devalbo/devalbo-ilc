//! The spike, as something runnable.
//!
//! A `cdylib` with no caller links to nothing — the first measurement came back
//! at 0.0 MB because the linker had correctly discarded a compiler nobody
//! invoked. A bin that actually calls it is the only way to get an honest size,
//! and it doubles as the functional half: building is not the same as working.
//!
//!     wasmtime run --dir . browser_precompile.wasm in.wasm out.cwasm

fn main() {
    let args: Vec<String> = std::env::args().collect();
    if args.len() != 3 {
        eprintln!("usage: browser-precompile <component.wasm> <out.cwasm>");
        std::process::exit(2);
    }
    let input = std::fs::read(&args[1]).expect("read input");
    match browser_precompile::precompile_pulley32(&input) {
        Ok(bytes) => {
            std::fs::write(&args[2], &bytes).expect("write output");
            println!("{} -> {} bytes for pulley32", input.len(), bytes.len());
        }
        Err(e) => {
            eprintln!("FAILED: {e}");
            std::process::exit(1);
        }
    }
}
