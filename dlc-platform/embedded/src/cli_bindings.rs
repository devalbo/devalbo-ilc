//! Generated `wasi:cli` stdio bindings.
//!
//! WHY GENERATED AND NOT HAND-WRITTEN. `get-stdout` returns
//! `own<output-stream>` — a resource DEFINED IN `wasi:io/streams`. A
//! `func_wrap` can only declare `Resource<DynOutputStream>`, and wasmtime
//! matches resource types by the identity the linker registered rather than
//! structurally, so the two never unify ("resource type mismatch" at
//! instantiation, whichever order you register in).
//!
//! `bindgen!`'s `with` map is what expresses "this resource IS
//! `wasmtime-wasi-io`'s" — the same trick that crate's own `bindings.rs` uses.
//! The mapping is the whole point of this file; everything else is generated.
//!
//! The WIT comes from the repo's vendored `wasi:cli@0.2.0`, so there is one copy
//! of the standard in the tree rather than a second that can drift.
use wasmtime::component::bindgen;

bindgen!({
    path: "../wit",
    inline: "
        package devalbo:badge;
        world stdio {
            import wasi:cli/stdout@0.2.0;
            import wasi:cli/stderr@0.2.0;
            import wasi:cli/stdin@0.2.0;
            import wasi:cli/environment@0.2.0;
            import wasi:clocks/monotonic-clock@0.2.0;
            import wasi:clocks/wall-clock@0.2.0;
            import wasi:filesystem/types@0.2.0;
            import wasi:filesystem/preopens@0.2.0;
        }
    ",
    world: "stdio",
    // The load-bearing lines: these resources are wasmtime-wasi-io's, not new
    // ones. Get this wrong and the generated code compiles and fails at
    // instantiation, which is a long way from the mistake.
    with: {
        // DOT, not slash: `interface.resource`. With a slash these read as
        // interface paths and bindgen reports them as "not referenced in the
        // target world", which is a confusing way to say "wrong separator".
        "wasi:io/poll.pollable": wasmtime_wasi_io::poll::DynPollable,
        "wasi:io/streams.input-stream": wasmtime_wasi_io::streams::DynInputStream,
        "wasi:io/streams.output-stream": wasmtime_wasi_io::streams::DynOutputStream,
        "wasi:io/error.error": wasmtime_wasi_io::streams::Error,
        // THE FILESYSTEM RESOURCES ARE OURS (D5). Left unmapped, bindgen invents
        // its own opaque types and the host cannot store anything useful behind
        // a descriptor — which is why the previous version could only refuse.
        // Naming our types here is what lets `open_at` hand back a real handle.
        "wasi:filesystem/types.descriptor": crate::ramfs::Node,
        "wasi:filesystem/types.directory-entry-stream": crate::ramfs::DirStream,
    },
});
