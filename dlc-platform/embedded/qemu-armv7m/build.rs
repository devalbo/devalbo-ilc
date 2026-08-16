//! Put the payload where `include_bytes!` can reach it, without a second copy
//! in the source tree.
//!
//! WHAT THIS REPLACES. The crate used to `include_bytes!("../hello.pulley32.cwasm")`,
//! which meant a `make qemu-payload` target existed purely to copy
//! `build/hello.pulley32.cwasm` next to this crate — one artifact, compiled once
//! and then duplicated, with two paths that had to be kept identical and no
//! check that they were. Copying into `OUT_DIR` instead means there is ONE
//! payload on disk and the emulator demonstrably runs the same bytes the badge
//! does, which is the entire claim the QEMU harness exists to support.
//!
//! Same pattern as `rp2350/build.rs`, deliberately — that crate solved this
//! first, and a second mechanism for the same job is how the two drift.

use std::path::PathBuf;

fn main() {
    println!("cargo::rerun-if-changed=build.rs");
    println!("cargo::rerun-if-env-changed=QEMU_PAYLOAD");

    // The badge's own artifact, by default: `make badge-cwasm` writes it and
    // this crate borrows it. QEMU_PAYLOAD overrides, for running a different
    // component through the harness without moving files about.
    let manifest = PathBuf::from(std::env::var_os("CARGO_MANIFEST_DIR").expect("cargo sets this"));
    let default = manifest.join("../../../build/hello.pulley32.cwasm");
    let source = std::env::var_os("QEMU_PAYLOAD")
        .map(PathBuf::from)
        .unwrap_or(default);

    // NAME THE FIX, not just the problem. A bare "No such file" here sends
    // someone hunting through include paths; the artifact is gitignored and
    // derived, so the answer is always the same command.
    let bytes = std::fs::read(&source).unwrap_or_else(|e| {
        panic!(
            "QEMU payload {}: {e}\n       run `make badge-cwasm` first (or set QEMU_PAYLOAD)",
            source.display()
        )
    });
    println!("cargo::rerun-if-changed={}", source.display());

    let out = PathBuf::from(std::env::var_os("OUT_DIR").expect("cargo sets OUT_DIR"))
        .join("payload.cwasm");
    std::fs::write(&out, &bytes).expect("writing the QEMU payload");
    println!("cargo::warning=QEMU payload: {} ({} KB)", source.display(), bytes.len() / 1024);
}
