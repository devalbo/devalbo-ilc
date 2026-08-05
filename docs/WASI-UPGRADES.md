# WASI / Component Model — version & platform upgrade gates

Living note for **when** to move WASI / Component Model versions, and **what must be green** before
we do. Complements the authoritative plan ([`DEVALBO-ILC-GO-PLAN.md`](./DEVALBO-ILC-GO-PLAN.md));
update this file when a gate flips or a spike changes the picture.

_Created 2026-07-25._

---

## One track. There used to be two.

*This file was built around a split — Rich/Component-Model for web and CLI, Portable/WAMR for embedded —
and the standing instruction was "do not design async or upgrades as if one mechanism covers both."
Decision 18 dropped WAMR for Wasmtime/Pulley, which runs components, so **every tier is on the Component
Model** and the split is gone.*

The consequence that matters here: **embedded is now ON the upgrade train, not beside it.** A WASI 0.3
move is no longer a web-and-CLI question that embedded can sit out — the `no_std` runtime has to reach the
same version, and "WAMR impact = none" is no longer an escape hatch on any checklist below. Concretely,
a bump now needs a Wasmtime `no_std` release that supports it, and that is a slower-moving pin than jco.

| Tier | Substrate | Async story |
| --- | --- | --- |
| Web (jco) · CLI/desktop · **RP2350 / ESP32-P4 (Pulley)** | wasip2 component today → WASI 0.3 when gated | **Ecosystem** CM / jco / TinyGo / Wasmtime — no ILC-owned async shim layer |
| RP2040 | native TinyGo, no wasm | n/a |

---

## Current baseline (do not casually move)

| Layer | Pin / choice | Evidence |
| --- | --- | --- |
| Guest target | TinyGo **`-target=wasip2`** (WASI 0.2 component in one shot) | Spike 1 ✅ |
| World WASI imports | `wasi:*@0.2.0` vendored in `wit/deps/` via `wkg` | Spike 1 |
| Web host | **jco** + **preview2-shim** | Spikes 1, 3 |
| Node (devbox) | **`nodejs@24`** (`--experimental-wasm-jspi` for Spike 5) | Spike 5 ✅ |
| Native CM host (planned) | **wasmtime** + WASI 0.2 | plan §4; Spike 5 off-matrix ✅ blocking import (Rust). **`wasmtime-go` lacks CM API** — B2 embed risk |
| Browser ↔ blocking guest | **Stock jco JSPI** (Node ≥24 + `--experimental-wasm-jspi`) | Decision 11; Spike 5 ✅ GREEN |
| Abandoned | wasip1 + `wasm-tools` preview1 **adapter** | Spike 1 findings |

**Bootstrap B1–B3 (CLI + web) ships on this baseline.** Embedded shares it, one release behind:
Wasmtime `no_std` is the constraint, not jco.

---

## Rule: no ILC async shims

ILC **must not** invent a private “Promise + Asyncify” host bridge for custom capabilities. That problem
belongs to the **Component Model / WASI / jco / TinyGo** stack.

- **Allowed:** use stock toolchain behavior; thin host functions that are normal CM imports.
- **Forbidden as product architecture:** vendored async unwind helpers, hand-rolled stack switching, or
  spike-only shims that the real app would have to keep.
- **If a future stock path fails:** record an **ecosystem gap** (yellow/red) and point at the upgrade
  gates below — do **not** paper over with ILC glue. (Spike 5 Rich is GREEN via jco JSPI on Node ≥24.)

(OPFS FileData hydrate in Spike 3 is a *filesystem backend* adapter, not an async runtime. Different
category; still prefer upstreaming when possible.)

---

## Rich / CM — version tracks

### CM-0.2 — stay on WASI 0.2 (bootstrap default)

- **Use for:** CLI / web / desktop while B2–B3 land.
- **Spike 5:** GREEN on this baseline via **jco JSPI** (Promise host imports); native CM `async`/`future`
  still wait on WASI 0.3 (gates below).
- **Cost:** no native CM `async func` / `future` until 0.3; TinyGo may use Asyncify internally — that’s
  *their* scheduler, not an ILC shim.

### CM-0.3 — WASI 0.3 / native Component Model async (destination for Rich track)

WASI **0.3.0** (ratified ~2026-06) rebases WASI onto CM async (`async func`, `future`, `stream`;
`wasi:io` absorbed into the Canonical ABI). **Preferred long-term for Rich/CM tiers** once the guest
can emit it.

| Gate | Must be true before we rebase the product world |
| --- | --- |
| **G-B1 Guest** | TinyGo (or chosen Go→component path) builds a trivial **0.3** component with an `async` import/export |
| **G-B2 Bindings** | `wit-bindgen-go` (or successor) generates usable 0.3 bindings for our world |
| **G-B3 Hosts** | **jco** (browser) + **wasmtime** (CLI/desktop) run that component on pins we can lock in `devbox` |
| **G-B4 Shim** | Browser WASI story clear (preview3-shim or equivalent); OPFS/FS path re-validated (Spike 3 class) |
| **G-B5 Spike** | Green probe under `make test-b1` (Spike 5 and/or `spikes/wasi03/`) with **no ILC async shim** |
| **G-B6 Cutover** | Deliberate bump: `wit/deps/`, world `include`s, devbox pins, docs — not mixed 0.2/0.3 in one engine |

