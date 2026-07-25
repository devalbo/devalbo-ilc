# spikes/ — de-risking proofs, kept as permanent regression

Each subdir is a minimal, self-contained proof of one load-bearing assumption. **Kept, not thrown away** —
they become standing regression tests so the foundation stays proven as code changes.

Bootstrap spikes (B1): `component/` ✅ (TinyGo→wasip2→jco), `proto/` ✅ (protobuf-go-lite ↔ es-lite),
`opfs/` (filesystem persistence), `cli/` (kong→TInput→engine, reflection-free), async.

Steps + pass criteria: [test-steps](../docs/DEVALBO-DLC-TEST-STEPS.md) Phase B1.

**Convention:** each spike documents its findings here (*what worked · what we assumed and didn't · why ·
implications*). A finding that contradicts the plan **updates the plan** — it's a deliverable, not optional.

---

## Spike 1 — `component/` (T-B1.1) — ✅ GREEN

**Goal:** prove a TinyGo engine can build to a WASM component and run under jco, returning `"ok:hi"`.

**Run:** `devbox run make spike-component` (or `make test-b1`).

### What works (the recipe we landed on)

```
TinyGo -target=wasip2 --wit-package ./wit --wit-world engine
  → engine.component.wasm
  → jco transpile → Node harness calls executeCli(["hi"]) → "ok:hi"
```

Key pieces:

- Engine sets `engine.Exports.ExecuteCli` in `init()`; returns `command-result{success, "ok:"+args[0]}`.
- World `engine` in `wit/ilc.wit` **includes** `wasi:cli/imports@0.2.0` (TinyGo runtime needs those imports).
- WASI WIT packages live under `wit/deps/` (fetched once via `wkg wit fetch`, committed).
- Spike-local `package.json` so jco / preview2-shim resolve under ESM (global `NODE_PATH` is ignored).
- `GOFLAGS=-buildvcs=false` in `devbox.json` (git VCS stamping fails inside the pure Nix env).

### Why we planned wasip1 first (and why that was over-constrained)

We never concluded wasip2 *couldn't* work on web/desktop/CLI — we **deprioritized** it. Three threads
tangled together:

1. **Chasing one artifact across *every* tier, including embedded.** WAMR (embedded) runs only core WASM +
   WASI Preview 1, not the Component Model — so a wasip2 *component* can't run on an MCU. To share one
   byte-identical artifact across web/desktop/CLI **and** embedded, we reasoned the shared unit had to be
   the lowest common denominator WAMR could run — a **wasip1 core module** — adapted "up" to a component
   for the rich tiers. **The flaw:** embedded needs its own core-wasm build *regardless*, so that unity was
   never achievable — we paid for adapter fragility to buy nothing. Relaxing the goal (embedded = a
   separate, deferred build) makes wasip2-direct the obvious choice.
2. **A Rust mental model.** "wasip1 core + the standard preview1 adapter" *is* the canonical, rock-solid
   path — **in Rust**. TinyGo is the opposite: its adapter story is weak (no `cabi_realloc`; the
   workarounds hit real bugs) while its **direct `-target=wasip2`** is the mature path. We assumed Go
   worked like Rust.
3. **wasip2 looked broken at first** — for the subtle reason in #2 below: TinyGo's runtime imports the full
   WASI surface, and the world must `include wasi:cli/imports@0.2.0` + vendor `wit/deps/`. The original
   plan (§6) wrongly assumed WASI came free from the target, so naive wasip2 failed until that was fixed.

**Lesson:** an over-constrained goal ("one artifact everywhere") + a wrong-ecosystem assumption pushed us
onto the fragile path. The concrete failures below are what surfaced it.

### What we thought should work — and didn't

#### 1. wasip1 → wasm-tools adapter → component

**Assumption (from the original plan sketch):**

```
tinygo build -target=wasip1 → core module
wasm-tools component embed + new --adapt wasi_snapshot_preview1.reactor.wasm → component
jco transpile → Node
```

