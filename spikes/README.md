# spikes/ — de-risking proofs, kept as permanent regression

Each subdir is a minimal, self-contained proof of one load-bearing assumption. **Kept, not thrown away** —
they become standing regression tests so the foundation stays proven as code changes.

Bootstrap spikes (B1): `component/` ✅, `proto/` ✅, `opfs/` ✅,
`cli/` ✅ (ffcli default), `async/` 🟡/✅ (Rich YELLOW · Portable GREEN).

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

---

## Spike 3 — `opfs/` (T-B1.3) — ✅ GREEN

**Goal:** engine `os.WriteFile` reaches OPFS via WASI preopen and survives a page reload.

**Run:**
- Headless (CI): `devbox run make spike-opfs`
- **Watch the browser:** `devbox run make spike-opfs-watch` (headed + slowMo; pauses at the end so you
  can open DevTools → Application → OPFS). Or `SPIKE_OPFS_HEADED=1` for headed without pause.

### What works

```
hydrate OPFS → preview2-shim FileData tree → instantiate wasip2 engine
  → executeCli(["write","/hello.txt","persist-me"])
  → flush tree → OPFS
  → page.reload()
  → hydrate + boot again → executeCli(["read",…]) === "persist-me"
  → direct OPFS getFileHandle also sees the bytes
```

Key pieces: `spikes/opfs/{main.go,app.js,opfs-bridge.js,shim/filesystem.js,opfs.spec.js}`, Vite + Playwright.

### What we thought should work — and didn't

#### 1. `setPreopens({'/': await navigator.storage.getDirectory()})`

**Assumption (plan §5.2 / test-steps):** pass an OPFS `FileSystemDirectoryHandle` straight into
preview2-shim preopens.

**Reality:** browser `_setPreopens` wants a **FileData tree**
(`{ dir: { name: { source: Uint8Array } } }`), not a DirectoryHandle. Node `_setPreopens` wants host
path strings. There is no first-class OPFS backend in current preview2-shim (0.17.x; browser FS is
experimental in-memory).

**Fix:** `opfs-bridge.js` hydrates OPFS → FileData before instantiate, and flushes FileData → OPFS
after writes. Document this as the web-host pattern until upstream grows a real OPFS backend.

#### 2. Stock browser `filesystem.js` is enough for TinyGo `os.WriteFile`

**Assumption:** once preopens are set, TinyGo WriteFile just works (it does under the **Node** shim).

**What broke:**
| Symptom | Cause |
| --- | --- |
| `write … errno 70` (ESPIPE / illegal seek) | `Descriptor.write` used `offset !== 0`, but WASI `filesize` is a **bigint** (`0n !== 0` is true) |
| `Cannot convert a BigInt value to a number` on read | `source.slice(offset, …)` passed bigint args |
| Incomplete APIs | upstream `getFlags` / `setSize` were stubs (we still patched them) |

**Fix:** vendored `spikes/opfs/shim/filesystem.js` on top of **preview2-shim@0.19.0** (upstream now
has read bigint coercion + `offset !== 0n`; we still patch `getFlags`/`setSize` stubs, `openAt`
truncate, and write offset coercion for `Number(0)`). Vite aliases
`@bytecodealliance/preview2-shim/filesystem` to that file. Worth upstreaming the remaining stubs.

#### 3. Hydrate after the engine is already imported

**Assumption:** `_setFileData` anytime before `executeCli` is fine.

**Reality:** guests snapshot the preopen Descriptor at instantiation. Later `_setFileData` swaps are
invisible to a live instance. Hydrate **then** dynamic-import the component.

### Implications

- Web host needs an **OPFS ↔ preview2-shim FileData bridge** (or a future upstream OPFS backend).
- Always coerce WASI u64/bigint to Number at the JS FS boundary.
- Prefer headed Playwright (`spike-opfs-watch`) when debugging persistence; default CI stays headless.
- Update plan §5.2 wording: preopen is FileData (browser) / path (Node), not a raw DirectoryHandle.

