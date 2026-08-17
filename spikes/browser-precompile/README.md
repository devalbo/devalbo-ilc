# spikes/browser-precompile — can a browser make a `.cwasm`?

**🟢 GREEN.** Wasmtime + Cranelift build with a **wasm32 host**, run under a wasm sandbox, and produce a
`.cwasm` **byte-for-byte identical** to the native toolchain's.

Gate for [`PAYLOAD-LOADING-PLAN.md`](../../docs/PAYLOAD-LOADING-PLAN.md) D5, which wants a browser to turn
a user's `.wasm` into something a badge can load — no toolchain, no install.

## The result

```
$ wasmtime run --dir . browser-precompile.wasm engine.component.wasm out.cwasm
1495894 -> 900720 bytes for pulley32

6a0556ade9a0452d92c8c6cb6224af0bf858e9a674a81f571123c75fc09d1bec  out.cwasm          (in wasm)
6a0556ade9a0452d92c8c6cb6224af0bf858e9a674a81f571123c75fc09d1bec  hello.pulley32.cwasm (native)
```

| | |
| --- | --- |
| host target | `wasm32-wasip1` |
| artifact | **9.9 MB** (release, unstripped, not compressed) |
| output | identical to native, and identical to what is flashed on the badge |

## Why it works at all

**Compiling to Pulley emits BYTECODE, not machine code.** No JIT, no executable pages, no signal
handlers — the usual reasons a compiler cannot live in a sandbox do not apply. It is a pure function from
`.wasm` bytes to `.cwasm` bytes, and `config.target("pulley32")` was already a cross-compile: the
precompiler never assumed it ran on the machine it compiled for.

## What it cost to get right, which is the transferable part

Three attempts, each producing a *working* compiler that made the *wrong artifact*:

| attempt | result | cause |
| --- | --- | --- |
| `runtime` + `gc` features | did not build | `mmap_vec.rs` — the runtime wants mmap; wasm32 has none |
| compile-only, default config | 1,660,072 bytes | no `no_virtual_memory` — CoW and guard pages the badge cannot honour |
| + `no_virtual_memory` | 1,586,584 bytes | no `generate_address_map(false)` — 679 KB of `.wasmtime.addrmap` |
| + both | **900,720 bytes, identical** | matches `precompile/src/main.rs` exactly |

**Every wrong version ran without complaint.** Each produced a plausible `.cwasm` that the badge would
have rejected at load — the middle two with "virtual memory disabled at compile time -- cannot enable
CoW", which reads like a bad file and is a config mistake. A spike that stopped at "it compiles" would
have reported success and been wrong.

So the finding is not "wasmtime builds for wasm32". It is: **the artifact is defined by the config, and
the config must be shared, not copied.** `no_vm.rs` is already included by `#[path]` for exactly this
reason; `generate_address_map(false)` is not, and lives in one crate as a loose line. A second producer
(D5a) makes that a real hazard rather than a tidy-up.

## What this does NOT show

- **Not run in an actual browser.** `wasmtime run` is a wasip1 host; a page needs jco or a wasip1 shim,
  and the web tier already has that machinery. The compiler half is what was in doubt.
- **Not measured for speed.** hello compiles fast enough not to notice under `wasmtime run`; a browser
  on a phone is a different question.
- **9.9 MB is unoptimised.** No `wasm-opt`, no stripping, no compression. Brotli over the wire and
  `--strip-debug` are the obvious next measurements, and the number to beat is whatever a page is
  willing to fetch once and cache.

## Retire it when

The web tier ships precompilation for real, with the config shared rather than duplicated — at which
point this crate is duplicating a live path and should go.