**What broke:**

| Step | Failure | Why |
| --- | --- | --- |
| `wasm-tools component new` | `module does not export a function named cabi_realloc` | Component Model needs a guest Canonical-ABI allocator for `list<string>` / records. TinyGo wasip1 does not export it. |
| Blank-import `go.bytecodealliance.org/cm/x/cabi` | module path mismatch | At `cm@v0.7.0` the published module's `go.mod` declares path `go.bytecodealliance.org`, not `…/cm`. Go refuses the require. Earlier `cm` tags (e.g. v0.3.0) import cleanly but **lack** `x/cabi`. Upstream packaging bug — no clean require works. |
| Hand-vendored `//go:wasmexport cabi_realloc` | pipeline builds, Node crashes with `RuntimeError: unreachable` in `wasmExportCheckRun` | Adapter calls `cabi_realloc` during early init (e.g. stack alloc / `fd_write`) **before** TinyGo's `_initialize` has run. `//go:wasmexport` injects a run-check that panics if the module isn't initialized yet. Same class of failure as [go-modules#184](https://github.com/bytecodealliance/go-modules/issues/184). |

**Conclusion:** the wasip1 + adapter path is fragile with current TinyGo (0.41) + wasm-tools + adapter versions. We abandoned it for Spike 1.

#### 2. TinyGo wasip2 without WASI WIT in the world

**Assumption:** `-target=wasip2` alone would emit a component; the world only needs our custom `execute-cli` export (plan §6 said Console/FS come from the TinyGo target, not custom WIT).

**What broke:**

```
failed to resolve import wasi:cli/environment@0.2.0::get-environment
module requires an import interface named wasi:cli/environment@0.2.0
```

Even a trivial program's TinyGo runtime imports stdio, clocks, filesystem, random, environment. `component new` (invoked inside TinyGo) cannot satisfy those unless the **world declares them**. Fix: `include wasi:cli/imports@0.2.0` in `world engine`.

#### 3. TinyGo ships the WASI `.wit` files

**Assumption:** after declaring the include, TinyGo's install tree would resolve `wasi:cli@0.2.0` etc.

**What broke:** `TINYGOROOT` has **no** `.wit` files. You must vendor WASI packages under `wit/deps/` (we used `wkg wit fetch` from the repo root). Those deps are committed so CI/`make spike-component` does not need `wkg` on PATH.

#### 4. Other small traps along the way

- **`go mod tidy` + `cm@latest`:** resolves to a broken/mis-declared `v0.7.0` of the `cm` module path. Pin / use what the bindings actually need (`cm v0.3.0` for the package API we import today; wasip2 supplies `cabi_realloc` so we never need `x/cabi`).
- **`GOFLAGS=-buildvcs=false -mod=mod` unquoted in JSON:** shell treated `-mod=mod` as a separate `export` token (`not a valid identifier`). Must be a single quoted value.
- **Adapter fetch / embed / new steps:** unnecessary once on wasip2 — TinyGo emits the component in one shot.

### WASI WIT deps — vendored, not fetched (and why)

`wit/deps/` (the WASI 0.2.0 packages the world `include`s) is **committed**, not fetched at build time.
`wkg` *can* fetch them (pinned by `wkg.lock`, the WIT analog of a lockfile), but we vendor because:

- **WASI 0.2.0 is frozen** — no churn, so there's nothing to keep fresh.
- **Hermetic + offline builds** — `make spike-component` / CI need no `wkg` on PATH and no network.
- **`wkg` is awkward to provision** — not in nixpkgs, no `cargo install` yet (manual / release-binary
  only), so fetching would mean a fragile version-pinned release-binary download in `devbox.json` just to
  materialize a frozen dependency.

`wkg.lock` is committed too (it records provenance regardless). **Revisit only when we pull *non-frozen*
interfaces** (e.g. an evolving `wasi-gfx` / `wasi:messaging` for the display/embedded tiers) — that's when
a lockfile-driven `wkg wit fetch` earns the extra moving parts.

### Implications for later spikes / the product engine

- Prefer **`-target=wasip2`** for any engine that becomes a component (CLI via wasmtime, web via jco). Do not reintroduce wasip1+adapter unless upstream fixes the `cabi_realloc` / init ordering story.
- Keep `wit/deps/` in sync with whatever the world `include`s; re-run `wkg wit fetch` when imports change.
- Spike-local npm deps for anything jco-transpiled (ESM resolution).
- Spike 2 (`proto/`) and friends can assume this round-trip shape is the baseline.

---

## Spike 2 — `proto/` (T-B1.2) — ✅ GREEN

**Goal:** prove reflection-free protobuf works in a TinyGo wasip2 engine **and** round-trips to JS via
`protobuf-es-lite` (binary + canonical JSON).

**Run:** `devbox run make spike-proto` (also in `make test-b1`).

### What works

```
fixture SpikeMessage{name:"spike", count:42, ok:true}
  → TinyGo wasip2 engine: executeCli(["binary"|"json"])
  → goldens (golden.hex / golden.json) match
  → Node: SpikeMessage.fromBinary + fromJsonString → same fields
```

Key pieces:

- Schema: `proto/devalbo/spike/v1/spike.proto` (idiomatic buf layout, not a flat `proto/spike.proto`).
- Engine modes: `["binary"]` → `MarshalVT()`, `["json"]` → `MarshalJSON()` (go-lite).
- Native `go test ./spikes/proto/` checks goldens + that `spikev1` does **not** depend on `encoding/json`.
- Harness uses `tsx` + a **copied** `spike.pb.ts` (see findings below).
- Regen goldens: `make spike-proto-goldens`.

### What we thought should work — and didn't

#### 1. Import generated TS straight from `gen/ts/…`

**Assumption:** harness can `import { SpikeMessage } from "../../gen/ts/devalbo/spike/v1/spike.pb.ts"` and
resolve `@aptre/protobuf-es-lite/*` from `spikes/proto/node_modules`.

**What broke:** Node walks `node_modules` from the **importing file's** directory (`gen/ts/…`), not from
cwd / the harness package. `gen/` is gitignored and has no deps → `Cannot find module '@aptre/protobuf-es-lite/message'`.

**Fix:** `make spike-proto` copies the binding to `spikes/proto/spike.pb.ts` (gitignored) before `tsx`
runs. Long-term the web host may want a root/`frontend` package that owns `@aptre/protobuf-es-lite`.

#### 2. Whole spike package is free of `encoding/json`

**Assumption:** `go list -deps ./spikes/proto` would show no `encoding/json` / `reflect`.

**What broke:** `go.bytecodealliance.org/cm` (pulled in by WIT bindings) imports `encoding/json`. That is
orthogonal to protobuf. go-lite's JSON path uses `json-iterator-lite`, not stdlib `encoding/json`.

**Fix:** assert `encoding/json` is absent from deps of **`gen/go/devalbo/spike/v1` only**. The real
TinyGo gate remains: the wasip2 build succeeds (as in Spike 1).

#### 3. Flat `proto/spike.proto` + wasip1

**Assumption (original test-steps sketch):** flat proto path and `-target=wasip1`.

**What we did instead:** versioned `devalbo/spike/v1` (buf `STANDARD`) and **wasip2** (Spike 1 baseline).

### Implications

- Engine I/O can use go-lite `MarshalVT` / `MarshalJSON` under wasip2; web host can decode with es-lite
  `fromBinary` / `fromJsonString` against the same schema.
- Checked-in goldens lock both encodings; regenerate deliberately via `make spike-proto-goldens`.
- Don't import `gen/ts` from a spike-local Node package without either copying the binding or installing
  `@aptre/protobuf-es-lite` where Node's resolver will find it.
