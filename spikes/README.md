# spikes/ — de-risking findings (mostly retired code, permanent findings)

Each spike was a minimal, self-contained proof of one load-bearing assumption, run *before* committing to
a build shape. **The findings below are the point and they stay** — several of them reshaped the plan, and
one of them (`preview2-shim` mangling TinyGo writes) is still load-bearing in shipped code today.

## What is still here

| Spike | State |
| --- | --- |
| `async/` (Spike 5) | **live** — Rich JSPI ✅ · Portable ✅. Nothing else covers it: there is no async capability yet, so this remains the only evidence for how one would work. |

## What was retired, and why

Spikes 1–4, `oneof/`, and `options/` were **deleted once product code covered their claims** — a spike that
duplicates a real check is maintenance without information:

| Spike | Now covered by |
| --- | --- |
| 1 `component/` | the real engine builds to wasip2 and runs under jco — `make test-b2` / `test-b3` |
| 2 `proto/` | the real proto pipeline runs go-lite ↔ es-lite on every build; the browser decodes real messages |
| 3 `opfs/` | B3 asserts scaffold → OPFS → survives reload, with the real engine |
| 4 `cli/` | **premise died**: Decision 22 (in-engine parsing) was superseded by Decision 28. It measured a choice we no longer make. |
| `oneof/` | flat messages + map dispatch are what shipped; the oneof half was for response variants that do not exist |
| `options/` | `protoc-gen-dlc-registry` walks the descriptor / `dynamicpb` path on every `make gen` |

Keeping them also meant maintaining a WIT world (`spike-engine`) purely so they could compile against
`execute-cli` — a boundary deliberately deleted when host-side parsing landed. That was the tell: the
scaffolding to preserve them had outgrown what they told us.

**The findings are unabridged below.** Where a finding still constrains shipped code, it is cross-referenced
from the code itself (`hosts/web/shim/README.md`, `docs/WASI-UPGRADES.md`, `AGENTS.md`).

Steps + pass criteria: [test-steps](../docs/DEVALBO-DLC-TEST-STEPS.md) Phase B1.

## Spike 1 — `component/` (T-B1.1) — ✅ GREEN · RETIRED (code deleted)

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

## Spike 2 — `proto/` (T-B1.2) — ✅ GREEN · RETIRED (code deleted)

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

## Spike 3 — `opfs/` (T-B1.3) — ✅ GREEN · RETIRED (code deleted)

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

## Spike 4 — `cli/` (T-B1.4) — ✅ GREEN · RETIRED (premise superseded by Decision 28)

**Goal:** prove a subcommand + flag parser can live **inside** the TinyGo engine (host = argv forwarder);
bake off candidates; pick a **default per ABI mode** (Decision 22 + 25).

> **Re-scoped by Decision 28.** Parsing moved **host-side** (the engine takes a structured proto request, not argv). These findings now inform the **host** parser choice — ffcli stays the reference, and the "must be reflection-free" constraint relaxes off the engine critical path. The bake-off data (kong panics, cobra `-name`, sizes) all still stand.

**Run:** `devbox run make spike-cli` (also in `make test-b1`).

### Bake-off (TinyGo → harness, 17-case matrix)

Matrix run is wasip2 → jco → Node. `make spike-cli` also builds each variant as a **wasip1 core module**
(same `spikes/cli` package, `-target=wasip1`, no WIT flags) and records its size — the Portable/WAMR-shaped
number below. That tier is deferred, so wasip1 is a **size probe, not a gate**; the ffcli decision rests on
the wasip2 column alone.

Sizes are a representative `make spike-cli` run (2026-07-25); TinyGo output varies a few hundred bytes.

| Variant | Tag | Compile | Matrix | wasip2 component | wasip1 core |
| --- | --- | ---: | ---: | ---: | ---: |
| stdlib `flag` | (default) | yes | yes | 1 049 921 | 837 513 |
| `ff/v3/ffcli` | `cliffcli` | yes | yes | 1 361 930 | 1 232 019 |
| hand-rolled | `clihand` | yes | yes | **592 619** | **497 125** |
| `google/subcommands` | `clisub` | yes | no | 1 158 502 | 947 320 |
| `spf13/cobra` | `clicobra` | yes | no | 2 517 699 | 2 478 311 |
| `alecthomas/kong` | `clikong` | yes | no (panic) | 2 597 017 | 2 562 221 |
| `alexflint/go-arg` | `cligoarg` | yes | **yes** | 1 511 895 | 1 403 781 |

### Decision 22 / 25 picks

**Scaffolder default: `ff/v3/ffcli`** — matrix-green, real subcommand tree, stdlib `flag` syntax; one
library until an ABI-mode split is forced.

