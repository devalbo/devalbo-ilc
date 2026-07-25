# WASI / Component Model — version & platform upgrade gates

Living note for **when** to move WASI / Component Model versions, and **what must be green** before
we do. Complements the authoritative plan ([`DEVALBO-ILC-GO-PLAN.md`](./DEVALBO-ILC-GO-PLAN.md));
update this file when a gate flips or a spike changes the picture.

_Created 2026-07-25._

---

## Two product tracks (keep separate)

Same split as Decision 25 (ABI mode). **Do not** design async, upgrades, or spikes as if one
mechanism covers both.

| Track | Tiers | Substrate | Async story |
| --- | --- | --- | --- |
| **Rich / Component Model** | Web (jco), CLI/desktop (wasmtime) | wasip2 component today → WASI 0.3 when gated | **Ecosystem** CM / jco / TinyGo — no ILC-owned async shim layer |
| **Portable / WAMR** | ESP32-S3, RP2350 (WAMR); related core-wasm | wasip1 core + native cap registration — **no CM** | Host may **block**; no browser event loop; no jco/Asyncify requirement |

Spike 5 and WASI 0.3 work are **Rich/CM-track only**. WAMR is a later, separate embedded spike.

---

## Current baseline (Rich / CM track — do not casually move)

| Layer | Pin / choice | Evidence |
| --- | --- | --- |
| Guest target | TinyGo **`-target=wasip2`** (WASI 0.2 component in one shot) | Spike 1 ✅ |
| World WASI imports | `wasi:*@0.2.0` vendored in `wit/deps/` via `wkg` | Spike 1 |
| Web host | **jco** + **preview2-shim** | Spikes 1, 3 |
| Native CM host (planned) | **wasmtime** + WASI 0.2 | plan §4 |
| Browser ↔ blocking guest | **Ecosystem probe** (Spike 5) — stock jco/TinyGo/WASI only | Decision 11; Spike 5 🟡 YELLOW |
| Abandoned | wasip1 + `wasm-tools` preview1 **adapter** | Spike 1 findings |

**Bootstrap B1–B3 (CLI + web) ships on the Rich/CM baseline.** Portable/WAMR is out of bootstrap scope.

---

## Rule: no ILC async shims

ILC **must not** invent a private “Promise + Asyncify” host bridge for custom capabilities. That problem
belongs to the **Component Model / WASI / jco / TinyGo** stack.

- **Allowed:** use stock toolchain behavior; thin host functions that are normal CM imports.
- **Forbidden as product architecture:** vendored async unwind helpers, hand-rolled stack switching, or
  spike-only shims that the real app would have to keep.
- **If stock path fails:** Spike 5 records an **ecosystem gap** (yellow/red finding) and points at the
  upgrade gates below — it does **not** paper over with ILC glue.

(OPFS FileData hydrate in Spike 3 is a *filesystem backend* adapter, not an async runtime. Different
category; still prefer upstreaming when possible.)

---

## Rich / CM — version tracks

### CM-0.2 — stay on WASI 0.2 (bootstrap default)

- **Use for:** CLI / web / desktop while B2–B3 land.
- **Spike 5:** ecosystem probe on this baseline (or on 0.3 if guest tooling already allows — see gates).
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

**Do not** make WASI 0.3 the silent B1 exit requirement. A red/yellow Spike 5 that says “wait for G-B1”
is an acceptable bootstrap outcome if documented.

### CM-JSPI — optional browser accelerator

- Browser-native promise↔wasm suspension; often paired with or succeeding guest Asyncify.
- **Gate:** browsers we care about + TinyGo/jco support; still no ILC-owned JSPI wrapper layer.
- Not a bootstrap blocker.

---

## Portable / WAMR track (separate)

| Reality | Implication |
| --- | --- |
| WAMR = **core wasm + WASI Preview 1** (no Component Model) | CM-0.2 / CM-0.3 / JSPI gates **do not apply** |
| Async | Firmware host can block on I/O; no JS event loop |
| Capability ABI | Decision 25 portable byte-ABI (`(ptr,len)` + protobuf), not CM futures |
| Upgrade / async spike | **Independent** embedded work when that tier lands — never blocked on jco 0.3 |

**Invariant:** clearing a Rich/CM gate must not be read as “embedded is ready.”

---

## Spike 5 — dual-track async probe

### Rich / CM

**Question:** Can the engine make a **blocking** call into a host import that is genuinely async in JS,
using **only** stock TinyGo + jco + WASI/CM — without an ILC async shim?

