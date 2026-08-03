# Spike — can `wasmtime-go` run an ILC component? (2026-08-02)

**🔴 RED, and the interesting part is *why*.** Decision 26 links the engine natively into the CLI host "to
sidestep the `wasmtime-go` Component-Model gap". `wasmtime-go` is now at **v47** and ships a component API,
so that justification was worth re-testing rather than inheriting. It survives — but **the sentence
describing it is now false and should be corrected**, because the next reader will check v47, see
`component_*.go`, and conclude the decision is stale.

Run it: `go -C spikes/wasmtime-go-cm run .` (its own module — `wasmtime-go` ships prebuilt native libraries
and does not belong in the repo's `go.mod` until it earns a place).

## What a host has to do, and where it stops

| Step | v47 |
| --- | --- |
| 1. Load a component | ✅ `NewComponent` on the real 1.7 MB artifact |
| 2. Introspect it | ✅ 13 imports, 1 export, by name |
| 3. Provide its imports | ❌ **no way to bind a host function** — `ComponentLinker` offers `Instantiate`, `DefineUnknownImportsAsTraps`, `Close`, and nothing else |
| 3b. Provide WASI 0.2 | ❌ not implemented — `component_linker_feat_component_model.go:81`: *"TODO: WASIp2 / wasi:http integration via `wasmtime_component_linker_add_*`"* |
| 4. Call an export | ❌ **impossible** — `component_feat_component_model.go:20`: *"TODO: ComponentFunc + value marshaling (call exported component functions"* |

So v47's component support is **load, introspect, and serialize — not execute**. That is a coherent API for
AOT-compilation and tooling; it is not a host.

**The old wording is the problem.** "`wasmtime-go` lacks a CM API" was true when written and is now
misleading: there *is* a CM API. The accurate statement is that it cannot **define imports or call
exports**, which is a narrower and more durable claim — and it names exactly what to re-check later.

Instantiation fails before any of that even matters:

```
unknown import: `wasi:random/random@0.2.0#get-random-u64` has not been defined
   at wasm backtrace: 0: main!_initialize
```

## The finding worth more than the verdict

The probe printed **the authoritative list of what any ILC host must provide** — not from the WIT source,
but from the compiled artifact:

```
devalbo:ilc/events                  ← the custom capability (Decision 33)
wasi:cli/environment@0.2.0
wasi:io/error@0.2.0
wasi:io/streams@0.2.0
wasi:cli/stdin|stdout|stderr@0.2.0
wasi:clocks/monotonic-clock@0.2.0
wasi:clocks/wall-clock@0.2.0
wasi:filesystem/types@0.2.0
wasi:filesystem/preopens@0.2.0
wasi:random/random@0.2.0
                                    → exports: execute
```

That is [`EMBEDDED-PLAN.md`](../../docs/EMBEDDED-PLAN.md) D4's table, confirmed empirically rather than read
off the WIT — and it is longer than D4 guessed: `wasi:io/streams`, `wasi:io/error` and `wasi:cli/environment`
come along with stdio whether or not the engine calls them.

**And one detail that changes the badge's first milestone:** `get-random-u64` is called from TinyGo's
`_initialize`, before any command runs. Random is not a capability the embedded host can defer — the
component will not instantiate without it. Cheap to satisfy (the RP2350 has a hardware RNG), but it has to
be there on day one, and a plan that scheduled it late would fail at instantiation with a backtrace pointing
at `_initialize` rather than at anything recognisable.

## Retire it when

`wasmtime-go` grows `ComponentFunc` + a way to define imports. Then re-run this probe: if all four steps go
green, **Decision 26's justification is gone** and the CLI can run the same component as every other tier.
Until then the native in-process path is not an optimisation, it is the only option.