Measured per-ABI winners (Decision 25; use if we split later):

| ABI mode | Measured winner | Why |
| --- | --- | --- |
| **Portable / WAMR** | hand-rolled (`clihand`) | Leanest green — **~497 KiB** wasip1 (~735 KiB smaller than ffcli). |
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

- Hosts stay thin argv forwarders; parser code ships in the engine (`hosts/native` must not reintroduce host-side app parsing).
- Scaffolder ships **ffcli** by default; portable size pressure is real (wasip1 hand ≪ ffcli) — split hand vs go-arg when Decision 25 forces it.
- Prefer go-arg over kong if we want struct tags later; don’t assume cobra/`-name` parity with stdlib `flag` (case 3 is Go-flag dialect, not a universal CLI law).
- Untested smaller subcommand mux worth a follow-up: [`cristalhq/acmd`](https://github.com/cristalhq/acmd) (dep-free, nested `Subcommands`, injectable `Config.Args`).

---

## Spike 5 — `async/` (T-B1.5) — Rich ✅ · Portable ✅

**Test execution matrix** (one row = one assertion):  
[`async/README.md`](./async/README.md) — **R1.\***, **R2.\***, **P1.\***.

**Run:** `devbox run make spike-async`. **No ILC async shims.**  
**Pin:** Node **≥24** (`nodejs@24`) + `--experimental-wasm-jspi`.

### What works

- **Rich / JSPI (R2.\*):** jco `--async-mode jspi` with `--async-imports 'devalbo:ilc/host-delay#delay'` **and** `--async-exports 'execute-cli'` → guest blocking `delay(50)` awaits a real `setTimeout` Promise; event loop keeps ticking.
- **Portable (P1.\*):** TinyGo wasip1 + wazero `env.host_delay` = `Sleep` (WAMR native-fn shape).

### Negative control + off-matrix checks (expected / measured)

- **Rich / sync transpile (R1.\*):** Promise host under sync jco → `expected a string, received [object]`.
- **Sync-host control:** same guest + host returning a plain string (no Promise) → **GREEN** under sync jco — gap is Promise-as-sync-result, not WIT wiring.
- **wasmtime (CLI/desktop shape):** same guest + blocking Rust wasmtime `host-delay` → **GREEN** (wall ≥ 50ms). Rich/CM async pain is **web/jco**, not “all CM hosts.” `wasmtime-go` still lacks a Component Model API for B2.

### Implications → [`docs/WASI-UPGRADES.md`](../docs/WASI-UPGRADES.md)

- Rich/web async custom caps: use **stock jco JSPI** (Node ≥24 + `--experimental-wasm-jspi`; async import **and** export). No ILC shims. WASI 0.3 remains the longer-term native CM async destination.
- Portable: blocking native-import path is good for TinyGo wasip1 / future WAMR (re-host under `iwasm` when embedded lands).
- Pin **Node 24+** in devbox — Node 22 has no JSPI APIs even with the experimental flag.
- Browser JSPI (Playwright) is a follow-up; Node is the CI gate.

## Registry de-risk — `oneof/` (Decision 29) — ✅ GREEN

**Goal:** prove a protobuf `oneof` command envelope (protobuf-go-lite) encodes/decodes **and** dispatches
via a **map keyed on the oneof discriminator (no switch)** under `tinygo -target=wasip2`. The whole command
registry (Decision 29) rides on this working reflection-free; Spike 2 only proved a *flat* message.

**Run:** `devbox run make spike-oneof`.

### What works

```
build Command oneof → MarshalVT/UnmarshalVT (binary) + MarshalJSON/UnmarshalJSON (canonical)
  → dispatch decoded Command via handlers[tagOf(c)]  (O(1) map, switch-free)
```

All six harness cases pass (greet/empty-name/add/signed-add + unknown-verb/no-command errors). Binary
**and** canonical-JSON oneof round-trips both survive TinyGo; signed `int32` round-trips.

### What we learned (the go-lite oneof shape)

- go-lite emits the standard protobuf-go oneof: an **unexported interface** `isCommand_Command` + **exported
  wrapper structs** `*Command_Greet` / `*Command_Add`, with `GetCommand()` and typed getters
  (`GetGreet()`/`GetAdd()` return the value or nil).
- **No `WhichX()` discriminator getter.** So the map key comes from a **1-line-per-arm type-switch**
  (`switch c.GetCommand().(type)`) over the *exported* wrapper types. That's the only switch — and it's
  exactly what **`protoc-gen-dlc-registry`** will emit; the app never writes it. Dispatch itself is a plain
  `map[int32]handler` lookup.
- Reflection-free throughout — no `reflect`, no descriptor walking. TinyGo compiles the generated
  `MarshalVT`/`UnmarshalVT`/`MarshalJSON`/`UnmarshalJSON` for oneofs without complaint.

### Implications

- **Decision 29 is viable as specified.** `protoc-gen-dlc-registry` must emit, per command proto: the tag
  constants, the `tagOf` type-switch, and the `Register(tag, handler)` wiring — the app supplies only
  handlers.
- Component core ~1.06 MiB (baseline with one 2-arm oneof + go-lite; grows with the real command set).
- **Not yet tested:** cross-language oneof (es-lite *encodes* → go-lite *decodes*). Wire format is standard
  TLV and Spike 2 proved flat es-lite↔go-lite, so risk is low; a follow-on can add es-lite encoding to the
  harness when the web host lands (B3).

---

## Registry de-risk — `options/` (Decision 29) — ✅ GREEN

**Goal:** gate the Decision 29 registry schema — custom options (`method_id`, field help/required/default/short)
must survive go-lite codegen + TinyGo without pulling reflection-heavy official protobuf into the engine,
while the host can still read `method_id` from a FileDescriptorSet.

**Run:** `devbox run make spike-options` · criteria check: `devbox run ./scripts/check-options-criteria.sh`

### Pass criteria (measured 2026-07-25)

| # | Criterion | Result |
| --- | --- | --- |
| **1** | `.proto` that `import "google/protobuf/descriptor.proto"` and `extend google.protobuf.MethodOptions { uint32 method_id = …; }` passes `buf lint` + `buf generate` through **protoc-gen-go-lite** without erroring on the import | 🟢 PASS |
| **2** | Generated Go for the **messages** builds under `tinygo build -target=wasip2` — go-lite does **not** pull `google.golang.org/protobuf/types/descriptorpb` into the engine import graph just because the file declares options | 🟢 PASS (`go list -deps ./spikes/options` has no `google.golang.org/protobuf`) |
| **3** | `buf build -o …` FileDescriptorSet: host-side protoreflect/dynamicpb reads `method_id` off a **service rpc** (and field options off request fields) | 🟢 PASS (`CommandsService.Greet` / `Add`) |

Fallbacks (not needed — all green): (1) keep options in a host-only proto the engine never imports; (2) move `method_id` into the plugin lock / `dlc.toml` keyed by rpc full name.

### What works

```
proto/devalbo/options/v1/options.proto          # extend MethodOptions / FieldOptions
proto/devalbo/optionsspike/v1/commands.proto    # service + option-bearing request fields
  → buf lint + generate (go-lite + es-lite)                    # C1
  → TinyGo wasip2: MarshalVT/JSON round-trip of messages        # C2
  → buf build → host-introspect reads method_id + field opts    # C3
```

### What we learned

- go-lite **does not emit extension types** — `options.pb.go` is only
  `_ "github.com/aperturerobotics/protobuf-go-lite/types/descriptorpb"` (go-lite's own copy, **not**
  `google.golang.org/protobuf`). Message codecs for files that *use* the options blank-import the
  options package → that go-lite `descriptorpb` lands in the guest dep graph. Criterion 2 still
  passes (no official protobuf). **Note:** go-lite's `descriptorpb` source is large (~500 KiB Go);
  if wasm size bites, prefer the fallback layout — **messages.proto** (engine/go-lite, no options
  import) + **commands.proto** (service + options, host/codegen/`buf build` only).
- **go-lite emits NO service stubs** (no `CommandsService` / rpc methods in generated Go). Therefore
  **`protoc-gen-dlc-registry` must read the FileDescriptorSet / buf image**, not generated Go, for
  `method_id` and the service surface. (Either outcome was acceptable; this is which one we got.)
- Custom options in a `buf build` image are **unknown fields** on `MethodOptions` / `FieldOptions`.
  Host: register via `dynamicpb.NewExtensionType`, re-unmarshal with `UnmarshalOptions{Resolver}`,
  read with `ProtoReflect().Range` (`HasExtension` is unreliable across dynamicpb type identities).
- `buf.yaml` needs `deps: [buf.build/protocolbuffers/wellknowntypes]` for `descriptor.proto`.

### Implications

- **Decision 29 registry schema is unblocked** — author `method_id` + field options in proto; engine
  stays reflection-free; host + `protoc-gen-dlc-registry` consume the descriptor set.
- Recommended package split when scaffolding for real: keep option definitions + `service` in a
  host-facing file; let the engine import only message packages (avoids blank-importing go-lite
  `descriptorpb` even though C2 is already green).
- go-lite's "no extensions" still means: no extension *fields on wire messages* the engine marshals.
  Descriptor custom options are a different category and are fine.
