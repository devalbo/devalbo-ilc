# DEVALBO-DLC — Test Steps (regression from first principles)

A repeatable test chain that keeps the foundation working as changes land. Covers the **first two phases**
of [`DEVALBO-DLC-GO-TASKS.md`](./DEVALBO-DLC-GO-TASKS.md): **B0 (migration & scaffolding)** and
**B1 (de-risking spikes)**. Later phases (B2 engine+CLI, B3 browser) get their own steps once they exist.

_Created 2026-07-25._

---

## How to use this

- **System under test = the `dlc` executable.** These tests treat `dlc` as a **black-box product** and
  assert its behavior (`dlc doctor`, `dlc new`, …). Building the engine/CLI is *setup* — how you **obtain**
  the `dlc` under test — not the test itself. See [System under test](#system-under-test--the-dlc-executable).
- **First principles:** the tests are **chained** — each builds on the one before (toolchain → trivial
  build → component round-trip → protobuf → filesystem → CLI boundary → async). A green test presumes the
  earlier ones are green.
- **Intermediate entry is fine:** for speed, start at the last stage you trust; run the full chain from
  scratch on a schedule (e.g. weekly / before a risky change / in CI).
- **Each test states:** *Goal · Builds on · Steps · Pass · Automate as.* "Automate as" is the eventual
  home (a `go test`, a Node test, a shell check, or a `make` target) — until the code exists, run the
  steps manually.
- **Target convention:** `make test-b0` … `make test-b3` run each phase's suite; `make test` runs all. These land as the tests are automated (§ Harness).

---

## System under test — the `dlc` executable

The thing under test is the **`dlc` binary**, not the build pipeline. Tests invoke `dlc <cmd>` and assert
the result. This keeps the contract durable: it survives changes to how `dlc` is built.

**Two layers of readiness check** (this resolves the bootstrap chicken-and-egg — you can't ask `dlc` whether
you can build `dlc`):

| Layer | System-under-test | Form | Runs when |
| --- | --- | --- | --- |
| **L0 — bootstrap gate** | the *machine* | [`scripts/preflight.sh`](../scripts/preflight.sh) — pure bash, **no toolchain dependency** | before anything exists; gets you to a first `dlc` |
| **L1 — `dlc doctor`** | the **`dlc` binary** | a `dlc` subcommand (richer, per-tier readiness) | once you have a `dlc` to test against |

L0 stays minimal and boring (the egg that lets you get the chicken). L1 (`dlc doctor`, Phase B2 —
[tasks](./DEVALBO-DLC-GO-TASKS.md), plan §16.7) is where the product contract lives.

### Setup — obtain the `dlc` under test

Every test below **assumes a `dlc` on `PATH`**. Get one by either:

- **Build it** (from a fresh checkout): walk the chain L0 gate → B0 → B1 → B2 build (`make build-engine` +
  CLI host). This is the reference path today.
- **Be handed one:** a released/prebuilt `dlc` binary (e.g. from CI artifacts). Then the tests are a pure
  black-box acceptance run — no repo build required.

Record which `dlc` you're testing: `dlc --version` (and, once it exists, `dlc doctor` for its own view of
readiness).

---

## Manual runbook (the chain, end to end)

