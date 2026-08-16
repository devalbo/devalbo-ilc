//! WHAT THIS FIRMWARE CAN RUN, decided at BUILD time rather than in the source.
//!
//! A badge is flashed, not configured, so "which app does it run" has to be
//! answerable without editing a file — otherwise every app is a source change and
//! a code review. Two environment variables decide it, and they are orthogonal on
//! purpose: three useful modes fall out of two independent facts rather than
//! being enumerated as a mode flag that has to grow a case each time.
//!
//! | mode | `BADGE_PAYLOAD` | `BADGE_REGION` |
//! | --- | --- | --- |
//! | **hard-coded app** — one badge, one app, nothing else flashable | a `.cwasm` | `off` |
//! | **default + loader** — boots into an app, and the region can add more | a `.cwasm` | unset |
//! | **empty loader** — ships with nothing; the region is the only source | unset | unset |
//!
//! The fourth combination — no payload and no region — builds and boots and can
//! run nothing. That is a legitimate milestone 1 firmware (does the board come up?),
//! so it warns rather than failing.
//!
//! WHY `OUT_DIR` AND NOT A FILE IN THE TREE. `include_bytes!` needs its target to
//! exist, so a source-tree default would have to be either committed (an 890 KB
//! artifact in git, version-locked to a Wasmtime pin) or gitignored (a fresh
//! clone fails to build). Writing a zero-byte file into `OUT_DIR` makes "no
//! built-in payload" a real, always-present file — the empty default is literal
//! rather than conditional.

use std::path::PathBuf;

fn main() {
    // Cargo re-runs a build script only when it is told to. Without these, a
    // second `cargo build` with a different payload silently keeps the first —
    // the exact failure that makes someone flash the wrong app and debug the
    // board instead of the build.
    println!("cargo::rerun-if-changed=build.rs");
    println!("cargo::rerun-if-changed=memory.x");
    println!("cargo::rerun-if-env-changed=BADGE_PAYLOAD");
    println!("cargo::rerun-if-env-changed=BADGE_REGION");
    println!("cargo::rerun-if-env-changed=BADGE_WORLD");
    println!("cargo::rerun-if-env-changed=BADGE_BEAT_MS");
    println!("cargo::rerun-if-env-changed=BADGE_SCREEN");

    // Declared so that a typo in a `cfg` name is a warning rather than a branch
    // that quietly never compiles.
    println!("cargo::rustc-check-cfg=cfg(has_builtin_payload)");
    println!("cargo::rustc-check-cfg=cfg(payload_region)");
    println!("cargo::rustc-check-cfg=cfg(badge_world_minimal)");
    println!("cargo::rustc-check-cfg=cfg(badge_screen_full)");

    // WHICH WORLD (see src/world.rs). Flash-time on purpose: the two differ only
    // in presentation, and a runtime switch would carry both onto a board that
    // wanted one. `normal` shows the app's text; `minimal` shows a status colour
    // and displays no text at all.
    let world = std::env::var("BADGE_WORLD").unwrap_or_default();
    match world.as_str() {
        "minimal" => println!("cargo::rustc-cfg=badge_world_minimal"),
        "" | "normal" => {}
        other => panic!("BADGE_WORLD={other:?}: expected `normal` or `minimal`"),
    }

    // HOW THE PANEL IS SHARED between the world and the app (see world.rs).
    // `split` keeps a band for the world; `full` gives the app everything and
    // leaves the world with the backlight and the status colour.
    match std::env::var("BADGE_SCREEN").unwrap_or_default().as_str() {
        "full" => println!("cargo::rustc-cfg=badge_screen_full"),
        "" | "split" => {}
        other => panic!("BADGE_SCREEN={other:?}: expected `split` or `full`"),
    }

    let out = PathBuf::from(std::env::var_os("OUT_DIR").expect("cargo sets OUT_DIR"));
    let default_cwasm = out.join("default.cwasm");

    let payload = std::env::var("BADGE_PAYLOAD").unwrap_or_default();
    let has_payload = !payload.trim().is_empty();
    if has_payload {
        let source = PathBuf::from(&payload);
        // FAIL LOUDLY. A missing payload is a mistyped path, and the alternative
        // — falling back to empty — produces a badge that boots fine and runs
        // nothing, which reads as a runtime bug.
        let bytes = std::fs::read(&source)
            .unwrap_or_else(|e| panic!("BADGE_PAYLOAD={}: {e}", source.display()));
        println!("cargo::rerun-if-changed={}", source.display());
        std::fs::write(&default_cwasm, &bytes).expect("writing the built-in payload");
        println!("cargo::rustc-cfg=has_builtin_payload");
        println!(
            "cargo::warning=built-in payload: {} ({} KB)",
            source.display(),
            bytes.len() / 1024
        );
    } else {
        // The explicit empty default. `include_bytes!` still has a file, and the
        // firmware reports "none" rather than being unable to express it.
        std::fs::write(&default_cwasm, []).expect("writing the empty default payload");
    }

    // HOW SLOWLY THE BRING-UP NARRATES ITSELF.
    //
    // **Zero by default, because normal boot is not a demo.** A badge someone is
    // wearing should reach its app as fast as it can; pausing to be readable is a
    // DEBUGGING mode, and paying for it on every power-on would be the tail
    // wagging the dog. `make badge-uf2 BADGE_BEAT_MS=700` is the watchable build,
    // and BRINGUP.md tells you to use it for a first run.
    //
    // Generated into a file rather than passed as an env string because
    // `u32::from_str_radix` is not a const fn, and the beat wants to be a const so
    // a zero compiles the delay away entirely.
    let beat: u32 = std::env::var("BADGE_BEAT_MS")
        .ok()
        .and_then(|v| v.trim().parse().ok())
        .unwrap_or(0);
    std::fs::write(
        out.join("beat.rs"),
        format!("/// Milliseconds between bring-up stages; 0 disables the pause entirely.\npub const BEAT_MS: u32 = {beat};\n"),
    )
    .expect("writing the bring-up beat");
    if beat > 0 {
        println!("cargo::warning=bring-up beat: {beat} ms per stage (watchable build)");
    }

    let region = std::env::var("BADGE_REGION").unwrap_or_default();
    let scan_region = !region.eq_ignore_ascii_case("off");
    if scan_region {
        println!("cargo::rustc-cfg=payload_region");
    }

    if !has_payload && !scan_region {
        println!(
            "cargo::warning=BADGE_PAYLOAD unset and BADGE_REGION=off — \
             this firmware boots but can run nothing"
        );
    }
}