| Outcome (2026-07-25) | Meaning |
| --- | --- |
| **YELLOW** | Sync jco: Promise→`[object]`; jco JSPI transpile OK, Node lacks `Suspending` → wait on CM-JSPI / G-B1 |

### Portable / WAMR-shaped

**Question:** Can a TinyGo **wasip1** guest sync-call a **blocking** host native import (`env.host_delay`)
— the same registration shape WAMR uses?

| Outcome (2026-07-25) | Meaning |
| --- | --- |
| **GREEN** | wazero host blocks with `time.Sleep`; `run_wait` returns after ≥ ms. Re-host under `iwasm` when embedded toolchain lands (same `engine.core.wasm`). |

**Forbidden on both tracks:** inventing `spikes/async/shim/` / private async runtimes.

**Test execution matrix** (one row per assertion: R1.\* / R2.\* / P1.\*): [`spikes/async/README.md`](../spikes/async/README.md).  
Also T-B1.5 in [`DEVALBO-DLC-TEST-STEPS.md`](./DEVALBO-DLC-TEST-STEPS.md).

---

## Platform × version matrix (intent)

| Tier | Track | Today (bootstrap) | Near-term upgrade | Hard floor |
| --- | --- | --- | --- | --- |
| **Web (jco)** | Rich / CM | wasip2 + preview2-shim | WASI 0.3 when G-B1…G-B5 green | Event loop must not deadlock (ecosystem path) |
| **CLI / Desktop (wasmtime)** | Rich / CM | wasip2 component | WASI 0.3 with web when gated | Same CM guest family as web |
| **ESP32-S3 / RP2350 (WAMR)** | Portable / WAMR | *(not started)* | Independent of CM 0.3 | No CM requirement |
| **RP2040** | native | native TinyGo | n/a | No wasm / no WASI |

---

## Decision log (timing)

| When | Decision | Rationale |
| --- | --- | --- |
| 2026-07-25 | **Baseline = WASI 0.2 / wasip2** (Rich/CM) | Spike 1 green; adapter path abandoned |
| 2026-07-25 | **Rich/CM vs Portable/WAMR kept separate** for upgrades & async | Decision 25; WAMR has no CM |
| 2026-07-25 | **No ILC async shims** — Spike 5 is an ecosystem probe | CM/jco/TinyGo own the bridge |
| 2026-07-25 | **WASI 0.3 = destination for Rich/CM**, gated (G-B1…G-B6) | Native CM async when guest catches up |
| 2026-07-25 | **Embedded/WAMR off the CM upgrade train** | Core-wasm; separate spikes later |
| 2026-07-25 | **Spike 5 Rich=YELLOW · Portable=GREEN** | Rich: Promise/JSPI ecosystem gap. Portable: wasip1 + blocking `host_delay` (wazero ≈ WAMR native-fn shape). |

Add a row when a gate clears or we bump pins.

---

## What “leverage the Component Model” means *now*

On the **Rich/CM** track, maximize CM without inventing runtime glue:

- Engine is a **wasip2 component** (not ad-hoc wasm + glue).
- Capabilities are **WIT imports**; hosts are CM hosts (jco / wasmtime).
- Prefer **upstream** WASI/CM async over project-specific bridges.
- Avoid reintroducing the wasip1 **adapter** bridge.

On the **Portable/WAMR** track, “leverage CM” does not apply — use the portable byte-ABI seam (§5.3).

---

## Operational checklist (before any WASI bump on Rich/CM)

1. Re-read this file + plan Decision 11, 20, 25, §5.3, §14 risk 6.
2. Run Spikes 1–4 (and 5 when it exists) on the **new** pins.
3. Re-validate OPFS / preview shim assumptions (Spike 3).
4. Update `wit/deps/`, `wkg.lock`, `devbox.json`, and this decision log in the **same** change.
5. State explicitly: **WAMR impact = none** (or open a separate embedded task).

---

## References

- Plan: Decision 11 (async), 20 (WASI reuse), 25 (ABI mode), §5.3 (per-tier build), §14 risk 6.
- Spikes: [`../spikes/README.md`](../spikes/README.md) (1 = wasip2; 3 = preview2-shim/OPFS; 5 = async probe).
- Upstream: [WASI roadmap](https://wasi.dev/roadmap), [WASI 0.3 announcement](https://bytecodealliance.org/articles/WASI-0.3).