Every automated target is just these manual steps chained — run them by hand, top to bottom, from a fresh
checkout. Each step is one command with a pass condition and its test ID.
**🟢 runnable today · 🟡 needs the toolchain (`devbox shell`) · ⚪ needs code not yet written.**
Each step links its **automated** equivalent (▶); the full automated story is the
[Harness](#harness-how-these-become-automated-regression) section.

**1 — Prerequisites (L0 bootstrap gate)** · 🟢 · T-B0.1 · ▶ **auto:** `make doctor` → [`scripts/preflight.sh`](../scripts/preflight.sh)
```bash
./scripts/preflight.sh
```
Pass: `git` ✓ and `devbox` ✓. Install Devbox (it auto-installs Nix) →
[installing-devbox](https://www.jetify.com/docs/devbox/installing-devbox); then re-run — system rows ✓
(the provisioned rows stay ✗ until step 4). This is **Layer 0** — it runs *before* a `dlc` exists; the
Layer 1 equivalent is `dlc doctor` (step 7).

**2 — Repo integrity** · 🟢 · T-B0.3/4 · ▶ **auto:** `make test-b0` → [`scripts/test-b0.sh`](../scripts/test-b0.sh)
```bash
make test-b0
```
Pass: structure / core files / READMEs / gitignore ✓. Migration rows ✗ is **expected** until step 3.

**3 — Tri-language removal** · 🟢 (done) · T-B0.3 · ▶ **auto:** `make test-b0` (absent-path checks)
```bash
# Historical one-shot (already done): tag then delete the Rust/tri-language tree.
# Standing gate is only "those paths stay gone" — no tag required in CI.
make test-b0                         # → B0 GREEN when compiler/Cargo/etc. are absent
```

**4 — Toolchain** · 🟡 · T-B0.2 · ▶ **auto:** `make doctor` (inside devbox) → [`scripts/preflight.sh`](../scripts/preflight.sh)
```bash
devbox shell
make doctor                          # every provisioned tool ✓
tinygo version                       # assert >= 0.34
```

**5 — Contract codegen** · 🟡 · Spike 1 & 2 (T-B1.1–2 foundation) · ▶ **auto:** `make gen` → [`Makefile`](../Makefile)
```bash
make gen                             # wit-bindgen-go + buf generate
ls gen/go gen/ts                     # bindings present
(cd proto && buf lint)               # no lint errors
```
Pass: `gen/` populated, clean lint — validates the `wit/` + `proto/` drafts (shakes out `buf.gen.yaml`
opts + `go_package`).

**6 — Spikes** · T-B1.1–5 ✅ (Rich JSPI + Portable) · ▶ **auto:** `make test-b1` → [`scripts/test-b1.sh`](../scripts/test-b1.sh)
```bash
make test-b1                         # component / proto / opfs / cli / async
```
Per-spike manual steps: Phase B1 below.

**7 — Engine + CLI (the `dlc` under test appears)** · ⚪ · B2 · ▶ **auto:** `make build-host` + `make verify-parity` → [`Makefile`](../Makefile)
```bash
make build-host                      # go build -o dlc ./hosts/native → the `dlc` binary (engine linked in-process, Decision 26)
make verify-parity                   # native dlc == wasip2 component on the golden vectors (the wasm is the contract)
./dlc doctor                         # L1 readiness — the command form of step 1's preflight
./dlc new myapp                      # scaffold from the terminal
```
`make build-host` produces the reference `dlc` (native, engine in-process); `make build-engine`/`component`
builds the wasip2 `engine.component.wasm` (web + the parity contract). From here the tests are **black-box
against `dlc`**: `dlc doctor` reports per-tier readiness; `dlc new` scaffolds. Everything above (steps 1–6)
is *setup* that produced this binary.

**8 — Browser** · ⚪ · B3 · ▶ **auto:** `make dev-web` → [`Makefile`](../Makefile)
```bash
make dev-web                         # dlc in the browser (React UI)
```

Today you can run **1–3** (and 4–5 the moment Nix/Devbox are installed). Steps 6–8 light up as the code
lands. The per-test sections below are the detail behind each step.

---

## Phase B0 — environment & structure integrity

Cheap, fast checks that the toolchain and repo skeleton are intact. Run these first every time — if B0 is
red, nothing downstream is trustworthy.

### T-B0.1 — System prerequisites present
- **Goal:** the machine can host the toolchain.
- **Builds on:** nothing (first principle).
- **Steps:** `./scripts/preflight.sh` (outside `devbox shell`).
- **Pass:** `git`, `nix`, `devbox` all ✓; exit 0. (direnv optional.)
- **Automate as:** `scripts/preflight.sh` (done) → `make doctor`.

### T-B0.2 — Toolchain provisions reproducibly
- **Goal:** `devbox.json` resolves the full pinned toolchain.
- **Builds on:** T-B0.1.
- **Steps:** `devbox shell`; then `make doctor` (i.e. preflight *inside* the shell).
- **Pass:** every provisioned tool ✓ — `go tinygo wit-bindgen-go wasm-tools buf wasmtime node jco
  protoc-gen-go-lite protoc-gen-es-lite`; exit 0. **Assert `tinygo --version` ≥ 0.34** (Component Model).
- **Automate as:** `devbox run doctor` in CI.

### T-B0.3 — Clean migration
- **Goal:** the retired tri-language machinery stays gone.
- **Builds on:** T-B0.1.
- **Steps:**
  ```bash
  test ! -d compiler && test ! -d packages && test ! -f Cargo.toml && test ! -f Cargo.lock
  test ! -f wit/environment.wit && test ! -f wit/console-io.wit
  ```
- **Pass:** removed paths stay absent. (`phase1-tri-language` was a one-time local checkpoint
  during the migration — not a standing CI assertion.)
- **Automate as:** `make test-b0` shell assertions.

### T-B0.4 — Directory skeleton matches the plan (§3)
- **Goal:** the bootstrap layout exists.
- **Builds on:** T-B0.3.
- **Steps:** assert dirs/files present: `wit/ proto/ engine/ hosts/native/ hosts/web/ frontend/ Makefile
  go.mod devbox.json`; `gen/` is gitignored (`git check-ignore gen`).
- **Pass:** all present; `gen/` ignored.
- **Automate as:** `make test-b0` shell assertions.

### T-B0.5 — Source & schema sanity
- **Goal:** what exists compiles / lints (catches broken scaffolding early).
- **Builds on:** T-B0.2, T-B0.4.
- **Steps (inside devbox):**
  ```bash
  go vet ./...            # (once engine/hosts have Go)
  buf lint                # proto style
  buf build               # proto compiles to a FileDescriptorSet
  ```
- **Pass:** no errors from any command.
- **Automate as:** `make test-b0` → `go vet`, `buf lint`, `buf build`.

**B0 exit:** all T-B0.* green ⇒ the environment is reproducible and the skeleton is sound.

---

## Phase B1 — foundational capability proofs (the spikes, as tests)

Each de-risking spike becomes a **standing test**: a minimal, self-contained proof of one load-bearing
assumption. Keep each spike's artifact under `spikes/<name>/` so it stays runnable as a regression.

### T-B1.1 — Component round-trip (Spike 1) — ✅ GREEN
- **Goal:** the same engine builds to a WASM component and runs under jco.
- **Builds on:** T-B0.2.
- **Steps (as implemented — `make spike-component`):**
  1. Trivial engine (`spikes/component/main.go`): sets `engine.Exports.ExecuteCli` to return `command-result{success, output:"ok:"+args[0]}`.
  2. `tinygo build -target=wasip2 --wit-package ./wit --wit-world engine -o engine.component.wasm ./spikes/component` — emits a **component directly**.
  3. `jco transpile engine.component.wasm -o out/` (deps resolve locally via `spikes/component/package.json`).
  4. Node harness (`harness.mjs`) imports `out/`, calls `executeCli(["hi"])`, asserts `success===true && output==="ok:hi"`.
- **Pass:** prints `PASS: ok:hi`; no build/transpile/runtime errors. ✅ verified under `devbox run make test-b1`.
- **Automate as:** `spikes/component/` + `harness.mjs`; `make spike-component` / `make test-b1`.

- **Spike findings (why the recipe differs from the original wasip1 sketch):**
  - **wasip2, not wasip1+adapter.** The `tinygo -target=wasip1` → `wasm-tools component new --adapt wasi_snapshot_preview1` path needs a guest `cabi_realloc`. The upstream `go.bytecodealliance.org/cm x/cabi` package that supplies it is unimportable at the version our bindings need (its `v0.7.0` module `go.mod` mis-declares its path), and a hand-vendored `cabi_realloc` crashes on init (`wasmExportCheckRun`/`unreachable`) because the adapter calls it before `_initialize`. TinyGo's **`-target=wasip2`** supplies `cabi_realloc` and wires `_initialize` for reactors (TinyGo ≥0.34; we're on 0.41), so we adopt it. This drops the adapter download **and** the two `wasm-tools` steps.
  - **The world must declare its WASI imports.** `world engine` now `include`s `wasi:cli/imports@0.2.0`; the TinyGo runtime imports stdio/clocks/filesystem/random/environment even for a trivial program, and `component new` fails to encode without them.
  - **WASI WIT is vendored.** TinyGo does **not** ship the WASI `.wit`. `wit/deps/` is populated with the six `wasi:*@0.2.0` packages via `wkg wit fetch` (from the parent of `wit/`) and **committed** so the build is reproducible without `wkg`. Re-fetch with `wkg wit fetch` if the world's imports change.
  - **`GOFLAGS=-buildvcs=false`** (set in `devbox.json` init_hook): git is non-functional inside the devbox pure env, so Go's VCS stamping aborts the build without it.
  - **npm deps are local to the spike.** ESM ignores `NODE_PATH`, so `spikes/component/package.json` pulls `jco` (which brings `@bytecodealliance/preview2-shim` transitively) into a local `node_modules` the transpiled output can resolve.

### T-B1.2 — protobuf-go-lite under TinyGo, cross-decoded by es-lite (Spike 2) — ✅ GREEN
- **Goal:** reflection-free protobuf works in the TinyGo engine **and** round-trips to the web host.
- **Builds on:** T-B0.2, T-B1.1 (wasip2 + jco baseline).
- **Steps (as implemented — `make spike-proto`):**
  1. `proto/devalbo/spike/v1/spike.proto` (`SpikeMessage`); `make gen` (go-lite + es-lite).
  2. TinyGo wasip2 engine: `executeCli(["binary"])` → `MarshalVT`, `["json"]` → `MarshalJSON`.
  3. Native `go test` + Node harness assert both match `golden.hex` / `golden.json`.
  4. Node (`tsx harness.ts`): `SpikeMessage.fromBinary` + `fromJsonString` → field-equal (`name=spike`, `count=42`, `ok=true`).
- **Pass:** TinyGo wasip2 build succeeds; goldens match; es-lite cross-decode OK. ✅ `make test-b1`.
- **Automate as:** `spikes/proto/` — native `go test` + wasip2 component + jco + `tsx` harness.
- **Findings:** see [`spikes/README.md`](../spikes/README.md) Spike 2 (gen/ts import resolution; `encoding/json` comes from `cm`, not go-lite).

### T-B1.3 — OPFS filesystem persistence (Spike 3) — ✅ GREEN
- **Goal:** the engine's `os.WriteFile` reaches OPFS via WASI preopen and survives reload.
- **Builds on:** T-B1.1.
- **Steps (as implemented — `make spike-opfs`):**
  1. Engine: `executeCli(["write","/hello.txt","persist-me"])` / `["read",…]` via `os.WriteFile`/`ReadFile`.
  2. Browser host hydrates OPFS → preview2-shim **FileData** tree (`_setFileData`), then instantiates the component (not a raw `DirectoryHandle` — see findings).
  3. After write, flush FileData → OPFS; **reload**; hydrate + boot again; read back.
  4. Assert engine read + direct `getFileHandle` both return `persist-me`.
- **Pass:** ✅ Playwright green. **Watch:** `make spike-opfs-watch` (headed, pauses for DevTools → OPFS).
- **Automate as:** `spikes/opfs/` + Playwright (Vite serves the page).
- **Findings:** [`spikes/README.md`](../spikes/README.md) Spike 3 (no DirectoryHandle preopen; browser shim bigint/`write` bug → vendored patch).

### T-B1.4 — in-engine CLI interpreter (Spike 4) — ✅ GREEN
- **Goal:** prove a **subcommand + flag parser can live inside** the TinyGo engine so every host is just an argv forwarder. **Bake off** candidates (incl. reflection-based) and pick a **default per ABI mode** (Decision 22 + 25 / plan §8) from measured data — not folklore.
- **Builds on:** T-B1.1 (wasip2 + jco). T-B1.2 optional (proto not required).
- **Authoritative spike plan:** [`spikes/cli/README.md`](../spikes/cli/README.md) (command surface, matrix, bake-off tags, decision output).

**B1 gate (must green):** at least one **lean** parser works under TinyGo — stdlib `flag` (default tag) and/or `ff/v3/ffcli` (`-tags cliffcli`); **hand-rolled** (`-tags clihand`) if both fail.

**Spike complete (Decision 22):** full bake-off table filled for every candidate tag — `flag` · `cliffcli` · `clisub` · `clicobra` · `clikong` · `cligoarg` · `clihand` — each: compiles under wasip2? matrix green? wasm size? Then record **two defaults**: portable/WAMR → leanest green reflection-free; rich/non-WAMR → most ergonomic that still compiles under TinyGo.

- **Steps:**
  1. Enrich the scaffold so `execute-cli` dispatches at least:
     - `greet` — `--name` / positional, `--shout`, `--times`
     - `count` — required `-n`, defaulted `--step`
     - `host add <tier>` — **2-level** nesting  
     Discipline for lean variants: no `fmt` in spike code; `flag.ContinueOnError` + `SetOutput(io.Discard)` (no `os.Exit`).
  2. `make spike-cli` (or per-tag targets) builds each variant: `tinygo -target=wasip2` → jco → `node harness.mjs`.
  3. Harness runs the **parsing matrix** with identical expects on every variant (see [`spikes/cli/README.md`](../spikes/cli/README.md)):

     | # | argv | expect |
     | --- | --- | --- |
     | 1–3 | `greet --name world` / `--name=world` / `-name world` | `hello world` |
     | 4 | `greet world` (positional) | `hello world` |
     | 5 | `greet --name x --shout` | `HELLO X` |
     | 6–7 | `greet --name x --times 2` (and reordered) | `hello x hello x` |
     | 8–9 | `count -n 3` / `count -n 3 --step 2` | `count=3 step=1` / `… step=2` |
     | 10–14 | type error, missing `-n`, unknown flag, unknown/empty command | `success=false` |
     | 15–17 | `host add web` / `host add` / `host bogus` | `host+web` / error / error |

  4. Per variant, record: **TinyGo compile?** · matrix green? · **`engine.component.wasm` size** (and note if compile failed — that’s a measured row, not a silent skip).
  5. From the table, write the Decision 22 pick: **portable default** + **rich default** (scaffolder later wires from ABI mode / Decision 25). Findings → [`spikes/README.md`](../spikes/README.md).

- **Pass:** ✅ B1 gate green (flag / ffcli / hand all matrix-green). Bake-off table in [`spikes/README.md`](../spikes/README.md) Spike 4 (wasip2 + wasip1 sizes); **default: ffcli** (hand ~497 KiB wasip1 / go-arg measured as per-ABI fall-backs). (kong panics; cobra fails Go-style `-name`; subcommands can't take injected argv; go-arg matrix-green under TinyGo.)
- **Automate as:** `spikes/cli/` + `make spike-cli` / `scripts/spike-cli.sh` (in `make test-b1`).

### T-B1.5 — Dual-track async probe (Spike 5) — Rich ✅ · Portable ✅
- **Goal:** on each Decision 25 track, can the guest **synchronously** call a host “wait ~50ms” import and get a correct result — **without an ILC async shim**? See [`WASI-UPGRADES.md`](./WASI-UPGRADES.md).
- **Builds on:** T-B1.1. **Pin:** Node **≥24** (`nodejs@24`) for Rich/JSPI.
- **Run:** `make spike-async` → [`scripts/spike-async.sh`](../scripts/spike-async.sh) (in `make test-b1`).
- **Authoritative matrix:** [`spikes/async/README.md`](../spikes/async/README.md) (keep that table updated when results change).

**Test execution matrix** (one row = one assertion; fixtures in spike README):

| ID | Fixture | Assertion | Result (2026-07-25) |
| --- | --- | --- | --- |
| **R1.1** | F-R-sync | Call completes within 5s (no throw / timeout) | 🔴 FAIL — `expected a string, received [object]` *(expected negative control)* |
| **R1.2** | F-R-sync | `command-result.success === true` | ⬜ SKIP (blocked by R1.1) |
| **R1.3** | F-R-sync | `output` decodes to `"ok:50"` | ⬜ SKIP (blocked by R1.1) |
| **R1.4** | F-R-sync | Event-loop tick counter &gt; 0 during the call | ⬜ SKIP (blocked by R1.1) |
| **R2.1** | F-R-jspi | Runtime has `WebAssembly.Suspending` or `WebAssembly.promising` | 🟢 PASS (Node ≥24 + `--experimental-wasm-jspi`) |
| **R2.2** | F-R-jspi | Transpiled module exports `executeCli` | 🟢 PASS |
| **R2.3** | F-R-jspi | Call completes within 5s (no throw / timeout) | 🟢 PASS |
| **R2.4** | F-R-jspi | `command-result.success === true` | 🟢 PASS |
| **R2.5** | F-R-jspi | `output` decodes to `"ok:50"` | 🟢 PASS |
| **R2.6** | F-R-jspi | Event-loop tick counter &gt; 0 during the call | 🟢 PASS |
| **P1.1** | F-P | `run_wait` export exists and `Call` returns without error | 🟢 PASS |
| **P1.2** | F-P | Return value `== 50` | 🟢 PASS |
| **P1.3** | F-P | Wall elapsed ≥ 50ms (host actually blocked) | 🟢 PASS |

Roll-up: **RICH=GREEN** (R2.\*; R1 = negative control) · **PORTABLE=GREEN**.

- **Pass:** P1 green; Rich R2.\* green (stock jco JSPI — no ILC shim).
- **Automate as:** `spikes/async/` + `make spike-async`.

**B1 exit:** T-B1.1–5 green ⇒ load-bearing assumptions proven (Rich async caps use jco JSPI on Node ≥24; Portable uses blocking native imports).

---

## Phase B2 — engine + native host (the `dlc` binary appears)

The first B2 proof exists (the walking skeleton + wasm-parity); the rest (`dlc doctor`, the `dlc new`
golden) land as those commands are built.

### T-B2.1 — Native/wasm parity of the engine (Decision 26)
- **Goal:** the one engine produces identical results whether **linked natively in-process** (the
  `dlc` binary) or compiled to the **wasip2 component** (web + the parity contract) — so native convenience
  never silently diverges from the wasm contract.
- **Builds on:** T-B1.1 (component round-trip) + a native `go build`.
- **Steps:**
  1. `make build-host` → a `dlc` binary (`go build ./hosts/native`); `./dlc version` prints `dlc 0.0.0-bootstrap`.
  2. `make verify-parity` — builds the wasip2 component (`./cmd/engine-component`), transpiles via jco, and
     runs golden vectors through **both** sides on **both boundaries**:
     - **method** (the only boundary — the argv stream retired with the `execute-cli` shim) — [`verify/parity/method-vectors.json`](../verify/parity/method-vectors.json) (method_id +
       hex proto request) through `cmd/parity-runner` vs `execute(method, request)`, diffing
       `<success>\t<base64(output)>\t<error>`. The **error string is part of the diff**: on this boundary
       the failure modes (envelope errors, unregistered ids, undecodable requests) are as much of the
       contract as the successes, and TinyGo must word them exactly as native Go does.
- **Regenerate the method vectors:** `make parity-vectors` — the hex requests are derived from typed
  fixtures in `cmd/parity-runner`, never hand-authored (same discipline as the T-B1.2 goldens). Re-run it
  when the schema moves; a diff is a regression to explain or re-bless.
- **Pass:** every vector matches on both boundaries (script prints `Decision 26 parity holds on both boundaries`).
- **Automate as:** [`scripts/verify-parity.sh`](../scripts/verify-parity.sh) via `make verify-parity`, run by `make test-b2`.

### T-B2.2 — The parity check can fail (falsification)
- **Goal:** prove T-B2.1 is capable of failing. A regression check nobody has seen fail is indistinguishable
  from one that is silently broken — it reports green forever and no one notices.
- **Steps:** `make verify-parity-selftest` writes a `//go:build tinygo` file into `engine/` that changes
  behavior in the **wasm build only**, runs T-B2.1, and asserts it reports `PARITY MISMATCH` on **both**
  boundaries; then deletes the injection and confirms the tree is restored.
- **Pass:** all six assertions green. Exit status alone is deliberately **not** sufficient — a compile error
  also exits non-zero, so the self-test requires the actual mismatch report on each boundary. (Verified by
  stubbing `verify-parity.sh` to `exit 0`: the self-test correctly goes red.)
- **What parity does NOT cover:** correctness. Edit shared engine code and *both* builds change together
  and still agree — `verify-parity.sh` rebuilds both sides every run, so it is not a golden-output test.
  It defends exactly one failure class: the native and TinyGo builds of one source disagreeing (a build
  tag, a TinyGo stdlib gap, map iteration order leaking into output). Correctness lives in
  `go test ./engine/` and the golden FS snapshot.
- **Automate as:** [`scripts/verify-parity-selftest.sh`](../scripts/verify-parity-selftest.sh) via
  `make verify-parity-selftest`, run by `make test-b2`.

---

## Phase B3 — the web tier

### T-B3.1 — `dlc new` in the browser, persisted in OPFS
- **Goal:** the **same engine** the CLI links natively runs as a wasip2 component under jco, writes a real
  scaffold tree into **OPFS** through a WASI preopen, and the tree **survives a page reload**. This is the
  browser half of the bootstrap milestone.
- **Builds on:** T-B1.1 (component round-trip), T-B1.3 (OPFS persistence), T-B2.1 (native↔wasm parity).
- **Steps:** `make verify-web` — builds the component, transpiles it via jco into `frontend/src/wasm`,
  copies the es-lite messages into the Vite root, then drives Chromium headless: fill the app name, click
  **dlc new**, assert the file list, **reload**, assert the tree is still there, and read `go.mod` back
  **directly through the OPFS API** (bypassing the engine) to prove the bytes are really on disk.
- **Pass:** three tests green — scaffold+reload, `--module` honored, and refusal to scaffold over an
  existing tree.
- **Cross-tier claim:** the assertions mirror `engine/execute_test.go` — same file set, same refusal, and
  the refusal's wording comes from `engine/`, not from any JavaScript. If the two disagree, the one-engine
  promise is broken.
- **Watch it:** `make verify-web-watch` (headed + slowMo).
- **Automate as:** `frontend/test/web.spec.ts` via `make verify-web` / `make test-b3`.

### T-B3.2 — BFT bundles cross tiers (§7.3)
- **Goal:** an app's whole state is portable as **one text blob**. Export in the browser, import in the
  terminal, get the same tree — because BFT is text and the engine reading it is the same engine.
- **Builds on:** T-B3.1.
- **Steps:** `make verify-bundle-xtier` — Playwright scaffolds `xtier` in Chromium and downloads the
  bundle to disk; the native `dlc` imports that file; the same tree is scaffolded natively; `diff -r`.
- **Pass:** the browser's bundle rebuilds the CLI's tree exactly.
- **Also covered by `make verify-web`:** export → clear → re-import round-trip in the browser, and refusal
  of a bundle whose paths escape the root (the refusal comes from `engine/`, so every tier inherits it).
- **Automate as:** [`scripts/verify-bundle-xtier.sh`](../scripts/verify-bundle-xtier.sh) via
  `make verify-bundle-xtier` / `make test-b3`.

---

## Continuous integration (provider-agnostic)

**One entry point, and it is the same command you run locally:**

```bash
./scripts/ci.sh fast     # structure + unit + fmt/vet — no wasm, no browser
./scripts/ci.sh full     # + engine boundary (B2) + web tier (B3)   ← per push
./scripts/ci.sh all      # + the B1 de-risking spikes               ← nightly
```

`make ci` / `make ci-fast` are aliases. Toolchain comes from devbox; the script wraps each step in
`devbox run` unless you are already inside `devbox shell`.

**Nothing in the suites knows about a CI provider** — no provider env vars, no caching hooks, no
annotations. A CI config is a thin adapter satisfying four requirements:

1. check out the repo
2. make `devbox` available
3. provide the system libraries headless Chromium needs (B3 only) — Playwright fetches the browser
   itself, but installing its shared libraries needs root, so it is an *environment* concern the suites
   deliberately do not handle
4. run `devbox run -- ./scripts/ci.sh full`

Porting to another provider means rewriting the adapter, never the tests.
[`.github/workflows/ci.yml`](../.github/workflows/ci.yml) is one such adapter, ~20 lines; a GitLab or
Jenkins equivalent is the same four steps.

**Reproducibility comes from `devbox.lock`**, which is committed and pins each package to an exact
nixpkgs commit — so `go@latest` in `devbox.json` is resolved-and-frozen, not floating. A toolchain change
is a lock diff you can review, not a surprise on a Tuesday.

The property this buys is that **"passes locally, fails in CI" is a devbox problem, not a mystery** —
both are running the identical command in the identical toolchain.

**Why nightly for B1:** the spikes are slow and rarely broken by day-to-day work, but the toolchain is
pinned to `@latest`, so a nightly run is what catches upstream drift nobody in this repo caused.

---

## Harness (how these become automated regression)

- **`spikes/`** dir holds each B1 proof as a runnable artifact (kept, not thrown away).
- **`make test-b0`** — shell assertions (presence, `go vet`, `buf lint/build`) + `make doctor`.
- **`make test-b1`** — builds + runs each spike; browser spikes via headless Chromium (Playwright).
- **`make test-b3`** — the web tier: `dlc new` in headless Chromium persisted in OPFS, plus BFT
  bundle interchange between the browser and the terminal.
- **`make test-b2`** — the engine command boundary in three layers: `go test ./engine/` (correctness) →
  `verify-parity.sh` (native == wasm) → `verify-parity-selftest.sh` (the parity check can fail).
- **`make test`** — runs all four (B0→B3), from first principles, in order.
- **CI:** `./scripts/ci.sh full` on push, `all` nightly — see [Continuous integration](#continuous-integration-provider-agnostic).
- **Golden files:** protobuf bytes/JSON (T-B1.2) and, later, FS snapshots are checked in; a diff = a
  regression to explain or re-bless.

---

## Not yet covered (arrives with B2/B3)

- **`dlc doctor`** (L1 readiness): black-box assertion that the command reports the same system + per-tier
  readiness as `scripts/preflight.sh` — the product-level successor to step 1.
- `dlc new myapp` produces a **buildable** project (scaffolder golden).
- **Cross-tier identity (fuller):** T-B2.1 already proves native↔component parity on golden vectors (Decision 26); the remaining piece is the **web tier** running the same sha256-pinned `engine.component.wasm` with matching `command-result` bytes (B3).
- React UI drives the engine; project persists in OPFS + downloads.
- Per-tier behavior tests (the plan's §11 verification matrix).