---

## Spike 4 — `cli/` (T-B1.4) — ✅ GREEN

**Goal:** prove a subcommand + flag parser can live **inside** the TinyGo engine (host = argv forwarder);
bake off candidates; pick a **default per ABI mode** (Decision 22 + 25).

**Run:** `devbox run make spike-cli` (also in `make test-b1`).

### Bake-off (TinyGo `-target=wasip2` → jco → Node harness, 17-case matrix)

| Variant | Tag | Compile | Matrix | `engine.component.wasm` |
| --- | --- | --- | ---: | ---: |
| stdlib `flag` | (default) | yes | yes | 1 050 185 |
| `ff/v3/ffcli` | `cliffcli` | yes | yes | 1 362 722 |
| hand-rolled | `clihand` | yes | yes | **592 883** |
| `google/subcommands` | `clisub` | yes | no | 1 158 729 |
| `spf13/cobra` | `clicobra` | yes | no | 2 519 247 |
| `alecthomas/kong` | `clikong` | yes | no (panic) | 2 597 633 |
| `alexflint/go-arg` | `cligoarg` | yes | **yes** | 1 512 511 |

### Decision 22 / 25 picks

**Default: `ff/v3/ffcli`** — matrix-green, real subcommand tree, stdlib `flag` syntax, one library for all ABI modes.

Measured alternatives (still valid if we revisit per Decision 25):

| ABI mode | Measured winner | Why |
| --- | --- | --- |
| **Portable / WAMR** | hand-rolled (`clihand`) | Leanest green (~593 KiB). Use if ESP32/WAMR size bites. |
| **Rich / non-WAMR** | go-arg (`cligoarg`) | Struct-tag ergonomics, matrix-green under TinyGo. |

### What we thought — and measured

#### 1. “Reflection breaks TinyGo” → not uniformly true

- **kong** compiles but **panics at runtime**: `reflect.Value.MethodByName` unimplemented (TinyGo).
- **go-arg** uses struct-tag reflection and **passes the full matrix** under the same TinyGo wasip2 target.
- Pick libraries from data; don’t ban the whole class.

#### 2. cobra almost wins; Go `flag`’s `-name` is the trap

cobra is green on 16/17 cases. It fails `greet -name world` because **pflag** treats single-dash as shorthand clusters (`-n -a -m -e`), while stdlib `flag` accepts `-name` as a long flag. Matrix case 3 is Go-flag-specific; rich apps that standardize on `--name` could still use cobra.

#### 3. `google/subcommands` hardcodes `os.Args`

`Commander.Execute` always parses `os.Args`. Assigning `os.Args = …` before `Execute` under TinyGo wasip2 **does not** feed the commander (every call → usage / fail). Unusable for in-engine `execute-cli(args)` without upstream API change.

### Implications

- Hosts stay thin argv forwarders; parser code ships in the engine.
- Scaffolder ships **ffcli**; can later split hand (portable) vs go-arg (rich) if needed (Decision 25).
- Prefer go-arg over kong if we want struct tags later; don’t assume cobra/`-name` parity with stdlib `flag`.
- Untested smaller subcommand mux worth a follow-up: [`cristalhq/acmd`](https://github.com/cristalhq/acmd) (dep-free, nested `Subcommands`, injectable `Config.Args`).

---

## Spike 5 — `async/` (T-B1.5) — Rich 🟡 · Portable ✅

**Test execution matrix** (one row = one assertion):  
[`async/README.md`](./async/README.md) — **R1.\***, **R2.\***, **P1.\***.

**Run:** `devbox run make spike-async`. **No ILC async shims.**

### Implications → [`docs/WASI-UPGRADES.md`](../docs/WASI-UPGRADES.md)

- Rich: async custom caps on web wait on JSPI / WASI 0.3 guest.
- Portable: blocking native-import path is good for TinyGo wasip1 / future WAMR.