**Do not** make WASI 0.3 the silent B1 exit requirement. Spike 5 greens Rich async via **JSPI on WASI 0.2**;
WASI 0.3 remains the longer-term native CM async destination (G-B1…G-B6).

### CM-JSPI — browser / Node accelerator (Spike 5 path)

- Browser/Node-native promise↔wasm suspension via `WebAssembly.Suspending` / `promising`.
- **Spike 5:** GREEN under Node ≥24 + `--experimental-wasm-jspi` + jco async import/export (no ILC shim).
- **Pin:** `nodejs@24` in `devbox.json` (Node 22 has no JSPI APIs even with the flag).
- Still no ILC-owned JSPI wrapper layer — call jco’s flags only.
- Browser re-validation (Playwright / headed) remains a follow-up; Node is the CI gate.

---

## Spike 5 — async probe

**Question:** Can the engine make a **blocking** call into a host import that is genuinely async in JS,
using **only** stock TinyGo + jco + WASI/CM — without an ILC async shim?

| Outcome (2026-07-25) | Meaning |
| --- | --- |
| **GREEN** | JSPI path (R2.\*): Node ≥24 + `--experimental-wasm-jspi` + jco `--async-mode jspi` with `--async-imports` **and** `--async-exports execute-cli`. Sync transpile (R1) still fails on Promise→`[object]` (negative control). Sync-host control + wasmtime blocking host also GREEN (off-matrix). |

**Forbidden:** inventing `spikes/async/shim/` / private async runtimes.

**Test execution matrix** (one row per assertion: R1.\* / R2.\*): [`spikes/async/README.md`](../spikes/async/README.md).  
Also T-B1.5 in [`DEVALBO-DLC-TEST-STEPS.md`](./DEVALBO-DLC-TEST-STEPS.md).

---

## Platform × version matrix (intent)

| Tier | Today (bootstrap) | Near-term upgrade | Hard floor |
| --- | --- | --- | --- |
| **Web (jco)** | wasip2 + preview2-shim + **JSPI** (Spike 5) | WASI 0.3 when G-B1…G-B5 green | Event loop must not deadlock (jco JSPI path) |
| **CLI / Desktop** | wasip2 component; **blocking** custom imports OK (Spike 5 off-matrix) | WASI 0.3 with web when gated | Same CM guest family as web; Go embed needs CM bindings (not in wasmtime-go yet) |
| **RP2350 / ESP32-P4 (Pulley)** | wasip2 component, AOT to `.cwasm`; hand-written `no_std` host | **gates on Wasmtime `no_std`**, which is the slowest pin — a WASI bump is now embedded's problem too | Host traits must exist in `wasmtime-wasi-io`; no `wasmtime-wasi` on `no_std` |
| **RP2040** | native TinyGo | n/a | No wasm / no WASI |

---

## Decision log (timing)

| When | Decision | Rationale |
| --- | --- | --- |
| 2026-07-25 | **Baseline = WASI 0.2 / wasip2** (Rich/CM) | Spike 1 green; adapter path abandoned |
| 2026-08-03 | **Tracks merged — there is one.** Embedded joins the CM upgrade train | Decision 18: Pulley runs components; supersedes the 2026-07-25 split |
| 2026-07-25 | **No ILC async shims** — Spike 5 is an ecosystem probe | CM/jco/TinyGo own the bridge |
| 2026-07-25 | **WASI 0.3 = destination for Rich/CM**, gated (G-B1…G-B6) | Native CM async when guest catches up |
| 2026-07-25 | **Spike 5 = YELLOW** (initial) | First pass under Node 22: no `Suspending`; sync jco Promise gap |
| 2026-07-25 | **Spike 5 = GREEN** | Node **24** + `--experimental-wasm-jspi` + jco `--async-exports execute-cli` |
| 2026-07-25 | **devbox pin `nodejs@24`** | Required for JSPI; Node 22 has no `Suspending`/`promising` even with the flag |

Add a row when a gate clears or we bump pins.

---

## What “leverage the Component Model” means *now*

Maximize CM without inventing runtime glue:

- Engine is a **wasip2 component** (not ad-hoc wasm + glue).
- Capabilities are **WIT imports**; hosts are CM hosts (jco / wasmtime).
- Prefer **upstream** WASI/CM async over project-specific bridges.
- Avoid reintroducing the wasip1 **adapter** bridge.
- **Including on embedded** — the `no_std` host implements the same WIT imports by hand rather than
  degrading to a byte ABI.

---

## Operational checklist (before any WASI bump)

1. Re-read this file + plan Decision 11, 20, 25, §5.3, §14 risk 6.
2. Run Spikes 1–4 (and 5 when it exists) on the **new** pins.
3. Re-validate OPFS / preview shim assumptions (Spike 3).
4. Update `wit/deps/`, `wkg.lock`, `devbox.json`, and this decision log in the **same** change.
5. State explicitly what the bump costs **embedded** — it shares the train now, and Wasmtime `no_std`
   is the gating pin. There is no longer a tier that can be waved through.

---

## References

- Plan: Decision 11 (async), 18 (embedded exec), 20 (WASI reuse), 25 (one ABI), §5.3 (per-tier build), §14 risk 6.
- Spikes: [`../spikes/README.md`](../spikes/README.md) (1 = wasip2; 3 = preview2-shim/OPFS; 4 = in-engine CLI; 5 = async / JSPI).
- Upstream: [WASI roadmap](https://wasi.dev/roadmap), [WASI 0.3 announcement](https://bytecodealliance.org/articles/WASI-0.3).
